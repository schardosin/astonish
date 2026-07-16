package drill

import (
	"context"
	"fmt"
	"strings"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
	"github.com/SAP/astonish/pkg/fleet"
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
)

// InjectionSpec is a credentials map + injection manifest for drill suites.
type InjectionSpec struct {
	OwnerKey    string // suite name or plan key (audit)
	Credentials map[string]string
	Injection   *fleet.CredentialInjection
}

// HasWork reports whether any env or file injection is declared.
func (s *InjectionSpec) HasWork() bool {
	if s == nil || s.Injection == nil {
		return false
	}
	return len(s.Injection.Env) > 0 || len(s.Injection.Files) > 0
}

// SuiteInjectionToFleet converts suite YAML injection types to fleet types.
func SuiteInjectionToFleet(inj *config.SuiteCredentialInjection) *fleet.CredentialInjection {
	if inj == nil {
		return nil
	}
	out := &fleet.CredentialInjection{
		Env:   make([]fleet.EnvInjection, 0, len(inj.Env)),
		Files: make([]fleet.FileInjection, 0, len(inj.Files)),
	}
	for _, e := range inj.Env {
		out.Env = append(out.Env, fleet.EnvInjection{
			Credential: e.Credential,
			Var:        e.Var,
			Field:      e.Field,
		})
	}
	for _, f := range inj.Files {
		out.Files = append(out.Files, fleet.FileInjection{
			Credential: f.Credential,
			Path:       f.Path,
			Format:     f.Format,
			Field:      f.Field,
			Mode:       f.Mode,
		})
	}
	if len(out.Env) == 0 && len(out.Files) == 0 {
		return nil
	}
	return out
}

// SpecFromSuite builds an injection spec from suite_config when declared.
func SpecFromSuite(suiteName string, sc *config.DrillSuiteConfig) (*InjectionSpec, error) {
	if sc == nil {
		return nil, nil
	}
	inj := SuiteInjectionToFleet(sc.CredentialInjection)
	if len(sc.Credentials) == 0 && inj == nil {
		return nil, nil
	}
	if inj == nil {
		inj = &fleet.CredentialInjection{}
	}
	inj = fleet.NormalizeCredentialInjection(sc.Credentials, inj)
	if err := fleet.ValidateCredentialInjectionSpec(sc.Credentials, inj); err != nil {
		return nil, err
	}
	if inj == nil || (len(inj.Env) == 0 && len(inj.Files) == 0) {
		return nil, nil
	}
	owner := suiteName
	if owner == "" {
		owner = "drill-suite"
	}
	return &InjectionSpec{
		OwnerKey:    owner,
		Credentials: sc.Credentials,
		Injection:   inj,
	}, nil
}

// SpecFromFleetPlan builds an injection spec from a fleet plan (fallback).
func SpecFromFleetPlan(plan *fleet.FleetPlan) *InjectionSpec {
	if plan == nil || len(plan.Credentials) == 0 {
		return nil
	}
	inj := plan.EffectiveCredentialInjection()
	if len(inj.Env) == 0 && len(inj.Files) == 0 {
		return nil
	}
	cp := inj
	return &InjectionSpec{
		OwnerKey:    plan.Key,
		Credentials: plan.Credentials,
		Injection:   &cp,
	}
}

// LookupFleetPlanInjection finds a plan by suite name, then by matching template.
func LookupFleetPlanInjection(ctx context.Context, planStore store.FleetPlanStore, suiteName, suiteTemplate string) *InjectionSpec {
	if planStore == nil {
		return nil
	}
	suiteName = strings.TrimSpace(suiteName)
	suiteTemplate = normalizeTemplateName(suiteTemplate)

	if suiteName != "" {
		if raw, ok := planStore.GetPlan(ctx, suiteName); ok {
			if plan, ok := raw.(*fleet.FleetPlan); ok {
				if spec := SpecFromFleetPlan(plan); spec != nil {
					return spec
				}
			}
		}
	}
	if suiteTemplate == "" {
		return nil
	}
	for _, summary := range planStore.ListPlans(ctx) {
		raw, ok := planStore.GetPlan(ctx, summary.Key)
		if !ok {
			continue
		}
		plan, ok := raw.(*fleet.FleetPlan)
		if !ok || plan == nil {
			continue
		}
		if normalizeTemplateName(plan.Template) == suiteTemplate {
			if spec := SpecFromFleetPlan(plan); spec != nil {
				return spec
			}
		}
	}
	return nil
}

