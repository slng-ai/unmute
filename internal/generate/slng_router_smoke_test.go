//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// L4 smoke for the SLNG Context Router's two run-time-only helpers. A golden can
// prove they were emitted and pin their text; only running them proves what they
// do, so gates.md puts both here (FR-018, FR-034f).
//
// No network: nothing here calls the router. The vertex helper reads a variable
// and the truncation helper reads a state object, which is the whole of what
// there is to check.

const slngHelpersSmokeScript = `"""Smoke check: the router's vertex credential and truncation helpers."""
import base64
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"

import bot  # noqa: E402

# --- the vertex credential, three accepted shapes and one refusal -------------
key = {"type": "service_account", "project_id": "smoke", "private_key_id": "abc"}
raw = json.dumps(key)

os.environ["SMOKE_GCP_KEY"] = raw
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "json shape"

os.environ["SMOKE_GCP_KEY"] = base64.b64encode(raw.encode()).decode()
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "base64 shape"

with open("smoke-key.json", "w") as handle:
    handle.write(raw)
os.environ["SMOKE_GCP_KEY"] = os.path.abspath("smoke-key.json")
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "path shape"

# The order matters: a base64 value can begin with "/" and look like a path, so
# base64 is tried before the filesystem.
os.environ["SMOKE_GCP_KEY"] = "/tmp/definitely-not-a-key-file"
try:
    bot._slng_vertex_credentials("SMOKE_GCP_KEY")
except RuntimeError as failure:
    text = str(failure)
    for phrase in ("key JSON", "base64", "path to the key file"):
        assert phrase in text, (phrase, text)
else:
    raise AssertionError("a malformed credential was accepted")

# --- the 4000 character ceiling ----------------------------------------------
# An over-long value truncates and the call continues. It must never raise:
# ending a live call over a variable is the one thing FR-018 forbids.
class _State:
    pass


state = _State()
limit = bot._SLNG_VARIABLE_LIMIT
assert limit == 4000, limit

state.short = "Aurora Salon"
assert bot._slng_template_variables(state, ("short",)) == {"short": "Aurora Salon"}

state.exact = "x" * limit
assert len(bot._slng_template_variables(state, ("exact",))["exact"]) == limit

state.over = "y" * (limit + 321)
assert len(bot._slng_template_variables(state, ("over",))["over"]) == limit

# A name with no value sends the empty string, never None and never "None".
values = bot._slng_template_variables(state, ("never_set",))
assert values["never_set"] == "", repr(values["never_set"])
assert bot._slng_template_variables(None, ("never_set",))["never_set"] == ""

print("slng router helpers ok")
`

// TestSmokeSlngRouterHelpers compiles the router example with a vertex upstream,
// because the vertex helper is emitted only when an upstream needs it, and drives
// both helpers on the real SDK.
func TestSmokeSlngRouterHelpers(t *testing.T) {
	runPipecatSmokeScript(t, "optimized-salon-concierge", nil, func(agent *ir.Agent) {
		// Swap the openai upstream for a vertex one so the credential helper is
		// emitted. The example ships openai, which is the case a reader copies;
		// this is the case only smoke can exercise.
		for name, target := range agent.Targets {
			for profile, binding := range target.Models.Reason {
				if !binding.Router() {
					continue
				}
				binding.Upstream = &ir.Upstream{
					Provider: "vertex", CredentialsEnv: "SMOKE_GCP_KEY", Location: "europe-west4",
				}
				target.Models.Reason[profile] = binding
			}
			agent.Targets[name] = target
		}
		agent.Secrets = append(agent.Secrets, "SMOKE_GCP_KEY")
		// The salon prompts reference no variables, so nothing would emit the
		// snapshot helper. The placeholder goes on here rather than in the example,
		// because the two example packages are a matched pair for measurement and
		// a variable in one of them is a line of difference that buys nothing.
		entry := agent.Agents[agent.EntryAgent]
		entry.Instructions += "\n\nThe caller is {{customer_name}}."
		agent.Agents[agent.EntryAgent] = entry
	}, slngHelpersSmokeScript)
}
