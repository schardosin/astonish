package entstore

import (
	"context"
	"fmt"

	personalent "github.com/schardosin/astonish/ent/personal"
	personalapp "github.com/schardosin/astonish/ent/personal/app"
	teament "github.com/schardosin/astonish/ent/team"
	teamapp "github.com/schardosin/astonish/ent/team/app"
	"github.com/schardosin/astonish/pkg/store"
)

func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func getSessionPinPersonal(ctx context.Context, client *personalent.Client, sessionID string) (*store.SessionPin, error) {
	ent, err := client.Session.Get(ctx, sessionID)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, fmt.Errorf("session pin: session %q not found: %w", sessionID, err)
		}
		return nil, fmt.Errorf("get session pin: %w", err)
	}
	return &store.SessionPin{
		Provider: derefOrEmpty(ent.ProviderName),
		Model:    derefOrEmpty(ent.ModelName),
	}, nil
}

func setSessionPinPersonal(ctx context.Context, client *personalent.Client, sessionID, provider, model string) error {
	upd := client.Session.UpdateOneID(sessionID)
	if provider == "" {
		upd = upd.ClearProviderName()
	} else {
		upd = upd.SetProviderName(provider)
	}
	if model == "" {
		upd = upd.ClearModelName()
	} else {
		upd = upd.SetModelName(model)
	}
	if err := upd.Exec(ctx); err != nil {
		if personalent.IsNotFound(err) {
			return fmt.Errorf("set session pin: session %q not found: %w", sessionID, err)
		}
		return fmt.Errorf("set session pin: %w", err)
	}
	return nil
}

func getSessionPinTeam(ctx context.Context, client *teament.Client, sessionID string) (*store.SessionPin, error) {
	ent, err := client.Session.Get(ctx, sessionID)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("session pin: session %q not found: %w", sessionID, err)
		}
		return nil, fmt.Errorf("get session pin: %w", err)
	}
	return &store.SessionPin{
		Provider: derefOrEmpty(ent.ProviderName),
		Model:    derefOrEmpty(ent.ModelName),
	}, nil
}

func setSessionPinTeam(ctx context.Context, client *teament.Client, sessionID, provider, model string) error {
	upd := client.Session.UpdateOneID(sessionID)
	if provider == "" {
		upd = upd.ClearProviderName()
	} else {
		upd = upd.SetProviderName(provider)
	}
	if model == "" {
		upd = upd.ClearModelName()
	} else {
		upd = upd.SetModelName(model)
	}
	if err := upd.Exec(ctx); err != nil {
		if teament.IsNotFound(err) {
			return fmt.Errorf("set session pin: session %q not found: %w", sessionID, err)
		}
		return fmt.Errorf("set session pin: %w", err)
	}
	return nil
}

func getAppPinPersonal(ctx context.Context, client *personalent.Client, slug string) (*store.AppPin, error) {
	ent, err := client.App.Query().Where(personalapp.SlugEQ(slug)).Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, fmt.Errorf("app pin: app %q not found: %w", slug, err)
		}
		return nil, fmt.Errorf("get app pin: %w", err)
	}
	return &store.AppPin{
		Provider: derefOrEmpty(ent.ProviderName),
		Model:    derefOrEmpty(ent.ModelName),
	}, nil
}

func setAppPinPersonal(ctx context.Context, client *personalent.Client, slug, provider, model string) error {
	ent, err := client.App.Query().Where(personalapp.SlugEQ(slug)).Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return fmt.Errorf("set app pin: app %q not found: %w", slug, err)
		}
		return fmt.Errorf("set app pin: query: %w", err)
	}
	upd := ent.Update()
	if provider == "" {
		upd = upd.ClearProviderName()
	} else {
		upd = upd.SetProviderName(provider)
	}
	if model == "" {
		upd = upd.ClearModelName()
	} else {
		upd = upd.SetModelName(model)
	}
	if err := upd.Exec(ctx); err != nil {
		return fmt.Errorf("set app pin: %w", err)
	}
	return nil
}

func getAppPinTeam(ctx context.Context, client *teament.Client, slug string) (*store.AppPin, error) {
	ent, err := client.App.Query().Where(teamapp.SlugEQ(slug)).Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, fmt.Errorf("app pin: app %q not found: %w", slug, err)
		}
		return nil, fmt.Errorf("get app pin: %w", err)
	}
	return &store.AppPin{
		Provider: derefOrEmpty(ent.ProviderName),
		Model:    derefOrEmpty(ent.ModelName),
	}, nil
}

func setAppPinTeam(ctx context.Context, client *teament.Client, slug, provider, model string) error {
	ent, err := client.App.Query().Where(teamapp.SlugEQ(slug)).Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return fmt.Errorf("set app pin: app %q not found: %w", slug, err)
		}
		return fmt.Errorf("set app pin: query: %w", err)
	}
	upd := ent.Update()
	if provider == "" {
		upd = upd.ClearProviderName()
	} else {
		upd = upd.SetProviderName(provider)
	}
	if model == "" {
		upd = upd.ClearModelName()
	} else {
		upd = upd.SetModelName(model)
	}
	if err := upd.Exec(ctx); err != nil {
		return fmt.Errorf("set app pin: %w", err)
	}
	return nil
}
