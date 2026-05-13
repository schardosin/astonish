package mailer

import "fmt"

// Message is the interface that all outbound email types implement.
// Each message type defines its own recipients, subject, and content.
type Message interface {
	To() []string
	Subject() string
	TextBody() string
	HTMLBody() string
}

// wrapHTML wraps inner HTML content in the shared Astonish email layout.
// Provides consistent branding: Inter font, max-width, footer.
func wrapHTML(innerHTML string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 520px; margin: 0 auto; padding: 32px 20px; color: #1f2937; background: #ffffff;">
%s
<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">
<p style="color: #9ca3af; font-size: 12px; text-align: center;">&mdash; Astonish</p>
</body>
</html>`, innerHTML)
}

// heading renders a styled h2 element.
func heading(text string) string {
	return fmt.Sprintf(`<h2 style="font-size: 20px; font-weight: 600; margin: 0 0 16px; color: #1f2937;">%s</h2>`, text)
}

// paragraph renders a styled paragraph.
func paragraph(text string) string {
	return fmt.Sprintf(`<p style="color: #4b5563; font-size: 15px; line-height: 1.6; margin: 0 0 12px;">%s</p>`, text)
}

// button renders a centered CTA button.
func button(label, href string) string {
	return fmt.Sprintf(`<div style="text-align: center; margin: 28px 0;">
  <a href="%s" style="display: inline-block; padding: 12px 32px; background: linear-gradient(135deg, #a855f7 0%%, #7c3aed 100%%); color: white; text-decoration: none; border-radius: 10px; font-weight: 500; font-size: 14px;">%s</a>
</div>`, href, label)
}
