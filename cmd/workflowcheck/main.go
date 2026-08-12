package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	cellWorkflowPath       = ".github/workflows/cell-plan.yml"
	foundationWorkflowPath = ".github/workflows/foundation-plan.yml"
	observabilityPath      = ".github/workflows/observability-plan.yml"
	proofWorkflowPath      = ".github/workflows/observability-proof.yml"
	proofSchedulePath      = ".github/workflows/observability-proof-schedule.yml"
	proofReusableWorkflow  = "./.github/workflows/observability-proof.yml"
)

type workflow struct {
	On   workflowTriggers       `yaml:"on"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowTriggers struct {
	WorkflowDispatch workflowDispatch   `yaml:"workflow_dispatch"`
	Schedule         []workflowSchedule `yaml:"schedule"`
}

type workflowDispatch struct {
	Inputs map[string]workflowInput `yaml:"inputs"`
}

type workflowInput struct {
	Options []string `yaml:"options"`
}

type workflowSchedule struct {
	Cron string `yaml:"cron"`
}

type workflowJob struct {
	Environment workflowEnvironment `yaml:"environment"`
	Steps       []workflowStep      `yaml:"steps"`
	Uses        string              `yaml:"uses"`
	With        map[string]any      `yaml:"with"`
}

type workflowEnvironment struct {
	Name string `yaml:"name"`
}

type workflowStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || !slices.Contains([]string{"all", "proof"}, args[0]) {
		return errors.New("usage: workflowcheck <all|proof>")
	}
	if args[0] == "proof" {
		if err := checkProofContract(); err != nil {
			return err
		}
		fmt.Println("check-proof-workflow: protected proof interface is complete")
		return nil
	}

	paths, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no GitHub workflows found")
	}
	slices.Sort(paths)
	for _, path := range paths {
		parsed, _, parseErr := parseWorkflow(path)
		if parseErr != nil {
			return parseErr
		}
		if err := checkRunBlocks(ctx, path, parsed); err != nil {
			return err
		}
	}
	if err := checkFoundationContract(); err != nil {
		return err
	}
	if err := checkCellContract(); err != nil {
		return err
	}
	if err := checkObservabilityContract(); err != nil {
		return err
	}
	fmt.Println("check-workflows: every workflow and run block parses")
	return nil
}

func checkObservabilityContract() error {
	observability, content, err := parseWorkflow(observabilityPath)
	if err != nil {
		return err
	}
	inputs := observability.On.WorkflowDispatch.Inputs
	if !sameKeys(inputs, []string{"target", "apply", "canary_active"}) {
		return errors.New("observability dispatch inputs are incomplete or unexpected")
	}
	if !sameStrings(inputs["target"].Options, []string{"development", "staging", "production", "global"}) {
		return errors.New("observability workflow must support every shared deployment target")
	}
	for _, fragment := range []string{
		"ACCOUNTABLE_ACTIVE_OBSERVABILITY_ENVIRONMENTS",
		`length > 0 and all(. == "development" or . == "staging" or . == "production")`,
		`with_entries(select(.key as $key | $active | index($key)))`,
	} {
		if !bytes.Contains(content, []byte(fragment)) {
			return fmt.Errorf("%s: missing active-environment contract fragment %q", observabilityPath, fragment)
		}
	}
	return nil
}

func checkCellContract() error {
	cell, content, err := parseWorkflow(cellWorkflowPath)
	if err != nil {
		return err
	}
	inputs := cell.On.WorkflowDispatch.Inputs
	if !sameStrings(inputs["operation"].Options, []string{"create", "prepare", "deploy", "converge", "destroy"}) {
		return errors.New("cell workflow must support every lifecycle operation")
	}
	plan, planExists := cell.Jobs["plan"]
	apply, applyExists := cell.Jobs["apply"]
	if !planExists || !applyExists || plan.Environment.Name != "" || apply.Environment.Name != "${{ inputs.environment }}" {
		return errors.New("cell plan must be branch-trusted and apply must use the selected protected environment")
	}
	for _, fragment := range []string{
		"api_count=0",
		"collector_count=0",
		`if [[ "$OPERATION" == "deploy" || "$OPERATION" == "converge" ]]`,
		`echo "DESIRED_COUNT=$api_count"`,
		`echo "COLLECTOR_COUNT=$collector_count"`,
		`-var="api_desired_count=$DESIRED_COUNT"`,
		`-var="collector_desired_count=$COLLECTOR_COUNT"`,
	} {
		if !bytes.Contains(content, []byte(fragment)) {
			return fmt.Errorf("%s: missing zero-service bootstrap contract fragment %q", cellWorkflowPath, fragment)
		}
	}
	return nil
}

func checkFoundationContract() error {
	foundation, content, err := parseWorkflow(foundationWorkflowPath)
	if err != nil {
		return err
	}
	inputs := foundation.On.WorkflowDispatch.Inputs
	if !sameKeys(inputs, []string{"environment", "apply"}) {
		return errors.New("foundation dispatch inputs are incomplete or unexpected")
	}
	if !sameStrings(inputs["environment"].Options, []string{"development", "staging", "production"}) {
		return errors.New("foundation workflow must support every deployment environment")
	}
	plan, planExists := foundation.Jobs["plan"]
	apply, applyExists := foundation.Jobs["apply"]
	if !planExists || !applyExists || plan.Environment.Name != "" || apply.Environment.Name != "${{ inputs.environment }}" {
		return errors.New("foundation plan must be branch-trusted and apply must use the selected protected environment")
	}
	for _, fragment := range []string{
		"accountable-$ENVIRONMENT-plan",
		"accountable-$ENVIRONMENT-foundation-apply",
		"foundation.plan.sha256",
		"sha256sum --check foundation.plan.sha256",
		`tofu -chdir="$ROOT" apply`,
		`"$RUNNER_TEMP/foundation-plan/foundation.plan"`,
	} {
		if !bytes.Contains(content, []byte(fragment)) {
			return fmt.Errorf("%s: missing contract fragment %q", foundationWorkflowPath, fragment)
		}
	}
	return nil
}

func parseWorkflow(path string) (workflow, []byte, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return workflow{}, nil, err
	}
	var parsed workflow
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return workflow{}, nil, fmt.Errorf("%s: invalid YAML: %w", path, err)
	}
	return parsed, content, nil
}

func checkRunBlocks(ctx context.Context, path string, parsed workflow) error {
	for jobName, job := range parsed.Jobs {
		for index, step := range job.Steps {
			if step.Run == "" {
				continue
			}
			command := exec.CommandContext(ctx, "bash", "-n")
			command.Stdin = strings.NewReader("set -euo pipefail\n" + step.Run)
			output, err := command.CombinedOutput()
			if err != nil {
				label := step.Name
				if label == "" {
					label = fmt.Sprintf("step %d", index)
				}
				return fmt.Errorf("%s: job %s: %s: %s", path, jobName, label, bytes.TrimSpace(output))
			}
		}
	}
	return nil
}

func checkProofContract() error {
	proof, content, err := parseWorkflow(proofWorkflowPath)
	if err != nil {
		return err
	}
	inputs := proof.On.WorkflowDispatch.Inputs
	if !sameKeys(inputs, []string{"environment", "cell", "profile", "duration", "rate"}) {
		return errors.New("managed proof dispatch inputs are incomplete or unexpected")
	}
	if !sameStrings(inputs["environment"].Options, []string{"development", "staging", "production"}) {
		return errors.New("managed proof must support every deployment environment")
	}
	if !sameStrings(inputs["profile"].Options, []string{"smoke", "baseline", "capacity", "soak", "controlled-failure"}) {
		return errors.New("managed proof must support every workload profile")
	}
	proofJob, exists := proof.Jobs["proof"]
	if !exists || proofJob.Environment.Name != "${{ inputs.environment }}" {
		return errors.New("managed proof must use the selected protected environment")
	}
	for _, fragment := range []string{
		"ACCOUNTABLE_PROOF_KEY: ${{ secrets.ACCOUNTABLE_PROOF_KEY }}",
		"ManagedProofSuccess",
		"PrivacyProofSuccess",
		"PagingProofSuccess",
		"scripts/observability/prove-paging.sh",
		"scripts/observability/prove-task-uncertainty.sh",
		"if: always()",
		`"$ENVIRONMENT" == "production"`,
		`"$PROFILE" != "smoke"`,
	} {
		if !bytes.Contains(content, []byte(fragment)) {
			return fmt.Errorf("%s: missing contract fragment %q", proofWorkflowPath, fragment)
		}
	}

	schedule, _, err := parseWorkflow(proofSchedulePath)
	if err != nil {
		return err
	}
	crons := make([]string, 0, len(schedule.On.Schedule))
	for _, entry := range schedule.On.Schedule {
		crons = append(crons, entry.Cron)
	}
	if !sameStrings(crons, []string{"17 6 * * 1", "43 5 1 1,4,7,10 *"}) {
		return errors.New("managed proof schedule is incomplete")
	}
	if !hasReusableProof(schedule, "staging", "smoke") ||
		!hasReusableProof(schedule, "production", "smoke") ||
		!hasReusableProof(schedule, "staging", "controlled-failure") {
		return errors.New("managed proof schedule does not cover staging, production, and game-day proofs")
	}
	return nil
}

func hasReusableProof(parsed workflow, environment string, profile string) bool {
	for _, job := range parsed.Jobs {
		if job.Uses == proofReusableWorkflow && job.With["environment"] == environment && job.With["profile"] == profile {
			return true
		}
	}
	return false
}

func sameKeys[T any](values map[string]T, expected []string) bool {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return sameStrings(keys, expected)
}

func sameStrings(actual []string, expected []string) bool {
	actual = slices.Clone(actual)
	expected = slices.Clone(expected)
	slices.Sort(actual)
	slices.Sort(expected)
	return slices.Equal(actual, expected)
}
