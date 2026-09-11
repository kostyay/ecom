package cli

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUserGuideDocumentsCurrentCommandsAndConfiguration(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("get documentation test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	guide := readDocumentation(t, filepath.Join(root, "docs", "user-guide.md"))
	readme := readDocumentation(t, filepath.Join(root, "README.md"))

	commandPaths := []string{
		"provider help", "search", "categories", "category-items", "brands",
		"brand-items", "deals", "filters", "item", "cache clear", "cache prune",
		"provider session clear",
	}
	command := (&application{}).newRootCommand(io.Discard, io.Discard)
	for _, path := range commandPaths {
		arguments := strings.Fields(path)
		found, _, err := command.Find(arguments)
		if err != nil || found == command || found.CommandPath() != "ecom "+path {
			t.Errorf("documented command %q does not resolve: found %q, error %v", path, found.CommandPath(), err)
		}
		if !strings.Contains(guide, "ecom "+path) {
			t.Errorf("user guide does not contain command %q", path)
		}
	}

	for _, text := range []string{
		"pricing:\n  include_shipping: false",
		"ttl: 24h",
		"requests_per_second: 1",
		"cdp_address:",
		"jsonpath=",
		"--interactive",
		"No currency conversion occurs.",
		"ecom provider help buscocotxe",
		"--provider buscocotxe --page 1 --page-size 30",
		"Search results can include sold cars and cars with no stated price.",
	} {
		if !strings.Contains(guide, text) {
			t.Errorf("user guide does not contain %q", text)
		}
	}
	for _, text := range []string{"docs/user-guide.md", "pricing:\n  include_shipping: false", "--provider buscocotxe --page 1 --page-size 30"} {
		if !strings.Contains(readme, text) {
			t.Errorf("README does not contain %q", text)
		}
	}
}

func readDocumentation(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
