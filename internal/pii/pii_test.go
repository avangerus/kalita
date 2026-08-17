package pii

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// LLD-16 §1 acceptance at the store level: seal hides the plaintext, shred makes
// it unreadable, shredding one subject never touches another, and keys survive
// a node reload (until explicitly shredded). SEC-4 (RC): seals are bound to
// (subject, location) — a transplant to another field does not open.

func TestSealHidesPlaintext(t *testing.T) {
	s, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal("subject-1", "Contract.terms", []byte("ФИО: Иванов Иван"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed["encrypted"] != true || sealed["key_id"] != "subject-1" {
		t.Fatalf("sealed shape wrong: %v", sealed)
	}
	if _, ok := sealed["ct"].(string); !ok {
		t.Fatalf("sealed ct missing: %v", sealed)
	}
	raw, _ := json.Marshal(sealed)
	if strings.Contains(string(raw), "Иванов") {
		t.Fatal("sealed value leaks plaintext")
	}
}

func TestOpenRoundTrip(t *testing.T) {
	s, _ := NewStore("")
	sealed, err := s.Seal("s-1", "Contract.terms", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	pt, ok := s.Open(sealed, "Contract.terms")
	if !ok || string(pt) != "secret" {
		t.Fatalf("round-trip failed: %q %v", pt, ok)
	}
}

func TestShredMakesUnreadable(t *testing.T) {
	s, _ := NewStore("")
	sealed, _ := s.Seal("s-1", "Contract.terms", []byte("secret"))
	if err := s.Destroy("s-1"); err != nil {
		t.Fatal(err)
	}
	if pt, ok := s.Open(sealed, "Contract.terms"); ok || pt != nil {
		t.Fatalf("shredded value must not open: %q %v", pt, ok)
	}
}

func TestShredIsPerSubject(t *testing.T) {
	s, _ := NewStore("")
	a, _ := s.Seal("a", "E.f", []byte("data-a"))
	b, _ := s.Seal("b", "E.f", []byte("data-b"))
	if err := s.Destroy("a"); err != nil {
		t.Fatal(err)
	}
	if pt, ok := s.Open(b, "E.f"); !ok || string(pt) != "data-b" {
		t.Fatalf("shredding a must not touch b: %q %v", pt, ok)
	}
	if pt, ok := s.Open(a, "E.f"); ok || pt != nil {
		t.Fatalf("a must be unreadable after shred: %q %v", pt, ok)
	}
}

func TestPersistenceSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	s1, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s1.Seal("s-1", "Contract.terms", []byte("durable"))
	if err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if pt, ok := s2.Open(sealed, "Contract.terms"); !ok || string(pt) != "durable" {
		t.Fatalf("key must survive reload: %q %v", pt, ok)
	}
	if fi, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file perms = %o, want 0600", fi.Mode().Perm())
	}
}

func TestPlainValueNotTreatedAsSealed(t *testing.T) {
	s, _ := NewStore("")
	if pt, ok := s.Open("just a plain string", "E.f"); ok || pt != nil {
		t.Fatalf("plain legacy value must not be treated as sealed: %q %v", pt, ok)
	}
	if pt, ok := s.Open(map[string]any{"foo": "bar"}, "E.f"); ok || pt != nil {
		t.Fatalf("unrelated map must not be treated as sealed: %q %v", pt, ok)
	}
}

// TestLocationBindingBlocksTransplant (SEC-4): the same sealed blob must not
// open at a different location.
func TestLocationBindingBlocksTransplant(t *testing.T) {
	s, _ := NewStore("")
	sealed, _ := s.Seal("s-1", "Contract.terms", []byte("secret"))
	if _, ok := s.Open(sealed, "Contract.notes"); ok {
		t.Fatal("field transplant must NOT open (SEC-4)")
	}
	if _, ok := s.Open(sealed, "Other.terms"); ok {
		t.Fatal("entity transplant must NOT open")
	}
}

// TestLegacySubjectOnlyAADFallback: a value sealed under the PRE-fix
// subject-only AAD still opens (forward-compatible with existing journals).
func TestLegacySubjectOnlyAADFallback(t *testing.T) {
	s, _ := NewStore("")
	// Seal with an EMPTY location approximates nothing — build the legacy
	// ciphertext directly instead: the pre-fix AAD was "kalita.pii:<subject>".
	// The key is created by a normal seal first.
	if _, err := s.Seal("s-1", "E.f", []byte("warm")); err != nil {
		t.Fatal(err)
	}
	key := s.keys["s-1"]
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	ct := aead.Seal(nil, nonce, []byte("legacy secret"), []byte("kalita.pii:s-1"))
	legacy := map[string]any{
		"encrypted": true,
		"key_id":    "s-1",
		"ct":        base64.StdEncoding.EncodeToString(append(nonce, ct...)),
	}
	pt, ok := s.Open(legacy, "Any.field")
	if !ok || string(pt) != "legacy secret" {
		t.Fatalf("legacy subject-only value must open via fallback: %q %v", pt, ok)
	}
}