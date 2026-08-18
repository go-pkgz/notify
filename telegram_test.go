package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegram_New(t *testing.T) {
	ts := mockTelegramServer(nil)
	defer ts.Close()

	tb, err := NewTelegram(TelegramParams{
		Token:     "good-token",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)
	assert.NotNil(t, tb)
	assert.Equal(t, time.Second*5, tb.Timeout)
	assert.Equal(t, "telegram", tb.Schema())
	assert.Equal(t, "telegram notifications destination", tb.String())

	_, err = NewTelegram(TelegramParams{
		Token:     "empty-json",
		apiPrefix: ts.URL + "/",
	})
	require.EqualError(t, err, "can't retrieve bot info from Telegram API: received empty result")

	st := time.Now()
	_, err = NewTelegram(TelegramParams{ //nolint:gosec // G101: test fixture token, not a real credential
		Token:     "non-json-resp",
		Timeout:   2 * time.Second,
		apiPrefix: ts.URL + "/",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode json response:")
	assert.GreaterOrEqual(t, time.Since(st), 250*2*time.Millisecond)

	_, err = NewTelegram(TelegramParams{
		Token:     "404",
		Timeout:   2 * time.Second,
		apiPrefix: ts.URL + "/",
	})
	require.EqualError(t, err, "can't retrieve bot info from Telegram API: unexpected telegram API status code 404")

	_, err = NewTelegram(TelegramParams{
		Token:     "no-such-thing",
		apiPrefix: "http://127.0.0.1:4321/",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't retrieve bot info from Telegram API")
	assert.Contains(t, err.Error(), "dial tcp 127.0.0.1:4321: connect: connection refused")

	_, err = NewTelegram(TelegramParams{
		Token:     "",
		apiPrefix: "",
	})
	require.Error(t, err, "empty api url not allowed")

	_, err = NewTelegram(TelegramParams{
		Token:     "good-token",
		Timeout:   2 * time.Second,
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err, "0 timeout allowed as default")
}

func TestTelegram_Send(t *testing.T) {
	ts := mockTelegramServer(nil)
	defer ts.Close()

	tb, err := NewTelegram(TelegramParams{
		Token:     "good-token",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)
	assert.NotNil(t, tb)

	err = tb.Send(context.Background(), "telegram:test_user_channel?parseMode=HTML", "test message")
	require.NoError(t, err)

	tb = &Telegram{
		TelegramParams: TelegramParams{ //nolint:gosec // G101: test fixture token, not a real credential
			Token:     "non-json-resp",
			apiPrefix: ts.URL + "/",
		}}
	err = tb.Send(context.Background(), "telegram:test_user_channel", "test message")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected telegram API status code 404", "send on broken tg")

	// bad API URL
	tb.apiPrefix = "http://non-existent"
	err = tb.Send(context.Background(), "telegram:test_user_channel", "test message")
	require.Error(t, err)
}

func TestTelegram_Formatting(t *testing.T) {
	text := `<h1 id="sample-markdown">Sample Markdown</h1>
<p>This is some basic, sample markdown.</p>
<h2 id="second-heading">Second Heading</h2>
<ul>
<li>Unordered lists, and:<ol>
<li>One</li>
<li>Two</li>
<li>Three</li>
</ol>
</li>
<li>More</li>
</ul>
<blockquote>
<p>Blockquote</p>
</blockquote>
<p>And <strong>bold</strong>, <em>italics</em>, and even <em>italics and later <strong>bold</strong></em>. Even <del>strikethrough</del>. <a href="https://markdowntohtml.com">A link</a> to somewhere.</p>
<p>And code highlighting:</p>
<pre language="c++"><code class="lang-js"><span class="hljs-keyword">var</span> foo = <span class="hljs-string">'bar'</span>;

<span class="hljs-function"><span class="hljs-keyword">function</span> <span class="hljs-title">baz</span><span class="hljs-params">(s)</span> </span>{
   <span class="hljs-keyword">return</span> foo + <span class="hljs-string">':'</span> + s;
}
</code></pre>
<h4 id="fourth-heading">Fourth Heading</h4>
<p>Or inline code like <code>var foo = 'bar';</code>.</p>
<p>Or an image of bears</p>
<p><img src="https://placebear.com/200/200" alt="bears"></p>
<p>The end ...</p>
`
	cleanText := `<b>Sample Markdown</b>
This is some basic, sample markdown.
<b>Second Heading</b>

Unordered lists, and:
One
Two
Three


More

<blockquote>
Blockquote
</blockquote>
And <strong>bold</strong>, <em>italics</em>, and even <em>italics and later <strong>bold</strong></em>. Even <del>strikethrough</del>. <a href="https://markdowntohtml.com">A link</a> to somewhere.
And code highlighting:
<pre language="c++"><code class="lang-js">var foo = &#39;bar&#39;;

function baz(s) {
   return foo + &#39;:&#39; + s;
}
</code></pre>
<i><b>Fourth Heading</b></i>
Or inline code like <code>var foo = &#39;bar&#39;;</code>.
Or an image of bears

The end ...`

	assert.Equal(t, cleanText, TelegramSupportedHTML(text))

	// taken from https://core.telegram.org/bots/api#html-style
	// `<tg-spoiler>spoiler</tg-spoiler>` for some reason doesn't work as expected, tag is stripped by bluemonday
	// also, there is no way to allow <span class="tg-spoiler"> but not empty spans
	telegramExampleTest := `<b>bold</b>, <strong>bold</strong>
<i>italic</i>, <em>italic</em>
<u>underline</u>, <ins>underline</ins>
<s>strikethrough</s>, <strike>strikethrough</strike>, <del>strikethrough</del>
<b>bold <i>italic bold <s>italic bold strikethrough italic bold strikethrough</s> <u>underline italic bold</u></i> bold</b>
<a href="http://www.example.com/">inline URL</a>
<a href="tg://user?id=123456789">inline mention of a user</a>
<tg-emoji emoji-id="5368324170671202286">👍</tg-emoji>
<code>inline fixed-width code</code>
<pre>pre-formatted fixed-width code block</pre>
<pre><code class="language-python">pre-formatted fixed-width code block written in the Python programming language</code></pre>
<blockquote>Block quotation started\nBlock quotation continued\nThe last line of the block quotation</blockquote>`
	assert.Equal(t, telegramExampleTest, TelegramSupportedHTML(telegramExampleTest))

	username := "test<user>"
	cleanUsername := "test&lt;user&gt;"
	assert.Equal(t, cleanUsername, EscapeTelegramText(username))
}

func TestTelegramSendClientError(t *testing.T) {
	ts := mockTelegramServer(nil)
	defer ts.Close()

	tg, err := NewTelegram(TelegramParams{
		Token:     "good-token",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)
	assert.NotNil(t, tg)

	// no destination set
	require.EqualError(t, tg.Send(context.Background(), "", ""),
		"problem parsing destination: unsupported scheme , should be telegram")

	// wrong scheme
	require.EqualError(t, tg.Send(context.Background(), "https://example.org", ""),
		"problem parsing destination: unsupported scheme https, should be telegram")

	// bad destination set
	require.EqualError(t, tg.Send(context.Background(), "%", ""),
		`problem parsing destination: parse "%": invalid URL escape "%"`)

	// canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.EqualError(t, tg.Send(ctx, "telegram:general?title=test", ""), "context canceled")
}

func TestTelegram_GetBotUsername(t *testing.T) {
	ts := mockTelegramServer(nil)
	defer ts.Close()

	tb, err := NewTelegram(TelegramParams{
		Token:     "good-token",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)
	assert.NotNil(t, tb)
	assert.Equal(t, "remark42_test_bot", tb.GetBotUsername())
}

const getUpdatesResp = `{
  "ok": true,
  "result": [
     {
        "update_id": 998,
        "message": {
           "chat": {
              "type": "group"
           }
        }
     },
     {
        "update_id": 999,
        "message": {
					 "text": "not starting with /start",
           "chat": {
              "type": "private"
           }
        }
     },
     {
        "update_id": 1000,
        "message": {
           "message_id": 4,
           "from": {
              "id": 313131313,
              "is_bot": false,
              "first_name": "Joe",
              "username": "joe123",
              "language_code": "en"
           },
           "chat": {
              "id": 313131313,
              "first_name": "Joe",
              "username": "joe123",
              "type": "private"
           },
           "date": 1601665548,
           "text": "/start token",
           "entities": [
              {
                 "offset": 0,
                 "length": 6,
                 "type": "bot_command"
              }
           ]
        }
     }
  ]
}`

func TestTelegram_GetUpdatesFlow(t *testing.T) {
	first := true
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), "sendMessage") {
			// respond normally to processUpdates attempt to send message back to user
			_, _ = w.Write([]byte("{}"))
			return
		}
		// responses to get updates calls to API
		if first {
			assert.Empty(t, r.URL.Query().Get("offset"))
			first = false
		} else {
			assert.Equal(t, "1001", r.URL.Query().Get("offset"))
		}
		_, _ = w.Write([]byte(getUpdatesResp))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{
		Token:     "xxxsupersecretxxx",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)

	// send request with no offset
	upd, err := tb.getUpdates(context.Background())
	require.NoError(t, err)

	assert.Len(t, upd.Result, 3)
	assert.Equal(t, 1001, tb.updateOffset)
	assert.Equal(t, "/start token", upd.Result[len(upd.Result)-1].Message.Text)

	tb.AddToken("token", "user", "site", time.Now().Add(time.Minute))
	_, _, err = tb.CheckToken("token", "user")
	require.Error(t, err)
	tb.processUpdates(context.Background(), upd)
	tgID, site, err := tb.CheckToken("token", "user")
	require.NoError(t, err)
	assert.Equal(t, "313131313", tgID)
	assert.Equal(t, "site", site)

	// send request with offset
	_, err = tb.getUpdates(context.Background())
	require.NoError(t, err)
}

func TestTelegram_ProcessUpdateFlow(t *testing.T) {
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		// respond normally to processUpdates attempt to send message back to user
		_, _ = w.Write([]byte("{}"))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{
		Token:     "xxxsupersecretxxx",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)

	tb.AddToken("token", "user", "site", time.Now().Add(time.Minute))
	tb.AddToken("expired token", "user", "site", time.Now().Add(-time.Minute))
	assert.Len(t, tb.requests.data, 2)
	_, _, err = tb.CheckToken("token", "user")
	require.Error(t, err)
	require.NoError(t, tb.ProcessUpdate(context.Background(), getUpdatesResp))
	assert.Len(t, tb.requests.data, 1, "expired token was cleaned up")
	tgID, site, err := tb.CheckToken("token", "user")
	require.NoError(t, err)
	assert.Empty(t, tb.requests.data, "token is deleted after successful check")
	assert.Equal(t, "313131313", tgID)
	assert.Equal(t, "site", site)

	tb.AddToken("expired token", "user", "site", time.Now().Add(-time.Minute))
	assert.Len(t, tb.requests.data, 1)
	require.EqualError(t, tb.ProcessUpdate(context.Background(), ""), "failed to decode provided telegram update: unexpected end of JSON input")
	assert.Empty(t, tb.requests.data, "expired token should be cleaned up despite the error")
}

const sendMessageResp = `{
  "ok": true,
  "result": {
     "message_id": 100,
     "from": {
        "id": 666666666,
        "is_bot": true,
        "first_name": "Test auth bot",
        "username": "TestAuthBot"
     },
     "chat": {
        "id": 313131313,
        "first_name": "Joe",
        "username": "joe123",
        "type": "private"
     },
     "date": 1602430546,
     "text": "123"
  }
}`

func TestTelegram_SendText(t *testing.T) {
	var expectedText string
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "123", r.URL.Query().Get("chat_id"))
		assert.Equal(t, expectedText, r.URL.Query().Get("text"))
		_, _ = w.Write([]byte(sendMessageResp))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{
		Token:     "xxxsupersecretxxx",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)

	messages := []string{"hello there", "C++ & R&D = fun", "100% sure?", "a+b=c"}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			expectedText = msg
			require.NoError(t, tb.sendText(context.Background(), 123, msg))
		})
	}
}

func TestTelegram_RequestDoesNotLeakToken(t *testing.T) {
	const token = "xxxsupersecretxxx"
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(getMeResp))
	})
	tb, err := NewTelegram(TelegramParams{Token: token, apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	t.Run("connection error", func(t *testing.T) {
		ts.Close() // no server on that address anymore
		err = tb.Request(context.Background(), "getUpdates", nil, &struct{}{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), token)
		assert.Contains(t, err.Error(), "<redacted>")
		assert.Contains(t, err.Error(), "connection refused", "cause of the error is preserved")
	})

	t.Run("timeout error", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(time.Second)
			_, _ = w.Write([]byte(getMeResp))
		}))
		defer slow.Close()
		tbSlow := &Telegram{TelegramParams: TelegramParams{Token: token, Timeout: time.Millisecond, apiPrefix: slow.URL + "/"}}
		err = tbSlow.Request(context.Background(), "getUpdates", nil, &struct{}{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), token)

		var urlErr *neturl.Error
		require.ErrorAs(t, err, &urlErr, "error type is preserved")
		assert.Equal(t, slow.URL+"/<redacted>/getUpdates", urlErr.URL, "only the token is replaced")
		assert.True(t, urlErr.Timeout(), "timeout detection still works")
	})
}

