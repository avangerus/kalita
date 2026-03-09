package blob

import (
	"io"
)

type BlobStore interface {
	Put(key string, r io.Reader) (string, int64, string, error) // returns key, size, sha256
	Delete(key string) error
	Path(key string) (string, error) // local path (для local)
}
