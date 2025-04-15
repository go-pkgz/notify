package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend(t *testing.T) {
	notifiers := []Notifier{NewWebhook(WebhookParams{})}

	ctx, cancel := context.WithCancel(context.Background())
	require.EqualError(t,
		Send(ctx, notifiers, "mailto:addr@example.org", ""),
		"unsupported destination schema: mailto")
	require.EqualError(t,
		Send(ctx, notifiers, "bad destination", ""),
		"unsupported destination schema: bad destination")

	cancel()
	require.EqualError(t,
		Send(ctx, notifiers, "https://example.org/webhook", ""),
		`webhook request failed: Post "https://example.org/webhook": context canceled`)
}

func TestInterface(t *testing.T) {
	assert.Implements(t, (*Notifier)(nil), new(Email))
	assert.Implements(t, (*Notifier)(nil), new(Webhook))
	assert.Implements(t, (*Notifier)(nil), new(Slack))
	assert.Implements(t, (*Notifier)(nil), new(Telegram))
}
