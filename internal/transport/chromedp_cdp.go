package transport

import (
	"context"

	"github.com/chromedp/chromedp"
)

// ChromedpRemoteConnector attaches to an existing Chrome instance.
type ChromedpRemoteConnector struct{}

// NewChromedpRemoteConnector makes the production remote Chrome connector.
func NewChromedpRemoteConnector() *ChromedpRemoteConnector {
	return &ChromedpRemoteConnector{}
}

// Connect creates an allocator that disconnects without stopping Chrome.
func (*ChromedpRemoteConnector) Connect(ctx context.Context, address string) (CDPConnection, error) {
	allocatorContext, cancelAllocator := chromedp.NewRemoteAllocator(ctx, address)
	return &chromedpRemoteConnection{context: allocatorContext, cancel: cancelAllocator}, nil
}

type chromedpRemoteConnection struct {
	context context.Context
	cancel  context.CancelFunc
}

func (connection *chromedpRemoteConnection) NewTarget(context.Context) (CDPTarget, error) {
	targetContext, cancelTarget := chromedp.NewContext(connection.context)
	return &chromedpRemoteTarget{context: targetContext, cancel: cancelTarget}, nil
}

func (connection *chromedpRemoteConnection) Close() error {
	connection.cancel()
	return nil
}

type chromedpRemoteTarget struct {
	context context.Context
	cancel  context.CancelFunc
}

func (target *chromedpRemoteTarget) Navigate(ctx context.Context, command BrowserCommand) (BrowserResult, error) {
	stop := context.AfterFunc(ctx, target.cancel)
	defer stop()
	return runChromedpPage(target.context, command, false)
}

func (target *chromedpRemoteTarget) Close() error {
	target.cancel()
	return nil
}

var _ CDPConnector = (*ChromedpRemoteConnector)(nil)
