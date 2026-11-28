package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const webhookTimeOut = 5000 * time.Millisecond

// limits for the response body of the webhook request: how much of it is quoted in the error
// for non-OK responses, and how much is drained to make the connection reusable
const (
	webhookErrBodyLimit   = 16 * 1024
	webhookDrainBodyLimit = 64 * 1024
)

// WebhookParams contain settings for webhook notifications
type WebhookParams struct {
	Timeout time.Duration
	Headers []string // headers in format "header:value"
}

// Webhook notifications client
type Webhook struct {
	WebhookParams
	webhookClient webhookClient
}

// webhookClient defines an interface of client for webhook
type webhookClient interface {
	Do(*http.Request) (*http.Response, error)
}

// NewWebhook makes Webhook
func NewWebhook(params WebhookParams) *Webhook {
	res := &Webhook{WebhookParams: params}

	if res.Timeout == 0 {
		res.Timeout = webhookTimeOut
	}

	res.webhookClient = &http.Client{Timeout: res.Timeout, CheckRedirect: webhookCheckRedirect}

	return res
}

// webhookCheckRedirect applies the redirect rules of Send. The host name is compared exactly:
// some Go releases drop Authorization and Cookie on a redirect that changes only its letter case,
// and a Unicode case fold joins names that DNS keeps apart.
func webhookCheckRedirect(req *http.Request, via []*http.Request) error {
	switch {
	case req.Response.StatusCode != http.StatusTemporaryRedirect && req.Response.StatusCode != http.StatusPermanentRedirect:
	case req.URL.Hostname() != via[0].URL.Hostname():
	case via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https":
	case len(via) > 10: // via holds the first request and every redirect followed before this one
	default:
		return nil
	}
	return http.ErrUseLastResponse
}

// Send sends Webhook notification. Destination field is expected to have http:// or https:// schema.
// The text is sent as the body of a POST request. A response status outside 2xx is returned as an error.
// A 307 or 308 to the same host name, spelled the same, is followed up to 10 times with the same body and
// Headers, on any port and from http to https, never from https to http; any other redirect is returned
// as an error.
//
// Example:
//
// - https://example.com/webhook
func (wh *Webhook) Send(ctx context.Context, destination, text string) error {
	payload := bytes.NewBufferString(text)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", destination, payload)
	if err != nil {
		return fmt.Errorf("unable to create webhook request: %w", err)
	}

	for _, h := range wh.Headers {
		elems := strings.SplitN(h, ":", 2)
		if len(elems) != 2 {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(elems[0]), strings.TrimSpace(elems[1]))
	}

	resp, err := wh.webhookClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() {
		// drain the remaining body to let the underlying connection be reused
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookDrainBodyLimit))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errMsg := fmt.Sprintf("webhook request failed with non-OK status code: %d", resp.StatusCode)
		respBody, e := io.ReadAll(io.LimitReader(resp.Body, webhookErrBodyLimit+1))
		if e != nil {
			return errors.New(errMsg)
		}
		if len(respBody) > webhookErrBodyLimit {
			return fmt.Errorf("%s, body: %s... (truncated)", errMsg, respBody[:webhookErrBodyLimit])
		}
		return fmt.Errorf("%s, body: %s", errMsg, respBody)
	}

	return nil
}

// Schema returns the scheme supported by this client, without the colon
func (wh *Webhook) Schema() string {
	return "http"
}

// String describes the webhook instance
func (wh *Webhook) String() string {
	str := fmt.Sprintf("webhook notification with timeout %s", wh.Timeout)
	if len(wh.Headers) != 0 {
		// header values are not printed as they might contain secrets, like authorization tokens
		str += fmt.Sprintf(" and %d headers", len(wh.Headers))
	}
	return str
}
