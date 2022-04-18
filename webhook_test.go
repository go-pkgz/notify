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

func TestWebhook_NewWebhook(t *testing.T) {

	wh, err := NewWebhook(WebhookParams{
		WebhookURL: "https://example.org/webhook",
		Headers:    []string{"Authorization:Basic AXVubzpwQDU1dzByYM=="},
	})
	assert.NoError(t, err)
	assert.NotNil(t, wh)

	assert.Equal(t, "https://example.org/webhook", wh.WebhookURL)
	assert.Equal(t, []string{"Authorization:Basic AXVubzpwQDU1dzByYM=="}, wh.Headers)

	wh, err = NewWebhook(WebhookParams{})
	assert.Nil(t, wh)
	assert.Error(t, err)
	assert.Equal(t, "webhook URL is required for webhook notifications", err.Error())
}

func TestWebhook_Send(t *testing.T) {
	wh, err := NewWebhook(WebhookParams{
		WebhookURL: "https://example.org/webhook",
		Headers:    []string{"Content-Type:application/json,text/plain", ""},
	})
	wh.webhookClient = funcWebhookClient(func(r *http.Request) (*http.Response, error) {
		assert.Len(t, r.Header, 1)
		assert.Equal(t, r.Header.Get("Content-Type"), "application/json,text/plain")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("")),
		}, nil
	})
	assert.NoError(t, err)
	assert.NotNil(t, wh)

	err = wh.Send(context.TODO(), "some_text")
	assert.NoError(t, err)

	wh, err = NewWebhook(WebhookParams{WebhookURL: "https://example.org/webhook"})
	wh.webhookClient = okWebhookClient
	assert.NoError(t, err)
	err = wh.Send(nil, "some_text") //nolint
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to create webhook request")

	wh, err = NewWebhook(WebhookParams{WebhookURL: "https://not-existing-url.net"})
	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})
	assert.NoError(t, err)
	err = wh.Send(context.TODO(), "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook request failed")

	wh, err = NewWebhook(WebhookParams{
		WebhookURL: "http:/example.org/invalid-url",
	})
	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString("not found")),
		}, nil
	})
	assert.NoError(t, err)
	err = wh.Send(context.TODO(), "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-OK status code: 404, body: not found")

	wh, err = NewWebhook(WebhookParams{
		WebhookURL: "http:/example.org/invalid-url",
	})
	wh.webhookClient = funcWebhookClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(errReader{}),
		}, nil
	})
	assert.NoError(t, err)
	err = wh.Send(context.TODO(), "some_text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-OK status code: 404")
	assert.NotContains(t, err.Error(), "body")
}

func TestWebhook_String(t *testing.T) {
	wh, err := NewWebhook(WebhookParams{WebhookURL: "https://example.org/webhook"})
	assert.NoError(t, err)
	assert.NotNil(t, wh)

	str := wh.String()
	assert.Equal(t, "webhook notification to https://example.org/webhook", str)
}
