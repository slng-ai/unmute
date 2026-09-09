package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
)

// Checking a package's supplied arguments against a published contract.
//
// An `inject:` entry supplies part of a tool's input and leaves the rest to the
// model. That is the whole reason this is not one call to a schema validator:
// a partial object is not a complete invocation, so validating it against the
// tool's whole schema would refuse every legitimate argument the model is meant
// to fill in. Stripping `required` recursively to get around that is worse,
// because it also weakens the constraints on the values that ARE supplied.
//
// So each supplied key is checked against its own property schema, and the
// arguments left to the model are checked for nothing, which is correct: nobody
// knows what they will be yet.
//
// Three further restrictions come from the platform rather than from JSON
// Schema, and each is a refusal a schema check alone would miss:
//
//   - An override must be a DECLARED property. A schema that allows additional
//     properties allows the MODEL to send extras; it does not make an
//     attachment able to pin one, because the attachment stores overrides
//     against the declared parameter list. The refusal has to say that, and not
//     claim the schema forbids the argument, because it does not.
//   - An override's destination must resolve to EXACTLY ONE non-null scalar
//     type. A parameter that could be a string or an integer is refused by the
//     platform with a message about leaving an ambiguous argument
//     model-supplied, and a schema validator accepts a string against it
//     happily, so this is the restriction a schema check is least able to see.
//   - An override is a scalar or one whole variable token. ir.validateSlngInject
//     holds the authored side of that offline; this is where it meets a real
//     schema.
//
// And one thing this deliberately does not do: it never claims to have
// validated a value that does not exist yet. A `{{customer_name}}` override is
// checked for its declaration and its destination, and the value itself is the
// platform's to validate when a call starts. A stored value is text, and the
// platform converts it into the parameter's declared scalar type, so the
// destination does not have to be a string: an integer parameter takes a
// variable whose stored text is a JSON integer.

// bindingCheck is one supplied argument's outcome.
type bindingCheck struct {
	Tool     string
	Argument string
	// Blocked is set when the deployment must stop. Detail says why, in words
	// naming the tool, the argument, the contract and the file.
	Blocked bool
	Detail  string
	// Deferred marks an override whose value arrives when a call starts, so a
	// reader is not told it was validated.
	Deferred bool
}

// checkBindings validates every supplied argument on every resolved reference.
//
// injected is the authored `inject:` map per package tool name, taken from the
// IR rather than from the emitted body, so the values are the author's own and
// the source location in a refusal is a file the author can open.
func checkBindings(resolved []resolvedTool, injected map[string]map[string]any) []bindingCheck {
	var checks []bindingCheck
	for _, reference := range resolved {
		if !reference.usable() {
			// Nothing to check against. The resolution's own finding already
			// says why, and a second refusal about its arguments would send the
			// author to the wrong problem.
			continue
		}
		origin, source := requirementFile(reference.Requirement), reference.Requirement.Source
		arguments := injected[source]
		for _, key := range sortedMapKeys(arguments) {
			checks = append(checks, checkOneBinding(reference, origin, key, arguments[key]))
		}
	}
	return checks
}

