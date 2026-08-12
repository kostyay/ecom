package cache

import (
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
)

func TestBuildKeyEquivalentRequests(t *testing.T) {
	tests := []struct {
		name  string
		left  provider.ResourceRequest
		right provider.ResourceRequest
	}{
		{
			name:  "default method and canonical identity",
			left:  request("", "HTTPS://SHOP.EXAMPLE:443/%7esearch?b=2&a=1"),
			right: request("get", "https://shop.example/~search?a=1&b=2#ignored"),
		},
		{
			name: "query and header ordering",
			left: withValues(request("GET", "https://shop.example/search"),
				[]provider.RequestValue{{Name: "brand", Values: []string{"B", "A"}}, {Name: "page", Values: []string{"2"}}},
				[]provider.RequestValue{{Name: "Accept", Values: []string{" text/html ", "application/json"}}, {Name: "X-Market", Values: []string{"de"}}}),
			right: withValues(request("GET", "https://shop.example/search?page=2"),
				[]provider.RequestValue{{Name: "brand", Values: []string{"A"}}, {Name: "brand", Values: []string{"B"}}},
				[]provider.RequestValue{{Name: "x-market", Values: []string{"de"}}, {Name: "accept", Values: []string{"application/json", "text/html"}}}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustBuildKey(t, " Bike-Discount ", test.left)
			right := mustBuildKey(t, "bike-discount", test.right)
			if left != right {
				t.Fatalf("equivalent keys differ: %q and %q", left, right)
			}
		})
	}
}

func TestBuildKeyKeepsEncodedPathSeparatorsDistinct(t *testing.T) {
	encoded := mustBuildKey(t, "bike-discount", request("GET", "https://shop.example/category%2Fitem"))
	separator := mustBuildKey(t, "bike-discount", request("GET", "https://shop.example/category/item"))
	if encoded == separator {
		t.Fatal("encoded path separator did not change key")
	}
}

func TestBuildKeyTreatsEmptyBodyAsNoBody(t *testing.T) {
	withoutBody := request("GET", "https://shop.example/item")
	withoutBody.Body.Bytes = nil
	emptyBody := withoutBody
	emptyBody.Body.Bytes = []byte{}
	if left, right := mustBuildKey(t, "bike-discount", withoutBody), mustBuildKey(t, "bike-discount", emptyBody); left != right {
		t.Fatalf("empty body keys differ: %q and %q", left, right)
	}
}

func TestBuildKeySeparatesResponseIdentity(t *testing.T) {
	base := request("GET", "https://shop.example/item?id=1")
	tests := []struct {
		name     string
		provider string
		change   func(*provider.ResourceRequest)
	}{
		{name: "provider", provider: "other-shop"},
		{name: "country", change: func(value *provider.ResourceRequest) { value.Market.Country = "FR" }},
		{name: "language", change: func(value *provider.ResourceRequest) { value.Market.Language = "fr" }},
		{name: "currency", change: func(value *provider.ResourceRequest) { value.Market.Currency = "CHF" }},
		{name: "method", change: func(value *provider.ResourceRequest) { value.Method = "POST" }},
		{name: "URL", change: func(value *provider.ResourceRequest) { value.URL = "https://shop.example/other?id=1" }},
		{name: "query", change: func(value *provider.ResourceRequest) {
			value.Query = []provider.RequestValue{{Name: "size", Values: []string{"M"}}}
		}},
		{name: "body", change: func(value *provider.ResourceRequest) { value.Body.Bytes = []byte("page=2") }},
		{name: "header", change: func(value *provider.ResourceRequest) {
			value.Headers = []provider.RequestValue{{Name: "Accept-Language", Values: []string{"de"}}}
		}},
		{name: "required transport", change: func(value *provider.ResourceRequest) { value.Transport.Required = provider.TransportBrowser }},
		{name: "DOM extraction", change: func(value *provider.ResourceRequest) {
			value.DOM = []provider.DOMExtraction{{Name: "prices", Selector: ".price", Kind: provider.DOMText, All: true}}
		}},
		{name: "preferred transport order", change: func(value *provider.ResourceRequest) {
			value.Transport.Preferred = []provider.TransportMode{provider.TransportBrowser, provider.TransportHTTP}
		}},
		{name: "cache partition", change: func(value *provider.ResourceRequest) { value.CachePartition = "account-two" }},
	}

	want := mustBuildKey(t, "bike-discount", base)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			changed.Body.Bytes = append([]byte(nil), base.Body.Bytes...)
			providerName := test.provider
			if providerName == "" {
				providerName = "bike-discount"
			}
			if test.change != nil {
				test.change(&changed)
			}
			if got := mustBuildKey(t, providerName, changed); got == want {
				t.Fatalf("meaningful %s change did not change key", test.name)
			}
		})
	}
}

