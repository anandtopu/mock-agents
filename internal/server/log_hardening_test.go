package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogHandlers_FilterByPrincipalTenant(t *testing.T) {
	store := newLogHandlerTestStore(t)
	require.NoError(t, store.Log(t.Context(), &storage.InteractionLog{
		Timestamp:      "2026-05-30T12:00:00Z",
		TenantID:       "ten-a",
		AgentName:      "agent-a",
		ResponseStatus: 200,
	}))
	require.NoError(t, store.Log(t.Context(), &storage.InteractionLog{
		Timestamp:      "2026-05-30T12:00:01Z",
		TenantID:       "ten-b",
		AgentName:      "agent-b",
		ResponseStatus: 200,
	}))

	h := &LogHandlers{Store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
		TenantID: "ten-a",
		KeyID:    "key-a",
		Role:     tenancy.RoleAdmin,
	}))
	rec := httptest.NewRecorder()
	h.ListLogs(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var rows []LogWithCost
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "ten-a", rows[0].TenantID)
	assert.Equal(t, "agent-a", rows[0].AgentName)
}

func TestLogHandlers_DeleteByPrincipalTenant(t *testing.T) {
	store := newLogHandlerTestStore(t)
	require.NoError(t, store.Log(t.Context(), &storage.InteractionLog{
		Timestamp:      "2026-05-30T12:00:00Z",
		TenantID:       "ten-a",
		AgentName:      "agent-a",
		ResponseStatus: 200,
	}))
	require.NoError(t, store.Log(t.Context(), &storage.InteractionLog{
		Timestamp:      "2026-05-30T12:00:01Z",
		TenantID:       "ten-b",
		AgentName:      "agent-b",
		ResponseStatus: 200,
	}))

	h := &LogHandlers{Store: store}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/logs", nil)
	req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
		TenantID: "ten-a",
		KeyID:    "key-a",
		Role:     tenancy.RoleAdmin,
	}))
	rec := httptest.NewRecorder()
	h.DeleteLogs(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	rows, err := store.Query(t.Context(), storage.InteractionFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "ten-b", rows[0].TenantID)
}

func newLogHandlerTestStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}
