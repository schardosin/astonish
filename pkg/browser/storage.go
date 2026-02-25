package browser

import (
	"encoding/json"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// CookieInfo is a simplified cookie representation returned to tool callers.
type CookieInfo struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"` // seconds since epoch, 0 = session
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

// GetCookies returns cookies for the current page. If urls is non-empty,
// cookies matching those URLs are returned instead.
func GetCookies(pg *rod.Page, urls []string) ([]CookieInfo, error) {
	cookies, err := pg.Cookies(urls)
	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}
	result := make([]CookieInfo, 0, len(cookies))
	for _, c := range cookies {
		result = append(result, CookieInfo{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: string(c.SameSite),
		})
	}
	return result, nil
}

// SetCookie sets a single cookie via the Network.setCookie CDP command.
func SetCookie(pg *rod.Page, name, value, url, domain, path, sameSite string, expires float64, httpOnly, secure bool) error {
	req := proto.NetworkSetCookie{
		Name:     name,
		Value:    value,
		URL:      url,
		Domain:   domain,
		Path:     path,
		HTTPOnly: httpOnly,
		Secure:   secure,
	}
	if expires > 0 {
		req.Expires = proto.TimeSinceEpoch(expires)
	}
	if sameSite != "" {
		req.SameSite = proto.NetworkCookieSameSite(sameSite)
	}
	_, err := req.Call(pg)
	if err != nil {
		return fmt.Errorf("failed to set cookie: %w", err)
	}
	return nil
}

// ClearCookies removes all browser cookies.
func ClearCookies(pg *rod.Page) error {
	// page.SetCookies(nil) calls NetworkClearBrowserCookies internally.
	if err := pg.SetCookies(nil); err != nil {
		return fmt.Errorf("failed to clear cookies: %w", err)
	}
	return nil
}

// GetStorageItem retrieves a value from localStorage or sessionStorage.
// kind must be "local" or "session".
func GetStorageItem(pg *rod.Page, kind, key string) (interface{}, error) {
	storageObj, err := storageObjectName(kind)
	if err != nil {
		return nil, err
	}

	js := fmt.Sprintf(`() => {
		const v = window.%s.getItem(%s);
		if (v === null) return null;
		try { return JSON.parse(v); } catch(e) { return v; }
	}`, storageObj, jsonString(key))

	res, err := pg.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s item: %w", kind, err)
	}
	return gsonToAny(res.Value), nil
}

// SetStorageItem sets a value in localStorage or sessionStorage.
func SetStorageItem(pg *rod.Page, kind, key, value string) error {
	storageObj, err := storageObjectName(kind)
	if err != nil {
		return err
	}

	js := fmt.Sprintf(`() => { window.%s.setItem(%s, %s); }`,
		storageObj, jsonString(key), jsonString(value))

	_, err = pg.Eval(js)
	if err != nil {
		return fmt.Errorf("failed to set %s item: %w", kind, err)
	}
	return nil
}

// ClearStorage removes all items from localStorage or sessionStorage.
func ClearStorage(pg *rod.Page, kind string) error {
	storageObj, err := storageObjectName(kind)
	if err != nil {
		return err
	}

	js := fmt.Sprintf(`() => { window.%s.clear(); }`, storageObj)
	_, err = pg.Eval(js)
	if err != nil {
		return fmt.Errorf("failed to clear %s storage: %w", kind, err)
	}
	return nil
}

// GetAllStorage returns all key-value pairs from localStorage or sessionStorage.
func GetAllStorage(pg *rod.Page, kind string) (map[string]interface{}, error) {
	storageObj, err := storageObjectName(kind)
	if err != nil {
		return nil, err
	}

	js := fmt.Sprintf(`() => {
		const s = window.%s;
		const result = {};
		for (let i = 0; i < s.length; i++) {
			const k = s.key(i);
			const v = s.getItem(k);
			try { result[k] = JSON.parse(v); } catch(e) { result[k] = v; }
		}
		return result;
	}`, storageObj)

	res, err := pg.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to get all %s storage: %w", kind, err)
	}

	raw := gsonToAny(res.Value)
	if m, ok := raw.(map[string]interface{}); ok {
		return m, nil
	}
	return make(map[string]interface{}), nil
}

func storageObjectName(kind string) (string, error) {
	switch kind {
	case "local":
		return "localStorage", nil
	case "session":
		return "sessionStorage", nil
	default:
		return "", fmt.Errorf("invalid storage kind %q: must be \"local\" or \"session\"", kind)
	}
}

// jsonString returns a JSON-encoded string literal safe for embedding in JS.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// gsonToAny converts a gson.JSON value to a Go interface.
func gsonToAny(v gson.JSON) interface{} {
	if v.Nil() {
		return nil
	}
	data, err := v.MarshalJSON()
	if err != nil {
		return v.String()
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return v.String()
	}
	return result
}
