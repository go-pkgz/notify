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
	"strconv"
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

	_, err = NewTelegram(TelegramParams{ //nolint:gosec // G101: test fixture token, not a real credential
		Token:     "non-json-resp",
		Timeout:   2 * time.Second,
		apiPrefix: ts.URL + "/",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode json response:")

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
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "123", r.URL.Query().Get("chat_id"))
		var msg telegramMsg
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&msg))
		assert.Equal(t, telegramMsg{Text: expectedText}, msg, "plain text, without parse_mode")
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
	var newConns atomic.Int32
	other := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	other.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
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
	require.Equal(t, int32(1), newConns.Load())

	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(getMeResp))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)
	require.NoError(t, tb.Request(context.Background(), "getMe", nil, &struct{}{}))

	call()
	assert.Equal(t, int32(1), newConns.Load(), "idle connection of the default transport should be reused")
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

	for i := range 50 {
		token := fmt.Sprintf("token-%d", i)
		tb.AddToken(token, "user", "site", time.Now().Add(time.Minute))
		tb.requests.Lock()
		authRequest := tb.requests.data[token]
		authRequest.confirmed = true
		authRequest.telegramID = "telegramID"
		tb.requests.data[token] = authRequest
		tb.requests.Unlock()

		var successes atomic.Int32
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				if _, _, e := tb.CheckToken(token, "user"); e == nil {
					successes.Add(1)
				}
			})
		}
		wg.Wait()
		require.Equal(t, int32(1), successes.Load(), "one-time token should be accepted exactly once")
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

	for i := range 30 {
		token := fmt.Sprintf("token-%d", i)
		tb.AddToken(token, "user", "site", time.Now().Add(time.Minute))

		var upd TelegramUpdate
		raw := fmt.Sprintf(`{"result":[{"update_id":%d,"message":{"chat":{"id":313131313,"type":"private"},"text":"/start %s"}}]}`, i, token)
		require.NoError(t, json.Unmarshal([]byte(raw), &upd))

		var successes atomic.Int32
		var wg, updates sync.WaitGroup
		// the same update delivered twice, as it happens with duplicate update receivers
		for range 2 {
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
						successes.Add(1)
					}
					return
				default:
					if _, _, e := tb.CheckToken(token, "user"); e == nil {
						successes.Add(1)
					}
				}
			}
		})
		wg.Wait()

		require.Equal(t, int32(1), successes.Load(), "confirmed request is used once and never restored after that")
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

const fixtureToken = "1234567:SECRET-TOK_EN-x"

// TestTelegram_APIURLRejectsUnusableValues covers the validation a caller-supplied base needs.
// Every request built from it carries the bot token in its path, so a base that resolves somewhere
// unintended ships the token there, and TrimRight alone is not a guard.
func TestTelegram_APIURLRejectsUnusableValues(t *testing.T) {
	// every value here exists to be refused; the userinfo one is a url whose host is evil.tld,
	// which is exactly what the validator has to catch
	for name, base := range map[string]string{ //nolint:gosec // G101: urls to be refused, not credentials
		"userinfo redirects the host": "https://api.telegram.org@evil.tld",
		"no scheme":                   "api.telegram.org",
		"wrong scheme":                "ftp://api.telegram.org",
		"opaque":                      "https:api.telegram.org",
		"no host":                     "https://",
		"port without a host":         "http://:9000",
		"carries a query":             "https://api.telegram.org?a=b",
		"carries a fragment":          "https://api.telegram.org#x",
		"credentialed and malformed":  "https://user:pa%zzss@api.telegram.org",
		"secret in place of a port":   "https://proxy.example.com:s3cr3t/tg",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewTelegram(TelegramParams{Token: fixtureToken, APIURL: base})
			require.Error(t, err, "%s has to be refused", base)

			// the refusal is logged by whoever built the client, and the value it refused is
			// untrusted configuration: without this every rejection could interpolate the base
			// back and stay green
			assert.NotContains(t, err.Error(), strings.TrimRight(base, "/"),
				"the rejection echoed the base url it refused")
		})
	}
}

