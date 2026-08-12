package provider_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	providerExampleStart = "<!-- provider-example:start -->\n```go\n"
	providerExampleEnd   = "\n```\n<!-- provider-example:end -->"
)

func TestProviderAuthorGuideExampleCompiles(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("get test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), ".."))
	guidePath := filepath.Join(root, "docs", "provider-author-guide.md")
	guide, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatal(err)
	}

	source := between(t, string(guide), providerExampleStart, providerExampleEnd)
	temporary := t.TempDir()
	module := "module example.com/ecom-tinyshop\n\ngo 1.26.5\n\n" +
		"require github.com/kostyay/ecom v0.0.0\n\n" +
		"replace github.com/kostyay/ecom => " + filepath.ToSlash(root) + "\n"
	writeFile(t, filepath.Join(temporary, "go.mod"), module)
	writeFile(t, filepath.Join(temporary, "provider.go"), source)

	command := exec.CommandContext(t.Context(), "go", "test", "./...")
	command.Dir = temporary
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compile provider guide example: %v\n%s", err, output)
	}
}

func between(t *testing.T, text, start, end string) string {
	t.Helper()
	startIndex := strings.Index(text, start)
	if startIndex < 0 {
		t.Fatalf("documentation does not contain %q", start)
	}
	startIndex += len(start)
	endIndex := strings.Index(text[startIndex:], end)
	if endIndex < 0 {
		t.Fatalf("documentation does not contain %q after example start", end)
	}
	return text[startIndex : startIndex+endIndex]
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
