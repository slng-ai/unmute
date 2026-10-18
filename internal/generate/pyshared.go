package generate

// Python rendering helpers the core, twilio and slng drivers share.

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// botImportWidth is ruff's default line length, the width isort wraps at.
const botImportWidth = 88

var (
	// stdlibModules are the standard library top-level modules bot.py imports.
	stdlibModules = map[string]bool{
		"asyncio": true, "base64": true, "collections": true, "contextlib": true, "copy": true,
		"dataclasses": true, "datetime": true, "functools": true, "hashlib": true,
		"hmac": true, "inspect": true, "json": true, "logging": true, "math": true,
		"os": true, "pathlib": true, "random": true, "re": true, "sys": true, "threading": true,
		"time": true, "typing": true, "urllib": true, "uuid": true, "xml": true, "zoneinfo": true,
	}
	// firstPartyModules are the sibling modules and packages a package emits
	// beside its entry file, which ruff files apart from third-party imports.
	firstPartyModules = map[string]bool{
		"agents": true, "brain": true, "call": true, "dev_metrics": true, "handoff": true,
		"knowledge": true, "logic": true, "prompts": true, "session": true, "settings": true, "state": true, "call_state": true,
		"telephony_helper": true, "tool_runner": true, "tools": true, "tracing": true, "utils": true,
	}
)

// pyImport is one import statement: `import module`, or `from module import
// names`, where a name may carry its own `as` alias.
type pyImport struct {
	module string
	names  []string // nil for a plain `import module`
}

// render writes the statement as isort does: one line when it fits, otherwise
// one member per line inside parentheses with a trailing comma.
func (s pyImport) render() string {
	if s.names == nil {
		return "import " + s.module + "\n"
	}
	sort.SliceStable(s.names, func(i, j int) bool {
		a, b := importMemberKey(s.names[i]), importMemberKey(s.names[j])
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.name < b.name
	})
	line := "from " + s.module + " import " + strings.Join(s.names, ", ")
	if len(line) <= botImportWidth {
		return line + "\n"
	}
	var out strings.Builder
	out.WriteString("from " + s.module + " import (\n")
	for _, name := range s.names {
		out.WriteString("    " + name + ",\n")
	}
	out.WriteString(")\n")
	return out.String()
}

// importMemberKey orders imported names the way isort's order-by-type does:
// CONSTANTS, then Classes, then everything else, each alphabetically without
// regard to case.
func importMemberKey(member string) (key struct {
	rank int
	name string
}) {
	name, _, _ := strings.Cut(member, " as ")
	key.name = strings.ToLower(name)
	switch {
	case len(name) > 1 && name == strings.ToUpper(name):
		key.rank = 0
	case name != "" && name[0] >= 'A' && name[0] <= 'Z':
		key.rank = 1
	default:
		key.rank = 2
	}
	return key
}

type pyKV struct {
	Key   string
	Value string // already a Python literal
}

// pyQuote renders a Go string as a Python string literal.
func pyQuote(s string) string { return strconv.Quote(s) }

// envSet collects the environment names a generated project needs, and records
// which of them the emitted code reads on **every** session.
//
// The distinction exists because a name can be two things at once. A connection
// maps `auth_token` onto a variable; a webhook tool's `auth.token_env` may name
// the same variable, because one gateway token really does serve both. Deciding
// what a browser session needs by matching names against the connection then
// strips a credential the browser session genuinely reads, and the emitted code
// raises KeyError on the first tool call — after a startup check that passed
// (Wave C, 2026-08-15).
//
// So provenance is recorded rather than re-derived: `read` holds the names a
// model binding or a tool registered, and those are never treated as route
// names however a connection happens to map them.
type envSet struct {
	seen map[string]bool
	read map[string]bool
}

func newEnvSet() *envSet {
	return &envSet{seen: map[string]bool{}, read: map[string]bool{}}
}

// addRead registers a name the emitted code reads directly, whatever channel the
// session arrived on.
func (e *envSet) addRead(name string) {
	if name != "" {
		e.seen[name] = true
		e.read[name] = true
	}
}

