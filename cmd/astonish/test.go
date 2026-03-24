package astonish

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	atesting "github.com/schardosin/astonish/pkg/testing"
	"github.com/schardosin/astonish/pkg/tools"
)

func handleTestCommand(args []string) error {
	if len(args) < 1 || args[0] == "--help" || args[0] == "-h" {
		printTestUsage()
		return nil
	}

	switch args[0] {
	case "run":
		return handleTestRunCommand(args[1:])
	case "list":
		return handleTestListCommand(args[1:])
	case "report":
		return handleTestReportCommand(args[1:])
	case "remove", "rm", "delete":
		return handleTestRemoveCommand(args[1:])
	default:
		printTestUsage()
		return fmt.Errorf("unknown test command: %s", args[0])
	}
}

func printTestUsage() {
	fmt.Println("usage: astonish test [-h] {run,list,report,remove} ...")
	fmt.Println("")
	fmt.Println("Deterministic test runner for AI-authored test suites.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  run                 Run a test suite or single test")
	fmt.Println("  list                List all test suites and tests")
	fmt.Println("  report              Show the last test report")
	fmt.Println("  remove              Remove a test suite or single test")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help          Show this help message")
}

// internalToolExecutor adapts pkg/tools.ExecuteTool to the ToolExecutor interface.
type internalToolExecutor struct{}

func (e *internalToolExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	return tools.ExecuteTool(ctx, name, args)
}

// browserToolNames lists all browser tool names for detection.
var browserToolNames = map[string]bool{
	"browser_navigate": true, "browser_navigate_back": true,
	"browser_click": true, "browser_type": true, "browser_hover": true,
	"browser_drag": true, "browser_press_key": true, "browser_select_option": true,
	"browser_fill_form": true, "browser_snapshot": true, "browser_take_screenshot": true,
	"browser_console_messages": true, "browser_network_requests": true,
	"browser_tabs": true, "browser_close": true, "browser_resize": true,
	"browser_wait_for": true, "browser_file_upload": true, "browser_handle_dialog": true,
	"browser_evaluate": true, "browser_run_code": true, "browser_pdf": true,
	"browser_response_body": true, "browser_cookies": true, "browser_storage": true,
	"browser_set_offline": true, "browser_set_headers": true, "browser_set_credentials": true,
	"browser_set_geolocation": true, "browser_set_media": true, "browser_set_timezone": true,
	"browser_set_locale": true, "browser_set_device": true,
	"browser_request_human": true, "browser_handoff_complete": true,
}

// browserToolExecutor lazily initializes a browser.Manager and dispatches browser tool calls
// using the same closure-based factory pattern as GetBrowserTools.
type browserToolExecutor struct {
	mu       sync.Mutex
	mgr      *browser.Manager
	guard    *browser.NavigationGuard
	refs     *browser.RefMap
	headless bool
}

func newBrowserToolExecutor(headless bool) *browserToolExecutor {
	return &browserToolExecutor{headless: headless}
}

// ensureInit lazily creates the browser manager, guard, and ref map.
func (b *browserToolExecutor) ensureInit() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mgr != nil {
		return
	}
	cfg := browser.DefaultConfig()
	cfg.Headless = b.headless
	b.mgr = browser.NewManager(cfg)
	b.guard = browser.DefaultNavigationGuard()
	b.refs = browser.NewRefMap()
}

// Close shuts down the browser if it was started.
func (b *browserToolExecutor) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mgr != nil {
		b.mgr.Cleanup()
	}
}

