package generate

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
)

// The code targets emit one Python project as several modules, and each
// module's imports are computed here rather than written in its template.
//
// A template says which names exist, under the same feature switches it always
// had, as one shared import block: the universe. Each module then gets the
// universe imports it references, plus a `from <module> import <name>` for
// every name another emitted module defines. So an import is present exactly
// when the module uses it, whatever mix of features a package switches on,
// which no hand-written import list per module could promise for combinations
// no example compiles.
//
// ponytail: the reference scan is lexical, not a Python parser. It drops
// comments and string contents, keeps f-string expressions, skips attributes
// and keyword arguments, and does not resolve scopes, so a local variable that
// shares a name with an import keeps that import. The emitted project's own
// ruff gate (make lint-emitted, and the smoke suite) is what catches that
// case; rename the local when it fires.

// pyModule is one emitted Python module before its imports are written: a
// module docstring, then code with no top-level import statement.
type pyModule struct {
	path string // "session.py", "utils/context.py"
	src  string
}

// moduleName is the dotted name the module is imported by.
func (m pyModule) moduleName() string {
	name := strings.TrimSuffix(strings.TrimSuffix(m.path, ".py"), "/__init__")
	return strings.ReplaceAll(name, "/", ".")
}

// renderPythonModules renders each module from its template, all of them one
// template set so a `define` in any is usable from every other, and links
// them. The universe is the `imports` template. A module with nothing past its
// docstring is left out. utils/ always gets its __init__.py, because the dev
// metrics module is emitted into it on every build.
func renderPythonModules(fsys embed.FS, dir string, funcs template.FuncMap, data any, modules []struct{ tmpl, path string }) ([]File, error) {
	names := []string{dir + "imports.py.tmpl"}
	for _, m := range modules {
		names = append(names, dir+m.tmpl+".tmpl")
	}
	set, err := template.New("").Funcs(funcs).ParseFS(fsys, names...)
	if err != nil {
		return nil, err
	}
	var universe bytes.Buffer
	if err := set.ExecuteTemplate(&universe, "imports", data); err != nil {
		return nil, err
	}
	lines := strings.Split(universe.String(), "\n")
	imports, end := parseImportBlock(lines)
	if rest := strings.TrimSpace(strings.Join(lines[end:], "\n")); rest != "" {
		return nil, fmt.Errorf("the imports template holds more than imports: %.60q", rest)
	}
	var rendered []pyModule
	for _, m := range modules {
		var buf bytes.Buffer
		if err := set.ExecuteTemplate(&buf, m.tmpl+".tmpl", data); err != nil {
			return nil, err
		}
		if _, body := splitDocstring(buf.String()); strings.TrimSpace(body) == "" {
			continue
		}
		rendered = append(rendered, pyModule{path: m.path, src: buf.String()})
	}
	files, err := linkPython(imports, rendered)
	if err != nil {
		return nil, err
	}
	return append(files, File{Path: "utils/__init__.py", Content: []byte(`"""Helpers the agent's modules share."""` + "\n")}), nil
}

// pyPrompt is one system prompt: the prompts/ constant that holds it and the
// Markdown file under prompts/ it is read from, without the suffix.
type pyPrompt struct {
	Const string
	File  string
	Text  string
}

// uniquePrompts drops a prompt listed twice with the same text, and refuses
// two different texts under one constant or one file.
func uniquePrompts(prompts []pyPrompt) ([]pyPrompt, error) {
	seen := map[string]string{}
	var out []pyPrompt
	for _, p := range prompts {
		constText, constSeen := seen["const "+p.Const]
		fileText, fileSeen := seen["file "+p.File]
		if constSeen && constText == p.Text && fileSeen && fileText == p.Text {
			continue
		}
		if constSeen || fileSeen {
			return nil, fmt.Errorf("two prompts compile to %s (prompts/%s.md); rename an agent or a task", p.Const, p.File)
		}
		seen["const "+p.Const], seen["file "+p.File] = p.Text, p.Text
		out = append(out, p)
	}
	return out, nil
}

// writePromptFiles writes each prompt as written, byte for byte, to prompts/.
func writePromptFiles(prompts []pyPrompt) []File {
	files := make([]File, 0, len(prompts))
	for _, p := range prompts {
		files = append(files, File{Path: "prompts/" + p.File + ".md", Content: []byte(p.Text)})
	}
	return files
}

