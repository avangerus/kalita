package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type BlobStore interface {
	Put(key string, r io.Reader) (string, int64, string, error) // returns key, size, sha256
	Delete(key string) error
	Path(key string) (string, error) // local path (для local)
}

type LocalBlobStore struct {
	Root string // например, "./uploads"
}

func (s *LocalBlobStore) ensureDir(p string) error {
	return os.MkdirAll(p, 0o755)
}

func (s *LocalBlobStore) Put(key string, r io.Reader) (string, int64, string, error) {
	if key == "" {
		now := time.Now().UTC()
		key = filepath.Join(
			fmt.Sprintf("%04d/%02d", now.Year(), int(now.Month())),
			randomHex(16), // локальный генератор уникальных идентификаторов
		)
	}
	full := filepath.Join(s.Root, filepath.FromSlash(key))
	if err := s.ensureDir(filepath.Dir(full)); err != nil {
		return "", 0, "", err
	}
	f, err := os.Create(full)
	if err != nil {
		return "", 0, "", err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		return "", 0, "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return key, n, sum, nil
}

func (s *LocalBlobStore) Delete(key string) error {
	return os.Remove(filepath.Join(s.Root, filepath.FromSlash(key)))
}

func (s *LocalBlobStore) Path(key string) (string, error) {
	return filepath.Join(s.Root, filepath.FromSlash(key)), nil
}

// randomHex возвращает hex длиной 2*n байт
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