// Execute dispatches a browser tool call by name.
func (b *browserToolExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	b.ensureInit()

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal browser tool args: %w", err)
	}

	// Each browser tool uses a different args struct. We dispatch to the factory closure
	// and unmarshal the JSON args into the appropriate struct type.
	switch name {
	case "browser_navigate":
		var a tools.BrowserNavigateArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserNavigate(b.mgr, b.guard)(nil, a)

	case "browser_navigate_back":
		var a tools.BrowserNavigateBackArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserNavigateBack(b.mgr)(nil, a)

	case "browser_click":
		var a tools.BrowserClickArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserClick(b.mgr, b.refs)(nil, a)

	case "browser_type":
		var a tools.BrowserTypeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserType(b.mgr, b.refs)(nil, a)

	case "browser_hover":
		var a tools.BrowserHoverArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserHover(b.mgr, b.refs)(nil, a)

	case "browser_drag":
		var a tools.BrowserDragArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserDrag(b.mgr, b.refs)(nil, a)

	case "browser_press_key":
		var a tools.BrowserPressKeyArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserPressKey(b.mgr)(nil, a)

	case "browser_select_option":
		var a tools.BrowserSelectOptionArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSelectOption(b.mgr, b.refs)(nil, a)

	case "browser_fill_form":
		var a tools.BrowserFillFormArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserFillForm(b.mgr, b.refs)(nil, a)

	case "browser_snapshot":
		var a tools.BrowserSnapshotArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSnapshot(b.mgr, b.refs)(nil, a)

	case "browser_take_screenshot":
		var a tools.BrowserScreenshotArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserTakeScreenshot(b.mgr, b.refs)(nil, a)

	case "browser_console_messages":
		var a tools.BrowserConsoleMessagesArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserConsoleMessages(b.mgr)(nil, a)

	case "browser_network_requests":
		var a tools.BrowserNetworkRequestsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserNetworkRequests(b.mgr)(nil, a)

	case "browser_tabs":
		var a tools.BrowserTabsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserTabs(b.mgr, b.guard)(nil, a)

	case "browser_close":
		var a tools.BrowserCloseArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserClose(b.mgr)(nil, a)

	case "browser_resize":
		var a tools.BrowserResizeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserResize(b.mgr)(nil, a)

	case "browser_wait_for":
		var a tools.BrowserWaitForArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserWaitFor(b.mgr)(nil, a)

	case "browser_file_upload":
		var a tools.BrowserFileUploadArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserFileUpload(b.mgr, b.refs)(nil, a)

	case "browser_handle_dialog":
		var a tools.BrowserHandleDialogArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserHandleDialog(b.mgr)(nil, a)

	case "browser_evaluate":
		var a tools.BrowserEvaluateArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserEvaluate(b.mgr, b.refs)(nil, a)

	case "browser_run_code":
		var a tools.BrowserRunCodeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserRunCode(b.mgr)(nil, a)

	case "browser_pdf":
		var a tools.BrowserPDFArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserPDF(b.mgr)(nil, a)

	case "browser_response_body":
		var a tools.BrowserResponseBodyArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserResponseBody(b.mgr)(nil, a)

	case "browser_cookies":
		var a tools.BrowserCookiesArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserCookies(b.mgr)(nil, a)

	case "browser_storage":
		var a tools.BrowserStorageArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserStorage(b.mgr)(nil, a)

	case "browser_set_offline":
		var a tools.BrowserSetOfflineArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetOffline(b.mgr)(nil, a)

	case "browser_set_headers":
		var a tools.BrowserSetHeadersArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetHeaders(b.mgr)(nil, a)

	case "browser_set_credentials":
		var a tools.BrowserSetCredentialsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetCredentials(b.mgr)(nil, a)

	case "browser_set_geolocation":
		var a tools.BrowserSetGeolocationArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetGeolocation(b.mgr)(nil, a)

	case "browser_set_media":
		var a tools.BrowserSetMediaArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetMedia(b.mgr)(nil, a)

	case "browser_set_timezone":
		var a tools.BrowserSetTimezoneArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetTimezone(b.mgr)(nil, a)

	case "browser_set_locale":
		var a tools.BrowserSetLocaleArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetLocale(b.mgr)(nil, a)

	case "browser_set_device":
		var a tools.BrowserSetDeviceArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserSetDevice(b.mgr)(nil, a)

	case "browser_request_human":
		var a tools.BrowserRequestHumanArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserRequestHuman(b.mgr)(nil, a)

	case "browser_handoff_complete":
		var a tools.BrowserHandoffCompleteArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return tools.BrowserHandoffComplete(b.mgr)(nil, a)

	default:
		return nil, fmt.Errorf("unknown browser tool: %s", name)
	}
}

// compositeToolExecutor tries the internal executor first, then the browser executor.
type compositeToolExecutor struct {
	internal *internalToolExecutor
	browser  *browserToolExecutor
}

func (c *compositeToolExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	if browserToolNames[name] {
		return c.browser.Execute(ctx, name, args)
	}
	return c.internal.Execute(ctx, name, args)
}

