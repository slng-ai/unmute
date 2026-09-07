package ir

import "fmt"

// AgentPromptSite and TaskPromptSite are the two site strings, and they are the
// strings the template validator already uses. Written once here so the
// composer and the validator cannot spell the same site two ways.
func AgentPromptSite(name string) string { return fmt.Sprintf("agent %q instructions", name) }

func TaskPromptSite(name string) string { return fmt.Sprintf("task %q instructions", name) }

// StateEmptyText is what a declared value with no contents renders as. Held
// here rather than in the emitted-code package so a compile-time test of a
// prompt and the runtime rendering read one string, and neither can drift into
// rendering `[]` where the other renders words (FR-005a).
func StateEmptyText() string { return stateEmptyText }

const stateEmptyText = "none recorded yet."