// checkOneBinding is the whole decision for one supplied argument.
func checkOneBinding(reference resolvedTool, origin, key string, value any) bindingCheck {
	out := bindingCheck{Tool: reference.Requirement.Name, Argument: key}
	blocked := func(format string, args ...any) bindingCheck {
		out.Blocked = true
		out.Detail = fmt.Sprintf("%s injects %q into `%s` version %d, and %s",
			origin, key, reference.Requirement.Name, reference.Version, fmt.Sprintf(format, args...))
		return out
	}

	schema := reference.Snapshot.ArgumentSchema
	if len(schema) == 0 {
		// No published schema to check against. Not a pass: the promise is that
		// a binding was checked, and an empty schema accepts everything, so
		// treating this as satisfied is the one outcome that would make the
		// check worse than not having it.
		return blocked("that version publishes no parameter schema, so nothing could check this argument: the deploy will not attach an argument it could not check. Publish the tool's parameters, or drop the `inject:` entry and let the model supply the value")
	}
	properties, _ := schema["properties"].(map[string]any)
	property, declared := properties[key].(map[string]any)
	if !declared {
		names := sortedMapKeys(properties)
		// Deliberately does NOT say the schema forbids it. A schema permitting
		// additional properties permits the model to send extras and still
		// cannot pin one from an attachment, and misreporting that would send an
		// author to edit a schema that is already right.
		if extra, ok := schema["additionalProperties"].(bool); ok && extra {
			return blocked("that version does not declare a parameter called %q. Its published parameters are %s. The schema lets the model send extra arguments, and an attachment can only fix a declared one, so this override has nowhere to bind",
				key, joinNames(names))
		}
		return blocked("that version does not declare a parameter called %q, and its schema accepts no others. Its published parameters are %s",
			key, joinNames(names))
	}

	// Every pointer this check will not follow, at any depth and through the
	// definitions it does follow. It runs before the constraint and type work
	// below because both of those read the same pointers, and neither can
	// report a constraint it never read.
	if err := checkReferences(schema, property); err != nil {
		return blocked("%v", err)
	}

	// A constraint at the ROOT that reaches this property. The per-property walk
	// cannot see one, and a root `allOf` carrying `properties: {query: {const:
	// ...}}` is legal on a published schema, so a fixed value violating it was
	// reported as checked. Refused rather than evaluated: a root composition
	// keyword may also carry `required`, and applying it to the supplied part
	// alone would refuse every argument the model is meant to fill in.
	if keyword := rootConstraintOn(schema, key); keyword != "" {
		return blocked("that version constrains %q from the root of its schema, under %q. Checking one supplied argument against a constraint written for the whole invocation would refuse the arguments the model fills in, so this one is not reported as checked: supply the value from the model instead, or publish the constraint on the parameter itself",
			key, keyword)
	}

	// One non-null scalar type, which is the platform's own restriction on any
	// pinned argument and the one a schema validator cannot see: a parameter
	// declared as a string OR an integer accepts a string perfectly well and is
	// still refused at attach time.
	kind, err := scalarDestination(schema, property)
	if err != nil {
		return blocked("%v", err)
	}

	if text, ok := value.(string); ok && ir.TemplateVar(text) != "" {
		// One whole token. Its destination is checked and its value is not,
		// because the value belongs to a call that has not happened.
		out.Deferred = true
		out.Detail = fmt.Sprintf("%s supplies `%s` from {{%s}}, which SLNG resolves when a call starts: the deploy checked that the parameter exists and takes one %s value, and the stored text is converted and checked then",
			origin, key, ir.TemplateVar(text), kind)
		return out
	}

	if err := validateAgainstProperty(schema, property, value); err != nil {
		return blocked("%v", err)
	}
	return out
}

// scalarDestination resolves a parameter to the one non-null scalar type an
// override may be pinned to, and names it.
//
// This mirrors the platform's own resolution, which is the point: an attachment
// stores a pinned argument against a parameter, and the platform refuses one
// whose type it cannot settle to a single scalar, telling the author to leave an
// ambiguous, array or object argument model-supplied. A JSON Schema validator
// sees nothing wrong with a string against `["string","integer"]`, so without
// this the deploy passed its own check and the push refused it.
//
// It also replaces a restriction that was wrong in the other direction. This
// used to require a string, on the reasoning that a stored value arrives as
// text. The platform converts that text into the parameter's declared scalar
// type instead, by parsing it as a JSON literal, so an integer parameter takes
// a variable whose stored text is `42` and refusing it was refusing something
// that works.
func scalarDestination(root, property map[string]any) (string, error) {
	types := withoutNull(resolveScalarTypes(root, property, nil))
	switch {
	case len(types) == 1:
		kind := types[0]
		if !slices.Contains([]string{"string", "integer", "number", "boolean"}, kind) {
			return "", fmt.Errorf("that parameter is %s, and a pinned argument is a single scalar: leave it to the model, which can send %s", article(kind), article(kind))
		}
		return kind, nil
	case len(types) == 0:
		// A contradiction rather than an absence: a declared type and a
		// constraint on it that have no type in common. It reads differently
		// from the case below, so it says something different.
		return "", fmt.Errorf("that parameter's declared type and the constraints on it have no type in common, so there is nothing a pinned argument could be: leave the value to the model")
	case slices.Equal(types, withoutNull(jsonSchemaTypes)):
		// Nothing narrowed it at all, which is what an untyped parameter looks
		// like once the platform's resolution has run: every type is still
		// possible. Listing all six back at the author would be noise.
		return "", fmt.Errorf("nothing in that parameter's schema settles what type it holds, and a pinned argument has to resolve to exactly one scalar type: publish a type for the parameter, or leave the value to the model")
	default:
		return "", fmt.Errorf("that parameter may be %s, and a pinned argument has to resolve to exactly one scalar type: leave an argument this open to the model, which can send any of them",
			joinNames(types))
	}
}

