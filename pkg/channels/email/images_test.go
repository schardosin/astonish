package email

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/channels"
	emailpkg "github.com/SAP/astonish/pkg/email"
)

// captureClient records the last OutgoingMessage sent.
type captureClient struct {
	last emailpkg.OutgoingMessage
}

func (c *captureClient) Connect(context.Context) error                          { return nil }
func (c *captureClient) Close() error                                           { return nil }
func (c *captureClient) IsConnected() bool                                      { return true }
func (c *captureClient) Address() string                                        { return "bot@example.com" }
func (c *captureClient) ListMessages(context.Context, emailpkg.ListOpts) ([]emailpkg.MessageSummary, error) {
	return nil, nil
}
func (c *captureClient) ReadMessage(context.Context, string) (*emailpkg.Message, error) {
	return nil, nil
}
func (c *captureClient) SearchMessages(context.Context, emailpkg.SearchQuery) ([]emailpkg.MessageSummary, error) {
	return nil, nil
}
func (c *captureClient) Send(_ context.Context, msg emailpkg.OutgoingMessage) (string, error) {
	c.last = msg
	return "<id@test>", nil
}
func (c *captureClient) Reply(_ context.Context, _ string, _ bool, msg emailpkg.OutgoingMessage) (string, error) {
	c.last = msg
	return "<id@test>", nil
}
func (c *captureClient) MarkRead(context.Context, []string) error   { return nil }
func (c *captureClient) MarkUnread(context.Context, []string) error { return nil }
func (c *captureClient) Delete(context.Context, []string, bool) error {
	return nil
}

func TestSend_AttachesImages(t *testing.T) {
	t.Parallel()
	cap := &captureClient{}
	ch := &EmailChannel{client: cap}

	err := ch.Send(context.Background(), channels.Target{ChatID: "user@example.com"}, channels.OutboundMessage{
		Text: "Here is an image",
		Images: []channels.ImageAttachment{
			{Data: []byte{0x89, 0x50, 0x4e, 0x47}, Format: "png"},
			{Data: []byte{0xff, 0xd8, 0xff}, Format: "jpg"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cap.last.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(cap.last.Attachments))
	}
	if cap.last.Attachments[0].Filename != "image.png" || cap.last.Attachments[0].ContentType != "image/png" {
		t.Errorf("first attachment: %+v", cap.last.Attachments[0])
	}
	if cap.last.Attachments[1].Filename != "image.jpeg" || cap.last.Attachments[1].ContentType != "image/jpeg" {
		t.Errorf("second attachment: %+v", cap.last.Attachments[1])
	}
}
