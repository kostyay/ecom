package ytindustries

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kostyay/ecom/provider"
	"github.com/kostyay/ecom/provider/conformance"
)

func TestStorefrontRequestSensitiveKeysAndChannelPartitions(t *testing.T) {
	home, err := os.ReadFile("testdata/home.html")
	if err != nil {
		t.Fatal(err)
	}
	contextBody, err := os.ReadFile("testdata/context.json")
	if err != nil {
		t.Fatal(err)
	}
	searchBody, err := os.ReadFile("testdata/search.json")
	if err != nil {
		t.Fatal(err)
	}
	var partitions []string
	for _, key := range []string{"FIXTUREPUBLICKEY", "OTHERCHANNELKEY"} {
		resources := conformance.NewFixtureService(
			conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: bytes.Replace(home, []byte("FIXTUREPUBLICKEY"), []byte(key), 1)}},
			conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: contextBody}},
			conformance.ResourceFixture{Response: provider.ResourceResponse{StatusCode: 200, Body: searchBody}},
		)
		_, err := (implementation{}).Search(t.Context(), provider.SearchRequest{Resources: resources, Market: provider.Market{Country: "DE", Language: "en", Currency: "EUR"}, Query: "capra"})
		if err != nil {
			t.Fatal(err)
		}
		requests := resources.Requests()
		if len(requests) != 3 {
			t.Fatalf("resource requests=%d", len(requests))
		}
		for _, request := range requests[1:] {
			sensitiveKey := false
			for _, header := range request.Headers {
				if header.Name == "sw-access-key" {
					sensitiveKey = header.Sensitive && len(header.Values) == 1 && header.Values[0] == key
				}
			}
			if !sensitiveKey || request.CachePartition == "" || strings.Contains(request.CachePartition, key) {
				t.Fatal("storefront key must be sensitive and its response cache safely partitioned")
			}
		}
		if requests[1].CachePartition != requests[2].CachePartition {
			t.Fatal("context and search used different channel partitions")
		}
		partitions = append(partitions, requests[2].CachePartition)
	}
	if partitions[0] == partitions[1] {
		t.Fatal("different storefront keys share a response cache partition")
	}
}
