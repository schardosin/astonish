package api

import (
	"net/http"
	"testing"
	"time"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name:    "standard WebSocket upgrade",
			headers: map[string]string{"Connection": "Upgrade", "Upgrade": "websocket"},
			want:    true,
		},
		{
			name:    "case insensitive connection",
			headers: map[string]string{"Connection": "upgrade", "Upgrade": "websocket"},
			want:    true,
		},
		{
			name:    "case insensitive upgrade",
			headers: map[string]string{"Connection": "Upgrade", "Upgrade": "WebSocket"},
			want:    true,
		},
		{
			name:    "multi-value connection header",
			headers: map[string]string{"Connection": "keep-alive, Upgrade", "Upgrade": "websocket"},
			want:    true,
		},
		{
			name:    "no connection header",
			headers: map[string]string{"Upgrade": "websocket"},
			want:    false,
		},
		{
			name:    "no upgrade header",
			headers: map[string]string{"Connection": "Upgrade"},
			want:    false,
		},
		{
			name:    "wrong upgrade protocol",
			headers: map[string]string{"Connection": "Upgrade", "Upgrade": "h2c"},
			want:    false,
		},
		{
			name:    "empty headers",
			headers: map[string]string{},
			want:    false,
		},
		{
			name:    "connection without upgrade token",
			headers: map[string]string{"Connection": "keep-alive", "Upgrade": "websocket"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "http://localhost/ws", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := isWebSocketUpgrade(r); got != tt.want {
				t.Errorf("isWebSocketUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPCache(t *testing.T) {
	// Clean the cache
	ipCache.Range(func(key, _ any) bool {
		ipCache.Delete(key)
		return true
	})

	containerName := "test-container-xyz"

	// Store an entry manually
	ipCache.Store(containerName, &ipCacheEntry{
		ip:     "10.100.0.5",
		expiry: time.Now().Add(30 * time.Second),
	})

	// Retrieve it
	if entry, ok := ipCache.Load(containerName); !ok {
		t.Error("expected to find cached IP")
	} else {
		cached := entry.(*ipCacheEntry)
		if cached.ip != "10.100.0.5" {
			t.Errorf("cached IP = %q, want 10.100.0.5", cached.ip)
		}
	}

	// Invalidate it
	InvalidateIPCache(containerName)

	if _, ok := ipCache.Load(containerName); ok {
		t.Error("expected cache entry to be invalidated")
	}
}

func TestIPCacheExpiry(t *testing.T) {
	// Clean the cache
	ipCache.Range(func(key, _ any) bool {
		ipCache.Delete(key)
		return true
	})

	containerName := "test-container-expired"

	// Store an entry that is already expired
	ipCache.Store(containerName, &ipCacheEntry{
		ip:     "10.100.0.99",
		expiry: time.Now().Add(-1 * time.Second),
	})

	if entry, ok := ipCache.Load(containerName); ok {
		cached := entry.(*ipCacheEntry)
		if time.Now().After(cached.expiry) {
			ipCache.Delete(containerName)
		}
	}

	if _, ok := ipCache.Load(containerName); ok {
		t.Error("expected expired cache entry to be cleaned up")
	}
}

func TestPortProxyManagerSingleton(t *testing.T) {
	mgr1 := GetPortProxyManager()
	mgr2 := GetPortProxyManager()
	if mgr1 != mgr2 {
		t.Error("GetPortProxyManager should return the same instance")
	}
}

func TestPortProxyManagerGetHostPortNotRunning(t *testing.T) {
	mgr := GetPortProxyManager()
	// Port that was never started should return 0
	hp := mgr.GetHostPort("nonexistent-container", 3000)
	if hp != 0 {
		t.Errorf("GetHostPort for non-running proxy returned %d, want 0", hp)
	}
}

func TestPortProxyManagerListForContainer(t *testing.T) {
	mgr := GetPortProxyManager()
	// Container with no active proxies should return empty map
	result := mgr.ListForContainer("nonexistent-container")
	if len(result) != 0 {
		t.Errorf("ListForContainer for unknown container returned %d entries, want 0", len(result))
	}
}

func TestPortProxyManagerStopNonexistent(t *testing.T) {
	mgr := GetPortProxyManager()
	stopped := mgr.StopProxy("nonexistent-container", 3000)
	if stopped {
		t.Error("StopProxy should return false for non-running proxy")
	}
}

func TestPortProxyManagerStopAllNonexistent(t *testing.T) {
	mgr := GetPortProxyManager()
	count := mgr.StopAllForContainer("nonexistent-container")
	if count != 0 {
		t.Errorf("StopAllForContainer returned %d, want 0", count)
	}
}

func TestProxyKey(t *testing.T) {
	key := proxyKey("astn-sess-abc123", 3000)
	if key != "astn-sess-abc123:3000" {
		t.Errorf("proxyKey = %q, want %q", key, "astn-sess-abc123:3000")
	}
}
