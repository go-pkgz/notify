package notify

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/slack-go/slack"
)

// Slack notifications client
type Slack struct {
	client *slack.Client
}

// NewSlack makes Slack client for notifications
func NewSlack(token string, opts ...slack.Option) *Slack {
	return &Slack{client: slack.New(token, opts...)}
}

// Send sends the message over Slack, with "title", "titleLink" and "attachmentText" parsed from destination field
// with "slack:" schema same way "mailto:" schema is constructed.
//
// A destination is either a Slack ID, an upper-case letter C, G, D, U or W followed by upper-case
// letters and digits, which is used as it is, or a channel name, which is looked up with
// conversations.list. The lookup needs the channels:read scope and finds public channels only.
//
// Example:
//
// - slack:channelName
// - slack:channelID
// - slack:userID
// - slack:channel?title=title&attachmentText=test%20text&titleLink=https://example.org
func (s *Slack) Send(ctx context.Context, destination, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	channelID, attachment, err := s.parseDestination(ctx, destination)
	if err != nil {
		return fmt.Errorf("problem parsing destination: %w", err)
	}
	options := []slack.MsgOption{slack.MsgOptionText(text, false)}
	// titleLink alone carries nothing, slack renders it as a link on the title and drops it without one
	if attachment.Title != "" || attachment.Text != "" {
		options = append(options, slack.MsgOptionAttachments(attachment))
	}

	_, _, err = s.client.PostMessageContext(ctx, channelID, options...)
	return err
}

// Schema returns the scheme supported by this client, without the colon
func (s *Slack) Schema() string {
	return "slack"
}

func (s *Slack) String() string {
	return "slack notifications destination"
}

// parses "slack:" in a manner "mailto:" URL is parsed url and returns channelID and attachment.
// if channelID is channel name and not ID (see isSlackID), then it will be resolved to ID.
func (s *Slack) parseDestination(ctx context.Context, destination string) (string, slack.Attachment, error) {
	// parse URL
	u, err := url.Parse(destination)
	if err != nil {
		return "", slack.Attachment{}, err
	}
	if u.Scheme != "slack" {
		return "", slack.Attachment{}, fmt.Errorf("unsupported scheme %s, should be slack", u.Scheme)
	}
	channelID := u.Opaque
	if !s.isSlackID(u.Opaque) {
		channelID, err = s.findChannelIDByName(ctx, u.Opaque)
		if err != nil {
			return "", slack.Attachment{}, fmt.Errorf("problem retrieving channel ID for #%s: %w", u.Opaque, err)
		}
	}

	return channelID,
		slack.Attachment{
			Title:     u.Query().Get("title"),
			TitleLink: u.Query().Get("titleLink"),
			Text:      u.Query().Get("attachmentText"),
		}, nil
}

// isSlackID reports whether destination is a Slack ID: C for a channel, G for a private channel or
// a group conversation, D for a direct message, U or W for a user, followed by upper-case letters
// and digits. Channel names are lower-case, so they never take this shape.
func (s *Slack) isSlackID(destination string) bool {
	if len(destination) < 2 || !strings.ContainsRune("CGDUW", rune(destination[0])) {
		return false
	}
	for _, r := range destination[1:] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (s *Slack) findChannelIDByName(ctx context.Context, name string) (string, error) {
	params := slack.GetConversationsParameters{}
	for {
		channels, next, err := s.client.GetConversationsContext(ctx, &params)
		if err != nil {
			return "", err
		}

		for i := range channels {
			if channels[i].Name == name {
				return channels[i].ID, nil
			}
		}

		if next == "" {
			break
		}
		params.Cursor = next
	}
	return "", errors.New("no such channel")
}
