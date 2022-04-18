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

type Notifier interface {
	fmt.Stringer
	Send(ctx context.Context, text string) error
}

func main() {
	// create notifiers
	notifiers := []Notifier{notify.NewWebhook(notify.WebhookParams{})}
	for _, n := range notifiers {
		err := n.Send(context.Background(), "https://example.org/webhook", "Hello, world!")
		fmt.Printf("Sent message using %s, error: %s", n, err))
	}
```

### Email

### Telegram

### Slack

### Webhook

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