func handleTestRunCommand(args []string) error {
	runCmd := flag.NewFlagSet("test run", flag.ExitOnError)
	tagFlag := runCmd.String("tag", "", "Filter tests by tag (comma-separated)")
	verbose := runCmd.Bool("verbose", false, "Verbose output")
	reportDir := runCmd.String("report-dir", "", "Directory to save report (default: .astonish/reports)")

	// Extract positional argument (suite or test name) before flags
	var targetName string
	var flagArgs []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
		} else if targetName == "" {
			targetName = arg
		} else {
			flagArgs = append(flagArgs, arg)
		}
	}

	if err := runCmd.Parse(flagArgs); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if targetName == "" {
		fmt.Println("usage: astonish test run <suite_or_test> [--tag tag1,tag2] [--verbose] [--report-dir dir]")
		return fmt.Errorf("no suite or test name provided")
	}

	dirs := getTestDirs()

	// Determine report directory
	rdir := *reportDir
	if rdir == "" {
		rdir = filepath.Join(".astonish", "reports", targetName)
	}

	// Create artifact manager
	am, err := atesting.NewArtifactManager(rdir, targetName)
	if err != nil {
		fmt.Printf("Warning: could not create artifact manager: %v\n", err)
	}

	// Create tool executor (supports both internal and browser tools)
	browserExec := newBrowserToolExecutor(true) // headless for CI
	defer browserExec.Close()
	executor := &compositeToolExecutor{
		internal: &internalToolExecutor{},
		browser:  browserExec,
	}
	runner := atesting.NewSuiteRunner(executor, am, *verbose)

	// Try to find as suite first
	suite, err := atesting.FindSuite(dirs, targetName)
	if err == nil {
		// Found a suite — run all its tests (optionally filtered by tag)
		if err := atesting.ValidateSuite(suite); err != nil {
			return fmt.Errorf("invalid suite: %w", err)
		}

		tests := suite.Tests
		if *tagFlag != "" {
			tags := strings.Split(*tagFlag, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}
			tests = atesting.FilterTestsByTag(tests, tags)
			if len(tests) == 0 {
				fmt.Printf("No tests matching tags: %s\n", *tagFlag)
				return nil
			}
		}

		fmt.Printf("Running suite: %s (%d tests)\n", suite.Name, len(tests))
		report, err := runner.RunSuite(context.Background(), suite, tests)
		if err != nil {
			return fmt.Errorf("suite run failed: %w", err)
		}

		atesting.PrintReport(report, os.Stdout)

		// Save report
		reportPath, err := atesting.SaveReport(report, rdir)
		if err != nil {
			fmt.Printf("Warning: could not save report: %v\n", err)
		} else {
			fmt.Printf("\nReport saved: %s\n", reportPath)
		}

		if report.Status != "passed" {
			return fmt.Errorf("suite %s: %s", suite.Name, report.Status)
		}
		return nil
	}

	// Not a suite — try as individual test
	test, suite, err := atesting.FindTestAndSuite(dirs, targetName)
	if err != nil {
		return fmt.Errorf("not found as suite or test: %s", targetName)
	}

	if err := atesting.ValidateSuite(suite); err != nil {
		return fmt.Errorf("invalid suite %q for test %q: %w", suite.Name, test.Name, err)
	}

	fmt.Printf("Running test: %s (suite: %s)\n", test.Name, suite.Name)
	report, err := runner.RunSuite(context.Background(), suite, []atesting.LoadedTest{*test})
	if err != nil {
		return fmt.Errorf("test run failed: %w", err)
	}

	atesting.PrintReport(report, os.Stdout)

	reportPath, err := atesting.SaveReport(report, rdir)
	if err != nil {
		fmt.Printf("Warning: could not save report: %v\n", err)
	} else {
		fmt.Printf("\nReport saved: %s\n", reportPath)
	}

	if report.Status != "passed" {
		return fmt.Errorf("test %s: %s", test.Name, report.Status)
	}
	return nil
}

