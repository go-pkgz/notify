package notify

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailNew(t *testing.T) {
	smtpParams := SMTPParams{
		Host:        "test@host",
		Port:        1000,
		TLS:         true,
		HELOHost:    "helo.example.org",
		Username:    "test@username",
		Password:    "test@password",
		LoginAuth:   true,
		ContentType: "text/html",
		Charset:     "UTF-8",
		TimeOut:     time.Second,
	}

	email := NewEmail(smtpParams)

	assert.NotNil(t, email, "email returned")

	assert.Equal(t, "mailto", email.Schema())
	assert.Equal(t, smtpParams.TimeOut, email.TimeOut, "SMTPParams.TimOut unchanged after creation")
	assert.Equal(t, smtpParams.Host, email.Host, "SMTPParams.Host unchanged after creation")
	assert.Equal(t, smtpParams.Username, email.Username, "SMTPParams.Username unchanged after creation")
	assert.Equal(t, smtpParams.Password, email.Password, "SMTPParams.Password unchanged after creation")
	assert.Equal(t, smtpParams.Port, email.Port, "SMTPParams.Port unchanged after creation")
	assert.Equal(t, smtpParams.TLS, email.TLS, "SMTPParams.TLS unchanged after creation")
	assert.Equal(t, smtpParams.ContentType, email.ContentType, "SMTPParams.ContentType unchanged after creation")
	assert.Equal(t, smtpParams.Charset, email.Charset, "SMTPParams.Charset unchanged after creation")
	assert.Equal(t, smtpParams.LoginAuth, email.LoginAuth, "SMTPParams.LoginAuth unchanged after creation")
	assert.Equal(t, smtpParams.HELOHost, email.HELOHost, "SMTPParams.HELOHost unchanged after creation")
	// sender reports the greeting hostname it will use, so this asserts the option reached it
	assert.Contains(t, email.sender.String(), `helo:"helo.example.org"`)
}

func TestEmailDefaultHELOHost(t *testing.T) {
	email := NewEmail(SMTPParams{Host: "test@host"})
	assert.Contains(t, email.sender.String(), `helo:"localhost"`, "greeting hostname is unchanged without HELOHost set")
}

func TestEmailSendClientError(t *testing.T) {
	email := NewEmail(SMTPParams{Host: "test@host", Username: "user", TLS: true})

	assert.Equal(t, "email: with username 'user' at server test@host:0 with TLS", email.String())

	// no destination set
	require.EqualError(t, email.Send(context.Background(), "", ""),
		"problem parsing destination: unsupported scheme , should be mailto")

	// wrong scheme
	require.EqualError(t, email.Send(context.Background(), "https://example.org", ""),
		"problem parsing destination: unsupported scheme https, should be mailto")

	// bad destination set
	require.EqualError(t, email.Send(context.Background(), "%", ""),
		`problem parsing destination: parse "%": invalid URL escape "%"`)

	// bad recipient
	require.EqualError(t, email.Send(context.Background(), "mailto:bad", ""),
		`problem parsing destination: problem parsing email recipients: mail: missing '@' or angle-addr`)

	// unable to find host, with advanced destination parsing test
	assert.Contains(t,
		email.Send(
			context.Background(),
			`mailto:addr1@example.org,"John Wayne"<john@example.org>?subject=test-subj&from="Notifier"<notify@example.org>`,
			"test",
		).Error(),
		"no such host",
	)

	// canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.EqualError(t, email.Send(ctx, "mailto:test@example.org", ""), "context canceled")
}

func TestEmail_SendCancellationAfterConnect(t *testing.T) {
	// server accepts the connection, greets and stops responding
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var wg sync.WaitGroup
	defer wg.Wait()
	defer func() { _ = ln.Close() }()

	var once sync.Once
	greeted := make(chan struct{}) // closed once the client sent a command, i.e. it is past the connection and the greeting
	wg.Go(func() {
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			wg.Go(func() {
				defer func() { _ = conn.Close() }()
				// server side gives up on its own, so a regression fails the test instead of hanging the suite
				_ = conn.SetDeadline(time.Now().Add(time.Second * 20))
				_, _ = fmt.Fprint(conn, "220 localhost ESMTP stub\r\n")
				buf := make([]byte, 1)
				if _, e := conn.Read(buf); e == nil {
					once.Do(func() { close(greeted) })
				}
				_, _ = io.Copy(io.Discard, conn)
			})
		}
	})

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	// connection timeout is long on purpose, the context should terminate the send
	email := NewEmail(SMTPParams{Host: host, Port: port, TimeOut: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// cancel only once the transaction is past the connection, otherwise the test proves nothing
	go func() {
		<-greeted
		cancel()
	}()
	failsafe := time.AfterFunc(time.Second*30, cancel)
	defer failsafe.Stop()

	st := time.Now()
	err = email.Send(ctx, "mailto:test@example.org?from=notify@example.org", "test")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "cancellation is visible to the caller")
	assert.Less(t, time.Since(st), time.Second*5, "send is terminated by the context, not by the stalled server")
	select {
	case <-greeted:
	default:
		t.Fatal("send was canceled before the connection was established, the test proves nothing")
	}
}
