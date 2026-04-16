package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// newAgent builds a LoadResult for ValidateDocuments tests. Node is
// nil because cross-document errors only use file paths — the
// per-document validators have already run line-number checks. When
// a test needs line numbers on cross-doc errors it can decode real
// YAML; for the ref-resolution checks a zero Node is fine.
func newAgent(name string) *LoadResult {
	return &LoadResult{
		Definition: &types.AgentDefinition{
			APIVersion: "mockagents/v1",
			Kind:       "Agent",
			Metadata:   types.Metadata{Name: name},
		},
		FilePath: name + ".yaml",
	}
}

func newPipeline(name string, nodes ...types.PipelineAgent) *PipelineLoadResult {
	return &PipelineLoadResult{
		Definition: &types.PipelineDefinition{
			APIVersion: "mockagents/v1",
			Kind:       "Pipeline",
			Metadata:   types.Metadata{Name: name},
			Spec: types.PipelineSpec{
				Topology: "sequential",
				Agents:   nodes,
			},
		},
		FilePath: name + ".yaml",
	}
}

func newSuite(name string, target types.TestTarget, cases ...types.TestCase) *TestSuiteLoadResult {
	return &TestSuiteLoadResult{
		Definition: &types.TestSuiteDefinition{
			APIVersion: "mockagents/v1",
			Kind:       "TestSuite",
			Metadata:   types.Metadata{Name: name},
			Spec: types.TestSuiteSpec{
				Target: target,
				Cases:  cases,
			},
		},
		FilePath: name + ".yaml",
	}
}