func TestTelegram_RequestKeepsDefaultTransportPool(t *testing.T) {
	// unrelated user of http.DefaultTransport, which telegram requests should not affect
	var newConns int32
	other := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	other.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	other.Start()
	defer other.Close()

	call := func() {
		resp, err := http.DefaultClient.Get(other.URL) //nolint:noctx // simple test call
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	call()
	require.Equal(t, int32(1), atomic.LoadInt32(&newConns))

	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(getMeResp))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)
	require.NoError(t, tb.Request(context.Background(), "getMe", nil, &struct{}{}))

	call()
	assert.Equal(t, int32(1), atomic.LoadInt32(&newConns), "idle connection of the default transport should be reused")
}

const errorResp = `{"ok":false,"error_code":400,"description":"Very bad request"}`

func TestTelegram_Error(t *testing.T) {
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(errorResp))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{
		Token:     "xxxsupersecretxxx",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)

	_, err = tb.getUpdates(context.Background())
	require.EqualError(t, err, "failed to fetch updates: unexpected telegram API status code 400, error: \"Very bad request\"")
}

func TestTelegram_TokenVerification(t *testing.T) {
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), "sendMessage") {
			// respond normally to processUpdates attempt to send message back to user
			_, _ = w.Write([]byte("{}"))
			return
		}
		// responses to get updates calls to API
		_, _ = w.Write([]byte(getUpdatesResp))
	})
	defer ts.Close()

	tb, err := NewTelegram(TelegramParams{
		Token:     "good-token",
		apiPrefix: ts.URL + "/",
	})
	require.NoError(t, err)
	assert.NotNil(t, tb)
	tb.AddToken("token", "user", "site", time.Now().Add(time.Minute))
	assert.Len(t, tb.requests.data, 1)

	// wrong token
	tgID, site, err := tb.CheckToken("unknown token", "user")
	assert.Empty(t, tgID)
	assert.Empty(t, site)
	require.EqualError(t, err, "request is not found")

	// right token and user, not verified yet
	tgID, site, err = tb.CheckToken("token", "user")
	assert.Empty(t, tgID)
	assert.Empty(t, site)
	require.EqualError(t, err, "request is not verified yet")

	// confirm request
	authRequest, ok := tb.requests.data["token"]
	assert.True(t, ok)
	authRequest.confirmed = true
	authRequest.telegramID = "telegramID"
	tb.requests.data["token"] = authRequest

	// wrong user
	tgID, site, err = tb.CheckToken("token", "wrong user")
	assert.Empty(t, tgID)
	assert.Empty(t, site)
	require.EqualError(t, err, "user does not match original requester")

	// successful check
	tgID, site, err = tb.CheckToken("token", "user")
	require.NoError(t, err)
	assert.Equal(t, "telegramID", tgID)
	assert.Equal(t, "site", site)

	// expired token
	tb.AddToken("expired token", "user", "site", time.Now().Add(-time.Minute))
	tgID, site, err = tb.CheckToken("expired token", "user")
	assert.Empty(t, tgID)
	assert.Empty(t, site)
	require.EqualError(t, err, "request expired")
	assert.Empty(t, tb.requests.data)

	// expired token, cleaned up by the cleanup
	tb.apiPollInterval = time.Millisecond * 15
	tb.expiredCleanupInterval = time.Millisecond * 10
	ctx, cancel := context.WithCancel(context.Background())
	go tb.Run(ctx)
	assert.Eventually(t, func() bool {
		return tb.ProcessUpdate(ctx, "").Error() == "the Run goroutine should not be used with ProcessUpdate"
	}, time.Millisecond*100, time.Millisecond*10, "ProcessUpdate should not work same time as Run")
	tb.AddToken("expired token", "user", "site", time.Now().Add(-time.Minute))
	tb.requests.Lock()
	assert.Len(t, tb.requests.data, 1)
	tb.requests.Unlock()
	time.Sleep(tb.expiredCleanupInterval * 2)
	tb.requests.Lock()
	assert.Empty(t, tb.requests.data)
	tb.requests.Unlock()
	cancel()
	// give enough time for Run() to finish
	time.Sleep(tb.expiredCleanupInterval)
}

