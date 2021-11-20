package notify

import (
	"context"

	log "github.com/go-pkgz/lgr"
	"github.com/pkg/errors"
	"github.com/slack-go/slack"
)

// Slack implements notify.Destination for Slack
type Slack struct {
	channelID   string
	channelName string
	client      *slack.Client
}

// NewSlack makes Slack bot for notifications
func NewSlack(token, channelName string, opts ...slack.Option) (*Slack, error) {
	if channelName == "" {
		channelName = "general"
	}

	client := slack.New(token, opts...)
	res := &Slack{client: client, channelName: channelName}

	channelID, err := res.findChannelIDByName(channelName)
	if err != nil {
		return nil, errors.Wrap(err, "can not find slack channel '"+channelName+"'")
	}

	res.channelID = channelID
	log.Printf("[DEBUG] create new slack notifier for chan %s", channelID)

	return res, nil
}

// SendWithAttachment message with link attachment to the Slack channel
func (t *Slack) SendWithAttachment(ctx context.Context, text, titleLink, title, attachmentText string) error {
	_, _, err := t.client.PostMessageContext(
		ctx,
		t.channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionAttachments(
			slack.Attachment{
				TitleLink: titleLink,
				Title:     title,
				Text:      attachmentText,
			},
		),
	)
	return err
}

func (t *Slack) String() string {
	return "slack: " + t.channelName + " (" + t.channelID + ")"
}

func (t *Slack) findChannelIDByName(name string) (string, error) {
	params := slack.GetConversationsParameters{}
	for {
		channels, next, err := t.client.GetConversations(&params)
		if err != nil {
			return "", err
		}

		for _, channel := range channels {
			if channel.Name == name {
				return channel.ID, nil
			}
		}

		if next == "" {
			break
		}
		params.Cursor = next
	}
	return "", errors.New("no such channel")
}