// article renders a type name in a sentence.
func article(kind string) string {
	switch kind {
	case "array", "object", "integer":
		return "an " + kind
	default:
		return "a " + kind
	}
}

// jsonSchemaTypes is where the resolution below STARTS for a node that declares
// no `type`, and it is not a detail: the platform starts from every type and
// narrows, so `{"allOf": [{"type": "integer"}]}` resolves to an integer. Reading
// an absent `type` as "no types" instead is what made a parameter narrowed by a
// constraint rather than by its own keyword resolve to nothing and be refused.
//
// Sorted, because a resolved list is compared against it and rendered from it.
var jsonSchemaTypes = []string{"array", "boolean", "integer", "null", "number", "object", "string"}

// resolveScalarTypes is the type a parameter settles on.
//
// This mirrors the platform's own `resolve_non_null_json_types` deliberately and
// closely, because a difference between the two is a deployment that passes its
// own check and is refused at the push, or the reverse. Four things narrow a
// node, and every one of them applies: its own `type`, the definition it points
// at, the union of its `anyOf`/`oneOf` branches, and each of its `allOf`
// branches. They are INTERSECTED, and the earlier version of this returned the
// declared `type` and stopped, so `{"type": ["string","integer"], "allOf":
// [{"type": "integer"}]}` was reported as ambiguous when it settles on integer.
//
// `null` travels through the resolution and is dropped at the end, the way the
// platform drops it, so a nullable parameter is one scalar type that also
// accepts nothing rather than two types.
//
// visited stops a schema that points at itself. The platform refuses a recursive
// local reference at publish time, so this is a floor rather than a feature.
// A pointer it cannot follow is IGNORED here, not refused, because that is what
// the platform does; checkReferences is what refuses one, before this runs.
func resolveScalarTypes(root, node map[string]any, visited []string) []string {
	resolved := propertyTypes(node)
	if resolved == nil {
		resolved = slices.Clone(jsonSchemaTypes)
	}
	if ref, ok := node["$ref"].(string); ok && !slices.Contains(visited, ref) {
		if target, found := localTarget(root, ref); found {
			resolved = intersectTypes(resolved, resolveScalarTypes(root, target, append(slices.Clone(visited), ref)))
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		branches, ok := node[keyword].([]any)
		if !ok || len(branches) == 0 {
			continue
		}
		var union []string
		for _, branch := range branches {
			shape, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			for _, kind := range resolveScalarTypes(root, shape, visited) {
				if !slices.Contains(union, kind) {
					union = append(union, kind)
				}
			}
		}
		resolved = intersectTypes(resolved, union)
	}
	if branches, ok := node["allOf"].([]any); ok {
		for _, branch := range branches {
			if shape, ok := branch.(map[string]any); ok {
				resolved = intersectTypes(resolved, resolveScalarTypes(root, shape, visited))
			}
		}
	}
	return resolved
}

// intersectTypes narrows one type set by another, carrying the platform's own
// exception: an integer satisfies a `number` constraint, so `number` narrowed by
// `integer` is `integer` rather than nothing at all.
func intersectTypes(left, right []string) []string {
	var kept []string
	for _, kind := range left {
		if slices.Contains(right, kind) {
			kept = append(kept, kind)
		}
	}
	only := func(set []string, kind string) bool { return len(set) == 1 && set[0] == kind }
	if (only(left, "number") && only(right, "integer")) || (only(left, "integer") && only(right, "number")) {
		if !slices.Contains(kept, "integer") {
			kept = append(kept, "integer")
		}
	}
	sort.Strings(kept)
	return kept
}

// withoutNull drops the nullable spelling, which the platform does once at the
// end of its resolution rather than at each step.
func withoutNull(types []string) []string {
	var out []string
	for _, kind := range types {
		if kind != "null" {
			out = append(out, kind)
		}
	}
	return out
}

// localTarget resolves one `#/$defs/<name>` pointer against the published root.
//
// Only that shape, because it is the only one the platform stores: a published
// parameter schema's references are held to `^#/$defs/...$`, and `definitions`
// is not on its supported keyword list at all. Anything else is a pointer this
// check does not follow, which unresolvableRef puts into words.
func localTarget(root map[string]any, ref string) (map[string]any, bool) {
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return nil, false
	}
	definitions, _ := root["$defs"].(map[string]any)
	target, ok := definitions[name].(map[string]any)
	return target, ok
}

