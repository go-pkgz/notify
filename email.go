package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/go-pkgz/email"
)

// SMTPParams contain settings for smtp server connection
type SMTPParams struct {
	Host               string        // SMTP host
	Port               int           // SMTP port
	TLS                bool          // TLS auth
	HELOHost           string        // SMTP HELO/EHLO hostname
	StartTLS           bool          // startTLS auth
	InsecureSkipVerify bool          // skip certificate verification
	ContentType        string        // content type
	Charset            string        // character set
	LoginAuth          bool          // LOGIN auth method instead of default PLAIN, needed for Office 365 and outlook.com
	Username           string        // username
	Password           string        // password
	TimeOut            time.Duration // TCP connection timeout, the rest of the transaction is bound by the context of Send
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

	if smtpParams.LoginAuth {
		opts = append(opts, email.LoginAuth())
	}

	if smtpParams.Port != 0 {
		opts = append(opts, email.Port(smtpParams.Port))
	}

	if smtpParams.HELOHost != "" {
		opts = append(opts, email.HELOHost(smtpParams.HELOHost))
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

	if smtpParams.InsecureSkipVerify {
		opts = append(opts, email.InsecureSkipVerify(true))
	}

	sender := email.NewSender(smtpParams.Host, opts...)

	return &Email{sender: sender, SMTPParams: smtpParams}
}

// Send sends the message over Email, with "from", "subject" and "unsubscribeLink" parsed from destination field
// with "mailto:" schema. "from" is required, without it an error is returned and nothing is sent.
// "unsubscribeLink" passed as a header, https://support.google.com/mail/answer/81126 -> "Use one-click unsubscribe"
//
// Note: query parameter values in the mailto URL must be properly URL-encoded. In particular, email addresses
// containing "+" (e.g. "noreply+tag@example.com") must use "%2B" instead, otherwise "+" is interpreted as a space
// per standard URL query string parsing. Use url.QueryEscape for all parameter values. The address of each
// recipient before "?" is percent-decoded, so "a%2Btag@example.org" goes to "a+tag@example.org", and a
// display name is used as written; an address that is not valid percent-encoding is used as written, and
// one that does not decode to a single plain address is refused.
//
// Example:
//
// - mailto:"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>
// - mailto:addr1@example.org,addr2@example.org?subject=test-subj&from=notify@example.org&unsubscribeLink=http://example.org/unsubscribe
func (e *Email) Send(ctx context.Context, destination, text string) error {
	emailParams, err := e.parseDestination(destination)
	if err != nil {
		return fmt.Errorf("problem parsing destination: %w", err)
	}

	// SendContext terminates the transaction when ctx is done, including the parts after the connection is made
	err = e.sender.SendContext(ctx, text, emailParams)
	if err != nil && ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
		// transaction was interrupted, report why on top of the error it failed with
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	return err
}

// Schema returns the scheme supported by this client, without the colon
func (e *Email) Schema() string {
	return "mailto"
}

// defaultSMTPPort is the port the email sender connects to when SMTPParams.Port is not set
const defaultSMTPPort = 25

// String representation of Email object
func (e *Email) String() string {
	port := e.Port
	if port == 0 {
		port = defaultSMTPPort
	}
	str := fmt.Sprintf("email: with username '%s' at server %s:%d", e.Username, e.Host, port)
	if e.TLS {
		str += " with TLS"
	}
	if e.StartTLS {
		str += " with StartTLS"
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
		if addr.Address, err = e.decodeRecipient(addr.Address); err != nil {
			return email.Params{}, fmt.Errorf("problem parsing email recipients: %w", err)
		}
		stringAddr := addr.String()
		// in case of mailgun, correct RFC5322 address with <> yield 501 error, so we need to remove brackets
		if strings.HasPrefix(stringAddr, "<") && strings.HasSuffix(stringAddr, ">") {
			stringAddr = stringAddr[1 : len(stringAddr)-1]
		}
		destinations = append(destinations, stringAddr)
	}

	from := u.Query().Get("from")
	if strings.TrimSpace(from) == "" {
		return email.Params{}, errors.New(`"from" query parameter is required`)
	}

	return email.Params{
		From:            from,
		To:              destinations,
		Subject:         u.Query().Get("subject"),
		UnsubscribeLink: u.Query().Get("unsubscribeLink"),
	}, nil
}

// decodeRecipient returns a recipient address with its percent-encoding undone, as RFC 6068 has it, so
// "a%2Btag@example.org" is sent to "a+tag@example.org". An address that is not valid percent-encoding,
// such as "user%host@relay.example.org", is returned as written. A decoded address must still be one
// plain address on its own, so an encoded comma or angle bracket can't change who receives the message;
// an encoded quoted local part is refused for the same reason.
func (e *Email) decodeRecipient(address string) (string, error) {
	decoded, err := url.PathUnescape(address)
	if err != nil || decoded == address {
		return address, nil // not percent-encoded, used as written
	}
	parsed, err := mail.ParseAddress(decoded)
	if err != nil || parsed.Address != decoded {
		return "", fmt.Errorf("recipient %q is not one address once decoded", address)
	}
	return decoded, nil
}
