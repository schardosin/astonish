package astonish

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	default:
		printTestUsage()
		return fmt.Errorf("unknown test command: %s", args[0])
	}
}

func printTestUsage() {
	fmt.Println("usage: astonish test [-h] {run,list,report} ...")
	fmt.Println("")
	fmt.Println("Deterministic test runner for AI-authored test suites.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  run                 Run a test suite or single test")
	fmt.Println("  list                List all test suites and tests")
	fmt.Println("  report              Show the last test report")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help          Show this help message")
}

// internalToolExecutor adapts pkg/tools.ExecuteTool to the ToolExecutor interface.
type internalToolExecutor struct{}

func (e *internalToolExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	return tools.ExecuteTool(ctx, name, args)
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

	// Create tool executor
	executor := &internalToolExecutor{}
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