// rootConstraintOn reports the root keyword that constrains one property, or
// "".
//
// Only the keywords a published schema may actually carry at its root are
// examined: the platform's supported list allows `allOf`, `anyOf`, `oneOf`,
// `not` and `$ref` there, and rejects `if`/`then`/`else`, `dependentSchemas`,
// `dependentRequired`, `patternProperties`, `propertyNames` and
// `unevaluatedProperties` at publish time, so a schema carrying one of those
// could not have been stored.
//
// Relevance is decided by whether the subtree names this property under a
// `properties` map. A root constraint about other parameters is not this
// argument's problem, and blocking on its mere presence would refuse a package
// over a part of a published schema that has nothing to do with it.
func rootConstraintOn(root map[string]any, key string) string {
	for _, keyword := range []string{"allOf", "anyOf", "oneOf", "not", "$ref"} {
		subtree, present := root[keyword]
		if !present {
			continue
		}
		if ref, ok := subtree.(string); ok && keyword == "$ref" {
			// Wrapped so the walk below meets it as a node with a pointer,
			// which is the one thing it knows how to follow.
			subtree = map[string]any{"$ref": ref}
		}
		if mentionsProperty(root, subtree, key) {
			return keyword
		}
	}
	return ""
}

// mentionsProperty reports whether a schema subtree declares a property of this
// name anywhere it reaches, POINTERS INCLUDED.
//
// Following them is the whole correction here. A root composition branch is
// usually a `$ref` into `$defs` rather than an inline object, and a walk that
// stopped at the pointer saw a branch mentioning no property, ruled it
// irrelevant, and reported an argument as checked against a constraint nobody
// had read. A pointer this check cannot follow counts as relevant for the same
// reason: an unread constraint may say anything.
func mentionsProperty(root map[string]any, subtree any, key string) bool {
	nodes, unfollowed := reachable(root, subtree)
	if len(unfollowed) > 0 {
		return true
	}
	for _, node := range nodes {
		properties, ok := node["properties"].(map[string]any)
		if !ok {
			continue
		}
		if _, declared := properties[key]; declared {
			return true
		}
	}
	return false
}

// propertyTypes reads a property's declared types, accepting both spellings:
// `"type": "string"` and `"type": ["string", "null"]`. A nullable parameter is
// the second, and reading only the first would refuse it.
//
// `null` is kept. It is dropped once, at the end of the resolution, because that
// is where the platform drops it, and dropping it here instead would make an
// intersection with a `null` branch mean something different.
//
// nil and empty are distinguished by the caller: a node with no `type` starts
// from every type, and a `"type": []` that narrows to nothing is a contradiction.
func propertyTypes(property map[string]any) []string {
	switch declared := property["type"].(type) {
	case string:
		return []string{declared}
	case []any:
		types := []string{}
		for _, entry := range declared {
			if text, ok := entry.(string); ok {
				types = append(types, text)
			}
		}
		return types
	}
	return nil
}

