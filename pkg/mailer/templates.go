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

func (m Welcome) Subject() string {
	return "Welcome to Astonish: Your new AI orchestration command center \U0001F680"
}

func (m Welcome) TextBody() string {
	return fmt.Sprintf(`Hi %s,

Welcome aboard! We built Astonish to be more than just another tool — it is your orchestration engine for bringing powerful, autonomous AI agents to life.

Whether you are looking to automate complex daily tasks, scale dynamic workflows, or empower your entire team to innovate faster, you now have everything you need in one place.

Here is a quick look at what you can do starting today:

💬 Interactive Chat Sandbox — This is not your standard chatbot. Every conversation is backed by its own dedicated, fully isolated container. Your AI agents have the real-world capability to securely connect via SSH, spawn interactive PTY shell sessions, manipulate browsers, and execute code on the fly.

⚡ Visual Automation — Build and deploy complex "Flows" visually, without wrestling with the underlying logic.

🎨 Generative Apps — Describe any dashboard, tool, or internal app in plain English and get a live, interactive React app instantly. Your apps have built-in persistent storage, embedded AI calls, and direct access to MCP tools and APIs with server-side credentials — no frontend setup, no deployment. Build for yourself, then share with your team when you're ready.

🔌 Limitless Connections — Use MCP Servers to securely plug your agents directly into your external tools and data sources.

🧠 Shared Intelligence — Knowledge compounds automatically from every conversation. When you solve a tricky problem, the solution enters your memory and surfaces whenever anyone on your team hits the same issue. Personal insights stay private until you choose to share them — then they benefit everyone.

The best way to learn is to jump right in and build your first agent.

Get started: %s

We can't wait to see what you build.

— The Astonish Team`, m.DisplayName, m.AppURL)
}

func (m Welcome) HTMLBody() string {
	inner := heading("Welcome to Astonish \U0001F680") +
		paragraph(fmt.Sprintf("Hi %s,", m.DisplayName)) +
		paragraph("Welcome aboard! We built Astonish to be more than just another tool &mdash; it is your <strong>orchestration engine</strong> for bringing powerful, autonomous AI agents to life.") +
		paragraph("Whether you are looking to automate complex daily tasks, scale dynamic workflows, or empower your entire team to innovate faster, you now have everything you need in one place.") +
		paragraph("<strong>Here is a quick look at what you can do starting today:</strong>") +
		`<table style="width: 100%; border-collapse: collapse; margin: 16px 0 24px;">` +
		benefitRow("\U0001F4AC", "Interactive Chat Sandbox", "This is not your standard chatbot. Every conversation is backed by its own dedicated, fully isolated container. Your AI agents have the real-world capability to securely connect via SSH, spawn interactive PTY shell sessions, manipulate browsers, and execute code on the fly.") +
		benefitRow("\u26A1", "Visual Automation", "Build and deploy complex &quot;Flows&quot; visually, without wrestling with the underlying logic.") +
		benefitRow("\U0001F3A8", "Generative Apps", "Describe any dashboard, tool, or internal app in plain English and get a live, interactive React app instantly. Your apps have built-in persistent storage, embedded AI calls, and direct access to MCP tools and APIs with server-side credentials &mdash; no frontend setup, no deployment. Build for yourself, then share with your team when you&#39;re ready.") +
		benefitRow("\U0001F50C", "Limitless Connections", "Use MCP Servers to securely plug your agents directly into your external tools and data sources.") +
		benefitRow("\U0001F9E0", "Shared Intelligence", "Knowledge compounds automatically from every conversation. When you solve a tricky problem, the solution enters your memory and surfaces whenever anyone on your team hits the same issue. Personal insights stay private until you choose to share them &mdash; then they benefit everyone.") +
		`</table>` +
		paragraph("The best way to learn is to jump right in and build your first agent.") +
		button("Get Started", m.AppURL) +
		`<p style="color: #4b5563; font-size: 15px; line-height: 1.6; margin: 24px 0 0; text-align: center;">We can&#39;t wait to see what you build.</p>`
	return wrapHTMLWithFooter(inner, "The Astonish Team")
}

// benefitRow renders a single benefit row with emoji, bold title, and description.
func benefitRow(emoji, title, desc string) string {
	return fmt.Sprintf(`<tr>
  <td style="padding: 10px 12px 10px 0; vertical-align: top; font-size: 20px; width: 36px;">%s</td>
  <td style="padding: 10px 0; color: #4b5563; font-size: 14px; line-height: 1.6;"><strong style="color: #1f2937;">%s</strong><br>%s</td>
</tr>`, emoji, title, desc)
}

// wrapHTMLWithFooter is like wrapHTML but allows a custom footer sign-off.
func wrapHTMLWithFooter(innerHTML, signoff string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 520px; margin: 0 auto; padding: 32px 20px; color: #1f2937; background: #ffffff;">
%s
<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">
<p style="color: #9ca3af; font-size: 12px; text-align: center;">&mdash; <strong>%s</strong></p>
</body>
</html>`, innerHTML, signoff)
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
