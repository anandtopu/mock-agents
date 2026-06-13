package adapter

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

// ProtocolOpenAIFiles is the wire-protocol label recorded for the OpenAI Files
// surface (POST /v1/files and the retrieve/list/content/delete routes). The
// Files API is the upload half of the Batch flow (A-08): a client uploads a
// JSONL of requests here, then references the returned file id from
// POST /v1/batches.
const ProtocolOpenAIFiles = "openai-files"

// File-store limits. A mock is a long-running process that arbitrary test
// traffic uploads into, so bound both the per-file size and the per-tenant
// file count to keep memory bounded (mirrors the embeddings/moderations batch
// caps rationale).
const (
	// maxFileBytes caps a single uploaded file. Generous enough for a large
	// batch JSONL while keeping the worst-case in-memory copy bounded.
	maxFileBytes = 50 << 20 // 50 MiB
	// maxFilesPerTenant bounds how many files one tenant can hold; the oldest
	// is evicted past the cap (FIFO), like the responses store.
	maxFilesPerTenant = 256
)

// --- file store ---

// fileEntry is one stored file: its OpenAI-wire metadata plus the raw bytes.
// The bytes back both GET /v1/files/{id}/content and the batch processor, which
// reads an input file's lines and writes its output/error files back through
// the same store.
type fileEntry struct {
	ID        string
	Bytes     int64
	CreatedAt int64
	Filename  string
	Purpose   string
	data      []byte
}

// fileStore is the in-memory, per-tenant file store shared by FilesHandler and
// BatchesHandler. It mirrors responseStore's bounded-FIFO shape but is keyed by
// tenant first so one tenant's files never leak into another's list/retrieve
// (the same isolation the agent registry enforces). The zero value is not
// usable — construct with newFileStore.
type fileStore struct {
	mu    sync.Mutex
	m     map[string]map[string]*fileEntry // tenant -> id -> entry
	order map[string][]string              // tenant -> insertion order (FIFO)
}

func newFileStore() *fileStore {
	return &fileStore{
		m:     make(map[string]map[string]*fileEntry),
		order: make(map[string][]string),
	}
}

// put stores e for tenant, evicting the tenant's oldest file once the per-tenant
// cap is exceeded so a long-lived mock can't grow without bound.
func (s *fileStore) put(tenant string, e *fileEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if byID == nil {
		byID = make(map[string]*fileEntry)
		s.m[tenant] = byID
	}
	if _, exists := byID[e.ID]; !exists {
		s.order[tenant] = append(s.order[tenant], e.ID)
		for len(s.order[tenant]) > maxFilesPerTenant {
			oldest := s.order[tenant][0]
			s.order[tenant] = s.order[tenant][1:]
			delete(byID, oldest)
		}
	}
	byID[e.ID] = e
}

// get returns the file for (tenant, id) and whether it was found.
func (s *fileStore) get(tenant, id string) (*fileEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[tenant][id]
	return e, ok
}

// list returns the tenant's files, newest first (matching the OpenAI list
// order). The slice is a fresh copy so callers can't mutate the store.
func (s *fileStore) list(tenant string) []*fileEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	out := make([]*fileEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// delete removes the file for (tenant, id) and reports whether it existed.
func (s *fileStore) delete(tenant, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if _, ok := byID[id]; !ok {
		return false
	}
	delete(byID, id)
	if order := s.order[tenant]; len(order) > 0 {
		for i, oid := range order {
			if oid == id {
				s.order[tenant] = append(order[:i:i], order[i+1:]...)
				break
			}
		}
	}
	return true
}

// --- wire types ---

// fileObject is the OpenAI File object returned by the upload/retrieve/list
// routes. status is always "processed" — a mock has nothing to scan.
type fileObject struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
}

func (e *fileEntry) wire() fileObject {
	return fileObject{
		ID:        e.ID,
		Object:    "file",
		Bytes:     e.Bytes,
		CreatedAt: e.CreatedAt,
		Filename:  e.Filename,
		Purpose:   e.Purpose,
		Status:    "processed",
	}
}

