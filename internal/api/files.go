package api

import (
	"io"
	"net/http"
)

// File upload/download. Upload stores bytes content-addressed and returns a
// FileRef to put into a `file` field. Download is gated by whether the actor
// may read a record that references the hash (a file is as readable as the
// records pointing at it).

const maxUpload = 64 << 20 // 64 MiB per file in v0

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "expected multipart/form-data with a 'file' field", "fix_hint": "POST a file under the form key 'file'"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "no 'file' in the form", "fix_hint": "attach the file under the key 'file'"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(data) > maxUpload {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "VALIDATION_ERROR",
			"message": "file exceeds 64 MiB"})
		return
	}
	_ = actor // upload is allowed for any authenticated actor; the reference
	// only becomes meaningful once written into a record they have rights to
	ref, err := s.eng.StoreBlob(header.Filename, data)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ref)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	hash := r.PathValue("hash")
	if !s.eng.ActorCanReadFile(r.Context(), actor, hash) {
		// invisible like any record the actor cannot read
		http.Error(w, `{"code":"NOT_FOUND","message":"file not found"}`, http.StatusNotFound)
		return
	}
	data, found, err := s.eng.LoadBlob(hash)
	if err != nil || !found {
		http.Error(w, `{"code":"NOT_FOUND","message":"file not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment")
	_, _ = w.Write(data)
}
