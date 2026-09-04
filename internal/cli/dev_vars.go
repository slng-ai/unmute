package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// CallStartEnv is the environment variable a local dev run uses to stand in for
// the dispatch payload production sends (variable_secrets_specs.md I.dispatch).
// The generated runtimes read it only when the call context supplies nothing.
const CallStartEnv = "UNMUTE_CALL_START"

// callFactsPayload turns repeated --source name=value flags into the JSON object
// the generated runtime's pre-fetch reads, standing in for the facts a carrier
// supplies on a real call.
//
// A separate flag from --var, and that is the whole point. --var stands in for the
// dispatch payload and writes a variable directly; a caller's number is not a
// dispatch value, and seeding the variable would skip the pre-fetch, mark nothing
// as awaiting confirmation, and let a local run book an appointment without ever
// reading a number back. That is a local run passing a path a real call fails,
// which is worse than no local path at all (research R8).
//
// Only a call fact is accepted. A name that is not one is refused rather than
// carried into an env var nothing reads, which is the same silent no-op V13
// forbids of --var.
func callFactsPayload(flags []string) (string, error) {
	if len(flags) == 0 {
		return "", nil
	}
	values := make(map[string]string, len(flags))
	for _, flag := range flags {
		name, raw, ok := strings.Cut(flag, "=")
		if !ok {
			return "", fmt.Errorf("--source %q must be name=value", flag)
		}
		source := ir.VariableSource(name)
		if !ir.IsSystemSource(source) {
			switch source {
			case ir.VariableSourceConversation:
				return "", fmt.Errorf("--source %s: a conversation value is one the model saves mid-call, "+
					"so there is nothing for a call to carry; talk to the agent instead", flag)
			case ir.VariableSourceCallStart:
				return "", fmt.Errorf("--source %s: call_start arrives with the dispatch, not from the carrier; "+
					"seed it with --var %s=... instead", flag, name)
			default:
				return "", fmt.Errorf("--source %s: %q is not a fact a call carries. The facts are: %s",
					flag, name, strings.Join(callFactNames(), ", "))
			}
		}
		values[name] = raw
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// callFactNames lists the facts --source accepts, sorted, for the refusal that
// names them. Derived from ir's own list so the two cannot drift.
func callFactNames() []string {
	names := []string{
		string(ir.VariableSourceCallID), string(ir.VariableSourceCarrier),
		string(ir.VariableSourceConnection), string(ir.VariableSourceDirection),
		string(ir.VariableSourceFromNumber), string(ir.VariableSourceSessionID),
		string(ir.VariableSourceStreamID), string(ir.VariableSourceToNumber),
	}
	slices.Sort(names)
	return names
}

// callStartPayload turns repeated --var name=value flags into the JSON object the
// generated runtime expects. Each value is parsed against its declared type, and
// a name the package never declares is refused: accepting it here and dropping it
// in the bot is exactly the silent no-op the schema forbids (V13).
func callStartPayload(agent *ir.Agent, flags []string) (string, error) {
	if len(flags) == 0 {
		return "", nil
	}
	values := make(map[string]any, len(flags))
	for _, flag := range flags {
		name, raw, ok := strings.Cut(flag, "=")
		if !ok {
			return "", fmt.Errorf("--var %q must be name=value", flag)
		}
		variable, declared := agent.Variables[name]
		if !declared {
			return "", fmt.Errorf("--var %s: no variable %q is declared in agent.yaml", flag, name)
		}
		// Two kinds of variable read the dispatch payload, and the flag takes
		// both: `source: call_start`, and a variable that declares no source at
		// all. Both drivers hydrate the same pair (`v.Source ==
		// ir.VariableSourceCallStart || v.Source == ""` in livekit_v1_build.go
		// and pipecat_v1_build.go), and both emitted runbooks print a
		// `--var <name>=...` line for each of them, so refusing the sourceless
		// half made the flag contradict the runbook it appears in. It also said
		// so badly: `%s` on an empty source rendered "has source , so the model
		// saves it mid-call through update_variables", and that reason is not
		// true of a sourceless variable either, because update_variables is
		// generated over `source: conversation` alone.
		//
		// A runtime-owned source still arrives from the telephony route and a
		// conversation source is still captured mid-call, so seeding either
		// would be accepted here and then dropped in build_state — the silent
		// no-op V13 forbids.
		if variable.Source != ir.VariableSourceCallStart && variable.Source != "" {
			reason := fmt.Sprintf("the model saves it mid-call through %s", ir.CaptureToolName)
			if ir.IsSystemSource(variable.Source) {
				reason = "the runtime supplies it"
			}
			return "", fmt.Errorf("--var %s: %q has source %s, so %s, not you", flag, name, variable.Source, reason)
		}
		value, err := parseVarValue(variable.Type, raw)
		if err != nil {
			return "", fmt.Errorf("--var %s: %w", flag, err)
		}
		values[name] = value
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// parseVarValue converts a flag's text to the variable's declared type, so the
// bot receives a JSON number where it expects one rather than a quoted string.
func parseVarValue(kind ir.PrimitiveType, raw string) (any, error) {
	switch kind {
	case ir.PrimitiveInteger:
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", raw)
		}
		return value, nil
	case ir.PrimitiveNumber:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return value, nil
	case ir.PrimitiveBoolean:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a boolean (true or false)", raw)
		}
		return value, nil
	default:
		return raw, nil
	}
}

// envValue returns the value of name in a KEY=VALUE env slice ("" when the
// name is empty or unset).
func envValue(env []string, name string) string {
	if name == "" {
		return ""
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}