// validateAgainstProperty checks one fixed value against one property schema,
// through the pinned library rather than by hand.
//
// The value is validated as itself, with no coercion: a fixed `"0"` is a string
// and a fixed `0` is a number, and silently accepting one for the other is how a
// tool receives an argument of the wrong type and reports something unrelated.
//
// root is the whole published schema, and it is here for one reason: the
// property's local references live in the ROOT's `$defs`. Marshalling the
// property alone and resolving it as its own document was a document with no
// `$defs` in it, so `{"$ref": "#/$defs/Query"}` on a perfectly good published
// schema failed to resolve and the argument was refused. The definitions travel
// with the property now, and a reference pointing anywhere this does not carry
// is refused by name rather than resolved against a document that lacks it.
//
// Remote references never reach here: checkReferences refuses them before this
// runs. A `$ref` to a URL would mean a deployment check that reaches out to
// whatever that URL serves, which is neither reproducible nor safe, and a schema
// whose applicable constraints cannot be checked is an explicit blocker rather
// than a quiet pass.
func validateAgainstProperty(root, property map[string]any, value any) error {
	if keyword := unsupportedKeyword(property); keyword != "" {
		return fmt.Errorf("that parameter's schema uses %q, which this check cannot evaluate reliably, so the argument was not checked and the deploy will not attach it: supply the value from the model instead, or simplify the published schema", keyword)
	}

	raw, err := json.Marshal(withDefinitions(root, property))
	if err != nil {
		return fmt.Errorf("that parameter's schema could not be read: %w", err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("that parameter's schema could not be read: %w", err)
	}
	resolved, err := parsed.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: false})
	if err != nil {
		return fmt.Errorf("that parameter's schema could not be prepared for checking: %w", err)
	}
	// Round-tripped through JSON so the value the validator sees is the value
	// the body will carry: YAML gives an int where JSON gives a float64, and
	// checking the Go value directly would disagree with what is sent.
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("the injected value cannot be written as JSON: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return fmt.Errorf("the injected value cannot be read back as JSON: %w", err)
	}
	if err := resolved.Validate(decoded); err != nil {
		return fmt.Errorf("the value does not fit that parameter: %s", firstLine(err.Error()))
	}
	return nil
}

// definitionKeywords are where a local reference may point.
//
// `$defs` alone. A published parameter schema's references are held to
// `^#/$defs/...$` and the legacy `definitions` keyword is not on the platform's
// supported list at all, so a `#/definitions/` pointer is not something a
// published schema can carry, and accepting one would be carrying breadth for a
// case that cannot arise.
var definitionKeywords = []string{"$defs"}

// withDefinitions builds the document the property is checked as: the property's
// own keywords, carrying the root's definitions so a local reference resolves.
//
// The property is copied over the definitions rather than under them, so a
// property that declares its own `$defs` keeps them.
func withDefinitions(root, property map[string]any) map[string]any {
	document := map[string]any{}
	for _, keyword := range definitionKeywords {
		if value, ok := root[keyword]; ok {
			document[keyword] = value
		}
	}
	for key, value := range property {
		document[key] = value
	}
	return document
}

// checkReferences refuses every pointer this check cannot follow, at any depth
// and through the definitions it does follow.
//
// Depth is the point. The refusal used to read the property's own top-level
// `$ref` alone, so the same URL one level down inside `items` was marshalled,
// silently failed to resolve, and the value was reported as checked against a
// schema that had not been applied.
//
// A pointer into the root's definitions is followed, because withDefinitions
// carries them and because a definition may point somewhere this check cannot
// go. Any other pointer is refused rather than followed: it addresses part of
// the published schema this check did not carry, and resolving it against a
// document that lacks that part is how a constraint gets skipped without
// anybody being told.
func checkReferences(root, schema map[string]any) error {
	if _, unfollowed := reachable(root, schema); len(unfollowed) > 0 {
		return unresolvableRef(unfollowed[0])
	}
	return nil
}

