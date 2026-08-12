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
