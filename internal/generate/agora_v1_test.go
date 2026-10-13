package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

func TestAgoraGeneration(t *testing.T) {
	agent := loadExample(t, "agora-voice")
	artifact, err := Generate(agent, agent.Targets["agora"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server.py", "client.js", "index.html", "Dockerfile", ".dockerignore", ".env.example", "requirements.txt", "settings.json", "compile-report.json"} {
		_ = artifactFile(t, artifact, name)
	}
	var settings agoraSettings
	if err := json.Unmarshal([]byte(artifactFile(t, artifact, "settings.json")), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Name != agent.Name || settings.Instructions != agent.Agents[agent.EntryAgent].Instructions || !settings.Interruptions || settings.MaxSessionSeconds != 600 || settings.STT.Language != "en" || settings.TTS.Voice != "English_captivating_female1" {
		t.Fatalf("settings mismatch: %+v", settings)
	}
	if !strings.Contains(artifactFile(t, artifact, "requirements.txt"), "agora-agents=="+target.AgoraSDKVersion) {
		t.Fatal("SDK pin missing")
	}
	agent.Conversation.MaxDuration = "1s"
	disabled := false
	agent.Conversation.Interruption.Enabled = &disabled
	artifact, err = Generate(agent, agent.Targets["agora"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(artifactFile(t, artifact, "settings.json")), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Interruptions || settings.MaxSessionSeconds != 1 {
		t.Fatalf("authored values lost: %+v", settings)
	}
}

func TestAgoraGenerationRejectsBeforeOutput(t *testing.T) {
	agent := loadExample(t, "agora-voice")
	agent.Capacity = &ir.Capacity{MaxSessions: 1}
	artifact, err := Generate(agent, agent.Targets["agora"], target.Default())
	if err == nil || len(artifact.Files) != 0 || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("unsupported package produced files: %+v, %v", artifact, err)
	}
}