func handleTestListCommand(args []string) error {
	listCmd := flag.NewFlagSet("test list", flag.ExitOnError)
	tagFlag := listCmd.String("tag", "", "Filter by tag (comma-separated)")
	if err := listCmd.Parse(args); err != nil {
		return err
	}

	dirs := getTestDirs()
	suites, err := atesting.DiscoverSuites(dirs)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	if len(suites) == 0 {
		fmt.Println("No test suites found.")
		return nil
	}

	// Sort suites by name
	sort.Slice(suites, func(i, j int) bool {
		return suites[i].Name < suites[j].Name
	})

	var tags []string
	if *tagFlag != "" {
		tags = strings.Split(*tagFlag, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	totalTests := 0
	for _, suite := range suites {
		tests := suite.Tests
		if len(tags) > 0 {
			tests = atesting.FilterTestsByTag(tests, tags)
		}

		fmt.Printf("Suite: %s", suite.Name)
		if suite.File != "" {
			fmt.Printf(" (%s)", suite.File)
		}
		fmt.Println()

		sc := suite.Config.SuiteConfig
		if sc != nil {
			if sc.Template != "" {
				fmt.Printf("  Template: %s\n", sc.Template)
			}
			if sc.BaseURL != "" {
				fmt.Printf("  Base URL: %s\n", sc.BaseURL)
			}
			if len(sc.Services) > 0 {
				names := make([]string, len(sc.Services))
				for i, svc := range sc.Services {
					names[i] = svc.Name
				}
				fmt.Printf("  Services: %s\n", strings.Join(names, ", "))
			}
			if len(sc.Setup) > 0 {
				fmt.Printf("  Setup: %d commands\n", len(sc.Setup))
			}
			if len(sc.Teardown) > 0 {
				fmt.Printf("  Teardown: %d commands\n", len(sc.Teardown))
			}
		}

		if len(tests) == 0 {
			fmt.Println("  (no tests)")
		} else {
			for _, test := range tests {
				desc := test.Name
				if test.Config.Description != "" {
					desc = test.Config.Description
				}
				tagStr := ""
				if test.Config.TestConfig != nil && len(test.Config.TestConfig.Tags) > 0 {
					tagStr = " [" + strings.Join(test.Config.TestConfig.Tags, ", ") + "]"
				}
				fmt.Printf("  - %s%s\n", desc, tagStr)
			}
		}
		totalTests += len(tests)
		fmt.Println()
	}

	fmt.Printf("%d suites, %d tests\n", len(suites), totalTests)
	return nil
}

func handleTestReportCommand(args []string) error {
	reportCmd := flag.NewFlagSet("test report", flag.ExitOnError)
	reportDir := reportCmd.String("dir", "", "Report directory (default: .astonish/reports)")
	if err := reportCmd.Parse(args); err != nil {
		return err
	}

	var targetName string
	if reportCmd.NArg() > 0 {
		targetName = reportCmd.Arg(0)
	}

	rdir := *reportDir
	if rdir == "" {
		if targetName != "" {
			rdir = filepath.Join(".astonish", "reports", targetName)
		} else {
			rdir = filepath.Join(".astonish", "reports")
		}
	}

	// If a specific suite/test was given, look for its report directly
	if targetName != "" {
		reportPath := filepath.Join(rdir, "suite_report.json")
		report, err := atesting.LoadReport(reportPath)
		if err != nil {
			return fmt.Errorf("no report found for %q: %w", targetName, err)
		}
		atesting.PrintReport(report, os.Stdout)
		return nil
	}

	// Otherwise, scan the reports directory for all reports
	entries, err := os.ReadDir(rdir)
	if err != nil {
		return fmt.Errorf("no reports found in %s: %w", rdir, err)
	}

	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		reportPath := filepath.Join(rdir, entry.Name(), "suite_report.json")
		report, err := atesting.LoadReport(reportPath)
		if err != nil {
			continue
		}
		found = true
		atesting.PrintReport(report, os.Stdout)
		fmt.Println()
	}

	if !found {
		fmt.Println("No reports found.")
		fmt.Println("Run tests first with: astonish test run <suite>")
	}

	return nil
}

func handleTestRemoveCommand(args []string) error {
	removeCmd := flag.NewFlagSet("test remove", flag.ExitOnError)
	forceFlag := removeCmd.Bool("force", false, "Skip confirmation prompt")
	keepTests := removeCmd.Bool("keep-tests", false, "When removing a suite, keep its test files")

	// Extract positional argument before flags
	var targetName string
	var flagArgs []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
		} else if targetName == "" {
			targetName = arg
		} else {
			flagArgs = append(flagArgs, arg)
		}
	}

	if err := removeCmd.Parse(flagArgs); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if targetName == "" {
		fmt.Println("usage: astonish test remove <suite_or_test> [--force] [--keep-tests]")
		fmt.Println("")
		fmt.Println("Removes a test suite and all its test files, or a single test file.")
		fmt.Println("When removing a suite, all associated test files are also deleted")
		fmt.Println("unless --keep-tests is specified.")
		return fmt.Errorf("no suite or test name provided")
	}

	// Strip .yaml extension if provided
	targetName = strings.TrimSuffix(targetName, ".yaml")
	targetName = strings.TrimSuffix(targetName, ".yml")

	dirs := getTestDirs()

	// Try as suite first
	suite, err := atesting.FindSuite(dirs, targetName)
	if err == nil {
		return handleRemoveSuite(dirs, suite, !*keepTests, *forceFlag)
	}

	// Try as individual test
	test, parentSuite, err := atesting.FindTestAndSuite(dirs, targetName)
	if err == nil {
		return handleRemoveTest(dirs, test, parentSuite, *forceFlag)
	}

	return fmt.Errorf("not found as suite or test: %s", targetName)
}

