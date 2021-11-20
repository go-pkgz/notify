package notify

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"time"

	log "github.com/go-pkgz/lgr"
	"github.com/pkg/errors"
)

// SMTPParams contain settings for smtp server connection
type SMTPParams struct {
	Host     string        // SMTP host
	Port     int           // SMTP port
	TLS      bool          // TLS auth
	Username string        // username
	Password string        // password
	TimeOut  time.Duration // TCP connection timeout
}

// Email client
type Email struct {
	SMTPParams

	smtp smtpClientCreator
}

// default email client implementation
type emailClient struct{ smtpClientCreator }

// smtpClient interface defines subset of net/smtp used by email client
type smtpClient interface {
	Mail(string) error
	Auth(smtp.Auth) error
	Rcpt(string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

// smtpClientCreator interface defines function for creating new smtpClients
type smtpClientCreator interface {
	Create(SMTPParams) (smtpClient, error)
}

// EmailMessage is a message to be sent with Email
type EmailMessage struct {
	From    string
	To      string
	Message string
}

const (
	defaultEmailTimeout = 10 * time.Second
)

// NewEmail makes new Email object
func NewEmail(smtpParams SMTPParams) *Email {
	var res = Email{SMTPParams: smtpParams}
	res.smtp = &emailClient{}
	if res.TimeOut <= 0 {
		res.TimeOut = defaultEmailTimeout
	}

	log.Printf("[DEBUG] Create new email notifier for server %s with user %s, timeout=%s",
		res.Host, res.Username, res.TimeOut)

	return &res
}

// SendMessage sends messages to server in a new connection, closing the connection after finishing.
// Thread safe.
func (e *Email) SendMessage(m EmailMessage) error {
	if e.smtp == nil {
		return errors.New("SendMessage called without client set")
	}
	client, err := e.smtp.Create(e.SMTPParams)
	if err != nil {
		return errors.Wrap(err, "failed to make smtp Create")
	}

	defer func() {
		if err = client.Quit(); err != nil {
			log.Printf("[WARN] failed to send quit command to %s:%d, %v", e.Host, e.Port, err)
			if err = client.Close(); err != nil {
				log.Printf("[WARN] can't close smtp connection, %v", err)
			}
		}
	}()

	if err = client.Mail(m.From); err != nil {
		return errors.Wrapf(err, "bad from address %q", m.From)
	}
	if err = client.Rcpt(m.To); err != nil {
		return errors.Wrapf(err, "bad to address %q", m.To)
	}

	writer, err := client.Data()
	if err != nil {
		return errors.Wrap(err, "can't make email writer")
	}

	defer func() {
		if err = writer.Close(); err != nil {
			log.Printf("[WARN] can't close smtp body writer, %v", err)
		}
	}()

	buf := bytes.NewBufferString(m.Message)
	if _, err = buf.WriteTo(writer); err != nil {
		return errors.Wrapf(err, "failed to send email body to %q", m.To)
	}

	return nil
}

// String representation of Email object
func (e *Email) String() string {
	return fmt.Sprintf("email: with username '%s' at server %s:%d", e.Username, e.Host, e.Port)
}

// Create establish SMTP connection with server using credentials in smtpClientWithCreator.SMTPParams
// and returns pointer to it. Thread safe.
func (s *emailClient) Create(params SMTPParams) (smtpClient, error) {
	authenticate := func(c *smtp.Client) error {
		if params.Username == "" || params.Password == "" {
			return nil
		}
		auth := smtp.PlainAuth("", params.Username, params.Password, params.Host)
		if err := c.Auth(auth); err != nil {
			return errors.Wrapf(err, "failed to auth to smtp %s:%d", params.Host, params.Port)
		}
		return nil
	}

	var c *smtp.Client
	srvAddress := fmt.Sprintf("%s:%d", params.Host, params.Port)
	if params.TLS {
		tlsConf := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         params.Host,
			MinVersion:         tls.VersionTLS12,
		}
		conn, err := tls.Dial("tcp", srvAddress, tlsConf)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to dial smtp tls to %s", srvAddress)
		}
		if c, err = smtp.NewClient(conn, params.Host); err != nil {
			return nil, errors.Wrapf(err, "failed to make smtp client for %s", srvAddress)
		}
		return c, authenticate(c)
	}

	conn, err := net.DialTimeout("tcp", srvAddress, params.TimeOut)
	if err != nil {
		return nil, errors.Wrapf(err, "timeout connecting to %s", srvAddress)
	}

	c, err = smtp.NewClient(conn, params.Host)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial")
	}

	return c, authenticate(c)
}
