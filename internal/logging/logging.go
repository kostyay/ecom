// Package logging configures structured application logs.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kostyay/ecom/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	logDirName    = "ecom"
	logFileName   = "ecom.log"
	maxSizeMB     = 10
	maxBackups    = 3
	maxAgeDays    = 7
	directoryMode = 0o700
)

// New creates a JSON logger with file rotation.
func New(settings config.LogSettings) (*slog.Logger, io.Closer, error) {
	level, err := parseLevel(settings.Level)
	if err != nil {
		return nil, nil, err
	}

	logFile := settings.File
	if logFile == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, nil, fmt.Errorf("find user cache directory: %w", err)
		}
		logFile = filepath.Join(cacheDir, logDirName, logFileName)
	}

	if err := os.MkdirAll(filepath.Dir(logFile), directoryMode); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}

	writer := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   true,
		LocalTime:  true,
	}
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})

	return slog.New(handler), writer, nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("parse log level: use debug, info, warn, or error")
	}
}
