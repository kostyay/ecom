package bikediscount_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const catalogFixtureDirectory = "testdata/catalog"

type catalogManifest struct {
	SchemaVersion string           `json:"schema_version"`
	Provider      string           `json:"provider"`
	CaptureDate   string           `json:"capture_date"`
	AssembledDate string           `json:"assembled_date"`
	Market        fixtureMarket    `json:"market"`
	Sanitization  json.RawMessage  `json:"sanitization"`
	Fixtures      []catalogFixture `json:"fixtures"`
}

type fixtureMarket struct {
	Country  string `json:"country"`
	Language string `json:"language"`
	Currency string `json:"currency"`
}

type catalogFixture struct {
	ID                   string   `json:"id"`
	Path                 string   `json:"path"`
	MediaType            string   `json:"media_type"`
	SourceURL            string   `json:"source_url"`
	FinalURL             string   `json:"final_url"`
	Roles                []string `json:"roles"`
	Provenance           string   `json:"provenance"`
	Verification         string   `json:"verification"`
	ContainsPlaceholders bool     `json:"contains_placeholders"`
	SHA256               string   `json:"sha256"`
	Notes                []string `json:"notes"`
}

func TestCatalogFixtureInventory(t *testing.T) {
	manifest := readCatalogManifest(t)
	if manifest.SchemaVersion != "1" || manifest.Provider != providerName {
		t.Fatalf("manifest identity = %q/%q, want 1/%s", manifest.SchemaVersion, manifest.Provider, providerName)
	}
	if manifest.Market != (fixtureMarket{Country: "DE", Language: "en", Currency: "EUR"}) {
		t.Fatalf("manifest market = %#v, want DE/en/EUR", manifest.Market)
	}
	for field, value := range map[string]string{"capture_date": manifest.CaptureDate, "assembled_date": manifest.AssembledDate} {
		if _, err := time.Parse(time.DateOnly, value); err != nil {
			t.Errorf("%s %q is not an ISO date: %v", field, value, err)
		}
	}

	requiredRoles := map[string]bool{
		"llms_roots": false, "category_roots": false, "category_tree": false,
		"category_listing": false, "search_listing": false, "filters": false,
		"sort": false, "page_size": false, "paging_page_1": false,
		"paging_page_2": false, "brands": false, "brand_listing": false,
		"deals": false, "item": false, "variants": false,
		"unavailable_item": false, "partial_parse": false,
	}
	seenIDs := make(map[string]struct{}, len(manifest.Fixtures))
	seenPaths := make(map[string]struct{}, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || fixture.Path == "" || fixture.MediaType == "" || fixture.SourceURL == "" || fixture.Provenance == "" || fixture.Verification == "" {
			t.Errorf("fixture has incomplete metadata: %#v", fixture)
		}
		if _, exists := seenIDs[fixture.ID]; exists {
			t.Errorf("duplicate fixture ID %q", fixture.ID)
		}
		seenIDs[fixture.ID] = struct{}{}
		if filepath.Base(fixture.Path) != fixture.Path {
			t.Errorf("fixture path %q is not a local file name", fixture.Path)
		}
		if _, exists := seenPaths[fixture.Path]; exists {
			t.Errorf("duplicate fixture path %q", fixture.Path)
		}
		seenPaths[fixture.Path] = struct{}{}
		content := readFixture(t, fixture.Path)
		if len(content) == 0 {
			t.Errorf("fixture %q is empty", fixture.Path)
		}
		gotHash := sha256.Sum256(content)
		if hex.EncodeToString(gotHash[:]) != fixture.SHA256 {
			t.Errorf("fixture %q hash changed; update it only after a provenance and security review", fixture.Path)
		}
		for _, role := range fixture.Roles {
			if _, required := requiredRoles[role]; required {
				requiredRoles[role] = true
			}
		}
	}
	for role, found := range requiredRoles {
		if !found {
			t.Errorf("required fixture role %q is missing", role)
		}
	}
	entries, err := os.ReadDir(catalogFixtureDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "manifest.json" || entry.Name() == "README.md" {
			continue
		}
		if _, listed := seenPaths[entry.Name()]; !listed {
			t.Errorf("fixture file %q is not in the manifest", entry.Name())
		}
	}
}

func TestCatalogFixturesContainNoSensitiveOrExcludedValues(t *testing.T) {
	manifest := readCatalogManifest(t)
	forbidden := map[string]*regexp.Regexp{
		"response cookie":    regexp.MustCompile(`(?i)set-cookie|cookie\s*:`),
		"browser state":      regexp.MustCompile(`(?i)localstorage|sessionstorage|session[_-]?id`),
		"Cloudflare ID":      regexp.MustCompile(`(?i)cf-ray|cf_chl|__cf|cloudflare\s+ray`),
		"analytics ID":       regexp.MustCompile(`(?i)google-analytics|googletagmanager|gtag\s*\(|\bUA-[0-9-]+\b|\bG-[A-Z0-9]{8,}\b`),
		"tracking parameter": regexp.MustCompile(`(?i)[?&](utm_[a-z]+|fbclid|gclid)=`),
		"email address":      regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
		"embedded secret":    regexp.MustCompile(`(?i)(access[_-]?token|refresh[_-]?token|authorization)\s*[=:]`),
		"delivery charge":    regexp.MustCompile(`(?i)plus shipping|shipping costs?|delivery (fee|charge)|versandkosten`),
	}
	for _, fixture := range manifest.Fixtures {
		content := string(readFixture(t, fixture.Path))
		for label, pattern := range forbidden {
			if pattern.MatchString(content) {
				t.Errorf("fixture %q contains a forbidden %s pattern", fixture.Path, label)
			}
		}
		if strings.Contains(content, "data-fixture-placeholder") != fixture.ContainsPlaceholders {
			t.Errorf("fixture %q placeholder metadata does not match its content", fixture.Path)
		}
	}
}

func readCatalogManifest(t *testing.T) catalogManifest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(catalogFixtureDirectory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest catalogManifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode fixture manifest: %v", err)
	}
	return manifest
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(catalogFixtureDirectory, name))
	if err != nil {
		t.Fatal(fmt.Errorf("read fixture %q: %w", name, err))
	}
	return content
}
