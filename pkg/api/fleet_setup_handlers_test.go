package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/fleet"
)

func TestCloneFleetSetupProfileHandler_CreatesCustomCopy(t *testing.T) {
	fromKey := "generic"
	newKey := "generic-copy-test"
	reqBody, _ := json.Marshal(map[string]string{"new_key": newKey, "name": "Generic Copy"})
	req := httptest.NewRequest(http.MethodPost, "/api/fleet-setup-profiles/"+fromKey+"/clone", bytes.NewReader(reqBody))
	req = mux.SetURLVars(req, map[string]string{"key": fromKey})
	rr := httptest.NewRecorder()

	CloneFleetSetupProfileHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["key"] != newKey {
		t.Fatalf("key = %q, want %q", resp["key"], newKey)
	}

	profileStore := getSetupProfileStore(nil)
	cloneAny, ok := profileStore.GetProfile(req.Context(), newKey)
	if !ok {
		t.Fatal("cloned profile not found")
	}
	clone, err := normalizeSetupProfile(cloneAny)
	if err != nil {
		t.Fatal(err)
	}
	if clone.Key != newKey {
		t.Fatalf("clone.Key = %q", clone.Key)
	}
	if clone.Name != "Generic Copy" {
		t.Fatalf("clone.Name = %q", clone.Name)
	}

	_ = profileStore.Delete(req.Context(), newKey)
}

func TestSaveFleetSetupProfileYAMLHandler_RejectsBundled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/fleet-setup-profiles/generic/yaml", strings.NewReader("key: generic\nname: x\nsteps: []"))
	req = mux.SetURLVars(req, map[string]string{"key": "generic"})
	rr := httptest.NewRecorder()

	SaveFleetSetupProfileYAMLHandler(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestSetupProfileValidate(t *testing.T) {
	p := fleet.DefaultSetupProfileTemplate("my-profile", "My Profile")
	if err := p.Validate(); err != nil {
		t.Fatalf("valid template: %v", err)
	}
}
