package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// graphBaseURL is the Microsoft Graph API v1.0 base URL.
	graphBaseURL = "https://graph.microsoft.com/v1.0"

	// graphTimeout is the HTTP timeout for Graph API requests.
	graphTimeout = 30 * time.Second

	// graphMaxPageSize is the maximum number of messages per request.
	graphMaxPageSize = 50
)

// TokenFunc is a function that returns a valid OAuth2 access token.
// The implementation should handle token refresh transparently.
type TokenFunc func() (string, error)

// MSGraphClient implements the Client interface using Microsoft Graph API
// with delegated OAuth2 permissions for a single user mailbox.
type MSGraphClient struct {
	cfg       *Config
	tokenFunc TokenFunc
	client    *http.Client
	mu        sync.Mutex
	connected bool
}

// NewMSGraphClient creates a new Microsoft Graph email client.
// The tokenFunc is called on each API request to obtain a valid access token.
func NewMSGraphClient(cfg *Config, tokenFunc TokenFunc) *MSGraphClient {
	return &MSGraphClient{
		cfg:       cfg,
		tokenFunc: tokenFunc,
		client:    &http.Client{Timeout: graphTimeout},
	}
}

// Connect verifies the token can be obtained and the mailbox is accessible.
func (c *MSGraphClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verify we can get a token and access the mailbox
	token, err := c.tokenFunc()
	if err != nil {
		return fmt.Errorf("msgraph: failed to obtain access token: %w", err)
	}

	// Test with a simple /me request
	req, err := http.NewRequestWithContext(ctx, "GET", graphBaseURL+"/me", nil)
	if err != nil {
		return fmt.Errorf("msgraph: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("msgraph: connection test failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("msgraph: connection test returned %d: %s", resp.StatusCode, string(body))
	}

	c.connected = true
	return nil
}

// Close is a no-op for HTTP-based Graph API client.
func (c *MSGraphClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	return nil
}

// IsConnected returns whether Connect() succeeded.
func (c *MSGraphClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Address returns the configured email address.
func (c *MSGraphClient) Address() string {
	return c.cfg.Address
}

// ListMessages returns email summaries from the mailbox.
func (c *MSGraphClient) ListMessages(ctx context.Context, opts ListOpts) ([]MessageSummary, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > graphMaxPageSize {
		limit = graphMaxPageSize
	}

	// Build OData filter
	var filters []string
	folder := opts.Folder
	if folder == "" {
		folder = c.cfg.Folder
	}
	if folder == "" {
		folder = "Inbox"
	}

	if opts.Unread {
		filters = append(filters, "isRead eq false")
	}
	if opts.From != "" {
		filters = append(filters, fmt.Sprintf("contains(from/emailAddress/address,'%s')", escapeOData(opts.From)))
	}
	if opts.Subject != "" {
		filters = append(filters, fmt.Sprintf("contains(subject,'%s')", escapeOData(opts.Subject)))
	}
	if !opts.Since.IsZero() {
		filters = append(filters, fmt.Sprintf("receivedDateTime ge %s", opts.Since.UTC().Format(time.RFC3339)))
	}

	// Build URL
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graphBaseURL, url.PathEscape(folder))
	params := url.Values{}
	params.Set("$top", fmt.Sprintf("%d", limit))
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,from,toRecipients,subject,receivedDateTime,isRead,hasAttachments,internetMessageId,internetMessageHeaders")
	if len(filters) > 0 {
		params.Set("$filter", strings.Join(filters, " and "))
	}

	var result graphMessageList
	if err := c.graphGet(ctx, endpoint+"?"+params.Encode(), &result); err != nil {
		return nil, fmt.Errorf("msgraph: list messages: %w", err)
	}

	summaries := make([]MessageSummary, 0, len(result.Value))
	for _, m := range result.Value {
		summaries = append(summaries, m.toSummary())
	}
	return summaries, nil
}

// ReadMessage returns the full content of a specific email.
func (c *MSGraphClient) ReadMessage(ctx context.Context, id string) (*Message, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(id))
	params := url.Values{}
	params.Set("$expand", "attachments($select=id,name,size,contentType,isInline)")

	var gMsg graphMessage
	if err := c.graphGet(ctx, endpoint+"?"+params.Encode(), &gMsg); err != nil {
		return nil, fmt.Errorf("msgraph: read message: %w", err)
	}

	msg := gMsg.toMessage(c.cfg.MaxBodyChars)
	return msg, nil
}

