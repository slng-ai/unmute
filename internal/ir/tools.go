package ir

// ToolDeclaration is one agent/tools/*.yaml file. The filename is the tool
// identity; there is intentionally no name field inside the YAML.
type ToolDeclaration struct {
	Description   string         `json:"description" yaml:"description"`
	Parameters    map[string]any `json:"parameters" yaml:"parameters"`
	NeedsApproval bool           `json:"needs_approval" yaml:"needs_approval"`
	Handler       ToolHandler    `json:"handler" yaml:"handler"`
}

type ToolHandler struct {
	Type string `json:"type" yaml:"type"`
	Ref  string `json:"ref" yaml:"ref"`
}

// ToolFile keeps the filename-derived tool name with its declaration.
type ToolFile struct {
	Name        string
	Declaration ToolDeclaration
}
