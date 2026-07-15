package ir

// ProjectConfig is the shape of project.yaml.
type ProjectConfig struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	Owner   string `json:"owner,omitempty" yaml:"owner,omitempty"`
}
