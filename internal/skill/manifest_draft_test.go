package skill

import (
	"strings"
	"testing"
)

func TestInstalledSkillTeachesManifestDraftWorkflow(t *testing.T) {
	project := t.TempDir()
	install(t, New("test"), project)
	for _, file := range []string{"SKILL.md", "references/workflow.md", "references/manifests.md"} {
		content := read(t, project, file)
		for _, want := range []string{"--manifest", "--draft", "unmute validate", "unmute compile", "contract and its link", "unmute skill install"} {
			if !strings.Contains(content, want) {
				t.Errorf("installed %s omits %q from the manifest draft workflow", file, want)
			}
		}
		if strings.Contains(content, "unattended creation is not supported") {
			t.Errorf("installed %s still rules out the draft workflow", file)
		}
	}
}
