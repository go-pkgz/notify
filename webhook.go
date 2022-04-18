package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/go-pkgz/lgr"
	"github.com/pkg/errors"
)

const webhookTimeOut = 5000 * time.Millisecond

// WebhookParams contain settings for webhook notifications
type WebhookParams struct {
	WebhookURL string
	Timeout    time.Duration
	Headers    []string
}

// Webhook implements notify.Destination for Webhook notifications
type Webhook struct {
	WebhookParams
	webhookClient webhookClient
}

// webhookClient defines an interface of client for webhook
type webhookClient interface {
	Do(*http.Request) (*http.Response, error)
}

// NewWebhook makes Webhook
func NewWebhook(params WebhookParams) (*Webhook, error) {
	res := &Webhook{WebhookParams: params}
	if res.WebhookURL == "" {
		return nil, errors.New("webhook URL is required for webhook notifications")
	}

	if res.Timeout == 0 {
		res.Timeout = webhookTimeOut
	}

	res.webhookClient = &http.Client{Timeout: 5 * time.Second}

	log.Printf("[DEBUG] create new webhook notifier for %s", res.WebhookURL)

	return res, nil
}

// Send sends Webhook notification
func (t *Webhook) Send(ctx context.Context, text string) error {
	payload := bytes.NewBufferString(text)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.WebhookURL, payload)
	if err != nil {
		return errors.Wrap(err, "unable to create webhook request")
	}

	for _, h := range t.Headers {
		elems := strings.Split(h, ":")
		if len(elems) != 2 {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(elems[0]), strings.TrimSpace(elems[1]))
	}

	resp, err := t.webhookClient.Do(httpReq)
	if err != nil {
		return errors.Wrap(err, "webhook request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("webhook request failed with non-OK status code: %d", resp.StatusCode)
		respBody, e := io.ReadAll(resp.Body)
		if e != nil {
			return errors.New(errMsg)
		}
		return fmt.Errorf("%s, body: %s", errMsg, respBody)
	}

	return nil
}

// String describes the webhook instance
func (t *Webhook) String() string {
	return fmt.Sprintf("webhook notification to %s", t.WebhookURL)
}