func TestBuildKeyExcludesSecretsAndCacheUsePolicy(t *testing.T) {
	base := request("GET", "https://shop.example/item")
	base.Query = []provider.RequestValue{{Name: "token", Values: []string{"first-secret"}, Sensitive: true}}
	base.Headers = []provider.RequestValue{{Name: "Authorization", Values: []string{"Bearer first-secret"}, Sensitive: true}}
	base.Body = provider.RequestBody{Bytes: []byte("first-secret"), Sensitive: true}

	changed := base
	changed.Query = []provider.RequestValue{{Name: "different-secret-name", Values: []string{"second-secret"}, Sensitive: true}}
	changed.Headers = []provider.RequestValue{{Name: "Cookie", Values: []string{"second-secret"}, Sensitive: true}}
	changed.Body.Bytes = []byte("second-secret")
	changed.Cache = provider.CachePolicy{Refresh: true, StaleIfError: true}
	changed.Interactive = true

	left := mustBuildKey(t, "bike-discount", base)
	right := mustBuildKey(t, "bike-discount", changed)
	if left != right {
		t.Fatalf("secret or cache-use policy changed key: %q and %q", left, right)
	}
	for _, secret := range []string{"first-secret", "second-secret", "Bearer"} {
		if strings.Contains(left, secret) || strings.Contains(right, secret) {
			t.Fatalf("key contains secret text %q", secret)
		}
	}

	changed.CachePartition = "session-two"
	if partitioned := mustBuildKey(t, "bike-discount", changed); partitioned == left {
		t.Fatal("cache partition did not separate sensitive response identity")
	}
}

func TestBuildKeyIgnoresPreferredTransportWhenTransportIsRequired(t *testing.T) {
	left := request("GET", "https://shop.example/item")
	left.Transport = provider.TransportPolicy{
		Required:  provider.TransportBrowser,
		Preferred: []provider.TransportMode{provider.TransportHTTP},
	}
	right := left
	right.Transport.Preferred = []provider.TransportMode{provider.TransportCDP, provider.TransportBrowser}

	leftKey := mustBuildKey(t, "bike-discount", left)
	rightKey := mustBuildKey(t, "bike-discount", right)
	if leftKey != rightKey {
		t.Fatalf("ignored preferred transports changed key: %q and %q", leftKey, rightKey)
	}
}

func TestBuildKeyRejectsMalformedInputWithoutSecretText(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		request   provider.ResourceRequest
		secretURL string
	}{
		{name: "missing provider", request: request("GET", "https://shop.example")},
		{name: "userinfo", provider: "shop", request: request("GET", "https://user:very-secret@shop.example/item"), secretURL: "very-secret"},
		{name: "malformed query", provider: "shop", request: request("GET", "https://shop.example/item?token=%zz"), secretURL: "%zz"},
		{name: "unsupported scheme", provider: "shop", request: request("GET", "file:///very-secret"), secretURL: "very-secret"},
		{name: "malformed method", provider: "shop", request: request("GET very-secret", "https://shop.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, err := BuildKey(test.provider, test.request)
			if err == nil {
				t.Fatalf("BuildKey() = %q, want error", key.String())
			}
			if key != (Key{}) {
				t.Fatalf("BuildKey() key = %#v, want zero value", key)
			}
			if test.secretURL != "" && strings.Contains(err.Error(), test.secretURL) {
				t.Fatalf("error contains request text: %q", err)
			}
		})
	}
}

func TestBuildKeyReturnsVersionedDigestOnly(t *testing.T) {
	key, err := BuildKey("bike-discount", request("GET", "https://shop.example/private-product"))
	if err != nil {
		t.Fatalf("BuildKey() error = %v", err)
	}
	if key.Version != "v1" || len(key.Digest) != sha256HexLength {
		t.Fatalf("BuildKey() = %#v, want v1 SHA-256 digest", key)
	}
	if got := key.String(); len(got) != len("v1:")+sha256HexLength || strings.Contains(got, "shop.example") {
		t.Fatalf("Key.String() = %q, want digest only", got)
	}
}

const sha256HexLength = 64

func request(method, rawURL string) provider.ResourceRequest {
	return provider.ResourceRequest{
		Method: method,
		URL:    rawURL,
		Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"},
		Body:   provider.RequestBody{Bytes: []byte("page=1")},
		Transport: provider.TransportPolicy{
			Preferred: []provider.TransportMode{provider.TransportHTTP, provider.TransportBrowser},
		},
		CachePartition: "account-one",
	}
}

func withValues(request provider.ResourceRequest, query, headers []provider.RequestValue) provider.ResourceRequest {
	request.Query = query
	request.Headers = headers
	return request
}

func mustBuildKey(t *testing.T, providerName string, request provider.ResourceRequest) string {
	t.Helper()
	key, err := BuildKey(providerName, request)
	if err != nil {
		t.Fatalf("BuildKey() error = %v", err)
	}
	return key.String()
}
