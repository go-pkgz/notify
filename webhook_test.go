package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type funcWebhookClient func(*http.Request) (*http.Response, error)

func (c funcWebhookClient) Do(r *http.Request) (*http.Response, error) {
	return c(r)
}

type errReader struct {
}

func (errReader) Read(_ []byte) (n int, err error) {
	return 0, errors.New("test error")
}

func assertNoErrorWithStatus(t *testing.T, wh *Webhook, status int) {
	t.Run(fmt.Sprintf("HTTP-Status %d", status), func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(errReader{}),
			}, nil
		})
		err := wh.Send(context.Background(), "http:/example.org/url", "")
		assert.NoError(t, err)
	})
}

func assertErrorWithStatus(t *testing.T, wh *Webhook, status int) {
	t.Run(fmt.Sprintf("HTTP-Status %d", status), func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(errReader{}),
			}, nil
		})
		err := wh.Send(context.Background(), "http:/example.org/url", "")
		assert.Error(t, err)
	})
}

func TestWebhook_Send(t *testing.T) {
	// empty header to check wrong header handling case
	wh := NewWebhook(WebhookParams{Headers: []string{"Content-Type:application/json,text/plain", ""}})
	assert.NotNil(t, wh)

	t.Run("OK with JSON response", func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(r *http.Request) (*http.Response, error) {
			assert.Len(t, r.Header, 1)
			assert.Equal(t, "application/json,text/plain", r.Header.Get("Content-Type"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("")),
			}, nil
		})
		err := wh.Send(context.Background(), "https://example.org/webhook", "some_text")
		assert.NoError(t, err)
	})

	t.Run("No context", func(t *testing.T) {
		err := wh.Send(nil, "https://example.org/webhook", "some_text") //nolint
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to create webhook request")
	})

	t.Run("Failed request", func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("request failed")
		})
		err := wh.Send(context.Background(), "https://not-existing-url.net", "some_text")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "webhook request failed")
	})

	t.Run("Not found with json response", func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString("not found")),
			}, nil
		})
		err := wh.Send(context.Background(), "http:/example.org/invalid-url", "some_text")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-OK status code: 404, body: not found")
	})

	t.Run("Not found with no response", func(t *testing.T) {
		wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(errReader{}),
			}, nil
		})
		err := wh.Send(context.Background(), "http:/example.org/invalid-url", "some_text")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-OK status code: 404")
		assert.NotContains(t, err.Error(), "body")
	})

	assertErrorWithStatus(t, wh, http.StatusOK-1)
	assertNoErrorWithStatus(t, wh, http.StatusOK)
	assertNoErrorWithStatus(t, wh, http.StatusNoContent)
	assertNoErrorWithStatus(t, wh, http.StatusMultipleChoices-1)
	assertErrorWithStatus(t, wh, http.StatusMultipleChoices)
	assertErrorWithStatus(t, wh, http.StatusMultipleChoices+1)
}

func TestWebhook_String(t *testing.T) {
	wh := NewWebhook(WebhookParams{})
	assert.NotNil(t, wh)
	assert.Equal(t, "webhook notification with timeout 5s", wh.String())

	wh = NewWebhook(WebhookParams{Headers: []string{"Content-Type:application/json,text/plain", "Authorization:Bearer secret-token"}})
	assert.NotNil(t, wh)

	str := wh.String()
	assert.Equal(t, "webhook notification with timeout 5s and 2 headers", str)
	assert.NotContains(t, str, "secret-token", "header values might contain secrets and should not be printed")
}

func TestWebhook_SendHeaderWithColons(t *testing.T) {
	wh := NewWebhook(WebhookParams{Headers: []string{
		"Authorization:Bearer user:password",
		"X-Callback: https://example.org/callback",
		"X-No-Value",
	}})
	require.NotNil(t, wh)

	wh.webhookClient = funcWebhookClient(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "Bearer user:password", r.Header.Get("Authorization"))
		assert.Equal(t, "https://example.org/callback", r.Header.Get("X-Callback"))
		assert.Len(t, r.Header, 2, "header without a value is skipped")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(""))}, nil
	})
	require.NoError(t, wh.Send(context.Background(), "https://example.org/webhook", "some_text"))
}

func TestWebhook_SendLimitsErrorBody(t *testing.T) {
	wh := NewWebhook(WebhookParams{})
	require.NotNil(t, wh)

	body := strings.Repeat("a", webhookErrBodyLimit*2)
	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
	})

	err := wh.Send(context.Background(), "https://example.org/webhook", "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-OK status code: 500")
	assert.Contains(t, err.Error(), "(truncated)")
	assert.Less(t, len(err.Error()), webhookErrBodyLimit+200, "error message is capped")
}

func TestWebhook_SendReusesConnection(t *testing.T) {
	var newConns int32
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("b", 1024)))
	}))
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	ts.Start()
	defer ts.Close()

	wh := NewWebhook(WebhookParams{})
	require.NotNil(t, wh)

	for range 3 {
		require.NoError(t, wh.Send(context.Background(), ts.URL, "some_text"))
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&newConns), "response body is drained, so the connection is reused")
}
