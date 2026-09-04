package transport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/kostyay/ecom/internal/session"
	"github.com/kostyay/ecom/provider"
)

type chromeRunner interface {
	Run(context.Context, BrowserCommand, string) (BrowserResult, error)
}

// ChromedpBackend runs Chrome through the DevTools Protocol in a temporary
// profile. It has no Node.js or installed Playwright dependency.
type ChromedpBackend struct {
	runner        chromeRunner
	makeProfile   func() (string, error)
	removeProfile func(string) error
}

// NewChromedpBackend makes the production isolated Chrome backend.
func NewChromedpBackend() *ChromedpBackend {
	return &ChromedpBackend{
		runner:        chromedpRunner{},
		makeProfile:   func() (string, error) { return os.MkdirTemp("", "ecom-chrome-*") },
		removeProfile: os.RemoveAll,
	}
}

// Navigate creates and always removes one isolated user-data directory.
func (backend *ChromedpBackend) Navigate(ctx context.Context, command BrowserCommand) (result BrowserResult, err error) {
	profile, err := backend.makeProfile()
	if err != nil {
		return BrowserResult{}, errors.New("create temporary browser profile")
	}
	defer func() {
		if cleanupErr := backend.removeProfile(profile); err == nil && cleanupErr != nil {
			err = errors.New("remove temporary browser profile")
		}
	}()
	return backend.runner.Run(ctx, command, profile)
}

type chromedpRunner struct{}

func (chromedpRunner) Run(ctx context.Context, command BrowserCommand, profile string) (BrowserResult, error) {
	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", !command.Headed),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()
	pageContext, cancelPage := chromedp.NewContext(allocatorContext)
	defer cancelPage()
	return runChromedpPage(pageContext, command, true)
}

func runChromedpPage(pageContext context.Context, command BrowserCommand, exportState bool) (BrowserResult, error) {
	var responseMu sync.Mutex
	var documentResponse *network.Response
	chromedp.ListenTarget(pageContext, func(event any) {
		received, ok := event.(*network.EventResponseReceived)
		if !ok || received.Type != network.ResourceTypeDocument || received.Response == nil {
			return
		}
		responseMu.Lock()
		responseCopy := *received.Response
		documentResponse = &responseCopy
		responseMu.Unlock()
	})

	initialActions, err := browserInitialActions(command)
	if err != nil {
		return BrowserResult{}, err
	}
	if err := chromedp.Run(pageContext, initialActions...); err != nil {
		return BrowserResult{}, err
	}

	var html string
	var finalURL string
	dom := make(map[string][]string, len(command.DOM))
	actions := chromedp.Tasks{
		chromedp.Navigate(command.URL),
		chromedp.Poll(`document.readyState === "interactive" || document.readyState === "complete"`, nil, chromedp.WithPollingTimeout(0)),
	}
	if command.WaitForChallenge {
		actions = append(actions, chromedp.Poll(`!document.querySelector('script[src*="/cdn-cgi/challenge-platform/"], [id^="cf-chl-"], .cf-challenge') && !/just a moment|checking your browser/i.test(document.title)`, nil, chromedp.WithPollingTimeout(0), chromedp.WithPollingInterval(250*time.Millisecond)))
	}
	actions = append(actions,
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		chromedp.Location(&finalURL),
	)
	for _, operation := range command.DOM {
		actions = append(actions, chromedp.ActionFunc(func(actionContext context.Context) error {
			values, err := extractDOM(actionContext, operation)
			if err == nil {
				dom[operation.Name] = values
			}
			return err
		}))
	}
	if err := chromedp.Run(pageContext, actions...); err != nil {
		return BrowserResult{}, err
	}
	if int64(len(html)) > command.MaxPageSize {
		return BrowserResult{}, provider.NewError(provider.ErrorCodeResponseTooLarge, "the browser page exceeds the configured size limit", nil)
	}

	exported := session.State{}
	if exportState {
		var err error
		exported, err = exportChromeState(pageContext, command.State, finalURL)
		if err != nil {
			return BrowserResult{}, err
		}
	}
	responseMu.Lock()
	captured := documentResponse
	responseMu.Unlock()
	if captured == nil {
		return BrowserResult{}, errors.New("browser did not report the document response")
	}
	return BrowserResult{
		HTML: []byte(html), DOM: dom, FinalURL: finalURL, StatusCode: int(captured.Status),
		Headers: chromeHeaders(captured.Headers), State: exported,
	}, nil
}

