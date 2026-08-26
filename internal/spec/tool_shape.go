package spec

import (
	"errors"
	"fmt"
	"strings"
)

// executionBlocks are the seven execution-keyed block names (SCHEMA §5.2). The
// block name is the execution kind; a tool file carries exactly one.
var executionBlocks = []string{"webhook", "local", "mcp", "builtin", "client", "provider_hosted", "knowledge"}

// movedToolKeys are the top-level keys the block shape retired. A package
// written against the old flat shape must say what to do, not just "unknown
// field" (SCHEMA §5, 2026-08-10).
var movedToolKeys = map[string]string{
	"execution":    "the execution kind is now the block name: replace it with a `webhook:`, `local:`, `mcp:`, or `builtin:` block",
	"url_env":      "move url_env inside the `webhook:` or `mcp:` block",
	"handler":      "move handler inside the `local:` block",
	"auth":         "move auth inside the `webhook:` block",
	"token_env":    "move token_env under `webhook.auth`",
	"instructions": "move instructions inside the `builtin:` block",
}

// contractToolKeys are the top-level fields that state one tool's contract with
// the model, mapped to why an mcp file cannot carry them: the server announces
// its own tools at run time, so a contract written here is a claim no driver
// ever reads (SCHEMA N40).
var contractToolKeys = map[string]string{
	"description":  "the server describes each of its tools",
	"input":        "the server owns each tool's parameters",
	"output":       "the server owns each tool's result",
	"inject":       "an MCP call has the server's own shape, with nothing to merge into",
	"interruption": "MCP tools take the platform's default interruption policy",
	"effect":       "MCP tools return data; ending the call is a `builtin:` tool",
	"announce":     "the server owns each tool's speech",
}

// knowledgeToolKeys are the top-level fields a `knowledge:` tool cannot carry,
// mapped to why. The tool owns both sides of its contract, so a schema written
// here is a claim no driver reads, the same reasoning `mcp:` uses.
//
// Shorter than contractToolKeys on purpose: `description`, `announce` and
// `interruption` ARE legal here. The author writes the description the model
// reads, and a lookup is a body to speak before.
var knowledgeToolKeys = map[string]string{
	"input":  "the tool owns its schema: one string, the caller's question",
	"output": "the tool owns its result: passages with their sources and scores",
	"inject": "a lookup has no request body to merge into",
	"effect": "a lookup returns data; ending the call is a `builtin:` tool",
}

// checkToolShape reports the file's shape errors before decoding, so a wrong
// shape reads as a migration instruction with a line number rather than a
// decoder complaint. It returns the one execution block it found, which Load
// re-checks against the decoded tool: a block written with an empty body decodes
// to nothing, and that has to name the file and line too.
func checkToolShape(file string, content []byte) (keyLine, error) {
	var blocks, keys []keyLine
	for _, key := range topLevelKeys(content) {
		keys = append(keys, key)
		if hint, moved := movedToolKeys[key.Key]; moved {
			return keyLine{}, fmt.Errorf("%s:%d: %q is no longer a top-level field: %s", file, key.Line, key.Key, hint)
		}
		// `builtin: end_call` was the old scalar form. An inline mapping
		// (`builtin: {id: end_call}`) is the new shape written on one line, so
		// only a bare scalar earns the migration message.
		if key.Key == "builtin" && key.Value != "" && !strings.HasPrefix(key.Value, "{") {
			return keyLine{}, fmt.Errorf("%s:%d: `builtin:` is a block now: write `builtin:` then `  id: %s` on the next line", file, key.Line, key.Value)
		}
		for _, block := range executionBlocks {
			if key.Key == block {
				blocks = append(blocks, key)
			}
		}
	}
	switch len(blocks) {
	case 1:
		refused := map[string]map[string]string{
			"mcp":       contractToolKeys,
			"knowledge": knowledgeToolKeys,
		}[blocks[0].Key]
		if err := checkRefusedKeys(file, blocks[0].Key, refused, keys); err != nil {
			return keyLine{}, err
		}
		return blocks[0], nil
	case 0:
		return keyLine{}, fmt.Errorf("%s: no execution block: add one of %s", file, strings.Join(executionBlocks, ", "))
	default:
		return keyLine{}, fmt.Errorf("%s:%d: two execution blocks (%s and %s): a tool runs exactly one way",
			file, blocks[1].Line, blocks[0].Key, blocks[1].Key)
	}
}

// checkRefusedKeys names every refused field the file declared, each with its own
// line, so one edit pass removes them all instead of one per re-run.
func checkRefusedKeys(file, block string, refused map[string]string, keys []keyLine) error {
	var lines []string
	for _, key := range keys {
		reason, isRefused := refused[key.Key]
		if !isRefused {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s:%d: remove `%s`: it is not legal on %s `%s:` tool, %s",
			file, key.Line, key.Key, article(block), block, reason))
	}
	if len(lines) == 0 {
		return nil
	}
	return errors.New(strings.Join(lines, "\n"))
}

// article picks "an" or "a" so an error message reads like a sentence. Two block
// names reach here; a table would be more machinery than the vowel test.
func article(block string) string {
	if strings.ContainsRune("aeiou", rune(block[0])) {
		return "an"
	}
	return "a"
}

// checkToolBlockBody catches the block whose body is empty: `webhook:` with
// nothing under it decodes to a nil pointer, indistinguishable from an absent
// block, so without this the tool would fail much later as an empty execution
// kind with no file or line to look at.
func checkToolBlockBody(file string, block keyLine, tool Tool) error {
	if tool.ExecutionKind() != "" {
		return nil
	}
	if block.Key == "client" || block.Key == "provider_hosted" {
		return fmt.Errorf("%s:%d: `%s:` needs an explicit empty body: write `%s: {}`", file, block.Line, block.Key, block.Key)
	}
	return fmt.Errorf("%s:%d: `%s:` block is empty: add its fields", file, block.Line, block.Key)
}

// keyLine is one top-level mapping key: its name, 1-based line, and inline
// value when it has one on the same line.
type keyLine struct {
	Key   string
	Value string
	Line  int
}

// topLevelKeys scans column-zero mapping keys. A tool file is small and flat
// enough that a line scan beats a second YAML pass, and it works on files the
// strict decoder would reject.
func topLevelKeys(content []byte) []keyLine {
	var keys []keyLine
	for i, line := range strings.Split(string(content), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		// A quoted key is still that key: `"webhook":` must not read as absent.
		name = strings.Trim(name, `"'`)
		if !found || strings.ContainsAny(name, " \t") {
			continue
		}
		keys = append(keys, keyLine{Key: name, Value: strings.TrimSpace(value), Line: i + 1})
	}
	return keys
}
