package app

import (
	"io"
	"log/slog"
	"os"
)

func setupLogger(path, level string) (*slog.Logger, func(), error) {
	var writer io.Writer = io.Discard
	var cleanup func()
	if path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, err
		}
		writer = file
		cleanup = func() { _ = file.Close() }
	} else {
		cleanup = func() {}
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slogLevel(level)})
	return slog.New(handler), cleanup, nil
}

func slogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