func browserInitialActions(command BrowserCommand) (chromedp.Tasks, error) {
	actions := chromedp.Tasks{network.Enable(), page.Enable()}
	if len(command.Headers) > 0 {
		headers := make(network.Headers, len(command.Headers))
		for name, value := range command.Headers {
			headers[name] = value
		}
		actions = append(actions, network.SetExtraHTTPHeaders(headers))
	}
	if len(command.State.Cookies) > 0 {
		cookies := make([]*network.CookieParam, 0, len(command.State.Cookies))
		for _, cookie := range command.State.Cookies {
			parameter := &network.CookieParam{
				Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
				Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: network.CookieSameSite(cookie.SameSite),
			}
			if cookie.Expires != nil {
				expires := cdp.TimeSinceEpoch(time.Unix(*cookie.Expires, 0).UTC())
				parameter.Expires = &expires
			}
			cookies = append(cookies, parameter)
		}
		actions = append(actions, network.SetCookies(cookies))
	}
	if len(command.State.Origins) > 0 {
		encoded, err := json.Marshal(command.State.Origins)
		if err != nil {
			return nil, err
		}
		script := `(function(){const origins=` + string(encoded) + `;const item=origins.find(x=>x.origin===location.origin);if(item){for(const entry of item.localStorage){localStorage.setItem(entry.name,entry.value);}}})()`
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
			return err
		}))
	}
	return actions, nil
}

func extractDOM(ctx context.Context, operation provider.DOMExtraction) ([]string, error) {
	selector, _ := json.Marshal(operation.Selector)
	attribute, _ := json.Marshal(operation.Attribute)
	kind, _ := json.Marshal(operation.Kind)
	all := "false"
	if operation.All {
		all = "true"
	}
	expression := `(function(){const nodes=Array.from(document.querySelectorAll(` + string(selector) + `));const selected=` + all + `?nodes:nodes.slice(0,1);return selected.map(node=>{switch(` + string(kind) + `){case "text":return node.textContent||"";case "html":return node.innerHTML;case "attribute":return node.getAttribute(` + string(attribute) + `)||"";default:return "";}})})()`
	var values []string
	if err := chromedp.Evaluate(expression, &values).Do(ctx); err != nil {
		return nil, err
	}
	return values, nil
}

func exportChromeState(ctx context.Context, imported session.State, finalURL string) (session.State, error) {
	var cookies []*network.Cookie
	var storage map[string]string
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(actionContext context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(actionContext)
			return err
		}),
		chromedp.Evaluate(`Object.fromEntries(Object.entries(localStorage))`, &storage),
	); err != nil {
		return session.State{}, err
	}

	result := session.State{Origins: append([]session.Origin(nil), imported.Origins...)}
	for _, cookie := range cookies {
		portable := session.Cookie{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			HTTPOnly: cookie.HTTPOnly, Secure: cookie.Secure, SameSite: session.SameSite(cookie.SameSite),
		}
		if !cookie.Session && cookie.Expires >= 0 {
			expires := int64(cookie.Expires)
			portable.Expires = &expires
		}
		result.Cookies = append(result.Cookies, portable)
	}
	slices.SortFunc(result.Cookies, func(left, right session.Cookie) int {
		return cmp.Compare(left.Domain+"\x00"+left.Path+"\x00"+left.Name, right.Domain+"\x00"+right.Path+"\x00"+right.Name)
	})

	origin := originFromURL(finalURL)
	entries := make([]session.StorageEntry, 0, len(storage))
	for name, value := range storage {
		entries = append(entries, session.StorageEntry{Name: name, Value: value})
	}
	slices.SortFunc(entries, func(left, right session.StorageEntry) int { return cmp.Compare(left.Name, right.Name) })
	replaced := false
	for index := range result.Origins {
		if result.Origins[index].Origin == origin {
			result.Origins[index].LocalStorage = entries
			replaced = true
		}
	}
	if !replaced {
		result.Origins = append(result.Origins, session.Origin{Origin: origin, LocalStorage: entries})
	}
	return result, nil
}

func originFromURL(rawURL string) string {
	parsed, _ := url.Parse(rawURL)
	return parsed.Scheme + "://" + parsed.Host
}

func chromeHeaders(input network.Headers) http.Header {
	result := make(http.Header)
	for name, value := range input {
		switch typed := value.(type) {
		case string:
			result.Add(name, typed)
		case []string:
			for _, item := range typed {
				result.Add(name, item)
			}
		}
	}
	return result
}

var _ BrowserBackend = (*ChromedpBackend)(nil)
