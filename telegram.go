package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/go-pkgz/lgr"
	"github.com/go-pkgz/repeater/v2"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// TelegramParams contain settings for telegram notifications
type TelegramParams struct {
	Token                string        // token for telegram bot API interactions
	Timeout              time.Duration // http client timeout
	ErrorMsg, SuccessMsg string        // messages for unsuccessful and successful subscription requests to bot

	// APIURL points the client at something other than the public bot API, for a proxy standing in
	// front of Telegram or a test double answering as it. Empty means the public API. Every request
	// carries the bot token in its path, so this is validated rather than trusted: see
	// validateTelegramBaseURL for what is refused and why.
	APIURL string

	apiPrefix string // derived from APIURL, or the public API
}

// Telegram notifications client
type Telegram struct {
	TelegramParams

	// identifier of the first update to be requested.
	// should be equal to LastSeenUpdateID + 1
	// See https://core.telegram.org/bots/api#getupdates
	updateOffset           int
	apiPollInterval        time.Duration // interval to check updates from Telegram API and answer to users
	expiredCleanupInterval time.Duration // interval to check and clean up expired notification requests
	username               string        // bot username
	updates                struct {
		sync.RWMutex
		running bool // set while the Run goroutine is active, ProcessUpdate is not allowed then
	}
	requests struct {
		sync.Mutex
		data map[string]tgAuthRequest
	}
}

