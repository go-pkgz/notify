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

All supported notification methods could adhere to the following interface. Example on how to use it:

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
	err := notify.Send(context.Background(), notifiers, "https://example.com/webhook", "Hello, world!")
	if err != nil {
		fmt.Printf("Sent message error: %s", err))
	}
}
```

### Email

`mailto:` [scheme](https://datatracker.ietf.org/doc/html/rfc6068) is supported. Only `subject` and `from` query params are used.

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
		Host:        "localhost", // the only required field, others are optional
		Port:        25,
		TLS:         false, // TLS, but not STARTTLS
		ContentType: "text/html",
		Charset:     "UTF-8",
		Username:    "username",
		Password:    "password",
		TimeOut:     time.Second * 10, // default is 30 seconds
	})
	err := wh.Send(
		context.Background(),
		`mailto:"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>`,
		"Hello, World!",
	)
	if err != nil {
		log.Fatalf("problem sending message using email, %v", err)
	}
}
```

### Telegram

### Slack

`slack:` scheme akin to `mailto:` is supported. `title`, `titleLink`, `attachmentText` and query params are used: if they are defined, message would be sent with a [text attachment](https://api.slack.com/reference/messaging/attachments). Examples:

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

`http://` and `https://` schemas are supported.

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
		Timeout: time.Second,                                          // optional, default is 5 seconds
		Headers: []string{"Content-Type:application/json,text/plain"}, // optional
	})
	err := wh.Send(context.Background(), "https://example.org/webhook", "Hello, World!")
	if err != nil {
		log.Fatalf("problem sending message using webhook, %v", err)
	}
}
```

## Status

The library extracted from [remark42](https://github.com/umputun/remark) project. The original code in production use on multiple sites and seems to work fine.

`go-pkgz/notify` library still in development and until version 1 released some breaking changes possible.