func TestTelegram_CheckTokenIsSingleUse(t *testing.T) {
	ts := mockTelegramServer(nil)
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	for i := 0; i < 50; i++ {
		token := fmt.Sprintf("token-%d", i)
		tb.AddToken(token, "user", "site", time.Now().Add(time.Minute))
		tb.requests.Lock()
		authRequest := tb.requests.data[token]
		authRequest.confirmed = true
		authRequest.telegramID = "telegramID"
		tb.requests.data[token] = authRequest
		tb.requests.Unlock()

		var successes int32
		var wg sync.WaitGroup
		for j := 0; j < 10; j++ {
			wg.Go(func() {
				if _, _, e := tb.CheckToken(token, "user"); e == nil {
					atomic.AddInt32(&successes, 1)
				}
			})
		}
		wg.Wait()
		require.Equal(t, int32(1), atomic.LoadInt32(&successes), "one-time token should be accepted exactly once")
	}
	assert.Empty(t, tb.requests.data)
}

func TestTelegram_ConcurrentConfirmAndCheck(t *testing.T) {
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	for i := 0; i < 30; i++ {
		token := fmt.Sprintf("token-%d", i)
		tb.AddToken(token, "user", "site", time.Now().Add(time.Minute))

		var upd TelegramUpdate
		raw := fmt.Sprintf(`{"result":[{"update_id":%d,"message":{"chat":{"id":313131313,"type":"private"},"text":"/start %s"}}]}`, i, token)
		require.NoError(t, json.Unmarshal([]byte(raw), &upd))

		var successes int32
		var wg, updates sync.WaitGroup
		// the same update delivered twice, as it happens with duplicate update receivers
		for j := 0; j < 2; j++ {
			updates.Go(func() {
				tb.processUpdates(context.Background(), &upd)
			})
		}
		updatesDone := make(chan struct{})
		wg.Go(func() {
			updates.Wait()
			close(updatesDone)
		})
		wg.Go(func() {
			for {
				select {
				case <-updatesDone:
					// last check with both updates processed, so the request is confirmed unless already used
					if _, _, e := tb.CheckToken(token, "user"); e == nil {
						atomic.AddInt32(&successes, 1)
					}
					return
				default:
					if _, _, e := tb.CheckToken(token, "user"); e == nil {
						atomic.AddInt32(&successes, 1)
					}
				}
			}
		})
		wg.Wait()

		require.Equal(t, int32(1), atomic.LoadInt32(&successes), "confirmed request is used once and never restored after that")
	}
}

