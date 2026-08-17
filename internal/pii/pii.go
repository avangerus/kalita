// Package pii is the node-level per-subject key store behind crypto-shredding
// (LLD-16 §1, EVENT-STORE §6): personal-data fields are sealed under a key per
// data subject, and shredding a subject = destroying that key, so the sealed
// payloads become unreadable while the journal's hash chain stays intact (the
// hash is over the ciphertext, which never changes). Keys live here — never in
// the DSL or the journal (I-4).
package pii

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// Store maps subjectID -> 32-byte XChaCha20-Poly1305 key. The zero path means
// in-memory (dev/tests): keys vanish on exit, which is itself correct shredding
// semantics but wrong for a node that must keep reading its own data.
type Store struct {
	mu   sync.Mutex
	path string
	keys map[string][]byte
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, keys: map[string][]byte{}}
	if path != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	for id, b64 := range m {
		k, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return err
		}
		s.keys[id] = k
	}
	return nil
}

func (s *Store) persist() error {
	if s.path == "" {
		return nil
	}
	m := map[string]string{}
	for id, k := range s.keys {
		m[id] = base64.StdEncoding.EncodeToString(k)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Seal encrypts plaintext under the subject's key, returning the wire shape the
// journal stores instead of the plaintext: {encrypted:true, key_id, ct}. The
// key is created on first use and persisted immediately (a sealed value must
// never be journaled under a key that only exists in memory).
func (s *Store) Seal(subjectID string, plaintext []byte) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[subjectID]
	if !ok {
		key = make([]byte, chacha20poly1305.KeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		s.keys[subjectID] = key
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// aad binds the ciphertext to (subject, field-is-implicit-by-value): a blob
	// can never be transplanted to another subject and still open.
	ct := aead.Seal(nil, nonce, plaintext, []byte("kalita.pii:"+subjectID))
	return map[string]any{
		"encrypted": true,
		"key_id":    subjectID,
		"ct":        base64.StdEncoding.EncodeToString(append(nonce, ct...)),
	}, nil
}

// Open decrypts a sealed value. Returns (nil, false) when the value is not a
// sealed shape (a plain legacy value) or when the subject's key is gone — the
// latter is the shred: the caller renders a tombstone, never an error.
func (s *Store) Open(v any) ([]byte, bool) {
	m, ok := v.(map[string]any)
	if !ok || m["encrypted"] != true {
		return nil, false
	}
	id, _ := m["key_id"].(string)
	b64, _ := m["ct"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[id]
	if !ok {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, false
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, false
	}
	pt, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("kalita.pii:"+id))
	if err != nil {
		return nil, false
	}
	return pt, true
}

// Destroy shreds a subject's key (idempotent). Sealed payloads under that
// subject become unreadable from this point; the journal is untouched.
func (s *Store) Destroy(subjectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[subjectID]; !ok {
		return nil
	}
	delete(s.keys, subjectID)
	return s.persist()
}