// telegramMsg is used to send message through Telegram bot API
type telegramMsg struct {
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type tgAuthRequest struct {
	confirmed  bool // whether login request has been confirmed and user info set
	expires    time.Time
	telegramID string
	user       string
	site       string
}

// TelegramBotInfo structure contains information about telegram bot, which is used from whole telegram API response
type TelegramBotInfo struct {
	Username string `json:"username"`
}

const telegramTimeOut = 5000 * time.Millisecond
const telegramAPIPrefix = "https://api.telegram.org/bot"
const tgPollInterval = time.Second * 5
const tgCleanupInterval = time.Minute * 5

// NewTelegram makes telegram bot for notifications
func NewTelegram(params TelegramParams) (*Telegram, error) {
	res := Telegram{TelegramParams: params}

	switch {
	case res.APIURL != "":
		base, err := validateTelegramBaseURL(res.APIURL)
		if err != nil {
			return nil, err
		}
		// the "bot" literal belongs to the API's own shape rather than to the operator's base, so
		// it is appended here: a caller passes https://proxy.example.com and the URLs come out as
		// https://proxy.example.com/bot<token>/<method>, which is what Telegram itself serves
		res.apiPrefix = base + "/bot"
	case res.apiPrefix == "":
		res.apiPrefix = telegramAPIPrefix
	default:
		// a prefix set directly, which the package's own tests do
	}
	if res.Timeout == 0 {
		res.Timeout = telegramTimeOut
	}

	if res.SuccessMsg == "" {
		res.SuccessMsg = "✅ You have successfully authenticated, check the web!"
	}

	res.apiPollInterval = tgPollInterval
	res.expiredCleanupInterval = tgCleanupInterval
	log.Printf("[DEBUG] create new telegram notifier for api=%s, timeout=%s", res.apiPrefix, res.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	botInfo, err := res.botInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't retrieve bot info from Telegram API: %w", err)
	}
	res.username = botInfo.Username

	res.requests.data = make(map[string]tgAuthRequest)

	return &res, nil
}

// Send sends provided message to Telegram chat, with `parseMode` parsed from destination field (Markdown by default,
// "none" for plain text) with "telegram:" schema same way "mailto:" schema is constructed.
//
// Example:
//
// - telegram:channel
// - telegram:chatID // chatID is a number, like `-1001480738202`
// - telegram:channel?parseMode=HTML
// - telegram:channel?parseMode=none
func (t *Telegram) Send(ctx context.Context, destination, text string) error {
	chatID, parseMode, err := t.parseDestination(destination)
	if err != nil {
		return fmt.Errorf("problem parsing destination: %w", err)
	}

	body := telegramMsg{Text: text, ParseMode: parseMode}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("sendMessage?chat_id=%s&disable_web_page_preview=true", chatID)
	return t.Request(ctx, url, b, &struct{}{})
}

// TelegramSupportedHTML returns HTML with only tags allowed in Telegram HTML message payload, also trims ending newlines.
// The end of a paragraph or a list item and a line break become a newline, h1-h3 become <b> and h4-h6 become <i><b>.
//
// https://core.telegram.org/bots/api#html-style, https://core.telegram.org/api/entities#allowed-entities
func TelegramSupportedHTML(htmlText string) string {
	adjustedHTMLText := adjustHTMLTags(htmlText)
	p := bluemonday.NewPolicy()
	p.AllowElements("b", "strong", "i", "em", "u", "ins", "s", "strike", "del", "a", "code", "pre", "tg-spoiler", "tg-emoji", "blockquote")
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("class").OnElements("code")
	p.AllowAttrs("title").OnElements("tg-spoiler")
	p.AllowAttrs("emoji-id").OnElements("tg-emoji")
	p.AllowAttrs("language").OnElements("pre")
	return strings.TrimRight(p.Sanitize(adjustedHTMLText), "\n")
}

// EscapeTelegramText returns text sanitized of symbols not allowed inside other HTML tags in Telegram HTML message payload
//
// https://core.telegram.org/bots/api#html-style
func EscapeTelegramText(text string) string {
	// order is important
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// telegramHeadings maps the heading tags telegram does not allow to the opening and closing tags replacing them
var telegramHeadings = map[string][2]string{
	"h1": {"<b>", "</b>"}, "h2": {"<b>", "</b>"}, "h3": {"<b>", "</b>"},
	"h4": {"<i><b>", "</b></i>"}, "h5": {"<i><b>", "</b></i>"}, "h6": {"<i><b>", "</b></i>"},
}

// telegram not allow h1-h6 tags
// replace these tags with a combination of <b> and <i> for visual distinction.
// telegram has no paragraphs, line breaks or lists either, and their tags are stripped, so the end of
// a paragraph or a list item and a line break are turned into a newline, unless the source already
// has one there.
func adjustHTMLTags(htmlText string) string {
	buff := strings.Builder{}
	tokenizer := html.NewTokenizer(strings.NewReader(htmlText))
	newlinePending := false // a block ended, the next text or opening tag starts on a new line
	for {
		if tokenizer.Next() == html.ErrorToken {
			return buff.String()
		}
		token := tokenizer.Token()
		switch token.Type {
		case html.TextToken:
			// the tokenizer has already turned "\r\n" into "\n"
			if newlinePending && !strings.HasPrefix(token.Data, "\n") {
				_, _ = buff.WriteString("\n")
			}
			newlinePending = false
			_, _ = buff.WriteString(token.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			if newlinePending {
				_, _ = buff.WriteString("\n")
			}
			newlinePending = token.Data == "br"
			_, _ = buff.WriteString(telegramTag(token))
		case html.EndTagToken:
			newlinePending = newlinePending || token.Data == "p" || token.Data == "li" || token.Data == "br"
			_, _ = buff.WriteString(telegramTag(token))
		default:
			_, _ = buff.WriteString(token.String())
		}
	}
}

// telegramTag returns a tag as it goes to the sanitizer: a heading replaced, a line break dropped, as
// adjustHTMLTags turns it into a newline, and anything else unchanged
func telegramTag(token html.Token) string {
	if token.Data == "br" {
		return ""
	}
	repl, ok := telegramHeadings[token.Data]
	switch {
	case ok && token.Type == html.StartTagToken:
		return repl[0]
	case ok && token.Type == html.EndTagToken:
		return repl[1]
	default:
		return token.String()
	}
}

// TelegramUpdate contains update information, which is used from whole telegram API response
type TelegramUpdate struct {
	Result []struct {
		UpdateID int `json:"update_id"`
		Message  struct {
			Chat struct {
				ID   int    `json:"id"`
				Name string `json:"first_name"`
				Type string `json:"type"`
			} `json:"chat"`
			Text string `json:"text"`
		} `json:"message"`
	} `json:"result"`
}

// GetBotUsername returns bot username
func (t *Telegram) GetBotUsername() string {
	return t.username
}

// AddToken adds token
func (t *Telegram) AddToken(token, user, site string, expires time.Time) {
	t.requests.Lock()
	t.requests.data[token] = tgAuthRequest{
		expires: expires,
		user:    user,
		site:    site,
	}
	t.requests.Unlock()
}

// CheckToken returns the Telegram chat ID and the site of a confirmed token, and consumes it;
// otherwise it returns an error
func (t *Telegram) CheckToken(token, user string) (telegram, site string, err error) {
	// lookup and consumption are a single transaction, so the one-time token can't be used twice
	t.requests.Lock()
	defer t.requests.Unlock()

	authRequest, ok := t.requests.data[token]
	if !ok {
		return "", "", errors.New("request is not found")
	}

	if time.Now().After(authRequest.expires) {
		delete(t.requests.data, token)
		return "", "", errors.New("request expired")
	}

	if !authRequest.confirmed {
		return "", "", errors.New("request is not verified yet")
	}

	if authRequest.user != user {
		return "", "", errors.New("user does not match original requester")
	}

	delete(t.requests.data, token)

	return authRequest.telegramID, authRequest.site, nil
}

// Run starts processing login requests sent in Telegram, required for users to subscribe through the bot;
// not needed for Send. Blocks caller
func (t *Telegram) Run(ctx context.Context) {
	t.updates.Lock()
	if t.updates.running {
		t.updates.Unlock()
		log.Print("[WARN] telegram updates processing is already running, ignoring the call")
		return
	}
	t.updates.running = true
	t.updates.Unlock()

	defer func() {
		t.updates.Lock()
		t.updates.running = false
		t.updates.Unlock()
	}()

	processUpdatedTicker := time.NewTicker(t.apiPollInterval)
	cleanupTicker := time.NewTicker(t.expiredCleanupInterval)

	for {
		select {
		case <-ctx.Done():
			processUpdatedTicker.Stop()
			cleanupTicker.Stop()
			return
		case <-processUpdatedTicker.C:
			updates, err := t.getUpdates(ctx)
			if err != nil {
				log.Printf("[WARN] Error while getting telegram updates: %v", err)
				continue
			}
			t.processUpdates(ctx, updates)
		case <-cleanupTicker.C:
			now := time.Now()
			t.requests.Lock()
			for key, req := range t.requests.data {
				if now.After(req.expires) {
					delete(t.requests.data, key)
				}
			}
			t.requests.Unlock()
		}
	}
}

// ProcessUpdate is alternative to Run, it processes provided plain text update from Telegram
// so that caller could get updates and send it not only there but to multiple sources
func (t *Telegram) ProcessUpdate(ctx context.Context, textUpdate string) error {
	// read lock is held for the whole call, so Run can't start in the middle of it,
	// while parallel ProcessUpdate calls are still allowed
	t.updates.RLock()
	defer t.updates.RUnlock()

	if t.updates.running {
		return errors.New("the Run goroutine should not be used with ProcessUpdate")
	}
	defer func() {
		// as Run goroutine is not running, clean up old requests on each update
		// even if we hit json decode error
		now := time.Now()
		t.requests.Lock()
		for key, req := range t.requests.data {
			if now.After(req.expires) {
				delete(t.requests.data, key)
			}
		}
		t.requests.Unlock()
	}()
	var updates TelegramUpdate
	if err := json.Unmarshal([]byte(textUpdate), &updates); err != nil {
		return fmt.Errorf("failed to decode provided telegram update: %w", err)
	}
	t.processUpdates(ctx, &updates)
	return nil
}

// Schema returns schema prefix supported by this client
func (t *Telegram) Schema() string {
	return "telegram"
}

func (t *Telegram) String() string {
	return "telegram notifications destination"
}

// parses "telegram:" in a manner "mailto:" URL is parsed url and returns chatID and parseMode.
// if chatID is channel name and not a numerical ID, `@` will be	added to it
func (t *Telegram) parseDestination(destination string) (chatID, parseMode string, err error) {
	// parse URL
	u, err := neturl.Parse(destination)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "telegram" {
		return "", "", fmt.Errorf("unsupported scheme %s, should be telegram", u.Scheme)
	}

	chatID = u.Opaque
	if _, err := strconv.ParseInt(chatID, 10, 64); err != nil {
		chatID = "@" + chatID // if chatID not a number enforce @ prefix
	}

	switch mode := u.Query().Get("parseMode"); {
	case strings.EqualFold(mode, "none"):
		parseMode = "" // plain text, parse_mode is left out of the request
	case mode != "":
		parseMode = mode
	default:
		parseMode = "Markdown"
	}

	return chatID, parseMode, nil
}

// getUpdates fetches incoming updates
func (t *Telegram) getUpdates(ctx context.Context) (*TelegramUpdate, error) {
	url := `getUpdates?allowed_updates=["message"]`
	if t.updateOffset != 0 {
		url += fmt.Sprintf("&offset=%d", t.updateOffset)
	}

	var result TelegramUpdate

	err := t.Request(ctx, url, nil, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updates: %w", err)
	}

	for _, u := range result.Result {
		if u.UpdateID >= t.updateOffset {
			t.updateOffset = u.UpdateID + 1
		}
	}

	return &result, nil
}

// processUpdates processes a batch of updates from telegram servers
func (t *Telegram) processUpdates(ctx context.Context, updates *TelegramUpdate) {
	for _, update := range updates.Result {
		if update.Message.Chat.Type != "private" {
			continue
		}

		if !strings.HasPrefix(update.Message.Text, "/start ") {
			continue
		}

		token := strings.TrimPrefix(update.Message.Text, "/start ")

		// confirmation is a single transaction, otherwise a request consumed by CheckToken
		// in the middle of it would be restored from the stale copy
		t.requests.Lock()
		authRequest, ok := t.requests.data[token]
		if ok {
			authRequest.confirmed = true
			authRequest.telegramID = strconv.Itoa(update.Message.Chat.ID)
			t.requests.data[token] = authRequest
		}
		t.requests.Unlock()

		if !ok { // no such token
			if t.ErrorMsg != "" {
				if err := t.sendText(ctx, update.Message.Chat.ID, t.ErrorMsg); err != nil {
					log.Printf("[WARN] failed to notify telegram peer: %v", err)
				}
			}
			continue
		}

		if err := t.sendText(ctx, update.Message.Chat.ID, t.SuccessMsg); err != nil {
			log.Printf("[ERROR] failed to notify telegram peer: %v", err)
		}
	}
}

// sendText sends a plain text message to telegram peer
// It is a POST, as in Send: net/http itself sends a GET again when a reused connection closes without
// an answer, and a repeated sendMessage delivers the message twice.
func (t *Telegram) sendText(ctx context.Context, recipientID int, msg string) error {
	b, _ := json.Marshal(telegramMsg{Text: msg}) // a struct of strings always marshals
	return t.Request(ctx, fmt.Sprintf("sendMessage?chat_id=%d", recipientID), b, &struct{}{})
}

// botInfo returns info about configured bot
func (t *Telegram) botInfo(ctx context.Context) (*TelegramBotInfo, error) {
	var resp = struct {
		Result *TelegramBotInfo `json:"result"`
	}{}

	err := t.Request(ctx, "getMe", nil, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Result == nil {
		return nil, errors.New("received empty result")
	}

	return resp.Result, nil
}

// validateTelegramBaseURL checks a caller-supplied API base and returns it without its trailing
// slash. Every request built from it carries the bot token in its path, so a base that resolves
// somewhere unintended ships the token there: "https://api.telegram.org@evil.tld" is a valid URL
// whose host is evil.tld, and a bare scheme or an opaque form silently produces a request to
// somewhere else again. A path prefix is allowed, for a proxy mounted under one.
//
// No rejection echoes the value. It is configuration that can carry credentials in its userinfo, a
// secret in its query or a password where the port belongs, and the error travels to whatever logs
// the constructor failure. The parse error is dropped for the same reason: *url.Error prints the
// URL it was given, and the inner error quotes the input back as well.
func validateTelegramBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	u, err := neturl.Parse(baseURL)
	if err != nil {
		return "", errors.New("telegram api url is not a valid url")
	}

	switch {
	case u.Opaque != "":
		return "", errors.New("telegram api url must not be opaque")
	case u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("telegram api url must be http or https, got %q", u.Scheme)
	case u.Hostname() == "":
		// u.Host is non-empty for "http://:9000", which resolves to the local machine
		return "", errors.New("telegram api url must have a host")
	case u.User != nil:
		return "", errors.New("telegram api url must not carry userinfo")
	case u.RawQuery != "" || u.ForceQuery:
		return "", errors.New("telegram api url must not carry a query")
	case u.Fragment != "" || strings.HasSuffix(baseURL, "#"):
		return "", errors.New("telegram api url must not carry a fragment")
	}

	// rebuilt from what was checked, so the string validated is the string used
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// errTelegramRedirect is the refusal of a redirect from the Telegram API, see Request
var errTelegramRedirect = errors.New("refusing to follow a telegram api redirect: the bot token travels in the URL")

// Request makes a request to the Telegram API and return the result.
//
// Timeout applies to each attempt. A call is tried up to three times, 250ms apart, when the API answers
// with 429 or the TCP connection to it can't be opened, as when it is refused or the name does not
// resolve. A method that only reads, one whose name starts with "get" such as getMe and getUpdates, is
// also tried again after any other transport failure, including a timeout and one while the answer is
// read, and after a 5xx answer. Anything else is returned at once: once a request such as sendMessage
// may have reached Telegram, as after a timeout or a 5xx, another attempt could deliver the message
// twice. A failed connection through a proxy, or one cut short by Timeout, is not told apart from such
// a case.
func (t *Telegram) Request(ctx context.Context, method string, b []byte, data any) error {
	methodName, _, _ := strings.Cut(method, "?")
	// telegram method names are case-insensitive, and the read-only ones are named get...
	readOnly := len(methodName) >= 3 && strings.EqualFold(methodName[:3], "get")

	retry := false // set by every attempt, read by the classifier right after it
	rpt := repeater.NewFixed(3, time.Millisecond*250)
	rpt.SetErrorClassifier(func(error) bool { return retry })
	return rpt.Do(ctx, func() error {
		retry = false
		url := fmt.Sprintf("%s%s/%s", t.apiPrefix, t.Token, method)

		var req *http.Request
		var err error
		if b == nil {
			req, err = http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
		} else {
			req, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
		}
		if err != nil {
			return fmt.Errorf("failed to create request: %w", t.redactToken(err))
		}
		if b != nil {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}

		// refusing redirects: Go copies the previous URL into Referer on any hop that is not
		// https-to-http, and every URL here carries the bot token in its path, so following one
		// hands the destination the token, usually into its access log. Telegram does not redirect;
		// something standing in for it can
		client := http.Client{
			Timeout: t.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errTelegramRedirect
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			// a refused redirect would be refused again, and a method that is not read-only is sent again
			// only when the connection could not be made, so the request never left
			retry = !errors.Is(err, errTelegramRedirect) && (readOnly || connectFailed(err))
			return fmt.Errorf("failed to send request: %w", t.redactToken(err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// 429 means the request was not processed; a 5xx may come after a message was taken
			retry = resp.StatusCode == http.StatusTooManyRequests ||
				(readOnly && resp.StatusCode >= http.StatusInternalServerError)
			return t.redactToken(t.parseError(resp.Body, resp.StatusCode))
		}

		// read in full first, so a connection failing in the middle of the answer is told apart from an
		// answer that is not valid JSON, which another attempt would not change
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			retry = readOnly
			return fmt.Errorf("failed to decode json response: %w", err)
		}
		if err = json.NewDecoder(bytes.NewReader(body)).Decode(data); err != nil {
			return fmt.Errorf("failed to decode json response: %w", err)
		}

		return nil
	})
}

// connectFailed reports whether a request failed because the direct connection to the server could not
// be made, including the name lookup, so nothing of the request was sent. A failed dial to a proxy and a
// dial stopped by the client timeout come back as other errors and are not recognized.
func connectFailed(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// redactToken removes the bot token from an error before it reaches a caller's log.
//
// Two routes, and the second only exists once APIURL can point somewhere the operator chose. The
// token is part of every API URL, so a transport failure carries it in *url.Error's URL field. And
// the upstream decides the text of an API error: something standing in for Telegram can echo the
// request URI into its description, in whatever encoding it likes, so scrubbing the token itself is
// what holds rather than matching a URL shape.
func (t *Telegram) redactToken(err error) error {
	if err == nil || t.Token == "" {
		return err
	}

	var urlErr *neturl.Error
	if errors.As(err, &urlErr) && strings.Contains(urlErr.URL, t.Token) {
		return &neturl.Error{
			Op: urlErr.Op, URL: strings.ReplaceAll(urlErr.URL, t.Token, "<redacted>"), Err: urlErr.Err,
		}
	}

	// the text scrub is for something that could actually be a bot token. Telegram issues them as
	// "<bot id>:<secret>", and blanking a short arbitrary string out of a diagnostic corrupts more
	// than it protects: a token of "404" would turn every "status code 404" into "status code
	// <redacted>". The URL-field redaction above stays unconditional, since there the token is
	// whatever the caller configured and the field is nothing else
	if !looksLikeBotToken(t.Token) {
		return err
	}

	msg := err.Error()
	for _, form := range []string{t.Token, neturl.QueryEscape(t.Token), neturl.PathEscape(t.Token)} {
		msg = strings.ReplaceAll(msg, form, "<redacted>")
	}

	// substitution only catches encodings we thought of, and the upstream picks the encoding, so the
	// result is checked once more against a decoded copy. If the token is still recoverable by any
	// of those routes the text goes rather than the token stays
	if tokenRecoverable(msg, t.Token) {
		return errors.New("unexpected telegram API error, text withheld: the bot token could not be ruled out of it")
	}

	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

// looksLikeBotToken reports whether token has the shape Telegram issues, "<bot id>:<secret>". Used
// to keep the text scrub off values that cannot be one, where it would damage diagnostics for
// nothing
func looksLikeBotToken(token string) bool {
	id, secret, found := strings.Cut(token, ":")
	if !found || len(secret) < 8 {
		return false
	}
	if _, err := strconv.Atoi(id); err != nil {
		return false
	}
	return true
}

// tokenRecoverable reports whether token can still be read out of msg after undoing the encodings
// an upstream might have applied to it.
//
// Fails closed: after a decode error, or after the cap runs out while the text is still changing,
// absence was never established, and withholding is the only answer that cannot leak. One stray "%"
// in whatever the upstream echoed is enough to reach that.
func tokenRecoverable(msg, token string) bool {
	lowered := strings.ToLower(token)
	seen := msg
	for i := 0; i < 5; i++ {
		if strings.Contains(strings.ToLower(seen), lowered) {
			return true
		}
		next, err := neturl.QueryUnescape(seen)
		if err != nil {
			return true
		}
		if next == seen {
			// decoding has converged and every form has been examined, so the token is absent
			// rather than undecided
			return false
		}
		seen = next
	}
	return true
}

func (t *Telegram) parseError(r io.Reader, statusCode int) error {
	tgErr := struct {
		Description string `json:"description"`
	}{}
	if err := json.NewDecoder(r).Decode(&tgErr); err != nil {
		return fmt.Errorf("unexpected telegram API status code %d", statusCode)
	}
	return fmt.Errorf("unexpected telegram API status code %d, error: %q", statusCode, tgErr.Description)
}
