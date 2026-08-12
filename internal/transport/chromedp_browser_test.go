package transport

import (
	"context"
	"errors"
	"testing"
)

type fakeChromeRunner struct {
	profile string
	result  BrowserResult
	err     error
}

func (runner *fakeChromeRunner) Run(_ context.Context, _ BrowserCommand, profile string) (BrowserResult, error) {
	runner.profile = profile
	return runner.result, runner.err
}

func TestChromedpBackendAlwaysRemovesTemporaryProfile(t *testing.T) {
	for _, runnerError := range []error{nil, errors.New("Chrome failed")} {
		t.Run(testErrorName(runnerError), func(t *testing.T) {
			runner := &fakeChromeRunner{err: runnerError}
			removed := ""
			backend := &ChromedpBackend{
				runner:        runner,
				makeProfile:   func() (string, error) { return "/tmp/ecom-profile-test", nil },
				removeProfile: func(path string) error { removed = path; return nil },
			}
			_, _ = backend.Navigate(context.Background(), BrowserCommand{})
			if runner.profile != "/tmp/ecom-profile-test" || removed != runner.profile {
				t.Fatalf("profile = %q, removed = %q", runner.profile, removed)
			}
		})
	}
}

func TestChromedpBackendReportsProfileCreationAndCleanupSafely(t *testing.T) {
	backend := &ChromedpBackend{
		runner:        &fakeChromeRunner{},
		makeProfile:   func() (string, error) { return "", errors.New("/private/path") },
		removeProfile: func(string) error { return nil },
	}
	_, err := backend.Navigate(context.Background(), BrowserCommand{})
	if err == nil || err.Error() != "create temporary browser profile" {
		t.Fatalf("Navigate() error = %v", err)
	}

	backend.makeProfile = func() (string, error) { return "/tmp/profile", nil }
	backend.removeProfile = func(string) error { return errors.New("secret cleanup path") }
	_, err = backend.Navigate(context.Background(), BrowserCommand{})
	if err == nil || err.Error() != "remove temporary browser profile" {
		t.Fatalf("Navigate() cleanup error = %v", err)
	}
}

func testErrorName(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}
