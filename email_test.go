package notify

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEmailNew(t *testing.T) {
	smtpParams := SMTPParams{
		Host:        "test@host",
		Port:        1000,
		TLS:         true,
		Username:    "test@username",
		Password:    "test@password",
		ContentType: "text/html",
		Charset:     "UTF-8",
		TimeOut:     time.Second,
	}

	email := NewEmail(smtpParams)

	assert.NotNil(t, email, "email returned")

	assert.Equal(t, smtpParams.TimeOut, email.TimeOut, "SMTPParams.TimOut unchanged after creation")
	assert.Equal(t, smtpParams.Host, email.Host, "SMTPParams.Host unchanged after creation")
	assert.Equal(t, smtpParams.Username, email.Username, "SMTPParams.Username unchanged after creation")
	assert.Equal(t, smtpParams.Password, email.Password, "SMTPParams.Password unchanged after creation")
	assert.Equal(t, smtpParams.Port, email.Port, "SMTPParams.Port unchanged after creation")
	assert.Equal(t, smtpParams.TLS, email.TLS, "SMTPParams.TLS unchanged after creation")
	assert.Equal(t, smtpParams.ContentType, email.ContentType, "SMTPParams.ContentType unchanged after creation")
	assert.Equal(t, smtpParams.Charset, email.Charset, "SMTPParams.Charset unchanged after creation")
}

func TestEmailSendClientError(t *testing.T) {
	email := NewEmail(SMTPParams{Host: "test@host", Username: "user", TLS: true})

	assert.Equal(t, "email: with username 'user' at server test@host:0 with TLS", email.String())

	// no destination set
	assert.EqualError(t, email.Send(context.Background(), "", ""), "empty destination")

	// unable to find host
	assert.Error(t, email.Send(context.Background(), "test", "test"), "unable to find host")

	// canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.EqualError(t, email.Send(ctx, "test", ""), "context canceled")
}
