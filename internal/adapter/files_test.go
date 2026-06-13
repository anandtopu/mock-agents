package adapter

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadFile posts a multipart file upload to the FilesHandler and returns the
// recorder. A blank purpose omits the field entirely (to exercise the missing
// case); an empty fieldName omits the file part.
func uploadFile(t *testing.T, h *FilesHandler, filename, purpose string, data []byte, includeFile bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if purpose != "" {
		require.NoError(t, mw.WriteField("purpose", purpose))
	}
	if includeFile {
		fw, err := mw.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest("POST", "/v1/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.HandleUpload(rec, req)
	return rec
}

func decodeFile(t *testing.T, rec *httptest.ResponseRecorder) fileObject {
	t.Helper()
	var f fileObject
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &f))
	return f
}

func TestFiles_UploadRoundTrip(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	content := []byte(`{"custom_id":"a"}` + "\n")

	rec := uploadFile(t, h, "requests.jsonl", "batch", content, true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	f := decodeFile(t, rec)
	assert.Equal(t, "file", f.Object)
	assert.Equal(t, "batch", f.Purpose)
	assert.Equal(t, "requests.jsonl", f.Filename)
	assert.Equal(t, int64(len(content)), f.Bytes)
	assert.Equal(t, "processed", f.Status)
	assert.Regexp(t, `^file-`, f.ID)

	// Retrieve metadata.
	req := httptest.NewRequest("GET", "/v1/files/"+f.ID, nil)
	req.SetPathValue("id", f.ID)
	rr := httptest.NewRecorder()
	h.HandleRetrieve(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, f.ID, decodeFile(t, rr).ID)

	// Retrieve content bytes verbatim.
	creq := httptest.NewRequest("GET", "/v1/files/"+f.ID+"/content", nil)
	creq.SetPathValue("id", f.ID)
	crr := httptest.NewRecorder()
	h.HandleContent(crr, creq)
	require.Equal(t, http.StatusOK, crr.Code)
	assert.Equal(t, content, crr.Body.Bytes())
}

func TestFiles_UploadMissingPurpose(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	rec := uploadFile(t, h, "x.jsonl", "", []byte("{}"), true)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "purpose is required")
}

func TestFiles_UploadMissingFile(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	rec := uploadFile(t, h, "", "batch", nil, false)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "file is required")
}

func TestFiles_ListAndPurposeFilter(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	uploadFile(t, h, "a.jsonl", "batch", []byte("a"), true)
	uploadFile(t, h, "b.jsonl", "fine-tune", []byte("b"), true)

	req := httptest.NewRequest("GET", "/v1/files", nil)
	rec := httptest.NewRecorder()
	h.HandleList(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var all struct {
		Object string       `json:"object"`
		Data   []fileObject `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &all))
	assert.Equal(t, "list", all.Object)
	assert.Len(t, all.Data, 2)

	// Filtered by purpose.
	freq := httptest.NewRequest("GET", "/v1/files?purpose=batch", nil)
	frec := httptest.NewRecorder()
	h.HandleList(frec, freq)
	var filtered struct {
		Data []fileObject `json:"data"`
	}
	require.NoError(t, json.Unmarshal(frec.Body.Bytes(), &filtered))
	require.Len(t, filtered.Data, 1)
	assert.Equal(t, "batch", filtered.Data[0].Purpose)
}

func TestFiles_RetrieveNotFound(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	req := httptest.NewRequest("GET", "/v1/files/file-missing", nil)
	req.SetPathValue("id", "file-missing")
	rec := httptest.NewRecorder()
	h.HandleRetrieve(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "No such File object")
}

func TestFiles_Delete(t *testing.T) {
	h := NewFilesHandler(newFileStore())
	f := decodeFile(t, uploadFile(t, h, "a.jsonl", "batch", []byte("a"), true))

	req := httptest.NewRequest("DELETE", "/v1/files/"+f.ID, nil)
	req.SetPathValue("id", f.ID)
	rec := httptest.NewRecorder()
	h.HandleDelete(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var del struct {
		Deleted bool `json:"deleted"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &del))
	assert.True(t, del.Deleted)

	// Second delete is a 404.
	req2 := httptest.NewRequest("DELETE", "/v1/files/"+f.ID, nil)
	req2.SetPathValue("id", f.ID)
	rec2 := httptest.NewRecorder()
	h.HandleDelete(rec2, req2)
	assert.Equal(t, http.StatusNotFound, rec2.Code)
}

func TestFileStore_FIFOEviction(t *testing.T) {
	s := newFileStore()
	for i := 0; i < maxFilesPerTenant+5; i++ {
		s.put("", &fileEntry{ID: "file-" + string(rune('a'+i%26)) + "-" + itoa(i), CreatedAt: int64(i)})
	}
	assert.LessOrEqual(t, len(s.list("")), maxFilesPerTenant)
}

func TestFileStore_TenantIsolation(t *testing.T) {
	s := newFileStore()
	s.put("t1", &fileEntry{ID: "file-1"})
	s.put("t2", &fileEntry{ID: "file-2"})
	assert.Len(t, s.list("t1"), 1)
	assert.Len(t, s.list("t2"), 1)
	_, ok := s.get("t1", "file-2")
	assert.False(t, ok, "tenant t1 must not see t2's file")
}
