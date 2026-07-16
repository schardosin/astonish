package api

import (
	"context"
	"fmt"
	"strings"

	adrill "github.com/SAP/astonish/pkg/drill"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/tools"
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
// Only suites that already contain tutorial drills are allowed — never append
// tutorial drills into a regular smoke/CI suite.
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
	if !adrill.IsTutorialSuite(suite) {
		return "", "", fmt.Errorf(
			"Suite %q is a regular drill suite, not a tutorial suite. "+
				"Do not mix tutorial drills into smoke/CI suites. "+
				"Run /tutorial and copy this suite's template/start script/credentials into a new sibling suite (e.g. %s-tutorial), "+
				"or use /tutorial-add on an existing tutorial suite",
			suiteName, suiteName,
		)
	}
	return suiteName, adrill.BuildSuiteContext(suite), nil
}
