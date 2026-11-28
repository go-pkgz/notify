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
	"net/url"
	"strconv"
	"strings"
	"sync"
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

func TestWebhook_SendRedirects(t *testing.T) {
	type landing struct{ method, body, header string }
	var mu sync.Mutex
	var landed []landing
	var hops atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		if r.URL.Path == "/landed" {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			mu.Lock()
			landed = append(landed, landing{method: r.Method, body: string(body), header: r.Header.Get("Authorization")})
			mu.Unlock()
			return
		}
		// /hop?code=<code>&n=<n>&to=<target> redirects with code to target, after n hops to itself
		q := r.URL.Query()
		code, _ := strconv.Atoi(q.Get("code"))
		if n, _ := strconv.Atoi(q.Get("n")); n > 0 {
			q.Set("n", strconv.Itoa(n-1))
			http.Redirect(w, r, "/hop?"+q.Encode(), code) //nolint:gosec // G710: a local test server redirecting where the test asks
			return
		}
		http.Redirect(w, r, q.Get("to"), code) //nolint:gosec // G710: a local test server redirecting where the test asks
	}
	plain := httptest.NewServer(http.HandlerFunc(handler))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer secure.Close()
	// the same server under another host name: localhost and 127.0.0.1 are different hosts to the client
	otherHost := strings.Replace(plain.URL, "127.0.0.1", "localhost", 1)

	wh := NewWebhook(WebhookParams{Headers: []string{"Authorization: Bearer secret"}})
	// trust the TLS test server, keep the client as NewWebhook made it otherwise
	wh.webhookClient.(*http.Client).Transport = secure.Client().Transport

	redirect := func(base string, code, extraHops int, target string) string {
		return base + "/hop?" + url.Values{"code": {strconv.Itoa(code)}, "n": {strconv.Itoa(extraHops)}, "to": {target}}.Encode()
	}
	tbl := []struct {
		name, destination string
		followed          bool
		code              int
		requests          int32 // requests the servers receive, checked when set
	}{
		{name: "307 on the same host", destination: redirect(plain.URL, 307, 0, "/landed"), followed: true},
		{name: "308 on the same host", destination: redirect(plain.URL, 308, 0, "/landed"), followed: true},
		{name: "308 from http to https on the same host", destination: redirect(plain.URL, 308, 0, secure.URL+"/landed"), followed: true},
		{name: "ten 307s", destination: redirect(plain.URL, 307, 9, "/landed"), followed: true, requests: 11},
		{name: "eleven 307s", destination: redirect(plain.URL, 307, 10, "/landed"), code: 307, requests: 11},
		{name: "301", destination: redirect(plain.URL, 301, 0, "/landed"), code: 301},
		{name: "302", destination: redirect(plain.URL, 302, 0, "/landed"), code: 302},
		{name: "303", destination: redirect(plain.URL, 303, 0, "/landed"), code: 303},
		{name: "307 to another host", destination: redirect(plain.URL, 307, 0, otherHost+"/landed"), code: 307},
		{name: "308 to another host", destination: redirect(plain.URL, 308, 0, otherHost+"/landed"), code: 308},
		{name: "307 to the host name in other letter case",
			destination: redirect(otherHost, 307, 0, strings.Replace(otherHost, "localhost", "LOCALHOST", 1)+"/landed"), code: 307},
		{name: "307 from https to http", destination: redirect(secure.URL, 307, 0, plain.URL+"/landed"), code: 307},
		{name: "308 to another host after a same-host one",
			destination: redirect(plain.URL, 308, 0, redirect("", 308, 0, otherHost+"/landed")), code: 308},
	}
	for _, tt := range tbl {
		t.Run(tt.name, func(t *testing.T) {
			hops.Store(0)
			mu.Lock()
			landed = nil
			mu.Unlock()
			err := wh.Send(context.Background(), tt.destination, "payload")
			if tt.requests != 0 {
				assert.Equal(t, tt.requests, hops.Load(), "requests received")
			}
			if tt.followed {
				require.NoError(t, err)
				mu.Lock()
				defer mu.Unlock()
				assert.Equal(t, []landing{{method: http.MethodPost, body: "payload", header: "Bearer secret"}}, landed)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "webhook request failed with non-OK status code: "+strconv.Itoa(tt.code))
			mu.Lock()
			defer mu.Unlock()
			assert.Empty(t, landed, "the redirect target is not reached")
		})
	}
}
