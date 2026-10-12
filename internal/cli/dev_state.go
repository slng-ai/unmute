package cli

import "github.com/slng-ai/unmute/internal/ir"

// devStateField is one field of the author's State as the dev page labels it.
// The page draws every field from the start, in the order state.py declares
// them, so a value nobody has set yet reads as empty rather than missing; the
// values themselves arrive as state records from the running agent.
type devStateField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	// Confirm names the step that has to hear the caller agree to the value.
	Confirm string `json:"confirm,omitempty"`
}

// devStateFields lists the package's State fields, or nothing when it keeps no state.
func devStateFields(agent *ir.Agent) []devStateField {
	if agent == nil || agent.State == nil {
		return nil
	}
	fields := make([]devStateField, 0, len(agent.State.Fields))
	for _, field := range agent.State.Fields {
		fields = append(fields, devStateField{
			Name:        field.Name,
			Type:        field.Type.String(),
			Description: field.Description,
			Confirm:     agent.Variables[field.Name].Confirm,
		})
	}
	return fields
}
