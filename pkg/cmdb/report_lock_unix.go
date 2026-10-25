//go:build unix

package cmdb

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock prevents two center processes from writing the same report archive.
// The OS releases ownership after a crash; the persistent lock file is never removed.
func (s *ReportStore) Lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.Root, ".writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("report directory already has a writer: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