// TestTelegram_APIURLIsUsedForRequests pins that a valid base actually redirects the calls, path
// prefix included, and that the "bot" segment is the API's own shape rather than the caller's
// problem.
func TestTelegram_APIURLIsUsedForRequests(t *testing.T) {
	const token = fixtureToken

	var seen string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"username":"somebot"}}`)
	}))
	defer ts.Close()

	tg, err := NewTelegram(TelegramParams{Token: token, APIURL: ts.URL + "/tg/"})
	require.NoError(t, err)
	assert.Equal(t, "somebot", tg.username)
	assert.Equal(t, "/tg/bot"+token+"/getMe", seen,
		"the request did not go through the configured base, or the bot segment was lost")
}

// TestTelegram_APIErrorDoesNotLeakTheToken covers the route the settable base opens. The upstream
// decides an error's text, so something standing in for Telegram can echo the request URI into its
// description, and the shape-matching redaction that came before only ever looked at *url.Error's
// URL field.
func TestTelegram_APIErrorDoesNotLeakTheToken(t *testing.T) {
	const token = fixtureToken

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		// the echo is the point: an upstream putting the request uri into its own error text is
		// the route this test exists for
		_, _ = fmt.Fprintf(w, `{"description":%q}`, "forwarding "+r.URL.RequestURI()) //nolint:gosec // G705: the response is read by this test, never rendered
	}))
	defer ts.Close()

	_, err := NewTelegram(TelegramParams{Token: token, APIURL: ts.URL})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token, "the api error carried the bot token")
}

// TestTelegram_APIErrorWithheldWhenEscapingIsMalformed covers the direction tokenRecoverable has to
// fail in: a bare "%" stops the decoder before it reaches an encoded copy of the token, and
// answering "not recoverable" there would release text whose encodings were never undone.
func TestTelegram_APIErrorWithheldWhenEscapingIsMalformed(t *testing.T) {
	const token = fixtureToken

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		doubled := neturl.QueryEscape(neturl.QueryEscape(r.URL.RequestURI()))
		_, _ = fmt.Fprintf(w, `{"description":%q}`, "forwarding "+doubled+" at 100% load")
	}))
	defer ts.Close()

	_, err := NewTelegram(TelegramParams{Token: token, APIURL: ts.URL})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text withheld", "an undecodable description was released")

	text := err.Error()
	for i := range 5 {
		assert.NotContains(t, text, token, "the token is recoverable after %d decoding passes", i)
		next, decErr := neturl.QueryUnescape(text)
		if decErr != nil || next == text {
			break
		}
		text = next
	}
}

// TestTelegram_DoesNotFollowRedirects covers the other route out. Go copies the previous URL into
// Referer on an https-to-https hop, and every URL here carries the token in its path, so following
// a redirect hands the destination the token.
func TestTelegram_DoesNotFollowRedirects(t *testing.T) {
	const token = fixtureToken

	var refererSeen string
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refererSeen = r.Header.Get("Referer")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"username":"somebot"}}`)
	}))
	defer dest.Close()

	var srcHits atomic.Int32
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srcHits.Add(1)
		http.Redirect(w, r, dest.URL+"/next", http.StatusFound)
	}))
	defer src.Close()

	_, err := NewTelegram(TelegramParams{Token: token, APIURL: src.URL})
	require.Error(t, err, "the redirect was followed")
	assert.Equal(t, int32(1), srcHits.Load(), "a refused redirect is not tried again")
	assert.Empty(t, refererSeen, "the redirect target was reached and saw Referer %q", refererSeen)
	assert.NotContains(t, err.Error(), token, "the refusal itself leaked the token")
}

