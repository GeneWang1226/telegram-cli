package corebridge

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSessionIntegrityAndAtomicWrites(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 256)
	hash := sha1.Sum(key)
	data, _ := json.Marshal(map[string]any{"Version": 1, "Data": map[string]any{"DC": 2, "AuthKey": key, "AuthKeyID": hash[12:]}})
	path := filepath.Join(t.TempDir(), "session.json")
	storage := privateSession{path: path}
	if _, err := storage.LoadSession(context.Background()); err == nil {
		t.Fatal("missing session accepted")
	}
	if err := storage.StoreSession(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	loaded, err := storage.LoadSession(context.Background())
	if err != nil || !bytes.Equal(loaded, data) {
		t.Fatal("session changed", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session is not private")
	}
	if err := storage.StoreSession(context.Background(), []byte("{}")); err == nil {
		t.Fatal("invalid session saved")
	}
	loaded, _ = os.ReadFile(path)
	if !bytes.Equal(loaded, data) {
		t.Fatal("failed write corrupted session")
	}
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	value["Version"] = 2
	wrongVersion, _ := json.Marshal(value)
	if validSession(wrongVersion) {
		t.Fatal("unsupported version accepted")
	}
	link := filepath.Join(t.TempDir(), "session-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSession(link); err == nil {
		t.Fatal("session symlink accepted")
	}
}
