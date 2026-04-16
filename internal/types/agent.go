package types

const (
	AgentAPIVersion = "mockagents/v1"
	AgentKind       = "Agent"
	DefaultModel    = "mock-agent"
)

// AgentDefinition is the top-level structure for a mock agent YAML/JSON file.
type AgentDefinition struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       AgentSpec `yaml:"spec" json:"spec"`
}

// Metadata contains identifying information for an agent.
//
// TenantID is an optional ownership marker for multi-tenant
// deployments. When set, the agent is only visible to admins of the
// matching tenant via the control-plane endpoints, and to LLM calls
// that pass `X-Mockagents-Tenant: <id>`. When empty (the v0.1
// default) the agent is "global" — visible to every tenant. This
// keeps single-tenant deployments unchanged while letting
// multi-tenant operators carve up the catalog.
type Metadata struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	TenantID    string   `yaml:"tenant_id,omitempty" json:"tenant_id,omitempty"`
}

// AgentSpec defines the agent's protocol, model, tools, and behavior.
type AgentSpec struct {
	Protocol     string           `yaml:"protocol" json:"protocol"`
	Model        string           `yaml:"model,omitempty" json:"model,omitempty"`
	SystemPrompt string           `yaml:"systemPrompt,omitempty" json:"systemPrompt,omitempty"`
	Tools        []ToolDefinition `yaml:"tools,omitempty" json:"tools,omitempty"`
	Behavior     BehaviorConfig   `yaml:"behavior" json:"behavior"`
}
