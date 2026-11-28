# Notify

[![Build Status](https://github.com/go-pkgz/notify/workflows/build/badge.svg)](https://github.com/go-pkgz/notify/actions) [![Coverage Status](https://coveralls.io/repos/github/go-pkgz/notify/badge.svg?branch=master)](https://coveralls.io/github/go-pkgz/notify?branch=master) [![Go Reference](https://pkg.go.dev/badge/github.com/go-pkgz/notify.svg)](https://pkg.go.dev/github.com/go-pkgz/notify)

This library provides ability to send notifications using multiple services:

- Email
- Telegram
- Slack
- Webhook

## Install

`go get -u github.com/go-pkgz/notify`

## Usage

All supported notification methods implement the `Notifier` interface:

```go
type Notifier interface {
	fmt.Stringer
	Schema() string                                           // the scheme supported by this client, without the colon
	Send(ctx context.Context, destination, text string) error // sends message to provided destination
}
```

`notify.Send` passes the destination to the first notifier whose scheme matches the scheme of the destination, compared case-insensitively, and returns its error; the other notifiers are not tried. A notifier with the `http` scheme also takes `https` destinations. With no match, `notify.Send` returns an `unsupported destination schema: <scheme>` error. Example on how to use it:

```go
package main

import (
	"context"
	"fmt"

	"github.com/go-pkgz/notify"
)

func main() {
	// create notifiers
	notifiers := []notify.Notifier{
		notify.NewWebhook(notify.WebhookParams{}),
		notify.NewEmail(notify.SMTPParams{}),
		notify.NewSlack("token"),
	}
	tg, err := notify.NewTelegram(notify.TelegramParams{Token: "token"})
	if err == nil {
		notifiers = append(notifiers, tg)
	}
	err = notify.Send(context.Background(), notifiers, "https://example.com/webhook", "Hello, world!")
	if err != nil {
		fmt.Printf("Sent message error: %s", err)
	}
}
```

A runnable version of the same flow, sending to a local test server instead of a real webhook, is in [`example_test.go`](example_test.go).

### Email

`mailto:` [scheme](https://datatracker.ietf.org/doc/html/rfc6068) is supported. The `subject`, `from` and `unsubscribeLink` query parameters are used; `unsubscribeLink` adds one-click `List-Unsubscribe` headers. Other parameters, such as `cc` and `body`, are ignored. The `from` parameter is required: without it, `Send` returns an error and sends nothing.

**Note:** Query parameter values must be URL-encoded. In particular, email addresses containing `+` (e.g. `noreply+tag@example.com`) must use `%2B`, otherwise `+` is interpreted as a space. Use `url.QueryEscape` for all parameter values. Percent-encoded recipients before `?` are decoded, as RFC 6068 has it, so `a%2Btag@example.org` and `a+tag@example.org` reach the same address. A recipient that does not decode to one plain address, such as one with an encoded comma or an encoded quoted local part, is an error, and one that is not valid percent-encoding, such as `user%host@example.org`, is used as written.

Examples:

- `mailto:"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>`
- `mailto:addr1@example.org,addr2@example.org?&subject=test-subj&from=notify@example.org`

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/go-pkgz/notify"
)

func main() {
	wh := notify.NewEmail(notify.SMTPParams{
		Host:               "localhost",        // the only required field, others are optional
		Port:               25,                 // 25 by default
		TLS:                false,              // TLS, but not STARTTLS
		StartTLS:           false,              // STARTTLS on a plain connection
		InsecureSkipVerify: false,              // skip verification of the server certificate
		HELOHost:           "mail.example.org", // hostname sent in the SMTP greeting, "localhost" by default
		ContentType:        "text/html",
		Charset:            "UTF-8",
		LoginAuth:          false,      // LOGIN authentication instead of PLAIN, needed for Office 365 and outlook.com
		Username:           "username", // with Username set, use TLS or StartTLS unless the server is localhost
		Password:           "password",
		TimeOut:            time.Second * 10, // TCP connect timeout, 30 seconds by default
	})
	// TimeOut covers the connection only, a context with a deadline bounds the whole send
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := wh.Send(
		ctx,
		`mailto:"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>`,
		"Hello, World!",
	)
	if err != nil {
		log.Fatalf("problem sending message using email, %v", err)
	}
}
```

With `Username` set, PLAIN and LOGIN authentication both refuse to send the credentials over a connection without TLS or STARTTLS, unless the server is `localhost`, `127.0.0.1` or `::1`.

### Telegram

`telegram:` scheme akin to `mailto:` is supported. Query params `parseMode` ([doc](https://core.telegram.org/bots/api#formatting-options), legacy `Markdown` by default, preferable use `MarkdownV2` or `HTML` instead, or `none` to send plain text). Examples:

- `telegram:channel` // the channel name without `@`
- `telegram:channelID` // channel ID is a number, like `-1001480738202`: use [that instruction](https://remark42.com/docs/configuration/telegram/#notifications-for-administrators) to obtain it
- `telegram:userID`

[Here](https://remark42.com/docs/configuration/telegram/#getting-bot-token-for-telegram) is an instruction on obtaining token for your notification bot.

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/go-pkgz/notify"
)

func main() {
	tg, err := notify.NewTelegram(notify.TelegramParams{
		Token:      "token",                    // required
		Timeout:    time.Second * 10,           // per attempt, default is 5 seconds
		SuccessMsg: "Success",                  // optional, for auth, set by default
		ErrorMsg:   "Error",                    // optional, for auth, unset by default
		APIURL:     "https://api.telegram.org", // optional, the public API by default
	})
	if err != nil {
		log.Fatalf("problem creating telegram notifier, %v", err)
	}
	err = tg.Send(context.Background(), "telegram:-1001480738202", "Hello, World!")
	if err != nil {
		log.Fatalf("problem sending message using telegram, %v", err)
	}
}
```

`NewTelegram` calls the `getMe` method of the API to learn the bot's name, so it fails without network access or with a bad token, within 30 seconds at most. `APIURL` points the client at a proxy or a test double instead of the public API; it must be an `http` or `https` URL with a host and without userinfo, a query or a fragment. Redirects from the API are refused, as every request URL carries the bot token.

Each call to the Telegram API is tried up to three times, 250 milliseconds apart, when the API answers with status 429 or the TCP connection to it can't be opened, as when it is refused or the name does not resolve. A method that only reads, one whose name starts with `get` such as `getMe` and `getUpdates`, is also tried again after any other transport failure, including a timeout and one while the answer is read, and after a 5xx answer. Anything else is returned at once, so a message that may have reached Telegram, as after a timeout or a 5xx answer, is not sent twice. A failed connection through a proxy, or one that takes longer than `Timeout`, counts as such a case and is not tried again. `Request` sends a body as JSON in a `POST`. Without a body, a `get` method goes as `GET` and any other method as a `POST` with its parameters in the query, because net/http itself sends a `GET` again when a reused connection closes before the answer arrives.

Text over 4096 characters after entity parsing is rejected by Telegram; split it before calling `Send`.

#### HTML Formatting

parseMode `HTML` supports [limited set of tags](https://core.telegram.org/bots/api#html-style), so the package provides the `TelegramSupportedHTML` function, which strips all unsupported tags and replaces `h1-h3` with `<b>` and `h4-h6` with `<i><b>` to preserve formatting. The end of a paragraph or a list item and a line break become a newline, unless the HTML already has one there, and trailing newlines are removed.

If you want to post text into HTML tag like `<a>text</a>`, you can use `EscapeTelegramText` method to escape it (by replacing symbols `&`, `<`, `>` with `&amp;`, `&lt;`, `&gt;`).

#### Authorisation

You can use Telegram notifications as described above, just to send messages. But also, you can use `Telegram` to authorise users as a login method or to sign them up for notifications. Functions used for processing updates from users are `GetBotUsername`, `AddToken`, `CheckToken`, `Request`, and `Run` or `ProcessUpdate` (only one of two can be used at a time).

Normal flow is following:
1. you run the `Run` goroutine 
2. call `AddToken` and provide user with that token
3. user clicks on the link `https://t.me/<BOT_USERNAME>/?start=<TOKEN>`
4. you call `CheckToken` to verify that the user clicked the link, and if so you will receive user's UserID

Alternative flow is the same, but instead of running the `Run` goroutine, you set up process update flow separately (to use with [auth](https://github.com/go-pkgz/auth/blob/master/provider/telegram.go) as well, for example) and run `ProcessUpdate` when update is received. Example of such a setup can be seen [in Remark42](https://github.com/umputun/remark42/blob/c027dcd/backend/app/providers/telegram.go).

### Slack

`slack:` scheme akin to `mailto:` is supported. The `title`, `titleLink` and `attachmentText` query parameters add a [text attachment](https://api.slack.com/reference/messaging/attachments) when `title` or `attachmentText` is set; `titleLink` makes the title a link. A destination that is a Slack ID, an upper-case `C`, `G`, `D`, `U` or `W` followed by upper-case letters and digits, is used as it is; anything else is a channel name, looked up with `conversations.list` on every `Send`. The lookup needs the `channels:read` scope and finds public channels only. Examples:

- `slack:channel`
- `slack:channelID`
- `slack:userID`
- `slack:channel?title=title&attachmentText=test%20text&titleLink=https://example.org`

```go
package main

import (
	"context"
	"log"

	"github.com/go-pkgz/notify"
	"github.com/slack-go/slack"
)

func main() {
	wh := notify.NewSlack(
		"token",
		slack.OptionDebug(true), // optional, you can pass any slack.Options
	)
	err := wh.Send(context.Background(), "slack:general", "Hello, World!")
	if err != nil {
		log.Fatalf("problem sending message using slack, %v", err)
	}
}
```

### Webhook

`http://` and `https://` schemas are supported. The text is sent as the raw body of a `POST` request, with no `Content-Type` unless one is set in `Headers`. A response status outside 2xx is returned as an error quoting up to 16 KiB of the response body. Redirects are not followed: a 3xx response is returned as an error.

Each `Headers` entry is a header name and a value separated by the first colon, with spaces around both trimmed. An entry without a colon is skipped, and when a name repeats, the last value is sent.

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/go-pkgz/notify"
)

func main() {
	wh := notify.NewWebhook(notify.WebhookParams{
		Timeout: time.Second,                                // optional, default is 5 seconds
		Headers: []string{"Content-Type: application/json"}, // optional
	})
	err := wh.Send(context.Background(), "https://example.org/webhook", "Hello, World!")
	if err != nil {
		log.Fatalf("problem sending message using webhook, %v", err)
	}
}
```

## Status

The library extracted from [remark42](https://github.com/umputun/remark) project. The original code in production use on multiple sites and seems to work fine.
