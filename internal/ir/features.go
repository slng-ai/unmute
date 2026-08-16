package ir

import (
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// UsedFrameworkFeatures reports the capabilities in a package whose emitted code
// needs a minimum framework version, so validation can check the declared
// version against each one's floor before anything is written.
//
// This is the one home for "does this package use a warm transfer / an MCP tool
// source" as a *version* question. The LiveKit driver answers the same question
// separately for its own reasons (which import to write, which extra to
// install), and TestFeatureUseAgreesWithDriver in internal/generate fails if the
// two ever disagree about a real package.
//
// The result is ordered, not a set, so an author reads the same violation first
// on every run.
func UsedFrameworkFeatures(agent *Agent) []targetcap.FrameworkFeature {
	if agent == nil {
		return nil
	}
	var used []targetcap.FrameworkFeature
	if usesWarmTransfer(agent) {
		used = append(used, targetcap.FeatureWarmTransfer)
	}
	if usesMCPTools(agent) {
		used = append(used, targetcap.FeatureMCPTools)
	}
	return used
}

// usesWarmTransfer reports whether any control is a human transfer in warm mode.
// Cold transfers act on the caller's existing leg and need no prebuilt, so they
// carry no floor.
func usesWarmTransfer(agent *Agent) bool {
	for _, control := range agent.Controls {
		if ht, ok := control.(*HumanTransfer); ok && ht.Mode == TransferWarm {
			return true
		}
	}
	return false
}

// usesMCPTools reports whether any tool is served by an MCP source.
func usesMCPTools(agent *Agent) bool {
	for _, tool := range agent.Tools {
		if tool.Execution == ToolMCP {
			return true
		}
	}
	return false
}
