"""Tests for the MCP bidirectional helper (:mod:`mockagents.mcp`).

The helper wraps the v0.3 server→client JSON-RPC flow:

- ``GET /mcp/events`` → parsed :class:`McpEvent` stream (handled via
  :class:`McpEventStream`).
- ``POST /mcp/response`` → :meth:`McpClient.send_response`.
- ``dispatch_request`` glue that routes one event through a handler
  map and posts the reply.

We test against MagicMock-backed ``requests`` responses — the Go
server is already covered by its own suite in ``internal/mcp``.
"""

from __future__ import annotations

from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from mockagents import McpClient, McpEvent, McpEventStream


# --- event parsing ---


def _fake_sse_response(lines: list[str], status_code: int = 200) -> MagicMock:
    resp = MagicMock()
    resp.status_code = status_code
    resp.iter_lines.return_value = iter(lines)
    resp.raise_for_status.return_value = None
    return resp


def test_event_stream_parses_request_frame():
    lines = [
        "event: request",
        'data: {"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"prompt":"hi"}}',
        "",
    ]
    stream = McpEventStream(_fake_sse_response(lines))
    events = list(stream)
    assert len(events) == 1
    ev = events[0]
    assert ev.kind == "request"
    assert ev.is_request is True
    assert ev.is_notification is False
    assert ev.request_id == 1
    assert ev.method == "sampling/createMessage"
    assert ev.params == {"prompt": "hi"}


def test_event_stream_parses_notification_frame():
    lines = [
        "event: notification",
        'data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}',
        "",
    ]
    stream = McpEventStream(_fake_sse_response(lines))
    events = list(stream)
    assert len(events) == 1
    ev = events[0]
    assert ev.kind == "notification"
    assert ev.is_request is False
    assert ev.is_notification is True
    assert ev.request_id is None
    assert ev.method == "notifications/tools/list_changed"


def test_event_stream_skips_heartbeat_and_malformed_data():
    lines = [
        ":heartbeat",
        "",
        "event: request",
        "data: not-json-at-all",
        "",
        "event: request",
        'data: {"jsonrpc":"2.0","id":2,"method":"roots/list"}',
        "",
    ]
    stream = McpEventStream(_fake_sse_response(lines))
    events = list(stream)
    # Heartbeat is dropped silently; malformed data drops its whole
    # frame; the trailing valid request comes through.
    assert len(events) == 1
    assert events[0].method == "roots/list"


def test_event_stream_joins_multiline_data():
    lines = [
        "event: request",
        'data: {"jsonrpc":"2.0","id":3,',
        'data: "method":"roots/list"}',
        "",
    ]
    stream = McpEventStream(_fake_sse_response(lines))
    events = list(stream)
    assert len(events) == 1
    assert events[0].request_id == 3
    assert events[0].method == "roots/list"


def test_event_stream_context_manager_closes():
    resp = _fake_sse_response([])
    with McpEventStream(resp):
        pass
    resp.close.assert_called_once()


# --- send_response ---


def test_send_response_result_payload():
    client = McpClient(base_url="http://test")
    post = MagicMock()
    post.return_value = _fake_sse_response([])
    with patch.object(client._session, "post", post):
        client.send_response(request_id=7, result={"text": "pong"})
    post.assert_called_once()
    args, kwargs = post.call_args
    assert args[0] == "http://test/mcp/response"
    assert kwargs["json"] == {
        "jsonrpc": "2.0",
        "id": 7,
        "result": {"text": "pong"},
    }


def test_send_response_error_payload():
    client = McpClient(base_url="http://test")
    with patch.object(client._session, "post") as post:
        post.return_value = _fake_sse_response([])
        client.send_response(
            request_id="abc",
            error={"code": -32603, "message": "boom"},
        )
    args, kwargs = post.call_args
    assert kwargs["json"] == {
        "jsonrpc": "2.0",
        "id": "abc",
        "error": {"code": -32603, "message": "boom"},
    }


def test_send_response_requires_exactly_one_of_result_or_error():
    client = McpClient()
    with pytest.raises(ValueError):
        client.send_response(request_id=1)
    with pytest.raises(ValueError):
        client.send_response(request_id=1, result={}, error={})


# --- dispatch_request ---


def _make_request_event(method: str, params: dict[str, Any], request_id: int = 1) -> McpEvent:
    return McpEvent(
        kind="request",
        payload={
            "jsonrpc": "2.0",
            "id": request_id,
            "method": method,
            "params": params,
        },
    )


def test_dispatch_request_routes_to_handler():
    client = McpClient(base_url="http://test")
    captured: dict[str, Any] = {}

    def sample_handler(params: dict[str, Any]) -> dict[str, Any]:
        captured.update(params)
        return {"text": "ok"}

    event = _make_request_event("sampling/createMessage", {"prompt": "hi"}, 42)
    with patch.object(client, "send_response") as send_response:
        result = client.dispatch_request(event, {"sampling/createMessage": sample_handler})
    assert result == {"text": "ok"}
    assert captured == {"prompt": "hi"}
    send_response.assert_called_once_with(42, result={"text": "ok"})


def test_dispatch_request_unknown_method_posts_32601():
    client = McpClient(base_url="http://test")
    event = _make_request_event("unknown/method", {})
    with patch.object(client, "send_response") as send_response:
        with pytest.raises(KeyError):
            client.dispatch_request(event, {})
    send_response.assert_called_once()
    _, kwargs = send_response.call_args
    assert kwargs["error"]["code"] == -32601


def test_dispatch_request_handler_raises_posts_32603_and_reraises():
    client = McpClient(base_url="http://test")

    def boom(params: dict[str, Any]) -> dict[str, Any]:
        raise RuntimeError("kaboom")

    event = _make_request_event("sampling/createMessage", {})
    with patch.object(client, "send_response") as send_response:
        with pytest.raises(RuntimeError):
            client.dispatch_request(event, {"sampling/createMessage": boom})
    _, kwargs = send_response.call_args
    assert kwargs["error"]["code"] == -32603
    assert "kaboom" in kwargs["error"]["message"]


def test_dispatch_request_rejects_notification_event():
    client = McpClient()
    event = McpEvent(
        kind="notification",
        payload={"jsonrpc": "2.0", "method": "notifications/tools/list_changed"},
    )
    with pytest.raises(ValueError):
        client.dispatch_request(event, {})


# --- McpEvent accessors ---


def test_event_params_always_returns_dict():
    # Payload has no params — .params should still be a dict.
    ev = McpEvent(kind="request", payload={"jsonrpc": "2.0", "id": 1, "method": "m"})
    assert ev.params == {}
    # Non-dict params (unusual) also yields an empty dict so handlers
    # never crash on params.get(...).
    ev2 = McpEvent(kind="request", payload={"jsonrpc": "2.0", "id": 1, "method": "m", "params": "oops"})
    assert ev2.params == {}


def test_event_is_request_requires_id():
    # Even if the SSE kind is "request", a payload without an id is
    # treated as a notification (defensive — real servers never emit
    # this shape).
    ev = McpEvent(kind="request", payload={"jsonrpc": "2.0", "method": "m"})
    assert ev.is_request is False
    assert ev.is_notification is True