// linkPython writes each module's import block. The universe is the shared
// import block every module draws from. An error names a module that uses a
// name two modules define, or a cycle between modules, because either would
// fail at import on every call.
func linkPython(universe []pyImport, modules []pyModule) ([]File, error) {
	owner := map[string]string{} // top-level name -> module path
	codes := make([]string, len(modules))
	for i, m := range modules {
		codes[i] = pyCode(m.src)
		for _, name := range pyDefinitions(codes[i]) {
			if other, taken := owner[name]; taken && other != m.path {
				return nil, fmt.Errorf("python module %s and %s both define %s", other, m.path, name)
			}
			owner[name] = m.path
		}
	}
	byPath := map[string]pyModule{}
	for _, m := range modules {
		byPath[m.path] = m
	}
	edges := map[string][]string{}
	files := make([]File, 0, len(modules))
	for i, m := range modules {
		for _, name := range quotedTypeNames(m.src) {
			if path, defined := owner[name]; defined && path != m.path {
				return nil, fmt.Errorf("python module %s names %s, which %s defines, inside a quoted annotation or cast; write it unquoted so its import is found", m.path, name, path)
			}
		}
		refs := pyReferences(codes[i])
		var imports []pyImport
		bound := map[string]bool{}
		for _, statement := range universe {
			kept := statement.referenced(codes[i], refs, owner, m.path)
			if kept == nil {
				continue
			}
			imports = append(imports, *kept)
			for _, name := range kept.boundNames() {
				bound[name] = true
			}
		}
		siblings := map[string][]string{}
		for name := range refs {
			path, defined := owner[name]
			if !defined || path == m.path {
				continue
			}
			if bound[name] {
				return nil, fmt.Errorf("python module %s imports %s and %s defines it too", m.path, name, path)
			}
			siblings[path] = append(siblings[path], name)
		}
		for path, names := range siblings {
			sort.Strings(names)
			imports = append(imports, pyImport{module: byPath[path].moduleName(), names: names})
			edges[m.path] = append(edges[m.path], path)
		}
		files = append(files, File{Path: m.path, Content: []byte(writeModule(m.src, imports))})
	}
	if cycle := importCycle(modules, edges); cycle != nil {
		return nil, fmt.Errorf("python modules import each other: %s", strings.Join(cycle, " -> "))
	}
	return files, nil
}

// pyQuotedType finds a type written as a string: the first argument of cast(),
// or an annotation after `:` or `->`. pyCode drops string contents, so a name
// used only there is invisible to the reference scan.
var pyQuotedType = regexp.MustCompile(`(?:\bcast\(\s*|->\s*|\w\s*:\s*)"([A-Za-z_][\w.\[\], |]*)"`)

// quotedTypeNames are the names inside every quoted type in src.
func quotedTypeNames(src string) []string {
	var names []string
	for _, m := range pyQuotedType.FindAllStringSubmatch(src, -1) {
		names = append(names, pyNameToken.FindAllString(m[1], -1)...)
	}
	return names
}

// referenced returns the part of an import statement this module uses, or nil.
func (s pyImport) referenced(code string, refs map[string]bool, owner map[string]string, self string) *pyImport {
	if s.names == nil {
		name := s.boundNames()[0]
		if owner[name] == self {
			return nil
		}
		module, alias, aliased := strings.Cut(s.module, " as ")
		if aliased || !strings.Contains(module, ".") {
			if refs[strings.TrimSpace(alias)] || (!aliased && refs[module]) {
				return &s
			}
			return nil
		}
		// `import a.b` binds `a` and is used only where `a.b` is read, and ruff
		// holds each dotted import to its own path.
		used := regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(module) + `\b`)
		if used.MatchString(code) {
			return &s
		}
		return nil
	}
	var names []string
	for _, member := range s.names {
		name, alias, aliased := strings.Cut(member, " as ")
		if aliased {
			name = alias
		}
		name = strings.TrimSpace(name)
		if refs[name] && owner[name] != self {
			names = append(names, member)
		}
	}
	if names == nil {
		return nil
	}
	return &pyImport{module: s.module, names: names}
}

// boundNames are the names the statement adds to the module's namespace.
func (s pyImport) boundNames() []string {
	if s.names == nil {
		module, alias, aliased := strings.Cut(s.module, " as ")
		if aliased {
			return []string{strings.TrimSpace(alias)}
		}
		top, _, _ := strings.Cut(module, ".")
		return []string{top}
	}
	names := make([]string, 0, len(s.names))
	for _, member := range s.names {
		name, alias, aliased := strings.Cut(member, " as ")
		if aliased {
			name = alias
		}
		names = append(names, strings.TrimSpace(name))
	}
	return names
}

