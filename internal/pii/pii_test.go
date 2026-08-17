package pii

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// LLD-16 §1 acceptance at the store level: seal hides the plaintext, shred makes
// it unreadable, shredding one subject never touches another, and keys survive
// a node reload (until explicitly shredded).

func TestSealHidesPlaintext(t *testing.T) {
	s, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal("subject-1", []byte("ФИО: Иванов Иван"))
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
	sealed, err := s.Seal("s-1", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	pt, ok := s.Open(sealed)
	if !ok || string(pt) != "secret" {
		t.Fatalf("round-trip failed: %q %v", pt, ok)
	}
}

func TestShredMakesUnreadable(t *testing.T) {
	s, _ := NewStore("")
	sealed, _ := s.Seal("s-1", []byte("secret"))
	if err := s.Destroy("s-1"); err != nil {
		t.Fatal(err)
	}
	if pt, ok := s.Open(sealed); ok || pt != nil {
		t.Fatalf("shredded value must not open: %q %v", pt, ok)
	}
}

func TestShredIsPerSubject(t *testing.T) {
	s, _ := NewStore("")
	a, _ := s.Seal("a", []byte("data-a"))
	b, _ := s.Seal("b", []byte("data-b"))
	if err := s.Destroy("a"); err != nil {
		t.Fatal(err)
	}
	if pt, ok := s.Open(b); !ok || string(pt) != "data-b" {
		t.Fatalf("shredding a must not touch b: %q %v", pt, ok)
	}
	if pt, ok := s.Open(a); ok || pt != nil {
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
	sealed, err := s1.Seal("s-1", []byte("durable"))
	if err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if pt, ok := s2.Open(sealed); !ok || string(pt) != "durable" {
		t.Fatalf("key must survive reload: %q %v", pt, ok)
	}
	if fi, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file perms = %o, want 0600", fi.Mode().Perm())
	}
}

func TestPlainValueNotTreatedAsSealed(t *testing.T) {
	s, _ := NewStore("")
	if pt, ok := s.Open("just a plain string"); ok || pt != nil {
		t.Fatalf("plain legacy value must not be treated as sealed: %q %v", pt, ok)
	}
	if pt, ok := s.Open(map[string]any{"foo": "bar"}); ok || pt != nil {
		t.Fatalf("unrelated map must not be treated as sealed: %q %v", pt, ok)
	}
}
