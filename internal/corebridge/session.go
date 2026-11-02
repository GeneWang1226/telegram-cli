package corebridge

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func validSession(data []byte) bool {
	var s struct {
		Version int
		Data    struct {
			DC        int
			AuthKey   []byte
			AuthKeyID []byte
		}
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &s) != nil || s.Version != 1 || s.Data.DC <= 0 || len(s.Data.AuthKey) != 256 || len(s.Data.AuthKeyID) != 8 || bytes.Equal(s.Data.AuthKey, make([]byte, 256)) {
		return false
	}
	hash := sha1.Sum(s.Data.AuthKey)
	return bytes.Equal(hash[12:], s.Data.AuthKeyID)
}

func readSession(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("invalid session")
	}
	data, err := os.ReadFile(path)
	if err != nil || !validSession(data) {
		return nil, errors.New("invalid session")
	}
	return data, nil
}

type privateSession struct{ path string }

func (s privateSession) LoadSession(context.Context) ([]byte, error) { return readSession(s.path) }
func (s privateSession) StoreSession(_ context.Context, data []byte) error {
	if !validSession(data) {
		return errors.New("invalid session")
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".core-session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	return err
}
