package notify

import (
	"bytes"
	"errors"
	"io"
	"net/smtp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEmailNew(t *testing.T) {
	smtpParams := SMTPParams{
		Host:     "test@host",
		Port:     1000,
		TLS:      true,
		Username: "test@username",
		Password: "test@password",
		TimeOut:  time.Second,
	}

	email := NewEmail(smtpParams)

	assert.NotNil(t, email, "email returned")

	if smtpParams.TimeOut == 0 {
		assert.Equal(t, defaultEmailTimeout, email.TimeOut, "empty SMTPParams.TimeOut changed to default")
	} else {
		assert.Equal(t, smtpParams.TimeOut, email.TimeOut, "SMTPParams.TimOut unchanged after creation")
	}
	assert.Equal(t, smtpParams.Host, email.Host, "SMTPParams.Host unchanged after creation")
	assert.Equal(t, smtpParams.Username, email.Username, "SMTPParams.Username unchanged after creation")
	assert.Equal(t, smtpParams.Password, email.Password, "SMTPParams.Password unchanged after creation")
	assert.Equal(t, smtpParams.Port, email.Port, "SMTPParams.Port unchanged after creation")
	assert.Equal(t, smtpParams.TLS, email.TLS, "SMTPParams.TLS unchanged after creation")
}

func TestEmailSendClientError(t *testing.T) {
	var testSet = []struct {
		name string
		smtp *fakeTestSMTP
		err  string
	}{
		{name: "failed to verify receiver", smtp: &fakeTestSMTP{fail: map[string]bool{"mail": true}},
			err: "bad from address \"\": failed to verify sender"},
		{name: "failed to verify sender", smtp: &fakeTestSMTP{fail: map[string]bool{"rcpt": true}},
			err: "bad to address \"\": failed to verify receiver"},
		{name: "failed to close connection", smtp: &fakeTestSMTP{fail: map[string]bool{"quit": true, "close": true}}},
		{name: "failed to make email writer", smtp: &fakeTestSMTP{fail: map[string]bool{"data": true}},
			err: "can't make email writer: failed to send"},
	}
	for _, d := range testSet {
		d := d
		t.Run(d.name, func(t *testing.T) {
			e := Email{smtp: d.smtp}
			if d.err != "" {
				assert.EqualError(t, e.SendMessage(EmailMessage{}), d.err,
					"expected error for e.SendMessage")
			} else {
				assert.NoError(t, e.SendMessage(EmailMessage{}),
					"expected no error for e.SendMessage")
			}
		})
	}
	e := Email{}
	e.smtp = nil
	assert.Error(t, e.SendMessage(EmailMessage{}),
		"nil e.smtp should return error")
	e.smtp = &fakeTestSMTP{}
	assert.NoError(t, e.SendMessage(EmailMessage{}), "",
		"no error expected for e.SendMessage in normal flow")
	e.smtp = &fakeTestSMTP{fail: map[string]bool{"quit": true}}
	assert.NoError(t, e.SendMessage(EmailMessage{}), "",
		"no error expected for e.SendMessage with failed smtpClient.Quit but successful smtpClient.Close")
	e.smtp = &fakeTestSMTP{fail: map[string]bool{"create": true}}
	assert.EqualError(t, e.SendMessage(EmailMessage{}), "failed to make smtp Create: failed to create client",
		"e.send called without smtpClient set returns error")
}

func Test_emailClient_Create(t *testing.T) {
	creator := emailClient{}
	client, err := creator.Create(SMTPParams{})
	assert.Error(t, err, "absence of address to connect results in error")
	assert.Nil(t, client, "no client returned in case of error")
}

type fakeTestSMTP struct {
	fail map[string]bool

	buff       bytes.Buffer
	mail, rcpt string
	auth       bool
	close      bool
	quitCount  int
	lock       sync.RWMutex
}

func (f *fakeTestSMTP) Create(SMTPParams) (smtpClient, error) {
	if f.fail["create"] {
		return nil, errors.New("failed to create client")
	}
	return f, nil
}

func (f *fakeTestSMTP) Auth(smtp.Auth) error { f.auth = true; return nil }

func (f *fakeTestSMTP) Mail(m string) error {
	f.lock.Lock()
	f.mail = m
	f.lock.Unlock()
	if f.fail["mail"] {
		return errors.New("failed to verify sender")
	}
	return nil
}

func (f *fakeTestSMTP) Rcpt(r string) error {
	f.lock.Lock()
	f.rcpt = r
	f.lock.Unlock()
	if f.fail["rcpt"] {
		return errors.New("failed to verify receiver")
	}
	return nil
}

func (f *fakeTestSMTP) Quit() error {
	f.lock.Lock()
	f.quitCount++
	f.lock.Unlock()
	if f.fail["quit"] {
		return errors.New("failed to quit")
	}
	return nil
}

func (f *fakeTestSMTP) Close() error {
	f.close = true
	if f.fail["close"] {
		return errors.New("failed to close")
	}
	return nil
}

func (f *fakeTestSMTP) Data() (io.WriteCloser, error) {
	if f.fail["data"] {
		return nil, errors.New("failed to send")
	}
	return nopCloser{&f.buff}, nil
}

//nolint:unused // TODO: test SMTP properly
func (f *fakeTestSMTP) readRcpt() string {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.rcpt
}

//nolint:unused // TODO: test SMTP properly
func (f *fakeTestSMTP) readMail() string {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.mail
}

//nolint:unused // TODO: test SMTP properly
func (f *fakeTestSMTP) readQuitCount() int {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.quitCount
}

func TokenGenFn(user, _, _ string) (string, error) {
	if user == "error" {
		return "", errors.New("token generation error")
	}
	return "token", nil
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}
