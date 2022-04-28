package notify

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"time"

	"github.com/go-pkgz/email"
)

// SMTPParams contain settings for smtp server connection
type SMTPParams struct {
	Host        string        // SMTP host
	Port        int           // SMTP port
	TLS         bool          // TLS auth
	StartTLS    bool          // StartTLS auth
	ContentType string        // Content type
	Charset     string        // Character set
	Username    string        // username
	Password    string        // password
	TimeOut     time.Duration // TCP connection timeout
}

// Email notifications client
type Email struct {
	SMTPParams
	sender *email.Sender
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

	if smtpParams.StartTLS {
		opts = append(opts, email.STARTTLS(true))
	}

	sender := email.NewSender(smtpParams.Host, opts...)

	return &Email{sender: sender, SMTPParams: smtpParams}
}

// Send sends the message over Email, with "from" and "subject" parsed from destination field
// with "mailto:" schema. Example:
// mailto:"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>
// mailto:addr1@example.org,addr2@example.org?subject=test-subj&from=notify@example.org
func (e *Email) Send(ctx context.Context, destination, text string) error {
	emailParams, err := e.parseDestination(destination)
	if err != nil {
		return fmt.Errorf("problem parsing destination: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return e.sender.Send(text, emailParams)
	}
}

// Schema returns schema prefix supported by this client
func (e *Email) Schema() string {
	return "mailto"
}

// String representation of Email object
func (e *Email) String() string {
	str := fmt.Sprintf("email: with username '%s' at server %s:%d", e.Username, e.Host, e.Port)
	if e.TLS {
		str += " with TLS"
	}
	return str
}

// parses "mailto:" URL and returns email parameters
func (e *Email) parseDestination(destination string) (email.Params, error) {
	// parse URL
	u, err := url.Parse(destination)
	if err != nil {
		return email.Params{}, err
	}
	if u.Scheme != "mailto" {
		return email.Params{}, fmt.Errorf("unsupported scheme %s, should be mailto", u.Scheme)
	}

	// parse destination address(es)
	addresses, err := mail.ParseAddressList(u.Opaque)
	if err != nil {
		return email.Params{}, fmt.Errorf("problem parsing email recipients: %w", err)
	}
	destinations := []string{}
	for _, addr := range addresses {
		destinations = append(destinations, addr.String())
	}

	return email.Params{
		From:    u.Query().Get("from"),
		To:      destinations,
		Subject: u.Query().Get("subject"),
	}, nil
}
