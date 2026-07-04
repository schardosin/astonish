package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/platformnetworkpolicy"
	"github.com/schardosin/astonish/pkg/store"
)

// platformNetworkPolicyStore implements store.NetworkPolicyStore using the platform Ent client.
type platformNetworkPolicyStore struct {
	client *platforment.Client
}

// PlatformNetworkPolicies returns the platform-wide network policy store.
func (s *Store) PlatformNetworkPolicies() store.NetworkPolicyStore {
	return &platformNetworkPolicyStore{client: s.platformClient}
}

var _ store.NetworkPolicyStore = (*platformNetworkPolicyStore)(nil)

func (s *platformNetworkPolicyStore) List(ctx context.Context) ([]store.NetworkPolicyRule, error) {
	rows, err := s.client.PlatformNetworkPolicy.Query().
		Order(platformnetworkpolicy.ByHost()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: PlatformNetworkPolicyStore.List: %w", err)
	}
	result := make([]store.NetworkPolicyRule, len(rows))
	for i, row := range rows {
		result[i] = platformEntNetworkPolicyToStore(row)
	}
	return result, nil
}

func (s *platformNetworkPolicyStore) Get(ctx context.Context, id string) (*store.NetworkPolicyRule, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("entstore: PlatformNetworkPolicyStore.Get: invalid id: %w", err)
	}
	row, err := s.client.PlatformNetworkPolicy.Get(ctx, uid)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entstore: PlatformNetworkPolicyStore.Get: %w", err)
	}
	r := platformEntNetworkPolicyToStore(row)
	return &r, nil
}

func (s *platformNetworkPolicyStore) Save(ctx context.Context, rule *store.NetworkPolicyRule) error {
	action := platformnetworkpolicy.ActionAllow
	if rule.Action == store.NetworkPolicyDeny {
		action = platformnetworkpolicy.ActionDeny
	}

	n, err := s.client.PlatformNetworkPolicy.Update().
		Where(
			platformnetworkpolicy.HostEQ(rule.Host),
			platformnetworkpolicy.PortEQ(rule.Port),
		).
		SetAction(action).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: PlatformNetworkPolicyStore.Save: update: %w", err)
	}
	if n == 0 {
		create := s.client.PlatformNetworkPolicy.Create().
			SetHost(rule.Host).
			SetPort(rule.Port).
			SetAction(action)
		if rule.CreatedBy != "" {
			create.SetCreatedBy(rule.CreatedBy)
		}
		_, err = create.Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: PlatformNetworkPolicyStore.Save: create: %w", err)
		}
	}
	return nil
}

func (s *platformNetworkPolicyStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("entstore: PlatformNetworkPolicyStore.Delete: invalid id: %w", err)
	}
	err = s.client.PlatformNetworkPolicy.DeleteOneID(uid).Exec(ctx)
	if err != nil && !platforment.IsNotFound(err) {
		return fmt.Errorf("entstore: PlatformNetworkPolicyStore.Delete: %w", err)
	}
	return nil
}

func platformEntNetworkPolicyToStore(row *platforment.PlatformNetworkPolicy) store.NetworkPolicyRule {
	action := store.NetworkPolicyAllow
	if row.Action == platformnetworkpolicy.ActionDeny {
		action = store.NetworkPolicyDeny
	}
	createdBy := ""
	if row.CreatedBy != nil {
		createdBy = *row.CreatedBy
	}
	return store.NetworkPolicyRule{
		ID:        row.ID.String(),
		Host:      row.Host,
		Port:      row.Port,
		Action:    action,
		CreatedBy: createdBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
