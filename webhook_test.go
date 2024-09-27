package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
			assert.Equal(t, r.Header.Get("Content-Type"), "application/json,text/plain")

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
	wh := NewWebhook(WebhookParams{Headers: []string{"Content-Type:application/json,text/plain"}})
	assert.NotNil(t, wh)

	str := wh.String()
	assert.Equal(t, "webhook notification with timeout 5s and headers [Content-Type:application/json,text/plain]", str)
}
