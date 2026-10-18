package generate

import (
	"strings"
	"testing"
)

func TestPromptsContainOnlyAuthoredStateReferences(t *testing.T) {
	agent := loadTypedState(t)
	for name, task := range agent.Tasks {
		if strings.Contains(task.Instructions, "Conversation info:") || strings.Contains(task.Instructions, "Request:") {
			t.Errorf("task %s includes an automatic sharing block", name)
		}
	}
}
