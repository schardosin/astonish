package browser

import (
	"fmt"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// SetOffline enables or disables network connectivity for the page.
func SetOffline(pg *rod.Page, offline bool) error {
	// When going offline, set throughput to -1 (unlimited when online,
	// but irrelevant when offline). Latency 0 means no added latency.
	return proto.NetworkEmulateNetworkConditions{
		Offline:            offline,
		Latency:            0,
		DownloadThroughput: -1,
		UploadThroughput:   -1,
	}.Call(pg)
}

// SetExtraHeaders adds extra HTTP headers to all requests from this page.
// Pass an empty map to clear previously set headers.
func SetExtraHeaders(pg *rod.Page, headers map[string]string) error {
	h := make(proto.NetworkHeaders, len(headers))
	for k, v := range headers {
		h[k] = gson.New(v)
	}
	return proto.NetworkSetExtraHTTPHeaders{Headers: h}.Call(pg)
}

// SetHTTPCredentials configures HTTP Basic Auth credentials for the page.
// The Fetch domain intercepts auth challenges and responds automatically.
// Pass empty username/password to disable.
func SetHTTPCredentials(pg *rod.Page, username, password string) error {
	if username == "" && password == "" {
		// Disable fetch interception
		return proto.FetchDisable{}.Call(pg)
	}

	// Enable the Fetch domain with auth handling
	if err := (proto.FetchEnable{
		HandleAuthRequests: true,
	}).Call(pg); err != nil {
		return fmt.Errorf("failed to enable fetch for auth: %w", err)
	}

	// Listen for auth challenges and respond with credentials
	go pg.EachEvent(func(e *proto.FetchAuthRequired) {
		_ = proto.FetchContinueWithAuth{
			RequestID: e.RequestID,
			AuthChallengeResponse: &proto.FetchAuthChallengeResponse{
				Response: proto.FetchAuthChallengeResponseResponseProvideCredentials,
				Username: username,
				Password: password,
			},
		}.Call(pg)
	}, func(e *proto.FetchRequestPaused) {
		// Continue non-auth requests normally
		_ = proto.FetchContinueRequest{
			RequestID: e.RequestID,
		}.Call(pg)
	})()

	return nil
}

// SetGeolocation overrides the browser's geolocation.
// Pass nil pointers to clear the override.
func SetGeolocation(pg *rod.Page, latitude, longitude, accuracy float64) error {
	return proto.EmulationSetGeolocationOverride{
		Latitude:  &latitude,
		Longitude: &longitude,
		Accuracy:  &accuracy,
	}.Call(pg)
}

// ClearGeolocation removes the geolocation override.
func ClearGeolocation(pg *rod.Page) error {
	return proto.EmulationSetGeolocationOverride{}.Call(pg)
}

// SetMediaColorScheme emulates a preferred color scheme.
// Valid values: "dark", "light", "no-preference".
func SetMediaColorScheme(pg *rod.Page, colorScheme string) error {
	return proto.EmulationSetEmulatedMedia{
		Features: []*proto.EmulationMediaFeature{
			{Name: "prefers-color-scheme", Value: colorScheme},
		},
	}.Call(pg)
}

// SetTimezone overrides the browser's timezone.
// Pass an IANA timezone ID like "America/New_York" or "Europe/London".
// Pass empty string to clear the override.
func SetTimezone(pg *rod.Page, timezoneID string) error {
	return proto.EmulationSetTimezoneOverride{
		TimezoneID: timezoneID,
	}.Call(pg)
}

// SetLocale overrides the browser's locale.
// Pass a BCP 47 locale string like "en-US" or "fr-FR".
// Pass empty string to clear the override.
func SetLocale(pg *rod.Page, locale string) error {
	return proto.EmulationSetLocaleOverride{
		Locale: locale,
	}.Call(pg)
}

// DeviceResult is returned after applying device emulation.
type DeviceResult struct {
	Title     string  `json:"title"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	DPR       float64 `json:"devicePixelRatio"`
	Mobile    bool    `json:"mobile"`
	UserAgent string  `json:"userAgent"`
}

// SetDevice emulates a specific device by name. Returns the applied
// device settings. Use landscape=true for landscape orientation.
func SetDevice(pg *rod.Page, name string, landscape bool) (*DeviceResult, error) {
	device, ok := findDevice(name)
	if !ok {
		return nil, fmt.Errorf("unknown device %q — available: %s", name, availableDeviceNames())
	}

	if landscape {
		device = device.Landscape()
	}

	if err := pg.Emulate(device); err != nil {
		return nil, fmt.Errorf("failed to emulate device: %w", err)
	}

	metrics := device.MetricsEmulation()
	ua := device.UserAgentEmulation()

	return &DeviceResult{
		Title:     device.Title,
		Width:     metrics.Width,
		Height:    metrics.Height,
		DPR:       metrics.DeviceScaleFactor,
		Mobile:    metrics.Mobile,
		UserAgent: ua.UserAgent,
	}, nil
}

// ClearDevice removes device emulation, restoring default viewport.
func ClearDevice(pg *rod.Page) error {
	return pg.Emulate(devices.Clear)
}

// deviceMap maps lowercase display names to device presets.
var deviceMap = buildDeviceMap()

func buildDeviceMap() map[string]devices.Device {
	all := []devices.Device{
		devices.IPhone4,
		devices.IPhone5orSE,
		devices.IPhone6or7or8,
		devices.IPhone6or7or8Plus,
		devices.IPhoneX,
		devices.BlackBerryZ30,
		devices.Nexus4,
		devices.Nexus5,
		devices.Nexus5X,
		devices.Nexus6,
		devices.Nexus6P,
		devices.Pixel2,
		devices.Pixel2XL,
		devices.LGOptimusL70,
		devices.NokiaN9,
		devices.NokiaLumia520,
		devices.MicrosoftLumia550,
		devices.MicrosoftLumia950,
		devices.GalaxySIII,
		devices.GalaxyS5,
		devices.JioPhone2,
		devices.KindleFireHDX,
		devices.IPadMini,
		devices.IPad,
		devices.IPadPro,
		devices.BlackberryPlayBook,
		devices.Nexus10,
		devices.Nexus7,
		devices.GalaxyNote3,
		devices.GalaxyNoteII,
		devices.LaptopWithTouch,
		devices.LaptopWithHiDPIScreen,
		devices.LaptopWithMDPIScreen,
		devices.MotoG4,
		devices.SurfaceDuo,
		devices.GalaxyFold,
	}

	m := make(map[string]devices.Device, len(all)*2)
	for _, d := range all {
		m[strings.ToLower(d.Title)] = d
	}
	return m
}

func findDevice(name string) (devices.Device, bool) {
	d, ok := deviceMap[strings.ToLower(name)]
	return d, ok
}

func availableDeviceNames() string {
	names := make([]string, 0, len(deviceMap))
	// Use the ordered list for deterministic output
	ordered := []devices.Device{
		devices.IPhone4, devices.IPhone5orSE, devices.IPhone6or7or8,
		devices.IPhone6or7or8Plus, devices.IPhoneX,
		devices.IPad, devices.IPadMini, devices.IPadPro,
		devices.Pixel2, devices.Pixel2XL,
		devices.GalaxyS5, devices.GalaxySIII, devices.GalaxyNote3,
		devices.GalaxyNoteII, devices.GalaxyFold,
		devices.Nexus4, devices.Nexus5, devices.Nexus5X,
		devices.Nexus6, devices.Nexus6P, devices.Nexus7, devices.Nexus10,
		devices.MotoG4, devices.SurfaceDuo,
		devices.KindleFireHDX, devices.BlackBerryZ30, devices.BlackberryPlayBook,
		devices.LaptopWithTouch, devices.LaptopWithHiDPIScreen, devices.LaptopWithMDPIScreen,
	}
	for _, d := range ordered {
		names = append(names, d.Title)
	}
	return strings.Join(names, ", ")
}
