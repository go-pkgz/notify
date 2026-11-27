package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlack_Send(t *testing.T) {
	ts := newMockSlackServer()
	defer ts.Close()

	tb := ts.newClient()
	assert.NotNil(t, tb)
	assert.Equal(t, "slack notifications destination", tb.String())
	assert.Equal(t, "slack", tb.Schema())

	err := tb.Send(context.Background(), "slack:general?title=title&attachmentText=test%20text&titleLink=https://example.org", "test text")
	require.NoError(t, err)
	assert.Contains(t, ts.lastMessage.Get("attachments"), "test text")

	// attachment text alone is enough, the title is not required for it
	ts.lastMessage = nil
	require.NoError(t, tb.Send(context.Background(), "slack:general?attachmentText=attachment%20text", "test text"))
	assert.Contains(t, ts.lastMessage.Get("attachments"), "attachment text", "attachment is sent without the title set")

	for _, destination := range []string{
		"slack:general",
		"slack:general?titleLink=https://example.org", // slack has nothing to put the link on without a title
	} {
		t.Run(destination, func(t *testing.T) {
			ts.lastMessage = nil
			require.NoError(t, tb.Send(context.Background(), destination, "test text"))
			assert.Empty(t, ts.lastMessage.Get("attachments"), "no attachment without a title or a text")
		})
	}

	ts.isServerDown = true
	err = tb.Send(context.Background(), "slack:general?title=title&attachmentText=test%20text&titleLink=https://example.org", "test text")
	assert.Contains(t, err.Error(), "slack server error", "send on broken client")
}

func TestSlackSendClientError(t *testing.T) {
	ts := newMockSlackServer()
	defer ts.Close()

	slck := ts.newClient()
	assert.NotNil(t, slck)
	assert.Equal(t, "slack notifications destination", slck.String())

	// no destination set
	require.EqualError(t, slck.Send(context.Background(), "", ""),
		"problem parsing destination: unsupported scheme , should be slack")

	// wrong scheme
	require.EqualError(t, slck.Send(context.Background(), "https://example.org", ""),
		"problem parsing destination: unsupported scheme https, should be slack")

	// bad destination set
	require.EqualError(t, slck.Send(context.Background(), "%", ""),
		`problem parsing destination: parse "%": invalid URL escape "%"`)

	// can't retrieve channel ID
	ts.listingIsBroken = true
	require.EqualError(t, slck.Send(context.Background(), "slack:general", ""),
		"problem parsing destination: problem retrieving channel ID for #general:"+
			" slack server error: 500 Internal Server Error")
	ts.listingIsBroken = false

	// non-existing channel
	require.EqualError(t, slck.Send(context.Background(), "slack:non-existent", ""),
		"problem parsing destination: problem retrieving channel ID for #non-existent: no such channel")

	// canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.EqualError(t, slck.Send(ctx, "slack:general?title=test", ""), "context canceled")
}

func TestSlack_SendToID(t *testing.T) {
	ts := newMockSlackServer()
	defer ts.Close()
	tb := ts.newClient()

	for _, id := range []string{"C0123ABC", "G0PRIVATE", "D0DIRECT", "U0123ABC", "W0ENTUSER"} {
		t.Run(id, func(t *testing.T) {
			ts.listCalls.Store(0)
			ts.lastMessage = nil
			require.NoError(t, tb.Send(context.Background(), "slack:"+id, "test text"))
			assert.Equal(t, id, ts.lastMessage.Get("channel"), "an ID is posted to as it is")
			assert.Zero(t, ts.listCalls.Load(), "an ID is not looked up by name")
		})
	}

	// anything else is a name: lower-case, starting with another letter or a digit, or just one letter
	for _, name := range []string{"general", "customers", "2024", "C", "Cgeneral", "C0123-ABC"} {
		t.Run(name, func(t *testing.T) {
			ts.listCalls.Store(0)
			err := tb.Send(context.Background(), "slack:"+name, "test text")
			if name == "general" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, "problem parsing destination: problem retrieving channel ID for #"+name+": no such channel")
			}
			assert.Equal(t, int32(1), ts.listCalls.Load(), "a name is looked up")
		})
	}
}

func TestSlack_SendNameLookupUsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the server notices the client went away only once the request body is read
		_, _ = io.Copy(io.Discard, r.Body)
		// the caller gives up while the listing is in flight
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
			_, _ = w.Write([]byte(`{"ok":true,"channels":[],"response_metadata":{"next_cursor":""}}`))
		}
	}))
	defer ts.Close()

	st := time.Now()
	err := NewSlack("any-token", slack.OptionAPIURL(ts.URL+"/")).Send(ctx, "slack:general", "test text")
	require.ErrorIs(t, err, context.Canceled, "the lookup ends with the context of Send")
	assert.Less(t, time.Since(st), 5*time.Second)
}

type mockSlackServer struct {
	*httptest.Server
	isServerDown    bool
	listingIsBroken bool
	lastMessage     url.Values   // form of the last chat.postMessage call
	listCalls       atomic.Int32 // number of conversations.list calls
}

func (ts *mockSlackServer) newClient() *Slack {
	return NewSlack("any-token", slack.OptionAPIURL(ts.URL+"/"))
}

func newMockSlackServer() *mockSlackServer {
	mockServer := mockSlackServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /conversations.list", func(w http.ResponseWriter, _ *http.Request) {
		mockServer.listCalls.Add(1)
		if mockServer.listingIsBroken {
			w.WriteHeader(500)
		} else {
			s := `{
		    "ok": true,
		    "channels": [
		        {
		            "id": "C12345678",
		            "name": "general",
		            "is_channel": true,
		            "is_group": false,
		            "is_im": false,
		            "created": 1503888888,
		            "is_archived": false,
		            "is_general": false,
		            "unlinked": 0,
		            "name_normalized": "random",
		            "is_shared": false,
		            "parent_conversation": null,
		            "creator": "U12345678",
		            "is_ext_shared": false,
		            "is_org_shared": false,
		            "pending_shared": [],
		            "pending_connected_team_ids": [],
		            "is_pending_ext_shared": false,
		            "is_member": false,
		            "is_private": false,
		            "is_mpim": false,
		            "previous_names": [],
		            "num_members": 1
		        }
		    ],
		    "response_metadata": {
		        "next_cursor": ""
		    }
		}`
			_, _ = w.Write([]byte(s))
		}
	})

	mux.HandleFunc("POST /chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil {
			mockServer.lastMessage = r.PostForm
		}
		if mockServer.isServerDown {
			w.WriteHeader(500)
		} else {
			s := `{
			    "ok": true,
			    "channel": "C12345678",
			    "ts": "1617008342.000100",
			    "message": {
			        "type": "message",
			        "subtype": "bot_message",
			        "text": "wowo",
			        "ts": "1617008342.000100",
			        "username": "slackbot",
			        "bot_id": "B12345678"
			    }
			}`
			_, _ = w.Write([]byte(s))
		}
	})

	mockServer.Server = httptest.NewServer(mux)
	return &mockServer
}
