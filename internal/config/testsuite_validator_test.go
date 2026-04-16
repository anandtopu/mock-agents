package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// decodeTestSuiteYAML parses a YAML string into a TestSuiteDefinition
// + its yaml.Node for line-number-aware validation.
func decodeTestSuiteYAML(t *testing.T, src string) (*types.TestSuiteDefinition, *yaml.Node) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var def types.TestSuiteDefinition
	if err := node.Decode(&def); err != nil {
		t.Fatalf("decode testsuite: %v", err)
	}
	return &def, &node
}

func TestValidateTestSuite_Valid(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: support-cases
spec:
  target:
    agent: support-agent
  cases:
    - name: greets-on-hello
      steps:
        - role: user
          content: hello
      assertions:
        - type: response_contains
          value: "hi"
        - type: latency_ms_lt
          max_ms: 500
`)
	if errs := ValidateTestSuite(def, "", node); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidateTestSuite_BothTargets(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
    pipeline: p
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.target") {
		t.Errorf("expected target error: %v", errs)
	}
}

func TestValidateTestSuite_NoTarget(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target: {}
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "no target") {
		t.Errorf("expected no-target error: %v", errs)
	}
}

func TestValidateTestSuite_NoCases(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases: []
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.cases") {
		t.Errorf("expected cases error: %v", errs)
	}
}

func TestValidateTestSuite_DuplicateCaseName(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
    - name: c
      steps:
        - role: user
          content: hi2
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate") {
		t.Errorf("expected duplicate-case error: %v", errs)
	}
}

func TestValidateTestSuite_CaseWithoutSteps(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "no steps") {
		t.Errorf("expected no-steps error: %v", errs)
	}
}

func TestValidateTestSuite_StepMissingRole(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.cases[0].steps[0].role") {
		t.Errorf("expected step.role error: %v", errs)
	}
}

func TestValidateTestSuite_AssertionRuleDispatch(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
      assertions:
        - type: tool_call
        - type: response_contains
        - type: scenario_matched
        - type: latency_ms_lt
          max_ms: 0
        - type: unknown_type
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil {
		t.Fatal("expected assertion errors")
	}
	// Each of the 5 assertions should contribute an error.
	if !containsMessage(errs, "tool_call assertion missing tool name") {
		t.Errorf("no tool_call error: %v", errs)
	}
	if !containsMessage(errs, "response_contains assertion missing value") {
		t.Errorf("no response_contains error: %v", errs)
	}
	if !containsMessage(errs, "scenario_matched assertion missing scenario name") {
		t.Errorf("no scenario_matched error: %v", errs)
	}
	if !containsMessage(errs, "latency_ms_lt assertion needs a positive max_ms") {
		t.Errorf("no latency_ms_lt error: %v", errs)
	}
	if !containsMessage(errs, "unknown assertion type") {
		t.Errorf("no unknown-type error: %v", errs)
	}
}

func TestValidateTestSuite_InvalidKind(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "kind") {
		t.Errorf("expected kind error: %v", errs)
	}
}