func handleRemoveSuite(dirs []string, suite *atesting.LoadedSuite, deleteTests bool, force bool) error {
	// Find all associated tests
	tests, _ := atesting.FindTestsForSuite(dirs, suite.Name)

	// Show what will be deleted
	fmt.Printf("Suite: %s (%s)\n", suite.Name, suite.File)
	if len(tests) > 0 && deleteTests {
		fmt.Printf("Tests (%d):\n", len(tests))
		for _, t := range tests {
			desc := t.Name
			if t.Config.Description != "" {
				desc = t.Config.Description
			}
			fmt.Printf("  - %s (%s)\n", desc, t.File)
		}
	} else if len(tests) > 0 {
		fmt.Printf("Warning: %d test file(s) reference this suite and will become orphaned.\n", len(tests))
	}

	// Check for report artifacts
	reportDir := filepath.Join(".astonish", "reports", suite.Name)
	hasReports := false
	if info, err := os.Stat(reportDir); err == nil && info.IsDir() {
		hasReports = true
		fmt.Printf("Reports: %s (will also be removed)\n", reportDir)
	}

	// Confirm
	if !force {
		totalFiles := 1 // suite file
		if deleteTests {
			totalFiles += len(tests)
		}
		fmt.Printf("\nRemove %d file(s)? [y/N]: ", totalFiles)
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Perform deletion
	deleted, err := atesting.DeleteSuite(dirs, suite.Name, deleteTests)
	if err != nil {
		// Show partial results
		for _, path := range deleted {
			fmt.Printf("  Deleted: %s\n", path)
		}
		return fmt.Errorf("deletion failed: %w", err)
	}

	for _, path := range deleted {
		fmt.Printf("  Deleted: %s\n", path)
	}

	// Clean up report artifacts
	if hasReports {
		if err := os.RemoveAll(reportDir); err == nil {
			fmt.Printf("  Deleted: %s\n", reportDir)
		}
	}

	// Clean up knowledge docs (best-effort)
	if memDir, err := config.GetMemoryDir(nil); err == nil {
		docPath := filepath.Join(memDir, "flows", suite.Name+".md")
		os.Remove(docPath)
		for _, t := range tests {
			docPath = filepath.Join(memDir, "flows", t.Name+".md")
			os.Remove(docPath)
		}
	}

	testCount := len(deleted) - 1
	if testCount > 0 {
		fmt.Printf("\nRemoved suite %q and %d test(s).\n", suite.Name, testCount)
	} else {
		fmt.Printf("\nRemoved suite %q.\n", suite.Name)
	}
	return nil
}

func handleRemoveTest(dirs []string, test *atesting.LoadedTest, suite *atesting.LoadedSuite, force bool) error {
	fmt.Printf("Test: %s (%s)\n", test.Name, test.File)
	fmt.Printf("Suite: %s\n", suite.Name)

	if !force {
		fmt.Printf("\nRemove this test? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	deletedPath, _, err := atesting.DeleteTest(dirs, test.Name)
	if err != nil {
		return fmt.Errorf("deletion failed: %w", err)
	}

	fmt.Printf("  Deleted: %s\n", deletedPath)

	// Clean up knowledge doc (best-effort)
	if memDir, err := config.GetMemoryDir(nil); err == nil {
		docPath := filepath.Join(memDir, "flows", test.Name+".md")
		os.Remove(docPath)
	}

	// Check for report artifacts
	reportDir := filepath.Join(".astonish", "reports", test.Name)
	if info, err := os.Stat(reportDir); err == nil && info.IsDir() {
		if err := os.RemoveAll(reportDir); err == nil {
			fmt.Printf("  Deleted: %s\n", reportDir)
		}
	}

	// Warn about remaining tests in suite
	remaining, _ := atesting.FindTestsForSuite(dirs, suite.Name)
	fmt.Printf("\nRemoved test %q. Suite %q has %d remaining test(s).\n", test.Name, suite.Name, len(remaining))
	return nil
}

// getTestDirs returns the directories to scan for test suites and tests.
func getTestDirs() []string {
	var dirs []string

	// System agents directory
	if sysDir, err := config.GetAgentsDir(); err == nil {
		dirs = append(dirs, sysDir)
	}

	// Local agents directory
	if info, err := os.Stat("agents"); err == nil && info.IsDir() {
		dirs = append(dirs, "agents")
	}

	// User flows directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		dirs = append(dirs, flowsDir)
	}

	return dirs
}
