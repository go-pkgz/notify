package notify

import (
	"bytes"
	"context"
	"errors"
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

var okWebhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("ok")),
	}, nil
})

type errReader struct {
}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("test error")
}

func TestWebhook_Send(t *testing.T) {
	// empty header to check wrong header handling case
	wh := NewWebhook(WebhookParams{Headers: []string{"Content-Type:application/json,text/plain", ""}})
	wh.webhookClient = funcWebhookClient(func(r *http.Request) (*http.Response, error) {
		assert.Len(t, r.Header, 1)
		assert.Equal(t, r.Header.Get("Content-Type"), "application/json,text/plain")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("")),
		}, nil
	})
	assert.NotNil(t, wh)

	err := wh.Send(context.Background(), "https://example.org/webhook", "some_text")
	assert.NoError(t, err)

	wh.webhookClient = okWebhookClient
	assert.NoError(t, err)
	err = wh.Send(nil, "https://example.org/webhook", "some_text") //nolint
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to create webhook request")

	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})
	err = wh.Send(context.Background(), "https://not-existing-url.net", "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook request failed")

	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString("not found")),
		}, nil
	})
	err = wh.Send(context.Background(), "http:/example.org/invalid-url", "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-OK status code: 404, body: not found")

	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(errReader{}),
		}, nil
	})
	err = wh.Send(context.Background(), "http:/example.org/invalid-url", "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-OK status code: 404")
	assert.NotContains(t, err.Error(), "body")
}

func TestWebhook_String(t *testing.T) {
	wh := NewWebhook(WebhookParams{Headers: []string{"Content-Type:application/json,text/plain"}})
	assert.NotNil(t, wh)

	str := wh.String()
	assert.Equal(t, "webhook notification with timeout 5s and headers [Content-Type:application/json,text/plain]", str)
}
