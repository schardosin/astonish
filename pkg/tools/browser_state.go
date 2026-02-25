package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

// --- browser_cookies ---

type BrowserCookiesArgs struct {
	Action   string  `json:"action" jsonschema:"Cookie action: get, set, or clear"`
	Name     string  `json:"name,omitempty" jsonschema:"Cookie name (used with action=set)"`
	Value    string  `json:"value,omitempty" jsonschema:"Cookie value (used with action=set)"`
	URL      string  `json:"url,omitempty" jsonschema:"URL scope for get (returns cookies for this URL) or set (sets cookie for this URL)"`
	Domain   string  `json:"domain,omitempty" jsonschema:"Cookie domain (used with action=set, e.g. '.example.com')"`
	Path     string  `json:"path,omitempty" jsonschema:"Cookie path (used with action=set, default '/')"`
	Expires  float64 `json:"expires,omitempty" jsonschema:"Expiry as seconds since epoch (used with action=set, 0 = session cookie)"`
	HTTPOnly bool    `json:"httpOnly,omitempty" jsonschema:"HTTP-only flag (used with action=set)"`
	Secure   bool    `json:"secure,omitempty" jsonschema:"Secure flag (used with action=set)"`
	SameSite string  `json:"sameSite,omitempty" jsonschema:"SameSite policy: Strict, Lax, or None (used with action=set)"`
}

type BrowserCookiesResult struct {
	Success bool                 `json:"success,omitempty"`
	Message string               `json:"message,omitempty"`
	Cookies []browser.CookieInfo `json:"cookies,omitempty"`
}

func BrowserCookies(mgr *browser.Manager) func(tool.Context, BrowserCookiesArgs) (BrowserCookiesResult, error) {
	return func(_ tool.Context, args BrowserCookiesArgs) (BrowserCookiesResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserCookiesResult{}, err
		}

		switch args.Action {
		case "get":
			var urls []string
			if args.URL != "" {
				urls = []string{args.URL}
			}
			cookies, err := browser.GetCookies(pg, urls)
			if err != nil {
				return BrowserCookiesResult{}, err
			}
			return BrowserCookiesResult{
				Success: true,
				Cookies: cookies,
				Message: fmt.Sprintf("Retrieved %d cookies", len(cookies)),
			}, nil

		case "set":
			if args.Name == "" {
				return BrowserCookiesResult{}, fmt.Errorf("cookie name is required for action=set")
			}
			if err := browser.SetCookie(pg, args.Name, args.Value, args.URL, args.Domain, args.Path, args.SameSite, args.Expires, args.HTTPOnly, args.Secure); err != nil {
				return BrowserCookiesResult{}, err
			}
			return BrowserCookiesResult{
				Success: true,
				Message: fmt.Sprintf("Cookie %q set", args.Name),
			}, nil

		case "clear":
			if err := browser.ClearCookies(pg); err != nil {
				return BrowserCookiesResult{}, err
			}
			return BrowserCookiesResult{
				Success: true,
				Message: "All cookies cleared",
			}, nil

		default:
			return BrowserCookiesResult{}, fmt.Errorf("invalid action %q: must be get, set, or clear", args.Action)
		}
	}
}

// --- browser_storage ---

type BrowserStorageArgs struct {
	Action string `json:"action" jsonschema:"Storage action: get, set, clear, or getAll"`
	Kind   string `json:"kind" jsonschema:"Storage type: local (localStorage) or session (sessionStorage)"`
	Key    string `json:"key,omitempty" jsonschema:"Storage key (used with action=get or set)"`
	Value  string `json:"value,omitempty" jsonschema:"Value to store (used with action=set)"`
}

