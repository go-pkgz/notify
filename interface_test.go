package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

type schemeNotifier struct{ name, schema string }

func (n schemeNotifier) String() string { return n.name }
func (n schemeNotifier) Schema() string { return n.schema }
func (n schemeNotifier) Send(_ context.Context, _, _ string) error {
	return errors.New("sent by " + n.name)
}

func TestSend_SchemeMatch(t *testing.T) {
	notifiers := []Notifier{
		schemeNotifier{name: "first webhook", schema: "http"},
		schemeNotifier{name: "second webhook", schema: "http"},
		schemeNotifier{name: "slack", schema: "slack"},
		schemeNotifier{name: "email", schema: "mailto"},
		schemeNotifier{name: "custom", schema: "custom:"},
		schemeNotifier{name: "secure", schema: "secure://"},
	}
	for destination, want := range map[string]string{
		"https://example.org/hook": "sent by first webhook",
		"http://example.org/hook":  "sent by first webhook",
		"HTTPS://example.org/hook": "sent by first webhook",
		"slack:general":            "sent by slack",
		"SLACK:general":            "sent by slack",
		"Mailto:a@example.org":     "sent by email",
		"httpx:foo":                "unsupported destination schema: httpx",
		"slackers:general":         "unsupported destination schema: slackers",
		"mailtox:a@example.org":    "unsupported destination schema: mailtox",
		"custom:destination":       "sent by custom",
		"secure://example.org":     "sent by secure",
		"SECURE://example.org":     "sent by secure",
		"ftp://example.org":        "unsupported destination schema: ftp",
		"nothing":                  "unsupported destination schema: nothing",
		"":                         "unsupported destination schema: ",
	} {
		t.Run(destination, func(t *testing.T) {
			require.EqualError(t, Send(context.Background(), notifiers, destination, ""), want)
		})
	}
	require.EqualError(t, Send(context.Background(), nil, "mailto:a@example.org", ""), "unsupported destination schema: mailto")

	separated := []Notifier{schemeNotifier{name: "plain", schema: "http://"}, schemeNotifier{name: "secure", schema: "https://"}}
	require.EqualError(t, Send(context.Background(), separated, "http://example.org", ""), "sent by plain")
	require.EqualError(t, Send(context.Background(), separated, "https://example.org", ""), "sent by secure")
	require.EqualError(t, Send(context.Background(), []Notifier{schemeNotifier{name: "plain", schema: "http:"}}, "https://example.org", ""),
		"unsupported destination schema: https")
}

func TestSend_EmptySchemaTakesTheRest(t *testing.T) {
	notifiers := []Notifier{
		schemeNotifier{name: "first catch-all", schema: ""},
		schemeNotifier{name: "slack", schema: "slack"},
		schemeNotifier{name: "second catch-all", schema: ""},
	}
	for destination, want := range map[string]string{
		"slack:general":       "sent by slack",
		"telegram:channel":    "sent by first catch-all",
		"no scheme":           "sent by first catch-all",
		"":                    "sent by first catch-all",
		"SLACK:general":       "sent by slack",
		"https://example.org": "sent by first catch-all",
	} {
		t.Run(destination, func(t *testing.T) {
			require.EqualError(t, Send(context.Background(), notifiers, destination, ""), want)
		})
	}
}

// the notifiers parse the destination themselves, so an upper-case scheme has to pass there as well
func TestSend_UpperCaseSchemeReachesNotifier(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer hook.Close()
	slackSrv := newMockSlackServer()
	defer slackSrv.Close()
	notifiers := []Notifier{NewWebhook(WebhookParams{}), slackSrv.newClient()}

	require.NoError(t, Send(context.Background(), notifiers, "HTTP"+strings.TrimPrefix(hook.URL, "http"), "text"))
	require.NoError(t, Send(context.Background(), notifiers, "SLACK:general", "text"))
	assert.Equal(t, "C12345678", slackSrv.lastMessage.Get("channel"))
}