// TestTelegram_RequestRetries covers which failures a call is tried again after: a reply that will not
// change, 4xx or a body that is not JSON, is final, and a method that is not read-only, sendMessage or
// any other, is not sent again once the request may have reached the API, or the chat gets it twice.
func TestTelegram_RequestRetries(t *testing.T) {
	var attempts atomic.Int32
	var reply atomic.Pointer[string] // what the API does with every call after getMe
	initial := "ok"
	reply.Store(&initial)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = w.Write([]byte(getMeResp))
			return
		}
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		switch v := *reply.Load(); v {
		case "hang up": // the request is taken, the connection closes without an answer
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case "slow": // the request is taken, the answer comes after the client gave up
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case "not json":
			_, _ = w.Write([]byte("not json"))
		case "truncated json": // a complete answer whose JSON is cut short
			_, _ = w.Write([]byte(`{"ok":true,"result":`))
		case "cut body": // the answer starts, the connection closes in the middle of it
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"ok":true,"result":`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		case "ok":
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			code, _ := strconv.Atoi(v)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(errorResp))
		}
	}))
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", Timeout: 5 * time.Second, apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	tbl := []struct {
		reply          string
		updates, sends int32 // attempts for getUpdates, a read-only method, and for any other method
	}{
		{reply: "ok", updates: 1, sends: 1},
		{reply: "400", updates: 1, sends: 1},
		{reply: "401", updates: 1, sends: 1},
		{reply: "403", updates: 1, sends: 1},
		{reply: "404", updates: 1, sends: 1},
		{reply: "not json", updates: 1, sends: 1},
		{reply: "truncated json", updates: 1, sends: 1},
		{reply: "cut body", updates: 3, sends: 1},
		{reply: "429", updates: 3, sends: 3},
		{reply: "500", updates: 3, sends: 1},
		{reply: "502", updates: 3, sends: 1},
		{reply: "hang up", updates: 3, sends: 1},
		{reply: "slow", updates: 3, sends: 1},
	}
	for _, tt := range tbl {
		t.Run(tt.reply, func(t *testing.T) {
			reply.Store(&tt.reply)
			tb.Timeout = 5 * time.Second
			if tt.reply == "slow" {
				tb.Timeout = 200 * time.Millisecond // the server never answers in time
			}

			attempts.Store(0)
			_, err := tb.getUpdates(context.Background())
			assert.Equal(t, tt.reply == "ok", err == nil, "getUpdates error: %v", err)
			if tt.reply == "hang up" {
				// net/http itself sends a GET again when a reused connection closes without an answer
				assert.GreaterOrEqual(t, attempts.Load(), tt.updates, "getUpdates attempts")
			} else {
				assert.Equal(t, tt.updates, attempts.Load(), "getUpdates attempts")
			}

			attempts.Store(0)
			err = tb.Send(context.Background(), "telegram:1", "text")
			assert.Equal(t, tt.reply == "ok", err == nil, "Send error: %v", err)
			assert.Equal(t, tt.sends, attempts.Load(), "sendMessage attempts of Send")

			// the reply to a subscriber goes through sendMessage as well
			attempts.Store(0)
			err = tb.sendText(context.Background(), 1, "text")
			assert.Equal(t, tt.reply == "ok", err == nil, "sendText error: %v", err)
			assert.Equal(t, tt.sends, attempts.Load(), "sendMessage attempts of sendText")

			// a method other than sendMessage that changes something, as the API's own name says
			attempts.Store(0)
			err = tb.Request(context.Background(), "forwardMessage?chat_id=1", []byte(`{}`), &struct{}{})
			assert.Equal(t, tt.reply == "ok", err == nil, "forwardMessage error: %v", err)
			assert.Equal(t, tt.sends, attempts.Load(), "forwardMessage attempts")

			// a read-only method in another case, telegram takes method names in any case
			attempts.Store(0)
			_ = tb.Request(context.Background(), "GETUPDATES", nil, &struct{}{})
			if tt.reply != "hang up" {
				assert.Equal(t, tt.updates, attempts.Load(), "GETUPDATES attempts")
			}
		})
	}
}

// an answer that can't be decoded into what the caller passed is not a reason to ask again
func TestTelegram_RequestDoesNotRetryDecodeIntoBadTarget(t *testing.T) {
	var attempts atomic.Int32
	ts := mockTelegramServer(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	require.ErrorContains(t, tb.Request(context.Background(), "getUpdates", nil, nil), "failed to decode json response")
	assert.Equal(t, int32(1), attempts.Load())
}

// a message that could not be delivered because nothing listens is sent again
func TestTelegram_RequestRetriesSendWhenNotConnected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	tb := &Telegram{TelegramParams: TelegramParams{Token: "good-token", Timeout: time.Second, apiPrefix: "http://" + addr + "/"}}
	st := time.Now()
	err = tb.Send(context.Background(), "telegram:1", "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect: connection refused")
	assert.GreaterOrEqual(t, time.Since(st), 2*250*time.Millisecond, "connection refused is tried three times")
}

func TestTelegram_SendParseMode(t *testing.T) {
	var body []byte
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	for destination, want := range map[string]string{
		"telegram:1":                      `{"text":"a_b*c","parse_mode":"Markdown"}`,
		"telegram:1?parseMode=":           `{"text":"a_b*c","parse_mode":"Markdown"}`,
		"telegram:1?parseMode=HTML":       `{"text":"a_b*c","parse_mode":"HTML"}`,
		"telegram:1?parseMode=MarkdownV2": `{"text":"a_b*c","parse_mode":"MarkdownV2"}`,
		"telegram:1?parseMode=none":       `{"text":"a_b*c"}`,
		"telegram:1?parseMode=None":       `{"text":"a_b*c"}`,
		"telegram:channel?parseMode=none": `{"text":"a_b*c"}`,
		"telegram:-100123?parseMode=NONE": `{"text":"a_b*c"}`,
	} {
		t.Run(destination, func(t *testing.T) {
			body = nil
			require.NoError(t, tb.Send(context.Background(), destination, "a_b*c"))
			assert.JSONEq(t, want, string(body))
		})
	}
}

func TestTelegram_SupportedHTMLKeepsLines(t *testing.T) {
	tbl := []struct{ in, out string }{
		{"<p>one</p><p>two</p>line<br>break", "one\ntwo\nline\nbreak"},
		{"line<br/>break", "line\nbreak"},
		{"a<br><br>b", "a\n\nb"},
		{"<ul><li>item</li></ul>", "item"},
		{"<ul><li>a</li><li>b</li></ul>after", "a\nb\nafter"},
		{"<p>one</p><p><b>two</b></p>", "one\n<b>two</b>"},
		{"<blockquote><p>one</p><p>two</p></blockquote>after", "<blockquote>one\ntwo</blockquote>\nafter"},
		{"<h1>Title</h1><p>text</p>", "<b>Title</b>text"},
		{"<p>end</p>", "end"},
		{"<p>one<!-- note --></p>two", "one\ntwo"},
		{"one</br>two", "one\ntwo"},
		// a newline already in the source is not doubled
		{"<p>one</p>\n<p>two</p>\n", "one\ntwo"},
		{"<p>one</p>\n\n<p>two</p>\n", "one\n\ntwo"},
		{"<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n", "\na\nb"},
		{"line<br>\nbreak", "line\nbreak"},
		{"line<br>\r\nbreak", "line\nbreak"},
	}
	for _, tt := range tbl {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.out, TelegramSupportedHTML(tt.in))
		})
	}
}

// net/http sends a GET again by itself when a reused connection closes without an answer, so a message
// sent with GET could arrive twice
func TestTelegram_RequestMethod(t *testing.T) {
	type seen struct{ method, contentType, body string }
	got := make(chan seen, 1)
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		// getMe of NewTelegram is answered by mockTelegramServer and does not get here
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		got <- seen{method: r.Method, contentType: r.Header.Get("Content-Type"), body: string(body)}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	tbl := []struct {
		name, method string
		body         []byte
		want         seen
	}{
		{name: "get method without a body", method: "getUpdates?offset=1", want: seen{method: http.MethodGet}},
		{name: "get method in another case", method: "GETME?x=1", want: seen{method: http.MethodGet}},
		{name: "other method without a body", method: "sendMessage?chat_id=1&text=a", want: seen{method: http.MethodPost}},
		{name: "method named like get", method: "ge?x=1", want: seen{method: http.MethodPost}},
		{name: "get method with a body", method: "getChat", body: []byte(`{"chat_id":1}`),
			want: seen{method: http.MethodPost, contentType: "application/json; charset=utf-8", body: `{"chat_id":1}`}},
		{name: "other method with a body", method: "sendMessage", body: []byte(`{"text":"a"}`),
			want: seen{method: http.MethodPost, contentType: "application/json; charset=utf-8", body: `{"text":"a"}`}},
	}
	for _, tt := range tbl {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tb.Request(context.Background(), tt.method, tt.body, &struct{}{}))
			assert.Equal(t, tt.want, <-got)
		})
	}
}

func TestTelegram_SendMessageNotRepeatedOnLostAnswer(t *testing.T) {
	var sends atomic.Int32
	ts := mockTelegramServer(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(strings.ToLower(r.URL.Path), "sendmessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			return
		}
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		// the message is taken, the connection closes without an answer
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	})
	defer ts.Close()
	tb, err := NewTelegram(TelegramParams{Token: "good-token", apiPrefix: ts.URL + "/"})
	require.NoError(t, err)

	for name, send := range map[string]func() error{
		"Send":     func() error { return tb.Send(context.Background(), "telegram:1", "text") },
		"sendText": func() error { return tb.sendText(context.Background(), 1, "text") },
		// telegram takes method names in any case
		"Request in lower case": func() error {
			return tb.Request(context.Background(), "sendmessage?chat_id=1", []byte(`{"text":"text"}`), &struct{}{})
		},
		// without a body the parameters travel in the query, as sendText sent them before
		"Request without a body": func() error {
			return tb.Request(context.Background(), "sendMessage?chat_id=1&text=text", nil, &struct{}{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			// a successful call leaves an idle connection, which the message then goes over
			_, err := tb.getUpdates(context.Background())
			require.NoError(t, err)
			sends.Store(0)
			require.Error(t, send())
			assert.Equal(t, int32(1), sends.Load(), "sendMessage requests received")
		})
	}
}
