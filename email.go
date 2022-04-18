package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-pkgz/email"
)

// SMTPParams contain settings for smtp server connection
type SMTPParams struct {
	Host        string        // SMTP host
	Port        int           // SMTP port
	TLS         bool          // TLS auth
	ContentType string        // Content type
	Charset     string        // Character set
	Username    string        // username
	Password    string        // password
	TimeOut     time.Duration // TCP connection timeout
}

// Email client
type Email struct {
	SMTPParams
	sender *email.Sender
}

// EmailMessage is a message to be sent with Email
type EmailMessage struct {
	From    string
	To      string
	Message string
}

// NewEmail makes new Email object
func NewEmail(smtpParams SMTPParams) *Email {
	var opts []email.Option

	if smtpParams.Username != "" {
		opts = append(opts, email.Auth(smtpParams.Username, smtpParams.Password))
	}

	if smtpParams.ContentType != "" {
		opts = append(opts, email.ContentType(smtpParams.ContentType))
	}

	if smtpParams.Charset != "" {
		opts = append(opts, email.Charset(smtpParams.Charset))
	}

	if smtpParams.Port != 0 {
		opts = append(opts, email.Port(smtpParams.Port))
	}

	if smtpParams.TimeOut != 0 {
		opts = append(opts, email.TimeOut(smtpParams.TimeOut))
	}

	if smtpParams.TLS {
		opts = append(opts, email.TLS(true))
	}

	sender := email.NewSender(smtpParams.Host, opts...)

	return &Email{sender: sender, SMTPParams: smtpParams}
}

// Send sends the message over email, with "from", "to" and "subject" encoded into destination field.
func (e *Email) Send(ctx context.Context, destination, text string) error {
	if destination == "" {
		return errors.New("empty destination")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return e.sender.Send(text,
			email.Params{
				// TODO: set properly from destination, document destination format
				From:    "me@example.com",
				To:      []string{"to@example.com"},
				Subject: "Hello world!",
			})
	}
}

// String representation of Email object
func (e *Email) String() string {
	str := fmt.Sprintf("email: with username '%s' at server %s:%d", e.Username, e.Host, e.Port)
	if e.TLS {
		str += " with TLS"
	}
	return str
}
