package telemetry

import (
	"os"
	"path/filepath"
	"syscall"
)

const (
	ScratchFileMode      os.FileMode = 0o600
	ScratchDirectoryMode os.FileMode = 0o700
	MaxScratchFileBytes  int64       = 1 << 20
	MaxScratchTotalBytes int64       = 64 << 20
)

// ScratchWriter is the only writer for the task-scoped runtime scratch volume.
// It writes regular data files and refuses anything a later process could execute
// or follow outside the volume.
type ScratchWriter struct {
	root         string
	writtenBytes int64
}

func NewScratchWriter(root string) (*ScratchWriter, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, contractError("scratch root")
	}
	return &ScratchWriter{root: root}, nil
}

func (writer *ScratchWriter) Write(relative string, contents []byte) error {
	if !validRelativeAssetPath(relative) || filepath.Dir(relative) != "." {
		return contractError("scratch path")
	}
	size := int64(len(contents))
	if size > MaxScratchFileBytes || writer.writtenBytes+size > MaxScratchTotalBytes {
		return contractError("scratch size")
	}
	path := filepath.Join(writer.root, relative)
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return contractError("scratch target")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, ScratchFileMode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, ScratchFileMode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != ScratchFileMode {
		return contractError("scratch mode")
	}
	writer.writtenBytes += size
	return nil
}