// SearchMessages searches emails with the given criteria.
func (c *MSGraphClient) SearchMessages(ctx context.Context, query SearchQuery) ([]MessageSummary, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > graphMaxPageSize {
		limit = graphMaxPageSize
	}

	folder := query.Folder
	if folder == "" {
		folder = c.cfg.Folder
	}
	if folder == "" {
		folder = "Inbox"
	}

	var filters []string
	if query.From != "" {
		filters = append(filters, fmt.Sprintf("contains(from/emailAddress/address,'%s')", escapeOData(query.From)))
	}
	if query.To != "" {
		filters = append(filters, fmt.Sprintf("contains(toRecipients/emailAddress/address,'%s')", escapeOData(query.To)))
	}
	if query.Subject != "" {
		filters = append(filters, fmt.Sprintf("contains(subject,'%s')", escapeOData(query.Subject)))
	}
	if !query.Since.IsZero() {
		filters = append(filters, fmt.Sprintf("receivedDateTime ge %s", query.Since.UTC().Format(time.RFC3339)))
	}
	if !query.Before.IsZero() {
		filters = append(filters, fmt.Sprintf("receivedDateTime lt %s", query.Before.UTC().Format(time.RFC3339)))
	}
	if query.HasAttachment {
		filters = append(filters, "hasAttachments eq true")
	}

	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graphBaseURL, url.PathEscape(folder))
	params := url.Values{}
	params.Set("$top", fmt.Sprintf("%d", limit))
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,from,toRecipients,subject,receivedDateTime,isRead,hasAttachments,internetMessageId")

	// Use $search for free-text query (requires ConsistencyLevel: eventual)
	if query.Query != "" {
		params.Set("$search", fmt.Sprintf(`"%s"`, escapeOData(query.Query)))
	}
	if len(filters) > 0 {
		params.Set("$filter", strings.Join(filters, " and "))
	}

	var result graphMessageList
	if err := c.graphGetWithHeaders(ctx, endpoint+"?"+params.Encode(), map[string]string{
		"ConsistencyLevel": "eventual",
	}, &result); err != nil {
		return nil, fmt.Errorf("msgraph: search messages: %w", err)
	}

	summaries := make([]MessageSummary, 0, len(result.Value))
	for _, m := range result.Value {
		summaries = append(summaries, m.toSummary())
	}
	return summaries, nil
}

// Send sends a new email via Microsoft Graph.
func (c *MSGraphClient) Send(ctx context.Context, msg OutgoingMessage) (string, error) {
	payload := buildSendMailPayload(msg)

	endpoint := graphBaseURL + "/me/sendMail"
	if err := c.graphPost(ctx, endpoint, payload); err != nil {
		return "", fmt.Errorf("msgraph: send mail: %w", err)
	}

	// Graph API doesn't return the Message-ID directly from sendMail.
	// Generate one for tracking purposes.
	domain := ExtractSenderDomain(c.cfg.Address)
	return BuildMessageID(domain), nil
}

// Reply sends a reply to an existing email.
func (c *MSGraphClient) Reply(ctx context.Context, replyToID string, replyAll bool, msg OutgoingMessage) (string, error) {
	action := "reply"
	if replyAll {
		action = "replyAll"
	}

	payload := map[string]any{
		"comment": msg.Body,
	}

	// If the user specified custom recipients or subject, use createReply + send pattern
	if len(msg.To) > 0 || msg.Subject != "" || msg.HTML != "" {
		return c.replyWithCustomContent(ctx, replyToID, replyAll, msg)
	}

	endpoint := fmt.Sprintf("%s/me/messages/%s/%s", graphBaseURL, url.PathEscape(replyToID), action)
	if err := c.graphPost(ctx, endpoint, payload); err != nil {
		return "", fmt.Errorf("msgraph: reply: %w", err)
	}

	domain := ExtractSenderDomain(c.cfg.Address)
	return BuildMessageID(domain), nil
}