// writeModule puts the import block after the module docstring, with the blank
// lines ruff's import sorter wants before whatever comes next: two before a
// definition, one before anything else.
func writeModule(src string, imports []pyImport) string {
	doc, body := splitDocstring(src)
	body = strings.Trim(body, "\n")
	if body != "" {
		body += "\n"
	}
	var b strings.Builder
	b.WriteString(doc)
	b.WriteString("\n\nfrom __future__ import annotations\n")
	if block := renderImportBlock(imports); block != "" {
		b.WriteString("\n" + block)
	}
	if body == "" {
		return b.String()
	}
	b.WriteString("\n")
	if startsWithDefinition(body) {
		b.WriteString("\n")
	}
	b.WriteString(body)
	return b.String()
}

// splitDocstring cuts the module docstring off the front of src.
func splitDocstring(src string) (doc, rest string) {
	src = strings.TrimLeft(src, "\n")
	if !strings.HasPrefix(src, `"""`) {
		return "", src
	}
	end := strings.Index(src[3:], `"""`)
	if end < 0 {
		return "", src
	}
	cut := 3 + end + 3
	return src[:cut], src[cut:]
}

// startsWithDefinition reports whether the body opens on a def, a class or a
// decorator. A comment right above one belongs to it; a comment with a blank
// line under it stands alone, and ruff counts it as the first statement.
func startsWithDefinition(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return false
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "async def ") ||
			strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "@")
	}
	return false
}

