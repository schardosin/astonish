package drill

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

// LoadSuiteFromStore constructs a LoadedSuite from a team-scoped FlowStore.
// It fetches the suite YAML and all child drills, matching the shape used by
// filesystem discovery (FindSuite) so callers can use BuildSuiteContext, etc.
func LoadSuiteFromStore(fs store.FlowStore, ctx context.Context, suiteName string) (*LoadedSuite, error) {
	if fs == nil {
		return nil, fmt.Errorf("suite %q not found: flow store unavailable", suiteName)
	}
	suiteYAML, err := fs.GetFlow(ctx, suiteName)
	if err != nil {
		return nil, fmt.Errorf("suite %q not found in store: %w", suiteName, err)
	}

	var suiteCfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(suiteYAML), &suiteCfg); err != nil {
		return nil, fmt.Errorf("failed to parse suite %q: %w", suiteName, err)
	}

	if suiteCfg.Type != "drill_suite" && suiteCfg.Type != "test_suite" {
		return nil, fmt.Errorf("%q has type %q, expected drill_suite", suiteName, suiteCfg.Type)
	}

	suite := &LoadedSuite{
		Name:   suiteName,
		Config: &suiteCfg,
	}

	drillFlows := fs.ListFlowsByType(ctx, []string{"drill", "test"})
	for _, d := range drillFlows {
		if d.Suite != suiteName {
			continue
		}
		drillYAML, dErr := fs.GetFlow(ctx, d.Name)
		if dErr != nil {
			continue
		}
		var drillCfg config.AgentConfig
		if yaml.Unmarshal([]byte(drillYAML), &drillCfg) != nil {
			continue
		}
		suite.Tests = append(suite.Tests, LoadedTest{
			Name:   d.Name,
			Config: &drillCfg,
		})
	}

	return suite, nil
}
