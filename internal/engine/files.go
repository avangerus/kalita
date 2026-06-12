package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Content-addressed file storage: a file is identified by the sha256 of its
// bytes. The same content stored twice is one blob; the journal records the
// reference, never the bytes. Blob persistence is pluggable (BlobStore) so the
// kernel stays storage-agnostic — disk on a node, S3/MinIO in the cloud.

// FileRef is what a `file` field holds: a content hash plus metadata.
type FileRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

// BlobStore persists file bytes by content hash. Disposable like any projection
// (ADR-002): the journal holds the refs, blobs can be re-uploaded.
type BlobStore interface {
	Put(hash string, data []byte) error
	Get(hash string) ([]byte, bool, error)
	Has(hash string) (bool, error)
}

// SetBlobStore wires file persistence; without it, file uploads are rejected.
func (e *Engine) SetBlobStore(bs BlobStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blobs = bs
}

// StoreBlob saves bytes and returns their content reference.
func (e *Engine) StoreBlob(name string, data []byte) (*FileRef, error) {
	e.mu.RLock()
	bs := e.blobs
	e.mu.RUnlock()
	if bs == nil {
		return nil, &Err{Code: CodeValidation, Message: "file storage is not configured on this node",
			FixHint: "start the node with --files-dir"}
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if err := bs.Put(hash, data); err != nil {
		return nil, err
	}
	return &FileRef{Hash: hash, Name: name, Size: int64(len(data)), Mime: sniffMime(name)}, nil
}

// LoadBlob returns bytes for a hash, gated by ActorCanReadFile at the API.
func (e *Engine) LoadBlob(hash string) ([]byte, bool, error) {
	e.mu.RLock()
	bs := e.blobs
	e.mu.RUnlock()
	if bs == nil {
		return nil, false, fmt.Errorf("no blob store")
	}
	return bs.Get(hash)
}

// ActorCanReadFile reports whether the actor may read a record referencing the
// hash — the permission gate for downloads (a file is as readable as the
// records that point to it).
func (e *Engine) ActorCanReadFile(ctx context.Context, actor eventstore.Actor, hash string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for entity, rows := range e.records {
		decl := e.model.Entities[entity]
		if decl == nil || !hasFileField(decl) {
			continue
		}
		for _, rec := range rows {
			if recordReferencesFile(rec.Values, hash) &&
				e.can(actor, "read", entity, "", rec.Values).allowed {
				return true
			}
		}
	}
	return false
}

func hasFileField(decl *dsl.EntityDecl) bool {
	for _, f := range decl.Fields {
		if f.Type.Kind == dsl.TyScalar && f.Type.Scalar == "file" {
			return true
		}
	}
	return false
}

func recordReferencesFile(values map[string]any, hash string) bool {
	for _, v := range values {
		if m, ok := v.(map[string]any); ok {
			if h, _ := m["hash"].(string); h == hash {
				return true
			}
		}
	}
	return false
}

func sniffMime(name string) string {
	ext := strings.ToLower(name)
	for suffix, m := range map[string]string{
		".pdf": "application/pdf", ".txt": "text/plain", ".md": "text/markdown",
		".csv": "text/csv", ".json": "application/json", ".png": "image/png",
		".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	} {
		if strings.HasSuffix(ext, suffix) {
			return m
		}
	}
	return "application/octet-stream"
}

// MarshalFileRef turns a FileRef into the map a record value carries.
func MarshalFileRef(ref *FileRef) map[string]any {
	b, _ := json.Marshal(ref)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
