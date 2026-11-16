//go:build !windows

package policy

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type ownedInfo struct {
	os.FileInfo
	uid uint32
}

func (i ownedInfo) Sys() any { return &syscall.Stat_t{Uid: i.uid} }

func TestTrustedOwnershipAndModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	for _, mode := range []os.FileMode{0644, 0600, 0664, 0666} {
		if err := os.WriteFile(path, []byte(`{}`), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = trustedEntry(path, ownedInfo{info, 0}); (err == nil) != (mode.Perm()&0022 == 0) {
			t.Fatalf("mode %o err %v", mode, err)
		}
		if err = trustedEntry(path, ownedInfo{info, 1000}); err == nil {
			t.Fatal("non-root owner accepted")
		}
	}
	if err := trusted(path); err == nil {
		t.Fatal("mutable temp ancestry accepted")
	}
	if err := trusted(path + "-missing"); err == nil {
		t.Fatal("missing policy in mutable ancestry accepted")
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := trusted(link); err == nil {
		t.Fatal("untrusted symlink target accepted")
	}
	if err := trusted("/etc/hosts"); err != nil {
		t.Fatal(err)
	}
	if err := trusted("/"); err != nil {
		t.Fatal(err)
	}
	if err := trustedPath("/", 33); err == nil {
		t.Fatal("excess symlink traversal accepted")
	}
}