// replyWithCustomContent creates a draft reply, modifies it, then sends.
func (c *MSGraphClient) replyWithCustomContent(ctx context.Context, replyToID string, replyAll bool, msg OutgoingMessage) (string, error) {
	action := "createReply"
	if replyAll {
		action = "createReplyAll"
	}

	// Step 1: Create draft reply
	endpoint := fmt.Sprintf("%s/me/messages/%s/%s", graphBaseURL, url.PathEscape(replyToID), action)
	var draft graphMessage
	if err := c.graphPostDecode(ctx, endpoint, nil, &draft); err != nil {
		return "", fmt.Errorf("msgraph: create draft reply: %w", err)
	}

	// Step 2: Update the draft with custom content
	update := map[string]any{}
	if msg.HTML != "" {
		update["body"] = map[string]string{
			"contentType": "HTML",
			"content":     msg.HTML,
		}
	} else if msg.Body != "" {
		update["body"] = map[string]string{
			"contentType": "Text",
			"content":     msg.Body,
		}
	}
	if msg.Subject != "" {
		update["subject"] = msg.Subject
	}
	if len(msg.To) > 0 {
		update["toRecipients"] = buildRecipients(msg.To)
	}
	if len(msg.CC) > 0 {
		update["ccRecipients"] = buildRecipients(msg.CC)
	}

	if len(update) > 0 {
		patchEndpoint := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(draft.ID))
		if err := c.graphPatch(ctx, patchEndpoint, update); err != nil {
			return "", fmt.Errorf("msgraph: update draft reply: %w", err)
		}
	}

	// Step 3: Send the draft
	sendEndpoint := fmt.Sprintf("%s/me/messages/%s/send", graphBaseURL, url.PathEscape(draft.ID))
	if err := c.graphPost(ctx, sendEndpoint, nil); err != nil {
		return "", fmt.Errorf("msgraph: send draft reply: %w", err)
	}

	domain := ExtractSenderDomain(c.cfg.Address)
	return BuildMessageID(domain), nil
}

// MarkRead marks the given messages as read.
func (c *MSGraphClient) MarkRead(ctx context.Context, ids []string) error {
	return c.setReadStatus(ctx, ids, true)
}

// MarkUnread marks the given messages as unread.
func (c *MSGraphClient) MarkUnread(ctx context.Context, ids []string) error {
	return c.setReadStatus(ctx, ids, false)
}

func (c *MSGraphClient) setReadStatus(ctx context.Context, ids []string, isRead bool) error {
	for _, id := range ids {
		endpoint := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(id))
		payload := map[string]any{"isRead": isRead}
		if err := c.graphPatch(ctx, endpoint, payload); err != nil {
			return fmt.Errorf("msgraph: set read status for %s: %w", id, err)
		}
	}
	return nil
}

// Delete moves messages to trash or permanently deletes them.
func (c *MSGraphClient) Delete(ctx context.Context, ids []string, permanent bool) error {
	for _, id := range ids {
		if permanent {
			endpoint := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(id))
			if err := c.graphDelete(ctx, endpoint); err != nil {
				return fmt.Errorf("msgraph: delete %s: %w", id, err)
			}
		} else {
			// Move to Deleted Items
			endpoint := fmt.Sprintf("%s/me/messages/%s/move", graphBaseURL, url.PathEscape(id))
			payload := map[string]string{"destinationId": "deleteditems"}
			if err := c.graphPost(ctx, endpoint, payload); err != nil {
				return fmt.Errorf("msgraph: move to trash %s: %w", id, err)
			}
		}
	}
	return nil
}

// --- HTTP helpers ---

func (c *MSGraphClient) graphGet(ctx context.Context, url string, dest any) error {
	return c.graphGetWithHeaders(ctx, url, nil, dest)
}