// reachable is every schema object a subtree reaches, following the local
// definition pointers a published schema is allowed to carry, together with the
// pointers it could not follow.
//
// One traversal, because the constraint walk and the reference refusal are the
// same question asked twice: what does this subtree actually say? A walk that
// stopped at a `$ref` answered it wrongly for both.
//
// The unfollowed list is sorted, so a schema with two bad pointers refuses the
// same one on every run rather than whichever the map iteration reached first.
func reachable(root map[string]any, node any) (nodes []map[string]any, unfollowed []string) {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			nodes = append(nodes, typed)
			if ref, ok := typed["$ref"].(string); ok && !seen[ref] {
				seen[ref] = true
				if target, found := localTarget(root, ref); found {
					walk(target)
				} else {
					unfollowed = append(unfollowed, ref)
				}
			}
			for key, value := range typed {
				if key == "$ref" {
					continue
				}
				walk(value)
			}
		case []any:
			for _, value := range typed {
				walk(value)
			}
		}
	}
	walk(node)
	sort.Strings(unfollowed)
	return nodes, unfollowed
}

// unresolvableRef is how a pointer this check will not follow reads.
//
// One function, because the same pointer is met by the constraint walk and by
// the reference refusal, and two wordings for one fact is how an author ends up
// thinking they have two problems. Three cases, because they read differently:
// a schema somewhere else on the internet, a definition the published schema
// does not declare, and a part of this schema that travelled no further than
// the root.
func unresolvableRef(ref string) error {
	name, defs := strings.CutPrefix(ref, "#/$defs/")
	switch {
	case !strings.HasPrefix(ref, "#"):
		return fmt.Errorf("that parameter's schema points at %q, which is outside the document: a deployment check resolves no external schema, so this argument cannot be checked", ref)
	case defs && name != "" && !strings.Contains(name, "/"):
		return fmt.Errorf("that parameter's schema points at %q and the published schema declares no such definition, so nothing could check this argument", ref)
	default:
		return fmt.Errorf("that parameter's schema points at %q, which is a part of the published schema this check does not carry, so the constraint there would go unapplied and the argument is not reported as checked: supply the value from the model instead, or publish the parameter with its constraints inline", ref)
	}
}

// unsupportedKeywords are the ones this check will not claim to have evaluated.
//
// Each is either ignored by the pinned library or has semantics that differ
// between implementations, so a value that passed could still be refused by the
// platform. Naming the keyword is what makes the blocker actionable: an author
// can see which part of a published schema is the problem.
var unsupportedKeywords = []string{"if", "then", "else", "dependentSchemas", "dependentRequired", "unevaluatedProperties", "unevaluatedItems"}

func unsupportedKeyword(property map[string]any) string {
	var found []string
	for _, keyword := range unsupportedKeywords {
		if _, ok := property[keyword]; ok {
			found = append(found, keyword)
		}
	}
	// A composition keyword is only ambiguous when it is the thing deciding the
	// value's type. `anyOf` of two scalar types is ordinary and checkable, so it
	// is left to the library rather than refused here.
	sort.Strings(found)
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

// bindingFindings turns binding outcomes into the same finding rows the rest of
// the preflight reports, so a bad argument is grouped and counted with
// everything else rather than printed through a second channel.
func bindingFindings(checks []bindingCheck) []finding {
	var findings []finding
	for _, check := range checks {
		if !check.Blocked {
			continue
		}
		findings = append(findings, finding{
			Kind:  "tool argument",
			State: wrongKind,
			Requirement: generate.Requirement{
				Name:  check.Tool + "." + check.Argument,
				Where: fmt.Sprintf("the package supplies this argument on every call to `%s`", check.Tool),
			},
			Detail: check.Detail,
		})
	}
	return findings
}

// deferredBindings are the overrides whose value arrives at call time, for the
// preview to name as deferred rather than as checked.
func deferredBindings(checks []bindingCheck) []string {
	var out []string
	for _, check := range checks {
		if check.Deferred {
			out = append(out, check.Detail)
		}
	}
	slices.Sort(out)
	return out
}
