package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
)

func TestPatchAppModel_Happy(t *testing.T) {
	t.Parallel()

	router := newMockTenantRouter()
	orgStore := newMockOrgDataStore()
	router.orgs["acme"] = orgStore

	personalStore := orgStore.ForUser("user-1").(*mockPersonalDataStore)
	personalStore.apps.apps["weather-app"] = map[string]any{"slug": "weather-app"}

	svc := &store.Services{Mode: store.ModePlatform, TenantRouter: router}
	pu := &PlatformUser{ID: "user-1", OrgSlug: "acme", TeamSlug: "eng", Role: "member"}

	body := patchAppModelRequest{Provider: "anthropic", Model: "claude-sonnet-4"}
	r := appSharingRequest(t, "PATCH", "/api/apps/weather-app/model", body, svc, pu)
	r = mux.SetURLVars(r, map[string]string{"name": "weather-app"})
	w := httptest.NewRecorder()

	PatchAppModelHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["pinnedProvider"] != "anthropic" {
		t.Errorf("pinnedProvider: got %v, want anthropic", resp["pinnedProvider"])
	}
	if resp["pinnedModel"] != "claude-sonnet-4" {
		t.Errorf("pinnedModel: got %v, want claude-sonnet-4", resp["pinnedModel"])
	}
	if resp["effectiveProvider"] != "anthropic" {
		t.Errorf("effectiveProvider: got %v, want anthropic (pin overrides cascade)", resp["effectiveProvider"])
	}
	if resp["effectiveModel"] != "claude-sonnet-4" {
		t.Errorf("effectiveModel: got %v, want claude-sonnet-4", resp["effectiveModel"])
	}
	pin, err := personalStore.AppPin(context.Background(), "weather-app")
	if err != nil || pin == nil {
		t.Fatalf("pin should be persisted: pin=%v err=%v", pin, err)
	}
	if pin.Provider != "anthropic" || pin.Model != "claude-sonnet-4" {
		t.Errorf("persisted pin mismatch: got %+v", pin)
	}
}

func TestPatchAppModel_UnknownSlug(t *testing.T) {
	t.Parallel()

	router := newMockTenantRouter()
	orgStore := newMockOrgDataStore()
	router.orgs["acme"] = orgStore

	svc := &store.Services{Mode: store.ModePlatform, TenantRouter: router}
	pu := &PlatformUser{ID: "user-1", OrgSlug: "acme", TeamSlug: "eng", Role: "member"}

	body := patchAppModelRequest{Provider: "anthropic", Model: "claude-sonnet-4"}
	r := appSharingRequest(t, "PATCH", "/api/apps/nonexistent/model", body, svc, pu)
	r = mux.SetURLVars(r, map[string]string{"name": "nonexistent"})
	w := httptest.NewRecorder()

	PatchAppModelHandler(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown slug, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchAppModel_ClearPin(t *testing.T) {
	t.Parallel()

	router := newMockTenantRouter()
	orgStore := newMockOrgDataStore()
	router.orgs["acme"] = orgStore

	personalStore := orgStore.ForUser("user-1").(*mockPersonalDataStore)
	personalStore.apps.apps["dashboard"] = map[string]any{"slug": "dashboard"}
	personalStore.appPins["dashboard"] = &store.AppPin{Provider: "openai", Model: "gpt-4"}

	svc := &store.Services{Mode: store.ModePlatform, TenantRouter: router}
	pu := &PlatformUser{ID: "user-1", OrgSlug: "acme", TeamSlug: "eng", Role: "member"}

	body := patchAppModelRequest{Provider: "", Model: ""}
	r := appSharingRequest(t, "PATCH", "/api/apps/dashboard/model", body, svc, pu)
	r = mux.SetURLVars(r, map[string]string{"name": "dashboard"})
	w := httptest.NewRecorder()

	PatchAppModelHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	pin, err := personalStore.AppPin(context.Background(), "dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if pin != nil {
		t.Errorf("pin should be cleared, got %+v", pin)
	}
}

func TestGetAppExposesPin(t *testing.T) {
	t.Parallel()

	router := newMockTenantRouter()
	orgStore := newMockOrgDataStore()
	router.orgs["acme"] = orgStore

	personalStore := orgStore.ForUser("user-1").(*mockPersonalDataStore)
	personalStore.apps.apps["weather-app"] = map[string]any{"slug": "weather-app", "name": "weather-app"}
	personalStore.appPins["weather-app"] = &store.AppPin{Provider: "anthropic", Model: "claude-sonnet-4"}

	svc := &store.Services{
		Mode:         store.ModePlatform,
		TenantRouter: router,
		PersonalApps: personalStore.apps,
	}
	pu := &PlatformUser{ID: "user-1", OrgSlug: "acme", TeamSlug: "eng", Role: "member"}

	r := appSharingRequest(t, "GET", "/api/apps/weather-app", nil, svc, pu)
	r = mux.SetURLVars(r, map[string]string{"name": "weather-app"})
	w := httptest.NewRecorder()

	GetAppHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["pinnedProvider"] != "anthropic" {
		t.Errorf("pinnedProvider: got %v, want anthropic", resp["pinnedProvider"])
	}
	if resp["pinnedModel"] != "claude-sonnet-4" {
		t.Errorf("pinnedModel: got %v, want claude-sonnet-4", resp["pinnedModel"])
	}
	if resp["effectiveProvider"] != "anthropic" {
		t.Errorf("effectiveProvider: got %v, want anthropic", resp["effectiveProvider"])
	}
	if resp["effectiveModel"] != "claude-sonnet-4" {
		t.Errorf("effectiveModel: got %v, want claude-sonnet-4", resp["effectiveModel"])
	}
	if resp["app"] == nil {
		t.Errorf("app field should be present alongside pin fields")
	}
}
