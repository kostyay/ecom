package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kostyay/ecom/internal/version"
	"github.com/spf13/viper"
)

func TestProviderFlagOverridesConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("provider: from-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := &application{viper: viper.New()}
	command := application.newRootCommand(io.Discard, io.Discard)
	command.SetArgs([]string{
		"version", "--config", configPath, "--provider", "from-flag",
		"--log-file", filepath.Join(t.TempDir(), "ecom.log"),
	})
	if err := command.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer application.logCloser.Close()
	if application.settings.Provider != "from-flag" {
		t.Errorf("provider = %q, want from-flag", application.settings.Provider)
	}
	if application.services != nil {
		t.Error("provider-free version command initialized application services")
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       func(t *testing.T) []string
		wantStatus int
		wantOut    string
		wantCode   string
	}{
		{
			name: "prints help without a command",
			args: func(t *testing.T) []string {
				return []string{"--log-file", filepath.Join(t.TempDir(), "ecom.log")}
			},
			wantStatus: 0,
			wantOut:    "Usage:",
		},
		{
			name: "prints machine-readable version",
			args: func(t *testing.T) []string {
				return []string{"version", "--json", "--log-file", filepath.Join(t.TempDir(), "ecom.log")}
			},
			wantStatus: 0,
			wantOut:    `"version"`,
		},
		{
			name: "returns machine-readable config error",
			args: func(t *testing.T) []string {
				return []string{"version", "--json", "--config", filepath.Join(t.TempDir(), "missing.yaml")}
			},
			wantStatus: 1,
			wantCode:   codeConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			status := Run(t.Context(), tc.args(t), &stdout, &stderr)

			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; stderr = %s", status, tc.wantStatus, stderr.String())
			}
			if tc.wantOut != "" && !strings.Contains(stdout.String(), tc.wantOut) {
				t.Errorf("stdout = %q, want text %q", stdout.String(), tc.wantOut)
			}
			if tc.wantCode != "" {
				var response errorEnvelope
				if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
					t.Fatalf("decode error JSON: %v", err)
				}
				if response.Error.Code != tc.wantCode {
					t.Errorf("error code = %q, want %q", response.Error.Code, tc.wantCode)
				}
			}
		})
	}
}

func TestVersionInfoHasRequiredFields(t *testing.T) {
	info := version.Current()
	if info.Version == "" {
		t.Error("version is empty")
	}
	if info.GoVersion == "" {
		t.Error("Go version is empty")
	}
}
