//go:build !windows

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const DefaultPath = "/etc/close/policy.json"

func Load() (*Policy, error) { return load(DefaultPath, trusted) }

func trusted(path string) error {
	return trustedPath(path, 0)
}

// Validate every ancestor and symlink target, including when the policy is
// missing. A policy can only be disabled by someone who controls its parents.
func trustedPath(path string, links int) error {
	if links > 32 {
		return fmt.Errorf("too many deployment policy symlinks")
	}
	info, err := os.Lstat(path)
	parent := filepath.Dir(path)
	if os.IsNotExist(err) && parent != path {
		return trustedPath(parent, links)
	}
	if err != nil {
		return err
	}
	if parent != path {
		if err = trustedPath(parent, links); err != nil {
			return err
		}
	}
	if err = trustedEntry(path, info); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		for _, part := range strings.Split(target, "/") {
			if part == ".." {
				return fmt.Errorf("deployment policy symlink targets must not contain parent traversal")
			}
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(parent, target)
		}
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("invalid deployment policy symlink target: %w", err)
		}
		return trustedPath(target, links+1)
	}
	return nil
}

func trustedEntry(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("%s must be root-owned", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("%s must not be group/world writable", path)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file or directory", path)
	}
	return nil
}
