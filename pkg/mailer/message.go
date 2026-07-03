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

// logoURL is the hosted Astonish logo used in the email header.
const logoURL = "https://schardosin.github.io/astonish/astonish-logo.png"

// wrapHTML wraps inner HTML content in the shared Astonish email layout.
// Provides consistent branding: logo, card container, purple accent, footer.
func wrapHTML(innerHTML string) string {
	return wrapHTMLWithFooter(innerHTML, "Astonish")
}

// wrapHTMLWithFooter is like wrapHTML but allows a custom footer sign-off.
// Uses forced-light design: white card on light gray background with explicit
// inline colors. Dark-mode clients may adjust the outer chrome but the card
// content stays readable against white.
func wrapHTMLWithFooter(innerHTML, signoff string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Astonish</title>
</head>
<body style="margin: 0; padding: 0; background-color: #f3f4f6; font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; -webkit-font-smoothing: antialiased;">
  <table width="100%%" cellpadding="0" cellspacing="0" border="0" style="background-color: #f3f4f6;">
    <tr>
      <td align="center" style="padding: 40px 20px;">
        <!--[if mso]><table width="580" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->
        <table width="100%%" cellpadding="0" cellspacing="0" border="0" style="max-width: 580px; background-color: #ffffff; border-radius: 12px; border: 1px solid #e5e7eb; overflow: hidden;">
          <tr>
            <td style="background-color: #7c3aed; height: 6px; font-size: 0; line-height: 0;">&nbsp;</td>
          </tr>
          <tr>
            <td align="center" style="padding: 32px 0 8px;">
              <img src="%s" alt="Astonish" width="150" style="display: block; width: 150px; height: auto; border: 0;">
            </td>
          </tr>
          <tr>
            <td style="padding: 16px 40px 32px;">
              %s
            </td>
          </tr>
          <tr>
            <td style="padding: 0 40px 32px;">
              <table width="100%%" cellpadding="0" cellspacing="0" border="0">
                <tr>
                  <td style="border-top: 1px solid #e5e7eb; padding-top: 20px;">
                    <p style="color: #9ca3af; font-size: 12px; text-align: center; margin: 0; font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">&mdash; <strong>%s</strong></p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
        </table>
        <!--[if mso]></td></tr></table><![endif]-->
      </td>
    </tr>
  </table>
</body>
</html>`, logoURL, innerHTML, signoff)
}

// heading renders a styled h2 element.
func heading(text string) string {
	return fmt.Sprintf(`<h2 style="font-size: 22px; font-weight: 700; margin: 0 0 20px; color: #1f2937; text-align: center; font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">%s</h2>`, text)
}

// paragraph renders a styled paragraph.
func paragraph(text string) string {
	return fmt.Sprintf(`<p style="color: #4b5563; font-size: 15px; line-height: 1.7; margin: 0 0 14px; font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;">%s</p>`, text)
}

// button renders a centered, bulletproof CTA button using a table cell
// background (renders correctly in Outlook and all email clients).
func button(label, href string) string {
	return fmt.Sprintf(`<table cellpadding="0" cellspacing="0" border="0" align="center" style="margin: 28px auto;">
  <tr>
    <td align="center" style="background-color: #7c3aed; border-radius: 10px; padding: 14px 40px;">
      <a href="%s" style="color: #ffffff; text-decoration: none; font-weight: 600; font-size: 15px; font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: inline-block; letter-spacing: 0.3px;">%s</a>
    </td>
  </tr>
</table>`, href, label)
}