// importCycle returns one cycle in the module graph, or nil.
func importCycle(modules []pyModule, edges map[string][]string) []string {
	const (
		unseen = iota
		open
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(path string) []string {
		state[path] = open
		stack = append(stack, path)
		targets := slices.Clone(edges[path])
		sort.Strings(targets)
		for _, next := range targets {
			switch state[next] {
			case open:
				at := slices.Index(stack, next)
				return append(slices.Clone(stack[at:]), next)
			case unseen:
				if cycle := visit(next); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[path] = done
		return nil
	}
	for _, m := range modules {
		if state[m.path] == unseen {
			if cycle := visit(m.path); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// pyCode returns src with comments and string contents removed, keeping
// f-string expressions, so what is left is only names and operators. A string
// becomes `""`, and an f-string `""` followed by each expression in brackets.
// Newlines inside a string are dropped with it, so a line of the result that
// starts at column 0 is a real top-level statement.
func pyCode(src string) string {
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); {
		c := src[i]
		switch c {
		case '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case '"', '\'':
			prefix := stringPrefix(src, i)
			out = out[:len(out)-len(prefix)]
			var literal strings.Builder
			i = skipString(src, i, strings.ContainsAny(prefix, "fF"), &literal)
			out = append(out, literal.String()...)
		default:
			out = append(out, c)
			i++
		}
	}
	return string(out)
}

// stringPrefix is the r/b/u/f prefix written right before the quote at i.
func stringPrefix(src string, i int) string {
	start := i
	for start > 0 && i-start < 2 && strings.ContainsRune("rRbBuUfF", rune(src[start-1])) {
		start--
	}
	if start > 0 && isNameByte(src[start-1]) {
		return ""
	}
	return src[start:i]
}

// skipString moves past the string literal opening at i, writing its
// placeholder, and returns the index after it.
func skipString(src string, i int, fstring bool, out *strings.Builder) int {
	quote := src[i : i+1]
	if strings.HasPrefix(src[i:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	i += len(quote)
	out.WriteString(`""`)
	for i < len(src) {
		switch {
		case src[i] == '\\':
			i += 2
		case strings.HasPrefix(src[i:], quote):
			return i + len(quote)
		case len(quote) == 1 && src[i] == '\n':
			return i // unterminated; stop at the line end
		case fstring && strings.HasPrefix(src[i:], "{{"):
			i += 2
		case fstring && src[i] == '{':
			var expr strings.Builder
			i = fstringExpression(src, i+1, &expr)
			out.WriteString("(" + pyCode(expr.String()) + ")")
		default:
			i++
		}
	}
	return i
}

// fstringExpression copies one replacement field's expression, starting after
// its `{`, and returns the index after its closing `}`. A format spec's own
// nested fields are expressions too; the rest of a spec is not.
func fstringExpression(src string, i int, expr *strings.Builder) int {
	depth := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			// A string inside the expression, copied whole so its quotes and
			// brackets do not count.
			end := i + 1
			for end < len(src) && src[end] != c {
				end++
			}
			expr.WriteString(src[i : end+1])
			i = end + 1
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == '}' && depth == 0:
			return i + 1
		case c == '}':
			depth--
		case depth == 0 && (c == ':' || (c == '!' && i+1 < len(src) && src[i+1] != '=')):
			for i < len(src) && src[i] != '}' {
				if src[i] == '{' {
					expr.WriteString(" ")
					i = fstringExpression(src, i+1, expr)
					continue
				}
				i++
			}
			return i + 1
		}
		expr.WriteByte(c)
		i++
	}
	return i
}

func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

var (
	pyDefLine    = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_]\w*)|^class\s+([A-Za-z_]\w*)`)
	pyAssignLine = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?::[^=]*)?=(?:[^=]|$)|^([A-Za-z_]\w*)\s*:\s*[^=\s]`)
	pyBlockLine  = regexp.MustCompile(`^(if|elif|else|try|except|finally|with|for|while)\b`)
	pyNameToken  = regexp.MustCompile(`[A-Za-z_]\w*`)
	pyKeywords   = map[string]bool{
		"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
		"async": true, "await": true, "break": true, "class": true, "continue": true,
		"def": true, "del": true, "elif": true, "else": true, "except": true,
		"finally": true, "for": true, "from": true, "global": true, "if": true,
		"import": true, "in": true, "is": true, "lambda": true, "nonlocal": true,
		"not": true, "or": true, "pass": true, "raise": true, "return": true,
		"try": true, "while": true, "with": true, "yield": true,
	}
)

// pyDefinitions lists the module-level names code defines: its top-level
// functions, classes and assignments, including those inside a top-level if,
// try or with block.
func pyDefinitions(code string) []string {
	var names []string
	block, blockIndent := false, -1
	for line := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(trimmed)
		if indent == 0 {
			block = pyBlockLine.MatchString(line)
			blockIndent = -1
		} else if block {
			if blockIndent < 0 {
				blockIndent = indent
			}
			if indent != blockIndent {
				continue
			}
		} else {
			continue
		}
		if m := pyDefLine.FindStringSubmatch(trimmed); m != nil {
			names = append(names, m[1]+m[2])
		} else if m := pyAssignLine.FindStringSubmatch(trimmed); m != nil && !pyKeywords[m[1]+m[2]] {
			names = append(names, m[1]+m[2])
		}
	}
	return names
}

// pyReferences lists the module-level names code reads: every name read
// somewhere and not bound in the function that reads it or one around it. A
// name after a dot is an attribute and a name before `=` inside brackets is a
// keyword argument, so neither is read.
//
// The binding rules are the plain ones: parameters, `name =` and `a, b =`
// assignments, `for` and comprehension targets, `as` names, walrus targets,
// lambda parameters, and what an indented import binds. They err on one side
// only. A binding this misses keeps an import the module does not need, which
// the project's ruff reports; a binding it invented would drop one the module
// does need, which Python reports on a call.
func pyReferences(code string) map[string]bool {
	type scope struct {
		indent int
		class  bool
		parent *scope
		bound  map[string]bool
		global map[string]bool
		reads  map[string]bool
	}
	newScope := func(indent int, class bool, parent *scope) *scope {
		return &scope{indent: indent, class: class, parent: parent,
			bound: map[string]bool{}, global: map[string]bool{}, reads: map[string]bool{}}
	}
	module := newScope(-1, false, nil)
	all := []*scope{module}
	current := module
	for _, line := range pyLogicalLines(code) {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		for current != module && indent <= current.indent {
			current = current.parent
		}
		if m := pyImportStatement.FindStringSubmatch(trimmed); m != nil {
			for _, name := range importedNames(m[1] != "", m[2]) {
				current.bound[name] = true
			}
			continue
		}
		if m := pyHeader.FindStringSubmatch(trimmed); m != nil {
			current.bound[m[2]] = true
			header := newScope(indent, m[1] == "class", current)
			all = append(all, header)
			if m[1] != "class" {
				for _, name := range pyParameters(trimmed[len(m[0]):]) {
					header.bound[name] = true
				}
			}
			current = header
		}
		for _, name := range pyBindings(trimmed) {
			current.bound[name] = true
		}
		if m := pyGlobalStatement.FindStringSubmatch(trimmed); m != nil {
			// A nonlocal name is bound in the function around this one.
			for _, name := range strings.Split(m[2], ",") {
				current.global[strings.TrimSpace(name)] = m[1] == "global"
			}
			continue
		}
		for name := range pyReads(trimmed) {
			current.reads[name] = true
		}
	}
	refs := map[string]bool{}
	for _, s := range all {
		for name := range s.reads {
			if s == module || s.global[name] {
				refs[name] = true
				continue
			}
			if s.bound[name] {
				continue
			}
			// A method cannot see its class body's names, so a class scope
			// above the reader is skipped.
			outer := s.parent
			for outer != module && (outer.class || !outer.bound[name]) {
				outer = outer.parent
			}
			if outer == module {
				refs[name] = true
			}
		}
	}
	return refs
}

var (
	pyHeader          = regexp.MustCompile(`^(?:async\s+)?(def|class)\s+([A-Za-z_]\w*)`)
	pyImportStatement = regexp.MustCompile(`^(from\s+\S+\s+)?import\s+(.*)$`)
	pyGlobalStatement = regexp.MustCompile(`^(global|nonlocal)\s+(.*)$`)
	pyAssignTargets   = regexp.MustCompile(`^([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s*(?::[^=]*)?(?:[-+*/%&|^@]|//|\*\*|<<|>>)?=(?:[^=]|$)`)
	pyAnnotatedName   = regexp.MustCompile(`^([A-Za-z_]\w*)\s*:\s*[^=\s]`)
	pyForTargets      = regexp.MustCompile(`\bfor\s+([A-Za-z_][\w\s,()]*?)\s+in\b`)
	pyAsTarget        = regexp.MustCompile(`\bas\s+([A-Za-z_]\w*)`)
	pyWalrusTarget    = regexp.MustCompile(`([A-Za-z_]\w*)\s*:=`)
	pyLambdaParams    = regexp.MustCompile(`\blambda\s+([^:]*):`)
)

// pyLogicalLines joins each statement's physical lines, so a bracket that
// spans lines is one line, and drops blank ones.
func pyLogicalLines(code string) []string {
	var lines []string
	var statement strings.Builder
	depth := 0
	for line := range strings.SplitSeq(code, "\n") {
		if depth == 0 && strings.TrimSpace(line) == "" {
			continue
		}
		if depth > 0 {
			statement.WriteString(" " + strings.TrimSpace(line))
		} else {
			statement.WriteString(line)
		}
		for i := range len(line) {
			switch line[i] {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			}
		}
		if depth <= 0 && !strings.HasSuffix(strings.TrimRight(line, " "), "\\") {
			depth = 0
			lines = append(lines, statement.String())
			statement.Reset()
		}
	}
	if statement.Len() > 0 {
		lines = append(lines, statement.String())
	}
	return lines
}

// importedNames are the names an import statement binds.
func importedNames(from bool, names string) []string {
	var bound []string
	for _, member := range strings.Split(strings.Trim(strings.TrimSpace(names), "()"), ",") {
		name, alias, aliased := strings.Cut(strings.TrimSpace(member), " as ")
		if aliased {
			name = alias
		} else if !from {
			name, _, _ = strings.Cut(name, ".")
		}
		if name = strings.TrimSpace(name); name != "" {
			bound = append(bound, name)
		}
	}
	return bound
}

// pyParameters are the parameter names of the def whose header continues at
// rest: the text from the name onwards, with the whole parameter list on it.
func pyParameters(rest string) []string {
	open := strings.Index(rest, "(")
	if open < 0 {
		return nil
	}
	var names []string
	depth, start := 0, open+1
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			depth--
			if depth > 0 {
				continue
			}
		case ',':
			if depth != 1 {
				continue
			}
		default:
			continue
		}
		param := strings.TrimLeft(strings.TrimSpace(rest[start:i]), "*")
		if m := pyNameToken.FindString(param); m != "" && strings.HasPrefix(param, m) {
			names = append(names, m)
		}
		start = i + 1
		if depth == 0 {
			break
		}
	}
	return names
}