func normalizeTemplateName(name string) string {
	name = strings.TrimSpace(name)
	return strings.TrimPrefix(name, "@")
}

// ResolveInjectionSpec prefers suite-declared injection, else fleet plan fallback.
func ResolveInjectionSpec(ctx context.Context, suiteName string, sc *config.DrillSuiteConfig, planStore store.FleetPlanStore) (*InjectionSpec, error) {
	spec, err := SpecFromSuite(suiteName, sc)
	if err != nil {
		return nil, err
	}
	if spec != nil && spec.HasWork() {
		return spec, nil
	}
	template := ""
	if sc != nil {
		template = sc.Template
	}
	return LookupFleetPlanInjection(ctx, planStore, suiteName, template), nil
}

// InjectionTarget describes where to apply env/files for a drill sandbox.
type InjectionTarget struct {
	SessionID  string
	LazyClient *sandbox.LazyNodeClient
	Backend    sandbox.Backend // OpenShell/K8s PushFile path
	// ExecIncus writes files via shell when Backend.PushFile is unavailable (Incus).
	ExecIncus func(command []string, env map[string]string) (stdout, stderr []byte, exitCode int, err error)
}

// ApplyCredentialInjection resolves secrets and materializes env+files into the sandbox.
// Fail-closed: returns an error if the spec declares work but credentials cannot be resolved
// or materialization fails.
func ApplyCredentialInjection(ctx context.Context, spec *InjectionSpec, cs store.CredentialStore, fileStore *credentials.Store, target InjectionTarget) (env map[string]string, err error) {
	if spec == nil || !spec.HasWork() {
		return nil, nil
	}
	if err := fleet.ValidateCredentialInjectionSpec(spec.Credentials, spec.Injection); err != nil {
		return nil, err
	}

	var resolved map[string]*fleet.ResolvedCredential
	switch {
	case cs != nil:
		resolved, err = fleet.ResolveCredentialsPlatformMap(ctx, spec.Credentials, cs)
	case fileStore != nil:
		resolved, err = fleet.ResolveCredentialsMap(spec.Credentials, fileStore)
	default:
		return nil, fmt.Errorf("credential store is not available; cannot inject suite credentials")
	}
	if err != nil {
		return nil, err
	}

	env, err = fleet.BuildInjectionEnvFromSpec(ctx, spec.Credentials, spec.Injection, resolved, cs, spec.OwnerKey)
	if err != nil {
		return nil, err
	}

	if target.LazyClient != nil && len(env) > 0 {
		if target.LazyClient.Env == nil {
			target.LazyClient.Env = map[string]string{}
		}
		for k, v := range env {
			target.LazyClient.Env[k] = v
		}
		if target.LazyClient.IsInitialized() {
			if restartErr := target.LazyClient.RestartNode(); restartErr != nil {
				// Env is still set for the next node start; file injection can proceed.
				_ = restartErr
			}
		}
	}

	if len(spec.Injection.Files) == 0 {
		return env, nil
	}

	switch {
	case target.Backend != nil && target.SessionID != "":
		err = fleet.MaterializeInjectionFilesFromSpec(ctx, target.Backend, target.SessionID, spec.Credentials, spec.Injection, resolved, cs, spec.OwnerKey)
	case target.ExecIncus != nil:
		err = fleet.MaterializeInjectionFilesIncusFromSpec(ctx, target.ExecIncus, spec.Credentials, spec.Injection, resolved, cs, spec.OwnerKey)
	default:
		err = fmt.Errorf("credential_injection.files: no sandbox target available to materialize files")
	}
	if err != nil {
		return env, err
	}
	return env, nil
}
