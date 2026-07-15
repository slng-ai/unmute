package ir

// EnvSecrets is the committed env/secrets.yaml shape. It maps runtime env
// names to local dotenv keys without storing values.
type EnvSecrets struct {
	Local   LocalEnvConfig       `json:"local" yaml:"local"`
	Secrets map[string]SecretRef `json:"secrets" yaml:"secrets"`
}

// LocalEnvConfig points local runs at the agent-root dotenv file.
type LocalEnvConfig struct {
	EnvFile string `json:"env_file" yaml:"env_file"`
}

// SecretRef names the local dotenv key that satisfies one runtime env name.
type SecretRef struct {
	LocalKey string `json:"local_key" yaml:"local_key"`
}

// RequiredPipecatSecrets are needed by the generated Pipecat v1 project.
var RequiredPipecatSecrets = []string{"SLNG_API_KEY", "OPENAI_API_KEY"}

// MissingRequired returns required runtime env names absent from env/secrets.yaml.
func (s EnvSecrets) MissingRequired(required []string) []string {
	var missing []string
	for _, name := range required {
		ref, ok := s.Secrets[name]
		if !ok || ref.LocalKey == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// PipecatTargetProfile is targets/pipecat/pipecat.yaml.
type PipecatTargetProfile struct {
	Docker     DockerProfile     `json:"docker" yaml:"docker"`
	PCC        PCCProfile        `json:"pcc" yaml:"pcc"`
	Kubernetes KubernetesProfile `json:"kubernetes" yaml:"kubernetes"`
	Local      LocalEnvConfig    `json:"local" yaml:"local"`
}

type DockerProfile struct {
	Image string `json:"image" yaml:"image"`
	Tag   string `json:"tag" yaml:"tag"`
}

type PCCProfile struct {
	AgentName    string            `json:"agent_name" yaml:"agent_name"`
	SecretSet    string            `json:"secret_set" yaml:"secret_set"`
	AgentProfile string            `json:"agent_profile" yaml:"agent_profile"`
	Scaling      PCCScalingProfile `json:"scaling" yaml:"scaling"`
}

type PCCScalingProfile struct {
	MinAgents int `json:"min_agents" yaml:"min_agents"`
}

type KubernetesProfile struct {
	Namespace  string `json:"namespace" yaml:"namespace"`
	SecretName string `json:"secret_name" yaml:"secret_name"`
	Replicas   int    `json:"replicas" yaml:"replicas"`
}

// DefaultPipecatTargetProfile returns stable defaults for a scaffolded agent.
func DefaultPipecatTargetProfile(agentName string) PipecatTargetProfile {
	return PipecatTargetProfile{
		Docker: DockerProfile{
			Image: agentName,
			Tag:   "latest",
		},
		PCC: PCCProfile{
			AgentName:    agentName,
			SecretSet:    agentName + "-local",
			AgentProfile: "voice-agent",
			Scaling: PCCScalingProfile{
				MinAgents: 1,
			},
		},
		Kubernetes: KubernetesProfile{
			Namespace:  "default",
			SecretName: agentName + "-secrets",
			Replicas:   1,
		},
		Local: LocalEnvConfig{
			EnvFile: ".env.local",
		},
	}
}

// ApplyDefaults fills omitted target-profile fields without overriding author
// choices.
func (p *PipecatTargetProfile) ApplyDefaults(agentName string) {
	defaults := DefaultPipecatTargetProfile(agentName)
	if p.Docker.Image == "" {
		p.Docker.Image = defaults.Docker.Image
	}
	if p.Docker.Tag == "" {
		p.Docker.Tag = defaults.Docker.Tag
	}
	if p.PCC.AgentName == "" {
		p.PCC.AgentName = defaults.PCC.AgentName
	}
	if p.PCC.SecretSet == "" {
		p.PCC.SecretSet = defaults.PCC.SecretSet
	}
	if p.PCC.AgentProfile == "" {
		p.PCC.AgentProfile = defaults.PCC.AgentProfile
	}
	if p.PCC.Scaling.MinAgents == 0 {
		p.PCC.Scaling.MinAgents = defaults.PCC.Scaling.MinAgents
	}
	if p.Kubernetes.Namespace == "" {
		p.Kubernetes.Namespace = defaults.Kubernetes.Namespace
	}
	if p.Kubernetes.SecretName == "" {
		p.Kubernetes.SecretName = defaults.Kubernetes.SecretName
	}
	if p.Kubernetes.Replicas == 0 {
		p.Kubernetes.Replicas = defaults.Kubernetes.Replicas
	}
	if p.Local.EnvFile == "" {
		p.Local.EnvFile = defaults.Local.EnvFile
	}
}
