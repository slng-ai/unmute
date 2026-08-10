package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/slng/unmute/internal/ir"
)

// CallStartEnv is the environment variable a local dev run uses to stand in for
// the dispatch payload production sends (variable_secrets_specs.md I.dispatch).
// The generated runtimes read it only when the call context supplies nothing.
const CallStartEnv = "UNMUTE_CALL_START"

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
		if ir.IsSystemSource(variable.Source) {
			return "", fmt.Errorf("--var %s: %q has source %s, so the runtime supplies it, not you", flag, name, variable.Source)
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
