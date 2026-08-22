package lock

import (
	"os"
	"path/filepath"

	"github.com/neprel/git-a2a/internal/manifest"
)

func Load(root string) (*manifest.Lock, error) {
	l, err := manifest.LoadLock(filepath.Join(root, "a2amodule.lock"))
	if os.IsNotExist(err) {
		return &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{}}, nil
	}
	return l, err
}

func Write(root string, l *manifest.Lock) error {
	b, err := manifest.MarshalLock(l)
	if err != nil {
		return err
	}
	return Atomic(filepath.Join(root, "a2amodule.lock"), b, 0o644)
}

func Atomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".git-a2a-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
