package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	orgent "github.com/schardosin/astonish/ent/org"
	"github.com/schardosin/astonish/ent/org/orgnetworkpolicy"
	"github.com/schardosin/astonish/pkg/store"
)

// orgNetworkPolicyStore implements store.NetworkPolicyStore using the Ent org client.
type orgNetworkPolicyStore struct {
	client *orgent.Client
}

var _ store.NetworkPolicyStore = (*orgNetworkPolicyStore)(nil)

func (s *orgNetworkPolicyStore) List(ctx context.Context) ([]store.NetworkPolicyRule, error) {
	rows, err := s.client.OrgNetworkPolicy.Query().
		Order(orgnetworkpolicy.ByHost()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: OrgNetworkPolicyStore.List: %w", err)
	}
	result := make([]store.NetworkPolicyRule, len(rows))
	for i, row := range rows {
		result[i] = orgEntNetworkPolicyToStore(row)
	}
	return result, nil
}

func (s *orgNetworkPolicyStore) Get(ctx context.Context, id string) (*store.NetworkPolicyRule, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("entstore: OrgNetworkPolicyStore.Get: invalid id: %w", err)
	}
	row, err := s.client.OrgNetworkPolicy.Get(ctx, uid)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entstore: OrgNetworkPolicyStore.Get: %w", err)
	}
	r := orgEntNetworkPolicyToStore(row)
	return &r, nil
}

func (s *orgNetworkPolicyStore) Save(ctx context.Context, rule *store.NetworkPolicyRule) error {
	createdBy := uuid.Nil
	if rule.CreatedBy != "" {
		if id, err := uuid.Parse(rule.CreatedBy); err == nil {
			createdBy = id
		}
	}

	action := orgnetworkpolicy.ActionAllow
	if rule.Action == store.NetworkPolicyDeny {
		action = orgnetworkpolicy.ActionDeny
	}

	n, err := s.client.OrgNetworkPolicy.Update().
		Where(
			orgnetworkpolicy.HostEQ(rule.Host),
			orgnetworkpolicy.PortEQ(rule.Port),
		).
		SetAction(action).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: OrgNetworkPolicyStore.Save: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.OrgNetworkPolicy.Create().
			SetHost(rule.Host).
			SetPort(rule.Port).
			SetAction(action).
			SetCreatedBy(createdBy).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: OrgNetworkPolicyStore.Save: create: %w", err)
		}
	}
	return nil
}

func (s *orgNetworkPolicyStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("entstore: OrgNetworkPolicyStore.Delete: invalid id: %w", err)
	}
	err = s.client.OrgNetworkPolicy.DeleteOneID(uid).Exec(ctx)
	if err != nil && !orgent.IsNotFound(err) {
		return fmt.Errorf("entstore: OrgNetworkPolicyStore.Delete: %w", err)
	}
	return nil
}

func orgEntNetworkPolicyToStore(row *orgent.OrgNetworkPolicy) store.NetworkPolicyRule {
	action := store.NetworkPolicyAllow
	if row.Action == orgnetworkpolicy.ActionDeny {
		action = store.NetworkPolicyDeny
	}
	return store.NetworkPolicyRule{
		ID:        row.ID.String(),
		Host:      row.Host,
		Port:      row.Port,
		Action:    action,
		CreatedBy: row.CreatedBy.String(),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