func (e *envSet) sorted() []string {
	names := make([]string, 0, len(e.seen))
	for name := range e.seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// pyLiteral renders a decoded YAML value as a Python literal.
// pyExpr is a value already written as a Python expression, so pyLiteral emits
// it verbatim instead of quoting it. It exists for the router's per-call values,
// which are variable names and helper calls rather than constants.
type pyExpr string

func pyLiteral(v any) string {
	switch value := v.(type) {
	case nil:
		return "None"
	case pyExpr:
		return string(value)
	case bool:
		if value {
			return "True"
		}
		return "False"
	case string:
		return strconv.Quote(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case []any:
		items := make([]string, len(value))
		for i, item := range value {
			items[i] = pyLiteral(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		pairs := make([]string, len(keys))
		for i, k := range keys {
			pairs[i] = strconv.Quote(k) + ": " + pyLiteral(value[k])
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return strconv.Quote(fmt.Sprintf("%v", value))
	}
}

func forwardParams(params map[string]any) []pyKV {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]pyKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, pyKV{Key: k, Value: pyLiteral(params[k])})
	}
	return out
}

func apiKeyEnv(provider string) string {
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
}

// sortedKeys is the stdlib one-liner. internal/ir already writes it this way;
// this file used to disagree with it. Generic in the value, because the guard's
// supplier index is a map[string]string and one sorted-keys helper is enough.
func sortedKeys[V any](set map[string]V) []string { return slices.Sorted(maps.Keys(set)) }

// livekitSessionIDField is the field the per-call session id occupies on the user
// data object, and the name both expressions above are built from.
const livekitSessionIDField = "slng_session_id"

// pipecatRealtimeSessionProperties writes the one nested expression the realtime
// service takes its session from. Everything an author can decide about the
// session that is not the model id or the prompt lives in here.
//
// Every class named is from pipecat.services.openai.realtime.events in
// pipecat-ai 1.10.0, imported as realtime_events (its package __init__ is empty,
// so the module is named in full).
//
// A field the package says nothing about is left out rather than written at the
// vendor's own default, so an omitted turn_detection leaves OpenAI deciding and
// this compiler pins nothing it was not asked to pin.
func pipecatRealtimeSessionProperties(turnDetection, voice string, halfCascade bool) (string, error) {
	// The indentation is the "svc" template's: a settings argument starts at
	// column 12, so the lines under it step in from there. Written out because
	// this is the one construction in this driver deep enough that a single line
	// stops being readable, and a template cannot indent an expression it is
	// handed.
	const (
		one   = "\n                "
		two   = "\n                    "
		three = "\n                        "
	)
	var session []string
	if halfCascade {
		// The half cascade, and the whole of it. With text as the only output
		// modality the service pushes plain LLMTextFrames (llm.py:1044-1050),
		// which an ordinary downstream TTSService consumes exactly as it
		// consumes a cascaded model's (services/tts_service.py:766-799). There is
		// no other wiring.
		session = append(session, `output_modalities=["text"]`)
	}
	// Transcription of the caller is OFF unless it is asked for: SessionProperties
	// leaves audio None (events.py:191-237), and with no transcription
	// configured the server sends no
	// conversation.item.input_audio_transcription.* events at all
	// (llm.py:967-971 is the only reader). The caller's words would then reach
	// neither the conversation context nor the dev page, so every package asks
	// for it. InputAudioTranscription()'s own default model is the one the
	// service checks its prompt rule against (services/openai/_constants.py:10),
	// so no model is named here.
	input := []string{"transcription=realtime_events.InputAudioTranscription()"}
	if turnDetection != "" {
		profile, err := targetcap.ResolveTurnDetection(targetcap.Pipecat, turnDetection)
		if err != nil {
			return "", err
		}
		if !profile.Omit {
			input = append(input, "turn_detection="+profile.Expr)
		}
	}
	audio := []string{"input=realtime_events.AudioInput(" + three + strings.Join(input, ","+three) + "," + two + ")"}
	if voice != "" {
		audio = append(audio, `output=realtime_events.AudioOutput(voice=`+pyQuote(voice)+`)`)
	}
	session = append(session, "audio=realtime_events.AudioConfiguration("+two+strings.Join(audio, ","+two)+","+one+")")
	return "realtime_events.SessionProperties(" + one + strings.Join(session, ","+one) + ",\n            )", nil
}
