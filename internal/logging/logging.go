package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	initOnce  sync.Once
	logWriter io.Writer
)

// Init configures logging to both console and file.
func Init() {
	writer := Writer()
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
}

func Writer() io.Writer {
	initOnce.Do(func() {
		logWriter = io.MultiWriter(os.Stdout, openLogFile())
	})
	if logWriter == nil {
		return os.Stdout
	}
	return logWriter
}

func openLogFile() *os.File {
	dir, err := os.UserConfigDir()
	if err != nil {
		return os.Stdout
	}
	path := filepath.Join(dir, "shinecore", "launcher.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return os.Stdout
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return os.Stdout
	}
	return file
}