// pyBindings are the names one logical line binds other than by def, class or
// import.
func pyBindings(line string) []string {
	var names []string
	add := func(list string) {
		for _, name := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == '(' || r == ')' || r == ' ' }) {
			if !pyKeywords[name] {
				names = append(names, name)
			}
		}
	}
	if m := pyAnnotatedName.FindStringSubmatch(line); m != nil {
		add(m[1]) // an annotation names a field, and is not a read of it
	}
	if m := pyAssignTargets.FindStringSubmatch(line); m != nil {
		add(m[1])
	}
	for _, m := range pyForTargets.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range pyAsTarget.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range pyWalrusTarget.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range pyLambdaParams.FindAllStringSubmatch(line, -1) {
		for _, param := range strings.Split(m[1], ",") {
			param, _, _ = strings.Cut(strings.TrimLeft(strings.TrimSpace(param), "*"), "=")
			add(param)
		}
	}
	return names
}

// pyReads are the names one logical line reads: not after a dot, not a
// keyword argument, not a keyword.
func pyReads(code string) map[string]bool {
	reads := map[string]bool{}
	depth := 0
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			depth--
			continue
		}
		if !isNameByte(code[i]) || (i > 0 && isNameByte(code[i-1])) {
			continue
		}
		loc := pyNameToken.FindStringIndex(code[i:])
		if loc == nil || loc[0] != 0 {
			continue // a number
		}
		name := code[i : i+loc[1]]
		end := i + loc[1]
		i = end - 1
		if pyKeywords[name] || previousByte(code, end-len(name)) == '.' {
			continue
		}
		if depth > 0 {
			after := strings.TrimLeft(code[end:], " \t")
			if strings.HasPrefix(after, "=") && !strings.HasPrefix(after, "==") {
				continue
			}
		}
		reads[name] = true
	}
	return reads
}

