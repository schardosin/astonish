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

// ---------------------------------------------------------------------------
// TeamAdded — sent when a user is added to a team.
// ---------------------------------------------------------------------------

// TeamAdded is the notification email sent when a user is added to a team.
type TeamAdded struct {
	Recipient   string // email address
	DisplayName string
	TeamName    string
	OrgName     string
	AppURL      string // e.g. "https://astonish.local.muxpie.com"
}

func (m TeamAdded) To() []string { return []string{m.Recipient} }

func (m TeamAdded) Subject() string {
	return fmt.Sprintf("You've been added to %s on Astonish", m.TeamName)
}

func (m TeamAdded) TextBody() string {
	return fmt.Sprintf(`Hi %s,

You've been added to the "%s" team in the "%s" organization on Astonish.

You now have access to the team's shared agents, knowledge, and resources.

Sign in here: %s

— Astonish`, m.DisplayName, m.TeamName, m.OrgName, m.AppURL)
}

func (m TeamAdded) HTMLBody() string {
	inner := heading(fmt.Sprintf("Added to %s", m.TeamName)) +
		paragraph(fmt.Sprintf("Hi %s,", m.DisplayName)) +
		paragraph(fmt.Sprintf("You've been added to the <strong>%s</strong> team in the <strong>%s</strong> organization on Astonish.", m.TeamName, m.OrgName)) +
		paragraph("You now have access to the team's shared agents, knowledge, and resources.") +
		button("Sign in to Astonish", m.AppURL)
	return wrapHTML(inner)
}

// ---------------------------------------------------------------------------
// Welcome — onboarding email sent once when a new user account becomes active.
// ---------------------------------------------------------------------------

// Welcome is the onboarding email introducing a new user to Astonish's features.
// Sent exactly once per user — either immediately on creation (admin invite,
// self-registration without verification) or after email verification succeeds.
type Welcome struct {
	Recipient   string // email address
	DisplayName string
	AppURL      string // e.g. "https://astonish.example.com"
}

func (m Welcome) To() []string { return []string{m.Recipient} }

func (m Welcome) Subject() string { return "Welcome to Astonish" }

func (m Welcome) TextBody() string {
	return fmt.Sprintf(`Hi %s,

Welcome to Astonish — your AI agent platform.

Here's what you can do:

- Chat: Converse with AI agents in natural language
- Flows: Build visual automation workflows
- Teams & Organizations: Collaborate and share resources
- Skills: Reusable agent capabilities shared across your team
- MCP Servers: Connect external tools and data sources
- Knowledge: Personal and team knowledge bases

Get started: %s

— Astonish`, m.DisplayName, m.AppURL)
}

func (m Welcome) HTMLBody() string {
	inner := heading("Welcome to Astonish") +
		paragraph(fmt.Sprintf("Hi %s,", m.DisplayName)) +
		paragraph("Astonish is your AI agent platform. Here's what you can do:") +
		`<table style="width: 100%; border-collapse: collapse; margin: 16px 0;">` +
		featureRow("Chat", "Converse with AI agents in natural language") +
		featureRow("Flows", "Build visual automation workflows") +
		featureRow("Teams &amp; Orgs", "Collaborate and share resources") +
		featureRow("Skills", "Reusable agent capabilities shared across your team") +
		featureRow("MCP Servers", "Connect external tools and data sources") +
		featureRow("Knowledge", "Personal and team knowledge bases") +
		`</table>` +
		button("Get Started", m.AppURL)
	return wrapHTML(inner)
}

// featureRow renders a single feature row in the welcome email table.
func featureRow(name, desc string) string {
	return fmt.Sprintf(`<tr>
  <td style="padding: 6px 12px 6px 0; vertical-align: top; font-weight: 600; color: #7c3aed; font-size: 14px; white-space: nowrap;">%s</td>
  <td style="padding: 6px 0; color: #4b5563; font-size: 14px; line-height: 1.5;">%s</td>
</tr>`, name, desc)
}

// ---------------------------------------------------------------------------
// RegistrationVerification — sent when a new user signs up via builtin auth.
// ---------------------------------------------------------------------------

// RegistrationVerification is sent when a new user signs up via builtin auth
// and must verify their email address before the account becomes active.
type RegistrationVerification struct {
	Recipient   string // email address
	DisplayName string
	Code        string // 6-digit code
	ExpiryMin   int    // expiry in minutes
}

func (m RegistrationVerification) To() []string { return []string{m.Recipient} }

func (m RegistrationVerification) Subject() string {
	return "Verify your email address — Astonish"
}

func (m RegistrationVerification) TextBody() string {
	return fmt.Sprintf(`Hi %s,

Your email verification code is:

    %s

Enter this code to complete your registration.

This code expires in %d minutes. If you did not create an account, please ignore this email.

— Astonish`, m.DisplayName, m.Code, m.ExpiryMin)
}

func (m RegistrationVerification) HTMLBody() string {
	inner := heading("Verify your email address") +
		paragraph(fmt.Sprintf("Hi %s,", m.DisplayName)) +
		paragraph("Your verification code is:") +
		fmt.Sprintf(`<div style="text-align: center; margin: 24px 0;">
  <span style="font-size: 32px; font-weight: bold; letter-spacing: 6px; font-family: monospace; background: #f3f4f6; padding: 12px 24px; border-radius: 8px; border: 1px solid #e5e7eb; color: #1f2937;">%s</span>
</div>`, m.Code) +
		paragraph("Enter this code to complete your registration.") +
		fmt.Sprintf(`<p style="color: #6b7280; font-size: 13px; margin: 0;">This code expires in %d minutes. If you did not create an account, please ignore this email.</p>`, m.ExpiryMin)
	return wrapHTML(inner)
}
