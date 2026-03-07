package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	privateDirPerm  = 0o700
	privateFilePerm = 0o600
)

type DirLock struct {
	path string
	file *os.File
}

func AcquireDirLock(dir string) (*DirLock, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("state_dir_invalid: directory path is empty")
	}
	if err := EnsurePrivateDir(dir); err != nil {
		return nil, err
	}

	lockPath := filepath.Join(dir, ".dbmigrate.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFilePerm)
	if err != nil {
		if os.IsExist(err) {
			owner := "unknown owner"
			if raw, readErr := os.ReadFile(lockPath); readErr == nil {
				trimmed := strings.TrimSpace(string(raw))
				if trimmed != "" {
					owner = trimmed
				}
			}
			return nil, fmt.Errorf(
				"state_dir_locked: %s is already in use; lock_file=%s owner={%s}. Verify no active dbmigrate process still owns this state-dir, then remove the stale lock file manually and retry",
				dir,
				lockPath,
				owner,
			)
		}
		return nil, fmt.Errorf("create state lock %s: %w", lockPath, err)
	}

	hostname, hostErr := os.Hostname()
	if hostErr != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	payload := fmt.Sprintf(
		"pid=%d hostname=%s started_at=%s cwd=%s",
		os.Getpid(),
		hostname,
		time.Now().UTC().Format(time.RFC3339Nano),
		mustGetwd(),
	)
	if _, err := file.WriteString(payload); err != nil {
		_ = file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("write state lock %s: %w", lockPath, err)
	}

	return &DirLock{
		path: lockPath,
		file: file,
	}, nil
}

func (l *DirLock) Release() error {
	if l == nil {
		return nil
	}

	var errs []string
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if l.path != "" {
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("release state lock: %s", strings.Join(errs, "; "))
	}
	return nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return "unknown-cwd"
	}
	return wd
}

func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, privateDirPerm); err != nil {
		return fmt.Errorf("mkdir state dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, privateDirPerm); err != nil {
		return fmt.Errorf("chmod state dir %s: %w", dir, err)
	}
	return nil
}

func WritePrivateFileAtomic(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := EnsurePrivateDir(dir); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(privateFilePerm); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp file %s: %w", tmpPath, err)
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp file %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanup = false
	return nil
}

func writePrivateFileAtomic(path string, raw []byte) error {
	return WritePrivateFileAtomic(path, raw)
}
