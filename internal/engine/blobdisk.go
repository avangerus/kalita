package engine

import (
	"os"
	"path/filepath"
)

// DiskBlobStore is the on-prem BlobStore: content-addressed files under a
// directory, sharded by hash prefix. Disposable by ADR-002 — re-uploadable
// from source if lost.
type DiskBlobStore struct {
	dir string
}

func NewDiskBlobStore(dir string) (*DiskBlobStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &DiskBlobStore{dir: dir}, nil
}

func (d *DiskBlobStore) path(hash string) (string, string) {
	sub := filepath.Join(d.dir, hash[:2])
	return sub, filepath.Join(sub, hash)
}

func (d *DiskBlobStore) Put(hash string, data []byte) error {
	sub, p := d.path(hash)
	if _, err := os.Stat(p); err == nil {
		return nil // content-addressed: identical bytes already stored
	}
	if err := os.MkdirAll(sub, 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (d *DiskBlobStore) Get(hash string) ([]byte, bool, error) {
	_, p := d.path(hash)
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (d *DiskBlobStore) Has(hash string) (bool, error) {
	_, p := d.path(hash)
	_, err := os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
