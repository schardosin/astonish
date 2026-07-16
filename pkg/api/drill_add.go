package api

import (
	"context"
	"fmt"
	"strings"

	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/tools"
)

// resolveDrillAddWizard loads a suite from the team-scoped FlowStore and
// builds the /drill-add wizard prompt. Drill suites live in the team store
// (not on the local filesystem), so callers must not use adrill.FindSuite.
func resolveDrillAddWizard(ctx context.Context, fs store.FlowStore, suiteName string) (suiteContext, wizardPrompt string, err error) {
	suiteName = strings.TrimSpace(suiteName)
	if suiteName == "" {
		return "", "", fmt.Errorf("Usage: /drill-add <suite_name>")
	}
	if fs == nil {
		return "", "", fmt.Errorf("drill management requires platform mode (team-scoped store not available)")
	}
	suite, err := adrill.LoadSuiteFromStore(fs, ctx, suiteName)
	if err != nil {
		return "", "", fmt.Errorf("Suite %q not found: %v", suiteName, err)
	}
	suiteContext = adrill.BuildSuiteContext(suite)
	return suiteContext, tools.GetDrillAddPrompt(suiteName, suiteContext), nil
}

// resolveTutorialAddContext loads suite context for /tutorial-add.
func resolveTutorialAddContext(ctx context.Context, fs store.FlowStore, suiteName string) (name, suiteContext string, err error) {
	suiteName = strings.TrimSpace(suiteName)
	if suiteName == "" {
		return "", "", fmt.Errorf("Usage: /tutorial-add <suite_name>")
	}
	if fs == nil {
		return "", "", fmt.Errorf("drill management requires platform mode (team-scoped store not available)")
	}
	suite, err := adrill.LoadSuiteFromStore(fs, ctx, suiteName)
	if err != nil {
		return "", "", fmt.Errorf("Suite %q not found: %v", suiteName, err)
	}
	return suiteName, adrill.BuildSuiteContext(suite), nil
}
