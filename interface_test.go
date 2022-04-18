package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSend(t *testing.T) {
	notifiers := []Notifier{NewWebhook(WebhookParams{})}

	ctx, cancel := context.WithCancel(context.Background())
	assert.EqualError(t,
		Send(ctx, notifiers, "mailto:addr@example.org", ""),
		"unsupported destination schema: mailto")
	assert.EqualError(t,
		Send(ctx, notifiers, "bad destination", ""),
		"unsupported destination schema: bad destination")

	cancel()
	assert.EqualError(t,
		Send(ctx, notifiers, "https://example.org/webhook", ""),
		`webhook request failed: Post "https://example.org/webhook": context canceled`)
}

func TestInterface(t *testing.T) {
	assert.Implements(t, (*Notifier)(nil), new(Email))
	assert.Implements(t, (*Notifier)(nil), new(Webhook))
	assert.Implements(t, (*Notifier)(nil), new(Slack))
}
