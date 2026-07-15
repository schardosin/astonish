package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	teament "github.com/SAP/astonish/ent/team"
	"github.com/SAP/astonish/ent/team/fleetsetupprofile"
	"github.com/SAP/astonish/pkg/fleet"
	"github.com/SAP/astonish/pkg/store"
)

type teamFleetSetupProfileStore struct {
	client *teament.Client
}

var _ store.FleetSetupProfileStore = (*teamFleetSetupProfileStore)(nil)

func (s *teamFleetSetupProfileStore) GetProfile(ctx context.Context, key string) (any, bool) {
	if bundled, err := fleet.LoadBundledSetupProfiles(); err == nil {
		if p, ok := bundled[key]; ok {
			return p, true
		}
	}
	ent, err := s.client.FleetSetupProfile.Query().
		Where(fleetsetupprofile.KeyEQ(key)).
		Only(ctx)
	if err != nil {
		return nil, false
	}
	p, err := profileDefinitionToSetupProfile(ent.Definition)
	if err != nil {
		return ent.Definition, true
	}
	return p, true
}

func (s *teamFleetSetupProfileStore) ListProfiles(ctx context.Context) []store.FleetSetupProfileSummary {
	summaries, _ := fleet.SetupProfileSummaries()
	out := make([]store.FleetSetupProfileSummary, 0, len(summaries))
	bundledKeys := fleet.BundledSetupProfileKeys()
	for _, s := range summaries {
		out = append(out, store.FleetSetupProfileSummary{
			Key: s.Key, Name: s.Name, Description: s.Description,
			Domain: s.Domain, StepCount: s.StepCount, Source: s.Source,
		})
	}
	profiles, err := s.client.FleetSetupProfile.Query().
		Order(fleetsetupprofile.ByKey()).
		All(ctx)
	if err != nil {
		return out
	}
	for _, p := range profiles {
		if _, isBundled := bundledKeys[p.Key]; isBundled {
			continue
		}
		summary := store.FleetSetupProfileSummary{
			Key: p.Key, Name: p.Name, Source: "custom",
		}
		if desc, ok := p.Definition["description"].(string); ok {
			summary.Description = desc
		}
		if domain, ok := p.Definition["domain"].(string); ok {
			summary.Domain = domain
		}
		if steps, ok := p.Definition["steps"].([]any); ok {
			summary.StepCount = len(steps)
		}
		out = append(out, summary)
	}
	return out
}

func (s *teamFleetSetupProfileStore) Save(ctx context.Context, key string, profile any) error {
	if fleet.IsBundledSetupProfileKey(key) {
		return store.ErrBundledSetupProfileImmutable
	}
	definition, err := toMapAny(profile)
	if err != nil {
		return fmt.Errorf("entstore: FleetSetupProfileStore.Save: %w", err)
	}
	name := key
	if n, ok := definition["name"].(string); ok && n != "" {
		name = n
	}
	n, err := s.client.FleetSetupProfile.Update().
		Where(fleetsetupprofile.KeyEQ(key)).
		SetName(name).
		SetDefinition(definition).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FleetSetupProfileStore.Save: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.FleetSetupProfile.Create().
			SetKey(key).
			SetName(name).
			SetDefinition(definition).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: FleetSetupProfileStore.Save: create: %w", err)
		}
	}
	return nil
}

func (s *teamFleetSetupProfileStore) Delete(ctx context.Context, key string) error {
	if fleet.IsBundledSetupProfileKey(key) {
		return store.ErrBundledSetupProfileImmutable
	}
	_, err := s.client.FleetSetupProfile.Delete().
		Where(fleetsetupprofile.KeyEQ(key)).
		Exec(ctx)
	return err
}

type teamFleetSetupDraftStore struct {
	client *teament.Client
}

var _ store.FleetSetupDraftStore = (*teamFleetSetupDraftStore)(nil)

func (s *teamFleetSetupDraftStore) Create(ctx context.Context, draft *store.FleetSetupDraft) error {
	if draft == nil {
		return fmt.Errorf("draft is nil")
	}
	id, err := uuid.Parse(draft.ID)
	if err != nil {
		id = uuid.New()
		draft.ID = id.String()
	}
	collected := draft.Collected
	if collected == nil {
		collected = map[string]any{}
	}
	created, err := s.client.FleetSetupDraft.Create().
		SetID(id).
		SetTemplateKey(draft.TemplateKey).
		SetSetupProfileKey(draft.SetupProfileKey).
		SetCollected(collected).
		SetCurrentStep(draft.CurrentStep).
		Save(ctx)
	if err != nil {
		return err
	}
	draft.CreatedAt = created.CreatedAt.Format(time.RFC3339)
	draft.UpdatedAt = created.UpdatedAt.Format(time.RFC3339)
	return nil
}

func (s *teamFleetSetupDraftStore) Get(ctx context.Context, id string) (*store.FleetSetupDraft, bool) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, false
	}
	ent, err := s.client.FleetSetupDraft.Get(ctx, uid)
	if err != nil {
		return nil, false
	}
	return entToSetupDraft(ent), true
}

func (s *teamFleetSetupDraftStore) Update(ctx context.Context, draft *store.FleetSetupDraft) error {
	if draft == nil {
		return fmt.Errorf("draft is nil")
	}
	uid, err := uuid.Parse(draft.ID)
	if err != nil {
		return fmt.Errorf("invalid draft id: %w", err)
	}
	collected := draft.Collected
	if collected == nil {
		collected = map[string]any{}
	}
	_, err = s.client.FleetSetupDraft.UpdateOneID(uid).
		SetCollected(collected).
		SetCurrentStep(draft.CurrentStep).
		Save(ctx)
	return err
}

func (s *teamFleetSetupDraftStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	return s.client.FleetSetupDraft.DeleteOneID(uid).Exec(ctx)
}

func entToSetupDraft(ent *teament.FleetSetupDraft) *store.FleetSetupDraft {
	d := &store.FleetSetupDraft{
		ID:              ent.ID.String(),
		TemplateKey:     ent.TemplateKey,
		SetupProfileKey: ent.SetupProfileKey,
		Collected:       ent.Collected,
		CreatedAt:       ent.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       ent.UpdatedAt.Format(time.RFC3339),
	}
	if ent.CurrentStep != "" {
		d.CurrentStep = ent.CurrentStep
	}
	return d
}

// profileDefinitionToSetupProfile converts stored JSON to SetupProfile when possible.
func profileDefinitionToSetupProfile(def map[string]any) (*fleet.SetupProfile, error) {
	data, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	return fleet.ParseSetupProfileYAML(data)
}
