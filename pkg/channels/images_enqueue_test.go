package channels

import (
	"testing"

	"github.com/SAP/astonish/pkg/agent"
	"google.golang.org/genai"
)

// Verifies the manager wiring pattern: enqueue model InlineData then drain
// into ImageAttachment — the same sequence used in handleInbound for orphan
// image-only turns (no accompanying text).
func TestEnqueueThenDrain_ModelInlineImage(t *testing.T) {
	t.Parallel()
	ca := &agent.ChatAgent{}
	content := &genai.Content{
		Role: "model",
		Parts: []*genai.Part{{
			InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}},
		}},
	}
	ca.EnqueueImagesFromContent(content)

	var pending []ImageAttachment
	for _, img := range ca.DrainImages() {
		pending = append(pending, ImageAttachment{Data: img.Data, Format: img.Format})
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(pending))
	}
	if pending[0].Format != "png" || len(pending[0].Data) != 2 {
		t.Errorf("unexpected attachment: %+v", pending[0])
	}

	// Orphan send shape used when text is empty.
	out := OutboundMessage{Images: pending}
	if len(out.Images) != 1 || out.Text != "" {
		t.Errorf("orphan outbound: %+v", out)
	}
}
