package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/schardosin/astonish/pkg/channels"
)

// channelManager holds a reference to the active ChannelManager.
// Set by the daemon during startup via SetChannelManager.
var (
	channelManagerMu sync.RWMutex
	channelManager   *channels.ChannelManager
)

// channelReloadFn holds a callback that reloads channel configuration.
// Set by the daemon run loop via SetChannelReloadFunc.
var (
	channelReloadMu sync.RWMutex
	channelReloadFn func() error
)

// SetChannelReloadFunc registers a callback that the daemon provides to
// reload channels from the latest config. Called once during daemon startup.
func SetChannelReloadFunc(fn func() error) {
	channelReloadMu.Lock()
	defer channelReloadMu.Unlock()
	channelReloadFn = fn
}

// getChannelReloadFunc returns the registered reload callback, or nil.
func getChannelReloadFunc() func() error {
	channelReloadMu.RLock()
	defer channelReloadMu.RUnlock()
	return channelReloadFn
}

// SetChannelManager registers the active channel manager for API access.
// Called by the daemon run loop after channel initialization.
func SetChannelManager(cm *channels.ChannelManager) {
	channelManagerMu.Lock()
	defer channelManagerMu.Unlock()
	channelManager = cm
}

// GetChannelManager returns the active channel manager, or nil if not set.
func GetChannelManager() *channels.ChannelManager {
	channelManagerMu.RLock()
	defer channelManagerMu.RUnlock()
	return channelManager
}

// ChannelsStatusHandler returns the status of all registered channels.
//
// GET /api/channels/status
//
// Response:
//
//	{
//	  "channels": {
//	    "telegram": { "connected": true, "account_id": "@bot", ... }
//	  }
//	}
func ChannelsStatusHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChannelManager()

	type channelStatusResponse struct {
		Connected    bool   `json:"connected"`
		AccountID    string `json:"account_id,omitempty"`
		ConnectedAt  string `json:"connected_at,omitempty"`
		Error        string `json:"error,omitempty"`
		MessageCount int64  `json:"message_count"`
	}

	response := map[string]any{
		"channels": map[string]any{},
	}

	if cm != nil {
		statuses := cm.Status()
		channelMap := make(map[string]channelStatusResponse, len(statuses))
		for id, status := range statuses {
			csr := channelStatusResponse{
				Connected:    status.Connected,
				AccountID:    status.AccountID,
				Error:        status.Error,
				MessageCount: status.MessageCount,
			}
			if !status.ConnectedAt.IsZero() {
				csr.ConnectedAt = status.ConnectedAt.Format("2006-01-02T15:04:05Z")
			}
			channelMap[id] = csr
		}
		response["channels"] = channelMap
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ChannelsReloadHandler re-reads channel configuration and reinitializes
// channel adapters without restarting the daemon process.
//
// POST /api/channels/reload
//
// Response:
//
//	{ "status": "ok", "message": "Channels reloaded" }
func ChannelsReloadHandler(w http.ResponseWriter, r *http.Request) {
	reload := getChannelReloadFunc()
	if reload == nil {
		http.Error(w, `{"error":"reload not available"}`, http.StatusServiceUnavailable)
		return
	}

	if err := reload(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Channels reloaded",
	})
}
