package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kostyay/ecom/internal/config"
)

func TestNewWritesJSON(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "ecom.log")
	logger, closer, err := New(config.LogSettings{Level: "debug", File: logFile})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.Debug("test message", "key", "value")
	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	file, err := os.Open(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close test log: %v", err)
		}
	}()

	var record map[string]any
	if err := json.NewDecoder(bufio.NewReader(file)).Decode(&record); err != nil {
		t.Fatalf("decode log JSON: %v", err)
	}
	if record["msg"] != "test message" {
		t.Errorf("message = %v, want test message", record["msg"])
	}
}

func TestNewWithoutFileDoesNotCreateUserCacheDirectory(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "cache-home")
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	logger, closer, err := New(config.LogSettings{Level: "info"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if closer != nil {
		t.Error("closer is not nil")
	}
	logger.Info("test message")

	if _, err := os.Stat(cacheHome); !os.IsNotExist(err) {
		t.Errorf("cache directory exists or stat failed unexpectedly: %v", err)
	}
}