type BrowserStorageResult struct {
	Success bool                   `json:"success,omitempty"`
	Message string                 `json:"message,omitempty"`
	Value   interface{}            `json:"value,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

func BrowserStorage(mgr *browser.Manager) func(tool.Context, BrowserStorageArgs) (BrowserStorageResult, error) {
	return func(_ tool.Context, args BrowserStorageArgs) (BrowserStorageResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserStorageResult{}, err
		}

		switch args.Action {
		case "get":
			if args.Key == "" {
				return BrowserStorageResult{}, fmt.Errorf("key is required for action=get")
			}
			val, err := browser.GetStorageItem(pg, args.Kind, args.Key)
			if err != nil {
				return BrowserStorageResult{}, err
			}
			return BrowserStorageResult{
				Success: true,
				Value:   val,
			}, nil

		case "getAll":
			data, err := browser.GetAllStorage(pg, args.Kind)
			if err != nil {
				return BrowserStorageResult{}, err
			}
			return BrowserStorageResult{
				Success: true,
				Data:    data,
				Message: fmt.Sprintf("Retrieved %d items from %sStorage", len(data), args.Kind),
			}, nil

		case "set":
			if args.Key == "" {
				return BrowserStorageResult{}, fmt.Errorf("key is required for action=set")
			}
			if err := browser.SetStorageItem(pg, args.Kind, args.Key, args.Value); err != nil {
				return BrowserStorageResult{}, err
			}
			return BrowserStorageResult{
				Success: true,
				Message: fmt.Sprintf("Set %sStorage[%q]", args.Kind, args.Key),
			}, nil

		case "clear":
			if err := browser.ClearStorage(pg, args.Kind); err != nil {
				return BrowserStorageResult{}, err
			}
			return BrowserStorageResult{
				Success: true,
				Message: fmt.Sprintf("Cleared %sStorage", args.Kind),
			}, nil

		default:
			return BrowserStorageResult{}, fmt.Errorf("invalid action %q: must be get, getAll, set, or clear", args.Action)
		}
	}
}

// --- browser_set_offline ---

type BrowserSetOfflineArgs struct {
	Offline bool `json:"offline" jsonschema:"Set to true to disable network, false to re-enable"`
}

type BrowserSetOfflineResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetOffline(mgr *browser.Manager) func(tool.Context, BrowserSetOfflineArgs) (BrowserSetOfflineResult, error) {
	return func(_ tool.Context, args BrowserSetOfflineArgs) (BrowserSetOfflineResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetOfflineResult{}, err
		}
		if err := browser.SetOffline(pg, args.Offline); err != nil {
			return BrowserSetOfflineResult{}, fmt.Errorf("failed to set offline mode: %w", err)
		}
		msg := "Network enabled"
		if args.Offline {
			msg = "Network disabled (offline mode)"
		}
		return BrowserSetOfflineResult{Success: true, Message: msg}, nil
	}
}

// --- browser_set_headers ---

type BrowserSetHeadersArgs struct {
	Headers map[string]string `json:"headers" jsonschema:"HTTP headers to add to all requests (empty map clears custom headers)"`
}

type BrowserSetHeadersResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetHeaders(mgr *browser.Manager) func(tool.Context, BrowserSetHeadersArgs) (BrowserSetHeadersResult, error) {
	return func(_ tool.Context, args BrowserSetHeadersArgs) (BrowserSetHeadersResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetHeadersResult{}, err
		}
		if err := browser.SetExtraHeaders(pg, args.Headers); err != nil {
			return BrowserSetHeadersResult{}, fmt.Errorf("failed to set headers: %w", err)
		}
		if len(args.Headers) == 0 {
			return BrowserSetHeadersResult{Success: true, Message: "Custom headers cleared"}, nil
		}
		return BrowserSetHeadersResult{
			Success: true,
			Message: fmt.Sprintf("Set %d custom header(s)", len(args.Headers)),
		}, nil
	}
}

// --- browser_set_credentials ---

type BrowserSetCredentialsArgs struct {
	Username string `json:"username" jsonschema:"HTTP Basic Auth username (empty to disable)"`
	Password string `json:"password" jsonschema:"HTTP Basic Auth password (empty to disable)"`
}

type BrowserSetCredentialsResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetCredentials(mgr *browser.Manager) func(tool.Context, BrowserSetCredentialsArgs) (BrowserSetCredentialsResult, error) {
	return func(_ tool.Context, args BrowserSetCredentialsArgs) (BrowserSetCredentialsResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetCredentialsResult{}, err
		}
		if err := browser.SetHTTPCredentials(pg, args.Username, args.Password); err != nil {
			return BrowserSetCredentialsResult{}, fmt.Errorf("failed to set credentials: %w", err)
		}
		if args.Username == "" && args.Password == "" {
			return BrowserSetCredentialsResult{Success: true, Message: "HTTP credentials cleared"}, nil
		}
		return BrowserSetCredentialsResult{Success: true, Message: "HTTP Basic Auth credentials set"}, nil
	}
}

// --- browser_set_geolocation ---

type BrowserSetGeolocationArgs struct {
	Latitude  float64 `json:"latitude" jsonschema:"Latitude (-90 to 90)"`
	Longitude float64 `json:"longitude" jsonschema:"Longitude (-180 to 180)"`
	Accuracy  float64 `json:"accuracy,omitempty" jsonschema:"Accuracy in meters (default 1)"`
}

type BrowserSetGeolocationResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetGeolocation(mgr *browser.Manager) func(tool.Context, BrowserSetGeolocationArgs) (BrowserSetGeolocationResult, error) {
	return func(_ tool.Context, args BrowserSetGeolocationArgs) (BrowserSetGeolocationResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetGeolocationResult{}, err
		}
		accuracy := args.Accuracy
		if accuracy <= 0 {
			accuracy = 1
		}
		if err := browser.SetGeolocation(pg, args.Latitude, args.Longitude, accuracy); err != nil {
			return BrowserSetGeolocationResult{}, fmt.Errorf("failed to set geolocation: %w", err)
		}
		return BrowserSetGeolocationResult{
			Success: true,
			Message: fmt.Sprintf("Geolocation set to %.4f, %.4f (accuracy: %.0fm)", args.Latitude, args.Longitude, accuracy),
		}, nil
	}
}

// --- browser_set_media ---

type BrowserSetMediaArgs struct {
	ColorScheme string `json:"colorScheme" jsonschema:"Preferred color scheme: dark, light, or no-preference"`
}

type BrowserSetMediaResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetMedia(mgr *browser.Manager) func(tool.Context, BrowserSetMediaArgs) (BrowserSetMediaResult, error) {
	return func(_ tool.Context, args BrowserSetMediaArgs) (BrowserSetMediaResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetMediaResult{}, err
		}
		switch args.ColorScheme {
		case "dark", "light", "no-preference":
			// valid
		default:
			return BrowserSetMediaResult{}, fmt.Errorf("invalid colorScheme %q: must be dark, light, or no-preference", args.ColorScheme)
		}
		if err := browser.SetMediaColorScheme(pg, args.ColorScheme); err != nil {
			return BrowserSetMediaResult{}, fmt.Errorf("failed to set media: %w", err)
		}
		return BrowserSetMediaResult{
			Success: true,
			Message: fmt.Sprintf("Color scheme set to %q", args.ColorScheme),
		}, nil
	}
}

// --- browser_set_timezone ---

type BrowserSetTimezoneArgs struct {
	Timezone string `json:"timezone" jsonschema:"IANA timezone ID (e.g. 'America/New_York', 'Europe/London', 'Asia/Tokyo'). Empty to clear."`
}

type BrowserSetTimezoneResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetTimezone(mgr *browser.Manager) func(tool.Context, BrowserSetTimezoneArgs) (BrowserSetTimezoneResult, error) {
	return func(_ tool.Context, args BrowserSetTimezoneArgs) (BrowserSetTimezoneResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetTimezoneResult{}, err
		}
		if err := browser.SetTimezone(pg, args.Timezone); err != nil {
			return BrowserSetTimezoneResult{}, fmt.Errorf("failed to set timezone: %w", err)
		}
		if args.Timezone == "" {
			return BrowserSetTimezoneResult{Success: true, Message: "Timezone override cleared"}, nil
		}
		return BrowserSetTimezoneResult{
			Success: true,
			Message: fmt.Sprintf("Timezone set to %q", args.Timezone),
		}, nil
	}
}

// --- browser_set_locale ---

type BrowserSetLocaleArgs struct {
	Locale string `json:"locale" jsonschema:"BCP 47 locale (e.g. 'en-US', 'fr-FR', 'ja-JP'). Empty to clear."`
}

type BrowserSetLocaleResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BrowserSetLocale(mgr *browser.Manager) func(tool.Context, BrowserSetLocaleArgs) (BrowserSetLocaleResult, error) {
	return func(_ tool.Context, args BrowserSetLocaleArgs) (BrowserSetLocaleResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetLocaleResult{}, err
		}
		if err := browser.SetLocale(pg, args.Locale); err != nil {
			return BrowserSetLocaleResult{}, fmt.Errorf("failed to set locale: %w", err)
		}
		if args.Locale == "" {
			return BrowserSetLocaleResult{Success: true, Message: "Locale override cleared"}, nil
		}
		return BrowserSetLocaleResult{
			Success: true,
			Message: fmt.Sprintf("Locale set to %q", args.Locale),
		}, nil
	}
}

// --- browser_set_device ---

type BrowserSetDeviceArgs struct {
	Device    string `json:"device" jsonschema:"Device name (e.g. 'iPhone X', 'iPad Pro', 'Galaxy S5', 'Pixel 2'). Use 'clear' to remove device emulation."`
	Landscape bool   `json:"landscape,omitempty" jsonschema:"Use landscape orientation (default: portrait)"`
}

type BrowserSetDeviceResult struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	DPR       float64 `json:"devicePixelRatio,omitempty"`
	Mobile    bool    `json:"mobile,omitempty"`
	UserAgent string  `json:"userAgent,omitempty"`
}

func BrowserSetDevice(mgr *browser.Manager) func(tool.Context, BrowserSetDeviceArgs) (BrowserSetDeviceResult, error) {
	return func(_ tool.Context, args BrowserSetDeviceArgs) (BrowserSetDeviceResult, error) {
		if args.Device == "" {
			return BrowserSetDeviceResult{}, fmt.Errorf("device name is required")
		}

		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSetDeviceResult{}, err
		}

		if args.Device == "clear" {
			if err := browser.ClearDevice(pg); err != nil {
				return BrowserSetDeviceResult{}, fmt.Errorf("failed to clear device emulation: %w", err)
			}
			return BrowserSetDeviceResult{
				Success: true,
				Message: "Device emulation cleared",
			}, nil
		}

		result, err := browser.SetDevice(pg, args.Device, args.Landscape)
		if err != nil {
			return BrowserSetDeviceResult{}, err
		}

		orientation := "portrait"
		if args.Landscape {
			orientation = "landscape"
		}
		return BrowserSetDeviceResult{
			Success:   true,
			Message:   fmt.Sprintf("Emulating %s (%s, %dx%d)", result.Title, orientation, result.Width, result.Height),
			Width:     result.Width,
			Height:    result.Height,
			DPR:       result.DPR,
			Mobile:    result.Mobile,
			UserAgent: result.UserAgent,
		}, nil
	}
}
