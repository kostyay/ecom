// Package session defines portable browser session state.
package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kostyay/ecom/provider"
)

var (
	// ErrStateNotFound means that storage has no state for a provider and market.
	ErrStateNotFound = errors.New("browser session state not found")
	providerPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
)

// SameSite is a browser cookie SameSite policy.
type SameSite string

const (
	// SameSiteStrict sends the cookie only for same-site requests.
	SameSiteStrict SameSite = "Strict"
	// SameSiteLax permits selected top-level cross-site requests.
	SameSiteLax SameSite = "Lax"
	// SameSiteNone permits cross-site requests when browser rules allow them.
	SameSiteNone SameSite = "None"
)

// Cookie is portable cookie data used by browser transports.
type Cookie struct {
	Name     string   `json:"name"`
	Value    string   `json:"value"`
	Domain   string   `json:"domain"`
	Path     string   `json:"path"`
	Expires  *int64   `json:"expires,omitempty"`
	HTTPOnly bool     `json:"httpOnly"`
	Secure   bool     `json:"secure"`
	SameSite SameSite `json:"sameSite,omitempty"`
}

// StorageEntry is one local-storage name and value.
type StorageEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Origin contains local storage for one exact web origin.
type Origin struct {
	Origin       string         `json:"origin"`
	LocalStorage []StorageEntry `json:"localStorage"`
}

// State is the portable subset of browser state that ecom persists.
// Its JSON shape is compatible with the relevant Playwright storage-state fields.
type State struct {
	Cookies []Cookie `json:"cookies"`
	Origins []Origin `json:"origins"`
}

// Record scopes portable state to one provider and exact market.
type Record struct {
	Provider  string
	Market    provider.Market
	State     State
	UpdatedAt time.Time
}

// Repository persists portable browser state independently from response caches.
type Repository interface {
	Put(context.Context, Record) (Record, error)
	Get(context.Context, string, provider.Market) (Record, error)
	Delete(context.Context, string, provider.Market) (bool, error)
}

// Validate checks the complete session record.
func (record Record) Validate() error {
	if !providerPattern.MatchString(record.Provider) || len(record.Provider) > 63 {
		return errors.New("browser session provider is invalid")
	}
	if err := validateMarket(record.Market); err != nil {
		return fmt.Errorf("browser session market: %w", err)
	}
	if record.UpdatedAt.IsZero() {
		return errors.New("browser session update time is required")
	}
	if err := record.State.Validate(); err != nil {
		return fmt.Errorf("browser session state: %w", err)
	}
	return nil
}

// Validate checks all cookies, origins, and local-storage entries.
func (state State) Validate() error {
	cookieKeys := make(map[string]struct{}, len(state.Cookies))
	for index, cookie := range state.Cookies {
		if err := cookie.validate(); err != nil {
			return fmt.Errorf("cookie %d: %w", index, err)
		}
		key := cookie.Name + "\x00" + cookie.Domain + "\x00" + cookie.Path
		if _, exists := cookieKeys[key]; exists {
			return fmt.Errorf("cookie %d duplicates its name, domain, and path", index)
		}
		cookieKeys[key] = struct{}{}
	}

	origins := make(map[string]struct{}, len(state.Origins))
	for index, origin := range state.Origins {
		if err := origin.validate(); err != nil {
			return fmt.Errorf("origin %d: %w", index, err)
		}
		if _, exists := origins[origin.Origin]; exists {
			return fmt.Errorf("origin %d duplicates %q", index, origin.Origin)
		}
		origins[origin.Origin] = struct{}{}
	}
	return nil
}

func (cookie Cookie) validate() error {
	if !validCookieName(cookie.Name) {
		return errors.New("name is invalid")
	}
	if containsControl(cookie.Value) {
		return errors.New("value contains a control character")
	}
	if !validCookieDomain(cookie.Domain) {
		return errors.New("domain is invalid")
	}
	if !strings.HasPrefix(cookie.Path, "/") || containsControl(cookie.Path) {
		return errors.New("path must start with a slash and contain no control characters")
	}
	if cookie.Expires != nil && *cookie.Expires < 0 {
		return errors.New("expiry must be a non-negative Unix timestamp")
	}
	if cookie.SameSite != "" && cookie.SameSite != SameSiteStrict && cookie.SameSite != SameSiteLax && cookie.SameSite != SameSiteNone {
		return errors.New("same-site policy is invalid")
	}
	return nil
}

func (origin Origin) validate() error {
	parsed, err := url.Parse(origin.Origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("URL must be an absolute HTTP origin")
	}
	if parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("URL must not contain a path, query, or fragment")
	}
	if !validHost(parsed.Hostname()) {
		return errors.New("URL host is invalid")
	}

	names := make(map[string]struct{}, len(origin.LocalStorage))
	for index, entry := range origin.LocalStorage {
		if entry.Name == "" || containsControl(entry.Name) {
			return fmt.Errorf("local-storage entry %d has an invalid name", index)
		}
		if _, exists := names[entry.Name]; exists {
			return fmt.Errorf("local-storage entry %d duplicates name %q", index, entry.Name)
		}
		names[entry.Name] = struct{}{}
	}
	return nil
}

func validateMarket(market provider.Market) error {
	if err := market.Validate(); err != nil {
		return err
	}
	if market.Country != strings.TrimSpace(market.Country) || market.Language != strings.TrimSpace(market.Language) ||
		market.Currency != strings.TrimSpace(market.Currency) {
		return errors.New("values must not have surrounding whitespace")
	}
	return nil
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, value := range []byte(name) {
		if value <= 0x20 || value >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(value)) {
			return false
		}
	}
	return true
}

func validCookieDomain(domain string) bool {
	if domain == "" || domain != strings.TrimSpace(domain) {
		return false
	}
	host := strings.TrimPrefix(domain, ".")
	return host != "" && validHost(host)
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, value := range []byte(label) {
			if (value < 'a' || value > 'z') && (value < 'A' || value > 'Z') &&
				(value < '0' || value > '9') && value != '-' {
				return false
			}
		}
	}
	return true
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