func (c *MSGraphClient) graphGetWithHeaders(ctx context.Context, url string, headers map[string]string, dest any) error {
	token, err := c.tokenFunc()
	if err != nil {
		return fmt.Errorf("obtain token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return parseGraphError(resp)
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *MSGraphClient) graphPost(ctx context.Context, url string, payload any) error {
	return c.graphRequest(ctx, "POST", url, payload, nil)
}

func (c *MSGraphClient) graphPostDecode(ctx context.Context, url string, payload any, dest any) error {
	return c.graphRequest(ctx, "POST", url, payload, dest)
}

func (c *MSGraphClient) graphPatch(ctx context.Context, url string, payload any) error {
	return c.graphRequest(ctx, "PATCH", url, payload, nil)
}

func (c *MSGraphClient) graphDelete(ctx context.Context, url string) error {
	return c.graphRequest(ctx, "DELETE", url, nil, nil)
}

func (c *MSGraphClient) graphRequest(ctx context.Context, method, url string, payload any, dest any) error {
	token, err := c.tokenFunc()
	if err != nil {
		return fmt.Errorf("obtain token: %w", err)
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	// Accept 2xx responses
	if resp.StatusCode >= 300 {
		return parseGraphError(resp)
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func parseGraphError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	var graphErr struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &graphErr) == nil && graphErr.Error.Message != "" {
		return fmt.Errorf("Graph API %d %s: %s", resp.StatusCode, graphErr.Error.Code, graphErr.Error.Message)
	}
	return fmt.Errorf("Graph API %d: %s", resp.StatusCode, string(body))
}

// --- Graph API data models ---

type graphMessageList struct {
	Value    []graphMessage `json:"value"`
	NextLink string         `json:"@odata.nextLink,omitempty"`
}

type graphMessage struct {
	ID                     string              `json:"id"`
	Subject                string              `json:"subject"`
	ReceivedDateTime       string              `json:"receivedDateTime"`
	IsRead                 bool                `json:"isRead"`
	HasAttachments         bool                `json:"hasAttachments"`
	InternetMessageID      string              `json:"internetMessageId"`
	From                   *graphEmailAddress  `json:"from"`
	ToRecipients           []graphRecipient    `json:"toRecipients"`
	CcRecipients           []graphRecipient    `json:"ccRecipients"`
	Body                   *graphBody          `json:"body"`
	InternetMessageHeaders []graphHeader       `json:"internetMessageHeaders"`
	Attachments            []graphAttachment   `json:"attachments"`
}

type graphEmailAddress struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type graphRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type graphBody struct {
	ContentType string `json:"contentType"` // "text" or "html"
	Content     string `json:"content"`
}

type graphHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type graphAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	IsInline    bool   `json:"isInline"`
}

// --- Conversion helpers ---

func (m *graphMessage) toSummary() MessageSummary {
	s := MessageSummary{
		ID:             m.ID,
		Subject:        m.Subject,
		Unread:         !m.IsRead,
		HasAttachments: m.HasAttachments,
		MessageID:      m.InternetMessageID,
	}

	if m.From != nil {
		s.From = formatGraphAddress(m.From.EmailAddress.Name, m.From.EmailAddress.Address)
	}
	for _, r := range m.ToRecipients {
		s.To = append(s.To, r.EmailAddress.Address)
	}
	if t, err := time.Parse(time.RFC3339, m.ReceivedDateTime); err == nil {
		s.Date = t
	}

	// Extract In-Reply-To from internet message headers
	for _, h := range m.InternetMessageHeaders {
		if strings.EqualFold(h.Name, "In-Reply-To") {
			s.InReplyTo = h.Value
			break
		}
	}

	return s
}

func (m *graphMessage) toMessage(maxBodyChars int) *Message {
	msg := &Message{
		ID:      m.ID,
		Subject: m.Subject,
		Unread:  !m.IsRead,
		Headers: make(map[string]string),
	}

	if m.From != nil {
		msg.From = formatGraphAddress(m.From.EmailAddress.Name, m.From.EmailAddress.Address)
	}
	for _, r := range m.ToRecipients {
		msg.To = append(msg.To, r.EmailAddress.Address)
	}
	for _, r := range m.CcRecipients {
		msg.CC = append(msg.CC, r.EmailAddress.Address)
	}
	if t, err := time.Parse(time.RFC3339, m.ReceivedDateTime); err == nil {
		msg.Date = t
	}

	// Body
	if m.Body != nil {
		if strings.EqualFold(m.Body.ContentType, "html") {
			msg.HTML = m.Body.Content
			msg.Body = stripHTMLBasic(m.Body.Content)
		} else {
			msg.Body = m.Body.Content
		}
	}

	// Truncate body if configured
	if maxBodyChars > 0 {
		if len(msg.Body) > maxBodyChars {
			msg.Body = msg.Body[:maxBodyChars]
		}
		if len(msg.HTML) > maxBodyChars {
			msg.HTML = msg.HTML[:maxBodyChars]
		}
	}

	// Headers from internet message headers
	msg.Headers["Message-ID"] = m.InternetMessageID
	for _, h := range m.InternetMessageHeaders {
		switch strings.ToLower(h.Name) {
		case "in-reply-to":
			msg.Headers["In-Reply-To"] = h.Value
		case "references":
			msg.Headers["References"] = h.Value
		case "message-id":
			msg.Headers["Message-ID"] = h.Value
		}
	}

	// Attachments
	for _, a := range m.Attachments {
		if a.IsInline {
			continue
		}
		msg.Attachments = append(msg.Attachments, AttachmentInfo{
			Name:        a.Name,
			Size:        a.Size,
			ContentType: a.ContentType,
		})
	}

	// Extract links from body
	msg.Links = extractLinks(msg.Body + " " + msg.HTML)

	return msg
}

// --- Payload builders ---

func buildSendMailPayload(msg OutgoingMessage) map[string]any {
	body := map[string]string{}
	if msg.HTML != "" {
		body["contentType"] = "HTML"
		body["content"] = msg.HTML
	} else {
		body["contentType"] = "Text"
		body["content"] = msg.Body
	}

	message := map[string]any{
		"subject":      msg.Subject,
		"body":         body,
		"toRecipients": buildRecipients(msg.To),
	}
	if len(msg.CC) > 0 {
		message["ccRecipients"] = buildRecipients(msg.CC)
	}
	if msg.ReplyTo != "" {
		message["replyTo"] = []map[string]any{
			{"emailAddress": map[string]string{"address": msg.ReplyTo}},
		}
	}

	payload := map[string]any{
		"message":         message,
		"saveToSentItems": true,
	}

	return payload
}

func buildRecipients(addrs []string) []map[string]any {
	recipients := make([]map[string]any, 0, len(addrs))
	for _, addr := range addrs {
		recipients = append(recipients, map[string]any{
			"emailAddress": map[string]string{"address": addr},
		})
	}
	return recipients
}

func formatGraphAddress(name, address string) string {
	if name != "" && name != address {
		return fmt.Sprintf("%s <%s>", name, address)
	}
	return address
}

// escapeOData escapes single quotes in OData filter values.
func escapeOData(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// stripHTMLBasic does a very basic HTML-to-text conversion (strips tags).
// For full conversion, the existing parser.go functions can be used.
func stripHTMLBasic(html string) string {
	var result strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}

// extractLinks extracts URLs from text content.
func extractLinks(text string) []string {
	var links []string
	seen := make(map[string]bool)
	for _, word := range strings.Fields(text) {
		// Clean common trailing punctuation
		word = strings.TrimRight(word, ".,;:!?\"')")
		if (strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://")) && !seen[word] {
			links = append(links, word)
			seen[word] = true
		}
	}
	return links
}

// --- Token Exchange ---

// MSGraphTokenResponse is the response from the Microsoft identity token endpoint.
type MSGraphTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// ExchangeMSGraphToken exchanges a refresh token for a new access token (and
// possibly a rotated refresh token) using the Microsoft identity platform.
// The tokenURL should be https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token.
func ExchangeMSGraphToken(ctx context.Context, tokenURL, clientID, clientSecret, refreshToken string) (*MSGraphTokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
		"scope":         {"https://graph.microsoft.com/.default"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp MSGraphTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &tokenResp, nil
}
