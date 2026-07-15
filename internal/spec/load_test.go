package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
)

func TestLoadEnvSecrets_scaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	secrets, err := LoadEnvSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if secrets.Local.EnvFile != ".env.local" {
		t.Fatalf("env file = %q, want .env.local", secrets.Local.EnvFile)
	}
	if missing := secrets.MissingRequired(ir.RequiredPipecatSecrets); len(missing) > 0 {
		t.Fatalf("missing required secrets: %v", missing)
	}
	if secrets.Secrets["SLNG_API_KEY"].LocalKey != "SLNG_API_KEY" {
		t.Fatalf("SLNG_API_KEY local key = %q", secrets.Secrets["SLNG_API_KEY"].LocalKey)
	}
}

func TestLoadProjectAndRuntime_scaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	project, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "support-bot" {
		t.Fatalf("project name = %q, want support-bot", project.Name)
	}

	compliance, err := LoadComplianceConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if compliance.Region != "ap-south" {
		t.Fatalf("region = %q, want ap-south", compliance.Region)
	}

	idle, err := LoadIdleNudgesConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !idle.Enabled || idle.FirstNudgeDelaySeconds != 15 {
		t.Fatalf("idle = %+v", idle)
	}

	interruption, err := LoadInterruptionConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !interruption.Enabled {
		t.Fatalf("interruption = %+v", interruption)
	}

	variables, err := LoadVariables(dir)
	if err != nil {
		t.Fatal(err)
	}
	if variables.User["user_name"].Default != "Axel" {
		t.Fatalf("user_name variable = %+v", variables.User["user_name"])
	}
	if variables.System["call_id"].Source != ir.SystemVariableSourceCallID {
		t.Fatalf("call_id variable = %+v", variables.System["call_id"])
	}
}

func TestLoadEnvSecrets_missingRequired(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if err := os.MkdirAll(filepath.Join(dir, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte(`local:
  env_file: .env.local
secrets:
  SLNG_API_KEY:
    local_key: SLNG_API_KEY
`)
	if err := os.WriteFile(filepath.Join(dir, "env", "secrets.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	secrets, err := LoadEnvSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}
	missing := secrets.MissingRequired(ir.RequiredPipecatSecrets)
	if len(missing) != 1 || missing[0] != "OPENAI_API_KEY" {
		t.Fatalf("missing = %v, want [OPENAI_API_KEY]", missing)
	}
}

func TestLoadPipecatTargetProfile_scaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	profile, err := LoadPipecatTargetProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Docker.Image != "support-bot" || profile.Docker.Tag != "latest" {
		t.Fatalf("docker = %+v", profile.Docker)
	}
	if profile.PCC.AgentName != "support-bot" || profile.PCC.SecretSet != "support-bot-local" {
		t.Fatalf("pcc = %+v", profile.PCC)
	}
	if profile.Kubernetes.SecretName != "support-bot-secrets" {
		t.Fatalf("kubernetes = %+v", profile.Kubernetes)
	}
	if profile.Local.EnvFile != ".env.local" {
		t.Fatalf("local = %+v", profile.Local)
	}
}

func TestLoadPipecatTargetProfile_defaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	targetDir := filepath.Join(dir, "targets", "pipecat")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "pipecat.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := LoadPipecatTargetProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := ir.DefaultPipecatTargetProfile("support-bot")
	if profile != want {
		t.Fatalf("profile = %+v, want %+v", profile, want)
	}
}

func TestLoadPipecatTargetProfile_strictYAML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	targetDir := filepath.Join(dir, "targets", "pipecat")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "pipecat.yaml"), []byte("surprise: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPipecatTargetProfile(dir)
	if err == nil {
		t.Fatal("expected strict YAML error")
	}
	if !strings.Contains(err.Error(), "pipecat.yaml") {
		t.Fatalf("error missing path: %v", err)
	}
}