func TestTelegram_RunSingleInstance(t *testing.T) {
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":[]}`))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)
	tb.apiPollInterval = time.Millisecond * 10
	tb.expiredCleanupInterval = time.Millisecond * 10

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan struct{})
	go func() {
		tb.Run(ctx)
		close(first)
	}()
	require.Eventually(t, func() bool {
		err = tb.ProcessUpdate(ctx, `{"result":[]}`)
		return err != nil && strings.Contains(err.Error(), "should not be used with ProcessUpdate")
	}, time.Second, time.Millisecond*10, "Run should be marked as running")

	second := make(chan struct{})
	go func() {
		tb.Run(ctx)
		close(second)
	}()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("second Run call should return instead of starting another updates processor")
	}

	cancel()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("Run should return on context cancellation")
	}

	// with Run stopped, ProcessUpdate is allowed again
	require.NoError(t, tb.ProcessUpdate(context.Background(), `{"result":[]}`))
}

const getMeResp = `{"ok": true,
				"result": {
					"first_name": "comments_test",
					"id": 707381019,
					"is_bot": true,
					"username": "remark42_test_bot"
				}}`

func mockTelegramServer(h http.HandlerFunc) *httptest.Server {
	if h != nil {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.String(), "getMe") {
				_, _ = w.Write([]byte(getMeResp))
				return
			}
			h(w, r)
		}))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /good-token/getMe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(getMeResp))
	})
	mux.HandleFunc("GET /empty-json/getMe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /non-json-resp/getMe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-a-json`))
	})
	mux.HandleFunc("GET /404/getMe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	})

	mux.HandleFunc("POST /good-token/sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true}`))
	})

	return httptest.NewServer(mux)
}
