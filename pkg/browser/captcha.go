package browser

import (
	"encoding/json"
	"strings"

	"github.com/go-rod/rod"
)

// CAPTCHAType identifies the type of CAPTCHA detected on a page.
type CAPTCHAType string

const (
	CAPTCHANone                CAPTCHAType = ""
	CAPTCHAReCaptchaV2         CAPTCHAType = "recaptcha_v2"
	CAPTCHAReCaptchaV3         CAPTCHAType = "recaptcha_v3"
	CAPTCHAHCaptcha            CAPTCHAType = "hcaptcha"
	CAPTCHACloudflareTurnstile CAPTCHAType = "cloudflare_turnstile"
	CAPTCHAUnknown             CAPTCHAType = "unknown"
)

// CAPTCHADetection holds the result of a CAPTCHA scan on a page.
type CAPTCHADetection struct {
	Found    bool        `json:"found"`
	Type     CAPTCHAType `json:"type"`
	SiteKey  string      `json:"site_key,omitempty"` // The data-sitekey attribute, if found
	Selector string      `json:"selector,omitempty"` // CSS selector where the CAPTCHA was found
	Details  string      `json:"details,omitempty"`  // Human-readable description
}

// detectCAPTCHAScript is the JavaScript executed in the page context to detect
// CAPTCHA providers. It returns a JSON string for reliable deserialization.
const detectCAPTCHAScript = `() => {
	const result = { found: false, type: '', siteKey: '', selector: '', details: '' };

	// reCAPTCHA v2 — visible checkbox or invisible
	const recaptchaV2 = document.querySelector('.g-recaptcha, [data-sitekey][class*="recaptcha"], iframe[src*="recaptcha/api2"], iframe[src*="recaptcha/enterprise"]');
	if (recaptchaV2) {
		result.found = true;
		result.type = 'recaptcha_v2';
		result.siteKey = recaptchaV2.getAttribute('data-sitekey') || '';
		result.selector = recaptchaV2.tagName.toLowerCase() + (recaptchaV2.className ? '.' + recaptchaV2.className.split(' ').join('.') : '');
		result.details = 'Google reCAPTCHA v2 detected';
		return JSON.stringify(result);
	}

	// reCAPTCHA v3 — typically loaded as a script, no visible widget
	const recaptchaV3Script = document.querySelector('script[src*="recaptcha/api.js?render="], script[src*="recaptcha/enterprise.js?render="]');
	if (recaptchaV3Script) {
		const src = recaptchaV3Script.getAttribute('src') || '';
		const renderMatch = src.match(/render=([^&]+)/);
		result.found = true;
		result.type = 'recaptcha_v3';
		result.siteKey = renderMatch ? renderMatch[1] : '';
		result.selector = 'script[src*="recaptcha"]';
		result.details = 'Google reCAPTCHA v3 detected (invisible, score-based)';
		return JSON.stringify(result);
	}

	// hCaptcha
	const hcaptcha = document.querySelector('.h-captcha, [data-sitekey][class*="hcaptcha"], iframe[src*="hcaptcha.com"]');
	if (hcaptcha) {
		result.found = true;
		result.type = 'hcaptcha';
		result.siteKey = hcaptcha.getAttribute('data-sitekey') || '';
		result.selector = hcaptcha.tagName.toLowerCase() + (hcaptcha.className ? '.' + hcaptcha.className.split(' ').join('.') : '');
		result.details = 'hCaptcha detected';
		return JSON.stringify(result);
	}

	// Cloudflare Turnstile
	const turnstile = document.querySelector('.cf-turnstile, [data-sitekey][class*="turnstile"], iframe[src*="challenges.cloudflare.com"]');
	if (turnstile) {
		result.found = true;
		result.type = 'cloudflare_turnstile';
		result.siteKey = turnstile.getAttribute('data-sitekey') || '';
		result.selector = turnstile.tagName.toLowerCase() + (turnstile.className ? '.' + turnstile.className.split(' ').join('.') : '');
		result.details = 'Cloudflare Turnstile detected';
		return JSON.stringify(result);
	}

	// Generic CAPTCHA detection — look for common patterns in page content
	const html = document.documentElement.innerHTML.toLowerCase();
	const captchaIndicators = ['captcha', 'are you a robot', 'are you human', 'prove you are human', 'verify you are human'];
	for (const indicator of captchaIndicators) {
		if (html.includes(indicator)) {
			// Confirm it is not just a mention in a script or comment
			const bodyText = document.body ? document.body.innerText.toLowerCase() : '';
			if (bodyText.includes(indicator)) {
				result.found = true;
				result.type = 'unknown';
				result.details = 'Possible CAPTCHA detected (text match: "' + indicator + '")';
				return JSON.stringify(result);
			}
		}
	}

	return JSON.stringify(result);
}`

// DetectCAPTCHA scans the given page for known CAPTCHA providers.
// It evaluates JavaScript in the page context to check for reCAPTCHA v2/v3,
// hCaptcha, and Cloudflare Turnstile widgets.
func DetectCAPTCHA(pg *rod.Page) (*CAPTCHADetection, error) {
	var raw struct {
		Found    bool   `json:"found"`
		Type     string `json:"type"`
		SiteKey  string `json:"siteKey"`
		Selector string `json:"selector"`
		Details  string `json:"details"`
	}

	res, err := pg.Eval(detectCAPTCHAScript)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(res.Value.Str()), &raw); err != nil {
		return nil, err
	}

	return &CAPTCHADetection{
		Found:    raw.Found,
		Type:     CAPTCHAType(raw.Type),
		SiteKey:  raw.SiteKey,
		Selector: raw.Selector,
		Details:  raw.Details,
	}, nil
}

// IsCAPTCHAPresent is a convenience wrapper that returns true if any CAPTCHA
// is detected on the page.
func IsCAPTCHAPresent(pg *rod.Page) bool {
	det, err := DetectCAPTCHA(pg)
	if err != nil {
		return false
	}
	return det.Found
}

// CAPTCHATypeFromString parses a CAPTCHA type string.
func CAPTCHATypeFromString(s string) CAPTCHAType {
	switch strings.ToLower(s) {
	case "recaptcha_v2":
		return CAPTCHAReCaptchaV2
	case "recaptcha_v3":
		return CAPTCHAReCaptchaV3
	case "hcaptcha":
		return CAPTCHAHCaptcha
	case "cloudflare_turnstile":
		return CAPTCHACloudflareTurnstile
	case "unknown":
		return CAPTCHAUnknown
	default:
		return CAPTCHANone
	}
}