func TestValidateDocuments_AllValid(t *testing.T) {
	docs := &Documents{
		Agents: []*LoadResult{
			newAgent("writer"),
			newAgent("reviewer"),
		},
		Pipelines: []*PipelineLoadResult{
			newPipeline("research",
				types.PipelineAgent{ID: "w", Ref: "writer"},
				types.PipelineAgent{ID: "r", Ref: "reviewer"},
			),
		},
		TestSuites: []*TestSuiteLoadResult{
			newSuite("pipeline-suite",
				types.TestTarget{Pipeline: "research"},
				types.TestCase{
					Name:  "happy path",
					Steps: []types.TestStep{{Role: "user", Content: "hi"}},
					Assertions: []types.TestAssertion{{
						Type:   types.AssertResponseContains,
						Value:  "ok",
						NodeID: "w",
					}},
				},
			),
		},
	}
	if errs := ValidateDocuments(docs); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidateDocuments_PipelineRefsMissingAgent(t *testing.T) {
	docs := &Documents{
		Agents: []*LoadResult{newAgent("writer")},
		Pipelines: []*PipelineLoadResult{
			newPipeline("research",
				types.PipelineAgent{ID: "w", Ref: "writer"},
				// reviewer is referenced but never declared.
				types.PipelineAgent{ID: "r", Ref: "reviewer"},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil || !containsMessage(errs, "unknown agent \"reviewer\"") {
		t.Errorf("expected unknown-agent error: %v", errs)
	}
	// Only the second ref should fail, not the first.
	for _, e := range errs.Errors {
		if e.Field != "spec.agents[1].ref" {
			t.Errorf("wrong field: %q", e.Field)
		}
	}
}

func TestValidateDocuments_TestSuiteAgentTargetMissing(t *testing.T) {
	docs := &Documents{
		Agents: []*LoadResult{newAgent("writer")},
		TestSuites: []*TestSuiteLoadResult{
			newSuite("s", types.TestTarget{Agent: "ghost"},
				types.TestCase{Name: "c", Steps: []types.TestStep{{Role: "user", Content: "hi"}}},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil || !containsMessage(errs, "unknown agent \"ghost\"") {
		t.Errorf("expected unknown-agent error: %v", errs)
	}
}

func TestValidateDocuments_TestSuitePipelineTargetMissing(t *testing.T) {
	docs := &Documents{
		TestSuites: []*TestSuiteLoadResult{
			newSuite("s", types.TestTarget{Pipeline: "ghost-pipe"},
				types.TestCase{Name: "c", Steps: []types.TestStep{{Role: "user", Content: "hi"}}},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil || !containsMessage(errs, "unknown pipeline \"ghost-pipe\"") {
		t.Errorf("expected unknown-pipeline error: %v", errs)
	}
}

func TestValidateDocuments_AssertionNodeIDMissingInPipeline(t *testing.T) {
	docs := &Documents{
		Agents: []*LoadResult{newAgent("writer")},
		Pipelines: []*PipelineLoadResult{
			newPipeline("research",
				types.PipelineAgent{ID: "w", Ref: "writer"},
			),
		},
		TestSuites: []*TestSuiteLoadResult{
			newSuite("s", types.TestTarget{Pipeline: "research"},
				types.TestCase{
					Name:  "c",
					Steps: []types.TestStep{{Role: "user", Content: "hi"}},
					Assertions: []types.TestAssertion{{
						Type:   types.AssertResponseContains,
						Value:  "ok",
						NodeID: "nope",
					}},
				},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil || !containsMessage(errs, "node_id \"nope\" does not exist") {
		t.Errorf("expected unknown-node-id error: %v", errs)
	}
}

func TestValidateDocuments_AssertionNodeIDWithoutPipelineTarget(t *testing.T) {
	// Agent-targeted suite can't use node_id — it makes no sense.
	docs := &Documents{
		Agents: []*LoadResult{newAgent("writer")},
		TestSuites: []*TestSuiteLoadResult{
			newSuite("s", types.TestTarget{Agent: "writer"},
				types.TestCase{
					Name:  "c",
					Steps: []types.TestStep{{Role: "user", Content: "hi"}},
					Assertions: []types.TestAssertion{{
						Type:   types.AssertResponseContains,
						Value:  "ok",
						NodeID: "w",
					}},
				},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil || !containsMessage(errs, "targets an agent, not a pipeline") {
		t.Errorf("expected agent-target-with-node-id error: %v", errs)
	}
}

func TestValidateDocuments_NoTestSuitesOrPipelinesIsANoOp(t *testing.T) {
	docs := &Documents{
		Agents: []*LoadResult{newAgent("writer")},
	}
	if errs := ValidateDocuments(docs); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidateDocuments_NilIsANoOp(t *testing.T) {
	if errs := ValidateDocuments(nil); errs != nil {
		t.Errorf("unexpected errors for nil docs: %v", errs.Error())
	}
}

func TestValidateDocuments_UnknownPipelineSuppressesNodeIDErrors(t *testing.T) {
	// When the pipeline target is unknown, we still need an error
	// for it — but we should NOT pile on with individual node_id
	// errors that all depend on the same root cause.
	docs := &Documents{
		TestSuites: []*TestSuiteLoadResult{
			newSuite("s", types.TestTarget{Pipeline: "ghost"},
				types.TestCase{
					Name:  "c",
					Steps: []types.TestStep{{Role: "user", Content: "hi"}},
					Assertions: []types.TestAssertion{
						{Type: types.AssertResponseContains, Value: "a", NodeID: "x"},
						{Type: types.AssertResponseContains, Value: "b", NodeID: "y"},
					},
				},
			),
		},
	}
	errs := ValidateDocuments(docs)
	if errs == nil {
		t.Fatal("expected error for unknown pipeline")
	}
	// Exactly one error: the unknown-pipeline message. No
	// "node_id X does not exist" follow-ons.
	var count int
	for _, e := range errs.Errors {
		if containsSingleMessage(e, "node_id") {
			t.Errorf("unexpected node_id error on unknown pipeline: %v", e)
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 error, got %d: %v", count, errs.Error())
	}
}

// containsSingleMessage is a one-entry variant of containsMessage
// (defined in pipeline_validator_test.go) used when we're inspecting
// a specific *ValidationError rather than a whole list.
func containsSingleMessage(e *ValidationError, substr string) bool {
	return e != nil && len(e.Message) >= len(substr) && containsSubstring(e.Message, substr)
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
