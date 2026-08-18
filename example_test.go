package notify_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-pkgz/notify"
)

// ExampleSend demonstrates sending a notification to a webhook destination.
// Webhook is the only notifier which needs no credentials, so this example is runnable as is.
func ExampleSend() {
	// stand-in for the real webhook receiver
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Printf("webhook received: %s\n", body)
	}))
	defer receiver.Close()

	notifiers := []notify.Notifier{notify.NewWebhook(notify.WebhookParams{})}

	if err := notify.Send(context.Background(), notifiers, receiver.URL, "Hello, world!"); err != nil {
		_, _ = fmt.Printf("send error: %s\n", err)
		return
	}
	_, _ = fmt.Println("sent")

	// Output:
	// webhook received: Hello, world!
	// sent
}