// --- handler ---

// FilesHandler serves the OpenAI Files API. It shares its store with
// BatchesHandler so an uploaded input file can be read by the batch processor
// and the generated output/error files appear under GET /v1/files.
type FilesHandler struct {
	store *fileStore
}

// NewFilesHandler builds a FilesHandler over store. The store is injected (not
// created here) because BatchesHandler must read and write the same store.
func NewFilesHandler(store *fileStore) *FilesHandler {
	return &FilesHandler{store: store}
}

// Name identifies this adapter in logs and diagnostics.
func (h *FilesHandler) Name() string { return "openai-files" }

// Routes returns the Files routes mounted through the adapter Registry.
func (h *FilesHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/files", Handler: h.HandleUpload},
		{Pattern: "GET /v1/files", Handler: h.HandleList},
		{Pattern: "GET /v1/files/{id}", Handler: h.HandleRetrieve},
		{Pattern: "GET /v1/files/{id}/content", Handler: h.HandleContent},
		{Pattern: "DELETE /v1/files/{id}", Handler: h.HandleDelete},
	}
}

// HandleUpload handles POST /v1/files (multipart/form-data with `file` and
// `purpose` fields).
func (h *FilesHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIFiles)
	tenant := engine.TenantIDFromContext(r.Context())

	// Bound the whole multipart body (envelope + file) so an oversized upload
	// can't allocate without limit. The slack covers the multipart boundary,
	// headers, and the `purpose` field around the file payload itself.
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(maxFileBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "uploaded file too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", "request must be multipart/form-data with a file and a purpose")
		return
	}
	// A part larger than maxFileBytes spills to a temp file on disk; without
	// this the temp file outlives the request and accumulates in os.TempDir()
	// (a slow disk-exhaustion leak). We copy the bytes into the store below, so
	// the on-disk form is safe to drop as soon as the handler returns.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	purpose := r.FormValue("purpose")
	if purpose == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "purpose is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "uploaded file too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", "could not read uploaded file")
		return
	}

	filename := "upload"
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}

	e := &fileEntry{
		ID:        "file-" + generateID(),
		Bytes:     int64(len(data)),
		CreatedAt: time.Now().Unix(),
		Filename:  filename,
		Purpose:   purpose,
		data:      data,
	}
	h.store.put(tenant, e)
	writeJSON(w, http.StatusOK, e.wire())
}

// HandleList handles GET /v1/files.
func (h *FilesHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIFiles)
	tenant := engine.TenantIDFromContext(r.Context())

	// Optional purpose filter, matching the OpenAI query param.
	purposeFilter := r.URL.Query().Get("purpose")

	entries := h.store.list(tenant)
	data := make([]fileObject, 0, len(entries))
	for _, e := range entries {
		if purposeFilter != "" && e.Purpose != purposeFilter {
			continue
		}
		data = append(data, e.wire())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

// HandleRetrieve handles GET /v1/files/{id}.
func (h *FilesHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIFiles)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	e, ok := h.store.get(tenant, id)
	if !ok {
		writeFileNotFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, e.wire())
}

// HandleContent handles GET /v1/files/{id}/content, returning the raw bytes.
func (h *FilesHandler) HandleContent(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIFiles)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	e, ok := h.store.get(tenant, id)
	if !ok {
		writeFileNotFound(w, id)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(e.data)
}

// HandleDelete handles DELETE /v1/files/{id}.
func (h *FilesHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIFiles)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	if !h.store.delete(tenant, id) {
		writeFileNotFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "file",
		"deleted": true,
	})
}

// writeFileNotFound writes the OpenAI 404 envelope for an unknown file id.
func writeFileNotFound(w http.ResponseWriter, id string) {
	writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("No such File object: %s", id))
}

// stampProtocol records the wire protocol on the request meta when capture
// middleware is installed (a no-op otherwise). Shared by the Files and Batch
// handlers so every route stamps its surface even on an early validation error.
func stampProtocol(r *http.Request, protocol string) {
	if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
		meta.Protocol = protocol
	}
}
