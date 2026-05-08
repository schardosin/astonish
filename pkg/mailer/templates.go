package mailer

import "fmt"

// ---------------------------------------------------------------------------
// OrgInvite — sent when a user is added to an organization.
// ---------------------------------------------------------------------------

// OrgInvite is the welcome email sent when a user is added to an organization.
type OrgInvite struct {
	Recipient   string // email address
	DisplayName string
	OrgName     string
	AppURL      string // e.g. "https://astonish.local.muxpie.com"
	IsNewUser   bool   // true = account was just created (hint SSO)
}

func (m OrgInvite) To() []string { return []string{m.Recipient} }

func (m OrgInvite) Subject() string {
	return fmt.Sprintf("You've been added to %s on Astonish", m.OrgName)
}

func (m OrgInvite) TextBody() string {
	hint := "You can log in with your existing credentials."
	if m.IsNewUser {
		hint = "Your account has been created. Please use your organization's Single Sign-On (SSO) to log in, or contact your administrator for credentials."
	}
	return fmt.Sprintf(`Hi %s,

You've been added to the "%s" organization on Astonish.

%s

Sign in here: %s

— Astonish`, m.DisplayName, m.OrgName, hint, m.AppURL)
}

func (m OrgInvite) HTMLBody() string {
	hint := "You can log in with your existing credentials."
	if m.IsNewUser {
		hint = "Your account has been created. Please use your organization's Single Sign-On (SSO) to log in, or contact your administrator for credentials."
	}
	inner := heading(fmt.Sprintf("Welcome to %s", m.OrgName)) +
		paragraph(fmt.Sprintf("Hi %s,", m.DisplayName)) +
		paragraph(fmt.Sprintf("You've been added to the <strong>%s</strong> organization on Astonish.", m.OrgName)) +
		paragraph(hint) +
		button("Sign in to Astonish", m.AppURL)
	return wrapHTML(inner)
}

// ---------------------------------------------------------------------------
// VerificationCode — sent during email channel linking.
// ---------------------------------------------------------------------------

// VerificationCode is sent when a user initiates email channel verification.
type VerificationCode struct {
	Recipient string // email address
	Code      string // 6-character code
}

func (m VerificationCode) To() []string { return []string{m.Recipient} }

func (m VerificationCode) Subject() string {
	return "Verify your email for Astonish"
}

func (m VerificationCode) TextBody() string {
	return fmt.Sprintf(`Hi,

Your email verification code is:

    %s

Enter this code in Settings → Channels to complete linking your email.

This code expires in 5 minutes.

— Astonish`, m.Code)
}

func (m VerificationCode) HTMLBody() string {
	inner := paragraph("Hi,") +
		paragraph("Your email verification code is:") +
		fmt.Sprintf(`<div style="text-align: center; margin: 24px 0;">
  <span style="font-size: 32px; font-weight: bold; letter-spacing: 6px; font-family: monospace; background: #f3f4f6; padding: 12px 24px; border-radius: 8px; border: 1px solid #e5e7eb; color: #1f2937;">%s</span>
</div>`, m.Code) +
		paragraph("Enter this code in <strong>Settings → Channels</strong> to complete linking your email.") +
		`<p style="color: #6b7280; font-size: 13px; margin: 0;">This code expires in 5 minutes.</p>`
	return wrapHTML(inner)
}
