package notify

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

func TestSlack_Send(t *testing.T) {
	ts := newMockSlackServer()
	defer ts.Close()

	tb := ts.newClient()
	assert.NotNil(t, tb)
	assert.Equal(t, "slack notifications destination", tb.String())

	err := tb.Send(context.TODO(), "slack:general?title=title&attachmentText=test%20text&titleLink=https://example.org", "test text")
	assert.NoError(t, err)

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
	assert.EqualError(t, slck.Send(context.Background(), "", ""),
		"problem parsing destination: unsupported scheme , should be slack")

	// wrong scheme
	assert.EqualError(t, slck.Send(context.Background(), "https://example.org", ""),
		"problem parsing destination: unsupported scheme https, should be slack")

	// bad destination set
	assert.EqualError(t, slck.Send(context.Background(), "%", ""),
		`problem parsing destination: parse "%": invalid URL escape "%"`)

	// can't retrieve channel ID
	ts.listingIsBroken = true
	assert.EqualError(t, slck.Send(context.Background(), "slack:general", ""),
		"problem parsing destination: problem retrieving channel ID for #general:"+
			" slack server error: 500 Internal Server Error")
	ts.listingIsBroken = false

	// non-existing channel
	assert.EqualError(t, slck.Send(context.Background(), "slack:non-existent", ""),
		"problem parsing destination: problem retrieving channel ID for #non-existent: no such channel")

	// canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.EqualError(t, slck.Send(ctx, "slack:general?title=test", ""), "context canceled")
}

type mockSlackServer struct {
	*httptest.Server
	isServerDown    bool
	listingIsBroken bool
}

func (ts *mockSlackServer) newClient() *Slack {
	return NewSlack("any-token", slack.OptionAPIURL(ts.URL+"/"))
}

func newMockSlackServer() *mockSlackServer {
	mockServer := mockSlackServer{}
	router := chi.NewRouter()
	router.Post("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
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

	router.Post("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
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

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("..... 404 for %s .....\n", r.URL)
	})

	mockServer.Server = httptest.NewServer(router)
	return &mockServer
}