// previousByte is the last non-space byte before i.
func previousByte(code string, i int) byte {
	for i--; i >= 0; i-- {
		if code[i] != ' ' && code[i] != '\t' && code[i] != '\n' {
			return code[i]
		}
	}
	return 0
}

// parseImportBlock reads the import statements at the start of lines, blank
// lines between them included, and returns them with the index of the first
// line after the block.
func parseImportBlock(lines []string) ([]pyImport, int) {
	var imports []pyImport
	end := 0
	for end < len(lines) {
		line := lines[end]
		switch {
		case strings.TrimSpace(line) == "":
			end++
		case strings.HasPrefix(line, "import "):
			imports = append(imports, pyImport{module: strings.TrimPrefix(line, "import ")})
			end++
		case strings.HasPrefix(line, "from "):
			statement := line
			end++
			for strings.Contains(statement, "(") && !strings.Contains(statement, ")") && end < len(lines) {
				statement += " " + strings.TrimSpace(lines[end])
				end++
			}
			module, names, _ := strings.Cut(strings.TrimPrefix(statement, "from "), " import ")
			names = strings.NewReplacer("(", "", ")", "").Replace(names)
			var members []string
			for _, name := range strings.Split(names, ",") {
				if name = strings.TrimSpace(name); name != "" {
					members = append(members, name)
				}
			}
			imports = append(imports, pyImport{module: module, names: members})
		default:
			return imports, end
		}
	}
	return imports, end
}

// renderImportBlock writes the statements the way ruff's isort rule (I001)
// writes them: standard library, third party, then first party, plain `import`
// statements ahead of `from` ones, members ordered constants, classes, then
// everything else, and two statements from one module merged into one.
func renderImportBlock(imports []pyImport) string {
	var sections [3][]pyImport
	merged := map[string]int{}
	for _, statement := range imports {
		section := 1
		top, _, _ := strings.Cut(statement.module, ".")
		if stdlibModules[top] {
			section = 0
		} else if firstPartyModules[top] {
			section = 2
		}
		key := fmt.Sprintf("%d %t %s", section, statement.names == nil, statement.module)
		if index, seen := merged[key]; seen {
			existing := &sections[section][index]
			for _, name := range statement.names {
				if !slices.Contains(existing.names, name) {
					existing.names = append(existing.names, name)
				}
			}
			continue
		}
		merged[key] = len(sections[section])
		statement.names = slices.Clone(statement.names)
		sections[section] = append(sections[section], statement)
	}
	var out strings.Builder
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		sort.SliceStable(section, func(i, j int) bool {
			a, b := section[i], section[j]
			if (a.names == nil) != (b.names == nil) {
				return a.names == nil
			}
			return strings.ToLower(a.module) < strings.ToLower(b.module)
		})
		for _, statement := range section {
			out.WriteString(statement.render())
		}
	}
	return out.String()
}
