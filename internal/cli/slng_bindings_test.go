package cli

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
)

// published builds one resolved reference with the given published parameter
// schema, so each case below differs only in the contract it is checked
// against.
func published(schema map[string]any) resolvedTool {
	return resolvedTool{
		Requirement: generate.Requirement{
			Name: "search_places_text",
			// Source is what deployment keys the authored `inject:` values and
			// the authored description by, so a helper that left it empty would
			// silently check nothing. That is the failure mode the field
			// replaced a prose scan to prevent, and leaving it out here makes
			// these tests fail rather than pass vacuously.
			Source: "search_places_text",
			Where:  "tools/search_places_text.yaml references a tool SLNG hosts, so SLNG must already have one of that name",
		},
		ToolID: "t-1", Version: 3,
		Snapshot: slngPublishedSnapshot{Name: "search_places_text", ArgumentSchema: schema},
		finding:  finding{State: satisfied},
	}
}

// object is the ordinary published schema shape: declared properties, some
// required, no extras.
func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		anyRequired := make([]any, 0, len(required))
		for _, name := range required {
			anyRequired = append(anyRequired, name)
		}
		schema["required"] = anyRequired
	}
	return schema
}

// TestBindingChecksTheSuppliedPartAndNothingElse is the property the whole
// binding check turns on: an `inject:` entry supplies part of a tool's input,
// and the arguments left to the model are checked for nothing at all.
//
// Validating the injected object against the tool's whole schema would refuse
// every one of these, because each leaves a required parameter for the model.
// That is why this is a per-property check and not one Validate call.
func TestBindingChecksTheSuppliedPartAndNothingElse(t *testing.T) {
	schema := object(map[string]any{
		"query":    map[string]any{"type": "string"},
		"near":     map[string]any{"type": "string"},
		"limit":    map[string]any{"type": "integer"},
		"open_now": map[string]any{"type": "boolean"},
	}, "query", "near")

	for _, tc := range []struct {
		name  string
		value any
		// blocked is whether the deployment must stop, and want is a fragment
		// the message has to carry when it does.
		blocked bool
		key     string
		want    string
	}{
		// The two values a careless predicate reads as "empty" and refuses.
		// Both are arguments, and refusing either is the bug this test exists
		// to catch.
		{name: "a fixed false", key: "open_now", value: false},
		{name: "a fixed zero", key: "limit", value: 0},
		{name: "a fixed string", key: "query", value: "Tapas bar in Madrid"},
		// No coercion in either direction. A tool receiving a string where its
		// schema says a number fails at call time and reports something
		// unrelated, so the disagreement is caught here.
		{name: "a string where the schema says integer", key: "limit", value: "5",
			blocked: true, want: "does not fit that parameter"},
		{name: "a number where the schema says string", key: "query", value: 5,
			blocked: true, want: "does not fit that parameter"},
		{name: "a boolean where the schema says string", key: "near", value: true,
			blocked: true, want: "does not fit that parameter"},
		// A key the published version does not declare. The message names what
		// it does declare, because that is the list an author corrects against.
		{name: "an undeclared parameter", key: "postcode", value: "SW1",
			blocked: true, want: "does not declare a parameter called"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := checkBindings(
				[]resolvedTool{published(schema)},
				map[string]map[string]any{"search_places_text": {tc.key: tc.value}},
			)
			if len(checks) != 1 {
				t.Fatalf("one supplied argument produced %d checks: %+v", len(checks), checks)
			}
			got := checks[0]
			if got.Blocked != tc.blocked {
				t.Fatalf("blocked = %v, want %v (%s)", got.Blocked, tc.blocked, got.Detail)
			}
			if !tc.blocked {
				return
			}
			if !strings.Contains(got.Detail, tc.want) {
				t.Errorf("the refusal does not say %q:\n%s", tc.want, got.Detail)
			}
			// Every refusal names the tool, the argument, the version it was
			// checked against and the file that supplied it, because a refusal
			// an author cannot trace is one they cannot act on.
			for _, needed := range []string{"search_places_text", tc.key, "version 3", "tools/search_places_text.yaml"} {
				if !strings.Contains(got.Detail, needed) {
					t.Errorf("the refusal does not name %q:\n%s", needed, got.Detail)
				}
			}
		})
	}
}

// TestBindingLeavesTheModelsArgumentsAlone: a package that supplies one
// argument and leaves two required ones to the model deploys.
func TestBindingLeavesTheModelsArgumentsAlone(t *testing.T) {
	schema := object(map[string]any{
		"query": map[string]any{"type": "string"},
		"near":  map[string]any{"type": "string"},
	}, "query", "near")
	checks := checkBindings(
		[]resolvedTool{published(schema)},
		map[string]map[string]any{"search_places_text": {"query": "Tapas"}},
	)
	for _, check := range checks {
		if check.Blocked {
			t.Errorf("supplying one of two required arguments was refused: %s", check.Detail)
		}
	}
	// And nothing was invented for the argument nobody supplied.
	if len(checks) != 1 {
		t.Errorf("checks = %d, want exactly the one argument the package supplies: %+v", len(checks), checks)
	}
}

// TestBindingRefusesAnExtraArgumentWithThePlatformsReason.
//
// A published schema with `additionalProperties: true` lets the MODEL send
// extras. It does not make an attachment able to pin one, because an override
// is stored against the declared parameter list. The refusal has to say that
// and must not claim the schema forbids the argument, because misreporting it
// sends an author to edit a schema that is already right.
func TestBindingRefusesAnExtraArgumentWithThePlatformsReason(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"query": map[string]any{"type": "string"}},
		"additionalProperties": true,
	}
	checks := checkBindings(
		[]resolvedTool{published(schema)},
		map[string]map[string]any{"search_places_text": {"trace_id": "abc"}},
	)
	if len(checks) != 1 || !checks[0].Blocked {
		t.Fatalf("an override on an undeclared parameter was accepted: %+v", checks)
	}
	detail := checks[0].Detail
	if !strings.Contains(detail, "an attachment can only fix a declared one") {
		t.Errorf("the refusal does not give the platform's reason:\n%s", detail)
	}
	for _, forbidden := range []string{"accepts no others", "schema forbids"} {
		if strings.Contains(detail, forbidden) {
			t.Errorf("the refusal claims the schema forbids the argument, and it does not:\n%s", detail)
		}
	}
}

// TestBindingDefersAWholeVariableToken.
//
// A `{{customer_name}}` override is resolved when a call starts, so deployment
// checks the destination and does not claim to have checked the value. Saying
// otherwise would be the one dishonest thing this check could do: nobody knows
// what a caller will supply.
func TestBindingDefersAWholeVariableToken(t *testing.T) {
	schema := object(map[string]any{"query": map[string]any{"type": "string"}}, "query")
	checks := checkBindings(
		[]resolvedTool{published(schema)},
		map[string]map[string]any{"search_places_text": {"query": "{{customer_name}}"}},
	)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v", checks)
	}
	if checks[0].Blocked {
		t.Fatalf("a whole variable token into a string parameter was refused: %s", checks[0].Detail)
	}
	if !checks[0].Deferred {
		t.Error("the check is not marked deferred, so a reader is told the future value was validated")
	}
	if !strings.Contains(checks[0].Detail, "when a call starts") {
		t.Errorf("the note does not say when the value is checked:\n%s", checks[0].Detail)
	}
}

// TestBindingAcceptsAVariableIntoATypedParameter.
//
// A stored value is text, and this used to refuse any destination that was not
// a string on that reasoning. The platform converts the text into the
// parameter's declared scalar type instead, by parsing it as a JSON literal, so
// an integer parameter takes a variable whose stored text is `42` and refusing
// it refused something that works.
//
// The deferred note has to say the conversion is coming, because `"forty two"`
// in the vault is a call that fails and this deploy cannot see the value.
func TestBindingAcceptsAVariableIntoATypedParameter(t *testing.T) {
	for _, kind := range []string{"integer", "number", "boolean", "string"} {
		t.Run(kind, func(t *testing.T) {
			schema := object(map[string]any{"limit": map[string]any{"type": kind}})
			checks := checkBindings(
				[]resolvedTool{published(schema)},
				map[string]map[string]any{"search_places_text": {"limit": "{{how_many}}"}},
			)
			if len(checks) != 1 {
				t.Fatalf("checks = %+v", checks)
			}
			if checks[0].Blocked {
				t.Fatalf("a variable into %s parameter was refused, and the platform converts the stored text: %s", article(kind), checks[0].Detail)
			}
			if !checks[0].Deferred {
				t.Error("the check is not deferred, so a reader is told a value nobody has seen was validated")
			}
			if !strings.Contains(checks[0].Detail, kind) {
				t.Errorf("the note does not name the type the stored text is converted into:\n%s", checks[0].Detail)
			}
		})
	}
}

// TestBindingRefusesAnAmbiguousDestination.
//
// The platform pins an argument only where the parameter resolves to exactly
// one non-null scalar type, and refuses anything else with a message about
// leaving an ambiguous, array or object argument model-supplied.
//
// This is the restriction a schema validator is least able to see: a string is
// perfectly valid against `["string","integer"]`, so the check passed and the
// push refused the attachment. Every shape below is legal JSON Schema and legal
// on a published tool.
func TestBindingRefusesAnAmbiguousDestination(t *testing.T) {
	for _, tc := range []struct {
		name     string
		property map[string]any
		value    any
		want     string
	}{
		{
			name:     "two scalar types",
			property: map[string]any{"type": []any{"string", "integer"}},
			value:    "Tapas",
			want:     "exactly one scalar type",
		},
		{
			name: "a union of two scalar branches",
			property: map[string]any{"anyOf": []any{
				map[string]any{"type": "string"}, map[string]any{"type": "integer"},
			}},
			value: "Tapas",
			want:  "exactly one scalar type",
		},
		{
			name:     "no type at all",
			property: map[string]any{"description": "anything you like"},
			value:    "Tapas",
			want:     "settles what type it holds",
		},
		{
			name:     "an object",
			property: map[string]any{"type": "object"},
			value:    "Tapas",
			want:     "single scalar",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := object(map[string]any{"query": tc.property})
			got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", tc.value)
			if !got.Blocked {
				t.Fatalf("an argument the platform will not pin was reported as checked: %+v", got)
			}
			if !strings.Contains(got.Detail, tc.want) {
				t.Errorf("the refusal does not say %q: %s", tc.want, got.Detail)
			}
		})
	}

	// An intersection that DOES settle is still accepted, or the resolution
	// above is just refusing composition.
	settled := object(map[string]any{"query": map[string]any{"allOf": []any{
		map[string]any{"type": "string"}, map[string]any{"minLength": float64(2)},
	}}})
	if got := checkOneBinding(published(settled), "tools/search_places_text.yaml", "query", "Tapas"); got.Blocked {
		t.Errorf("an `allOf` that settles on one scalar type was refused: %s", got.Detail)
	}
}

// TestBindingRefusesARootConstraintOnTheSuppliedArgument.
//
// The platform's supported keyword list allows `allOf`, `anyOf`, `oneOf`, `not`
// and `$ref` at the ROOT of a published parameter schema. A root `allOf`
// carrying `properties: {query: {const: "allowed"}}` therefore constrains this
// argument, and the per-property walk cannot see it: a fixed value violating it
// was reported as checked.
//
// It is refused rather than evaluated. A root composition keyword may also
// carry `required`, so applying it to the one supplied argument would refuse
// every argument the model is meant to fill in, which is the whole reason this
// is a per-property check.
//
// A root constraint about OTHER parameters is not this argument's problem, and
// that case is the last one here: the author does not own the published schema,
// so a refusal they cannot act on is worse than no check.
func TestBindingRefusesARootConstraintOnTheSuppliedArgument(t *testing.T) {
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		t.Run(keyword, func(t *testing.T) {
			schema := object(map[string]any{"query": map[string]any{"type": "string"}})
			schema[keyword] = []any{map[string]any{"properties": map[string]any{
				"query": map[string]any{"const": "allowed"},
			}}}
			got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", "forbidden")
			if !got.Blocked {
				t.Fatalf("a value violating a root constraint was reported as checked: %+v", got)
			}
			for _, want := range []string{keyword, "query"} {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("the refusal does not name %q: %s", want, got.Detail)
				}
			}
		})
	}

	t.Run("a root constraint about another parameter", func(t *testing.T) {
		schema := object(map[string]any{
			"query": map[string]any{"type": "string"},
			"near":  map[string]any{"type": "string"},
		})
		schema["allOf"] = []any{map[string]any{"properties": map[string]any{
			"near": map[string]any{"const": "Dublin"},
		}}}
		if got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", "Tapas"); got.Blocked {
			t.Errorf("a root constraint about a different parameter refused this one: %s", got.Detail)
		}
	})
}

// TestBindingAcceptsANullableStringParameter: `["string","null"]` is how a
// published schema spells an optional string, and reading only the first
// spelling of `type` would refuse it.
func TestBindingAcceptsANullableStringParameter(t *testing.T) {
	schema := object(map[string]any{"query": map[string]any{"type": []any{"string", "null"}}})
	checks := checkBindings(
		[]resolvedTool{published(schema)},
		map[string]map[string]any{"search_places_text": {"query": "{{customer_name}}"}},
	)
	if len(checks) != 1 || checks[0].Blocked {
		t.Fatalf("a nullable string parameter refused a variable: %+v", checks)
	}
}

// TestBindingBlocksWhenTheContractCannotBeChecked.
//
// Three shapes where the honest answer is "not checked", and in every one of
// them the deployment stops. An empty schema accepts everything, an external
// `$ref` would mean opening a network connection during a deployment check, and
// an unsupported keyword means the value could pass here and be refused by the
// platform. Treating any of the three as satisfied is what would make this
// check worse than not having it.
func TestBindingBlocksWhenTheContractCannotBeChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{
			name:   "no published parameter schema at all",
			schema: nil,
			want:   "publishes no parameter schema",
		},
		{
			name:   "a reference outside the document",
			schema: object(map[string]any{"query": map[string]any{"$ref": "https://example.invalid/schema.json"}}),
			want:   "resolves no external schema",
		},
		{
			name: "a keyword this check cannot evaluate",
			schema: object(map[string]any{"query": map[string]any{
				"type": "string",
				"if":   map[string]any{"const": "a"},
				"then": map[string]any{"maxLength": float64(1)},
			}}),
			want: "cannot evaluate reliably",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := checkBindings(
				[]resolvedTool{published(tc.schema)},
				map[string]map[string]any{"search_places_text": {"query": "Tapas"}},
			)
			if len(checks) != 1 || !checks[0].Blocked {
				t.Fatalf("an uncheckable contract was accepted: %+v", checks)
			}
			if !strings.Contains(checks[0].Detail, tc.want) {
				t.Errorf("the refusal does not say %q:\n%s", tc.want, checks[0].Detail)
			}
		})
	}
}

// TestBindingChecksNothingAgainstAnUnresolvedReference.
//
// A reference that could not be resolved already has its own finding saying
// why. A second refusal about its arguments would send the author to the wrong
// problem, and it would be a refusal derived from an empty snapshot.
func TestBindingChecksNothingAgainstAnUnresolvedReference(t *testing.T) {
	unresolved := published(object(map[string]any{"query": map[string]any{"type": "string"}}))
	unresolved.ToolID, unresolved.Version = "", 0
	unresolved.finding = finding{State: absent}
	checks := checkBindings(
		[]resolvedTool{unresolved},
		map[string]map[string]any{"search_places_text": {"query": 5}},
	)
	if len(checks) != 0 {
		t.Errorf("an unresolved reference produced argument findings as well: %+v", checks)
	}
}

// TestBindingFindingsAreGroupedWithEverythingElse: a bad argument reports
// through the same finding rows as a missing secret, so it is counted in the
// satisfied tally and printed by the one renderer rather than a second channel.
func TestBindingFindingsAreGroupedWithEverythingElse(t *testing.T) {
	schema := object(map[string]any{"query": map[string]any{"type": "string"}})
	checks := checkBindings(
		[]resolvedTool{published(schema)},
		map[string]map[string]any{"search_places_text": {"query": 5}},
	)
	findings := bindingFindings(checks)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Kind != "tool argument" {
		t.Errorf("kind = %q, want a kind renderPreflight groups", findings[0].Kind)
	}
	if !findings[0].blocks() {
		t.Error("a refused argument does not block, so it would be a warning on a deployment that then made a 400 on every call")
	}
	if !strings.Contains(findings[0].Requirement.Name, "search_places_text") {
		t.Errorf("the finding does not name the tool: %q", findings[0].Requirement.Name)
	}
}

// TestPublishedVaultNeedsFindTheNamesAMirrorUsedTo.
//
// A hosted tool's credentials are the platform's. The old path read them from a
// committed mirror, which went stale the moment somebody changed the tool; this
// reads them from the version actually being attached.
//
// The `api_request` shape is the one that matters: a real captured tool
// declared no secret and named one under `config.auth.secret_name`, so a check
// reading only `declared_secrets` would report nothing missing for a tool that
// cannot authenticate.
func TestPublishedVaultNeedsFindTheNamesAMirrorUsedTo(t *testing.T) {
	reference := published(nil)
	reference.Snapshot = slngPublishedSnapshot{
		Name:            "search_places_text",
		DeclaredSecrets: []string{"DECLARED_TOKEN"},
		Config: map[string]any{
			"auth":    map[string]any{"type": "bearer", "secret_name": "AUTH_TOKEN"},
			"headers": []any{map[string]any{"name": "X-Region", "secret_name": "HEADER_TOKEN"}},
			"url":     "https://places.example.invalid/{{$REGION_PATH}}/search",
		},
		ArgumentDefaults: map[string]any{"locale": "{{$DEFAULT_LOCALE}}"},
	}
	needs := publishedVaultNeeds(reference)

	want := map[string]string{
		"DECLARED_TOKEN": "secret",
		"AUTH_TOKEN":     "secret",
		"HEADER_TOKEN":   "secret",
		// A token substituted into text is a vault VARIABLE, not a secret. The
		// two are created differently, so reporting the wrong kind sends an
		// author to make an entry the platform then refuses as a duplicate.
		"REGION_PATH":    "variable",
		"DEFAULT_LOCALE": "variable",
	}
	got := map[string]string{}
	for _, need := range needs {
		got[need.Name] = need.Kind
		if need.Where == "" {
			t.Errorf("%s is reported with no reason, so an author cannot trace it", need.Name)
		}
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s = %q, want kind %q", name, got[name], kind)
		}
	}
	// And nothing else. A config payload holds a whole request definition, and
	// harvesting every string in it would put a URL and a method into a report.
	if len(got) != len(want) {
		t.Errorf("discovered %d names, want %d: %v", len(got), len(want), got)
	}
	// No value, no auth block, no URL reaches the report.
	for _, need := range needs {
		for _, forbidden := range []string{"bearer", "https://", "X-Region"} {
			if strings.Contains(need.Where, forbidden) {
				t.Errorf("%s's reason leaks %q from the tool's config: %s", need.Name, forbidden, need.Where)
			}
		}
	}
}

// TestBindingResolvesARootLocalDefinition.
//
// A published schema may keep its property shapes in the root's `$defs` and
// point at them, which is ordinary JSON Schema and what a generated schema
// usually looks like. The check extracted the property and resolved it as its
// own document, so `{"$ref": "#/$defs/Query"}` had no `$defs` to resolve
// against, and a valid argument against a valid contract was refused with a
// message about a schema that could not be prepared.
//
// The definitions travel with the property now. Both spellings, because a
// published schema may have been written to either draft.
func TestBindingResolvesARootLocalDefinition(t *testing.T) {
	// `$defs` alone: a published schema's references are held to
	// `^#/$defs/...$` and the legacy `definitions` keyword is not on the
	// platform's supported list, so a `#/definitions/` pointer cannot reach
	// this check from a real account.
	for _, keyword := range []string{"$defs"} {
		t.Run(keyword, func(t *testing.T) {
			schema := object(map[string]any{"query": map[string]any{"$ref": "#/" + keyword + "/Query"}})
			schema[keyword] = map[string]any{"Query": map[string]any{"type": "string", "minLength": float64(2)}}

			if got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", "pizza"); got.Blocked {
				t.Errorf("a valid value against a root-local definition was refused: %s", got.Detail)
			}
			// And the definition is really applied, not merely resolvable: a
			// value the referenced schema forbids is still refused, or this
			// test would pass on a check that skipped the constraint.
			if got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", "x"); !got.Blocked {
				t.Error("a value the referenced definition forbids was accepted, so the reference resolved to nothing")
			}
		})
	}
}

// TestBindingRefusesAReferenceItCannotResolve.
//
// The external-reference refusal read the property's own top-level `$ref` and
// nothing deeper, so the same URL one level down inside `items` was marshalled,
// silently failed to resolve, and the value was reported as checked against a
// schema that had never been applied. Depth is the whole point of this one.
//
// A pointer into a part of the published schema this check does not carry is
// refused for the same reason rather than followed into a document that lacks
// it: a constraint that goes unapplied has to be said out loud.
func TestBindingRefusesAReferenceItCannotResolve(t *testing.T) {
	for _, tc := range []struct {
		name     string
		property map[string]any
		value    any
		want     string
	}{
		{
			name:     "an external reference at the top",
			property: map[string]any{"$ref": "https://example.invalid/query.json"},
			value:    "pizza",
			want:     "outside the document",
		},
		{
			// The type resolves, so this reaches the constraint walk rather
			// than being refused earlier for being an array.
			name: "an external reference nested one level down",
			property: map[string]any{
				"type":  "string",
				"allOf": []any{map[string]any{"$ref": "https://example.invalid/query.json"}},
			},
			value: "pizza",
			want:  "outside the document",
		},
		{
			name:     "a pointer into a part this check does not carry",
			property: map[string]any{"$ref": "#/properties/near"},
			value:    "pizza",
			want:     "does not carry",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := object(map[string]any{"query": tc.property, "near": map[string]any{"type": "string"}})
			got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", tc.value)
			if !got.Blocked {
				t.Fatalf("a reference this check cannot resolve was reported as checked: %+v", got)
			}
			if !strings.Contains(got.Detail, tc.want) {
				t.Errorf("the refusal does not say %q: %s", tc.want, got.Detail)
			}
		})
	}
}

// TestBindingRefusesARootConstraintBehindAReference.
//
// The half the first version of the root-constraint check missed. A published
// schema keeps its shapes in `$defs` and points at them, so a root composition
// branch is usually `{"$ref": "#/$defs/Limit"}` rather than an inline object.
// The relevance walk stopped at the pointer, saw a branch that named no
// property, ruled it irrelevant, and reported a fixed value as checked against
// a constraint nobody had read.
//
// The last two cases are why this is a walk and not a blanket refusal: a
// definition about another parameter is still not this argument's problem, and
// a pointer the check cannot follow is refused because an unread constraint may
// say anything.
func TestBindingRefusesARootConstraintBehindAReference(t *testing.T) {
	withDefs := func(defs map[string]any, root map[string]any) map[string]any {
		schema := object(map[string]any{"query": map[string]any{"type": "string"}})
		schema["$defs"] = defs
		for key, value := range root {
			schema[key] = value
		}
		return schema
	}
	constrainsQuery := map[string]any{"Limit": map[string]any{
		"type": "object", "properties": map[string]any{"query": map[string]any{"const": "allowed"}},
	}}

	for _, tc := range []struct {
		name    string
		schema  map[string]any
		blocked bool
	}{
		{
			name:    "an allOf branch pointing at a definition that constrains it",
			schema:  withDefs(constrainsQuery, map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/Limit"}}}),
			blocked: true,
		},
		{
			name:    "a oneOf branch pointing at a definition that constrains it",
			schema:  withDefs(constrainsQuery, map[string]any{"oneOf": []any{map[string]any{"$ref": "#/$defs/Limit"}}}),
			blocked: true,
		},
		{
			name:    "a root $ref at the top of the schema",
			schema:  withDefs(constrainsQuery, map[string]any{"$ref": "#/$defs/Limit"}),
			blocked: true,
		},
		{
			name: "a branch two definitions deep",
			schema: withDefs(map[string]any{
				"Outer": map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/Limit"}}},
				"Limit": constrainsQuery["Limit"],
			}, map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/Outer"}}}),
			blocked: true,
		},
		{
			name: "a branch pointing at a definition about another parameter",
			schema: withDefs(map[string]any{"Elsewhere": map[string]any{
				"type": "object", "properties": map[string]any{"near": map[string]any{"const": "Dublin"}},
			}}, map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/Elsewhere"}}}),
			blocked: false,
		},
		{
			name:    "a branch pointing somewhere this check cannot follow",
			schema:  withDefs(constrainsQuery, map[string]any{"allOf": []any{map[string]any{"$ref": "https://example.invalid/limit.json"}}}),
			blocked: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkOneBinding(published(tc.schema), "tools/search_places_text.yaml", "query", "forbidden")
			if got.Blocked != tc.blocked {
				t.Fatalf("blocked = %v, want %v: %s", got.Blocked, tc.blocked, got.Detail)
			}
		})
	}
}

// TestBindingResolvesADestinationTheWayThePlatformDoes.
//
// The type resolution is a deliberate mirror of the platform's own
// `resolve_non_null_json_types`, and this is the property that makes the mirror
// worth having: a difference between the two is a deployment that passes its
// own check and is refused at the push, or one refused here that would have
// worked.
//
// Every one of these was wrong before. The resolution returned a node's
// declared `type` and stopped, so nothing narrowed it further and a node with
// no `type` at all settled on nothing rather than on everything.
func TestBindingResolvesADestinationTheWayThePlatformDoes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		property map[string]any
		value    any
		kind     string
		refusal  string
	}{
		{
			// The review's own case: two declared types narrowed to one by a
			// constraint on the same parameter.
			name:     "two declared types narrowed by an allOf",
			property: map[string]any{"type": []any{"string", "integer"}, "allOf": []any{map[string]any{"type": "integer"}}},
			value:    float64(0),
			kind:     "integer",
		},
		{
			// No `type` of its own. The platform starts from every type and
			// narrows, so this settles on integer; starting from none instead
			// made it "nothing settles what type it holds".
			name:     "a type declared only by a constraint",
			property: map[string]any{"allOf": []any{map[string]any{"type": "integer"}}},
			value:    float64(7),
			kind:     "integer",
		},
		{
			// The platform's own exception, and without it this resolves to
			// nothing: an integer satisfies a `number` constraint.
			name:     "number narrowed by integer",
			property: map[string]any{"type": "number", "allOf": []any{map[string]any{"type": "integer"}}},
			value:    float64(3),
			kind:     "integer",
		},
		{
			name:     "a definition that declares the type",
			property: map[string]any{"$ref": "#/$defs/Count"},
			value:    float64(2),
			kind:     "integer",
		},
		{
			name:     "a union narrowed to one branch by a declared type",
			property: map[string]any{"type": "string", "anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}},
			value:    "Tapas",
			kind:     "string",
		},
		{
			name:     "a declared type and a constraint with nothing in common",
			property: map[string]any{"type": "string", "allOf": []any{map[string]any{"type": "integer"}}},
			value:    "Tapas",
			refusal:  "no type in common",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := object(map[string]any{"query": tc.property})
			schema["$defs"] = map[string]any{"Count": map[string]any{"type": "integer"}}

			got := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", tc.value)
			if tc.refusal != "" {
				if !got.Blocked {
					t.Fatalf("a destination with no settled type was reported as checked: %+v", got)
				}
				if !strings.Contains(got.Detail, tc.refusal) {
					t.Errorf("the refusal does not say %q: %s", tc.refusal, got.Detail)
				}
				return
			}
			if got.Blocked {
				t.Fatalf("a destination that settles on %s was refused: %s", tc.kind, got.Detail)
			}
			// And the resolved type is really the one named, which the deferred
			// note is where a reader sees it: a variable's stored text is
			// converted into exactly this type when a call starts.
			deferred := checkOneBinding(published(schema), "tools/search_places_text.yaml", "query", "{{order_number}}")
			if deferred.Blocked || !deferred.Deferred {
				t.Fatalf("a variable into that parameter was refused: %s", deferred.Detail)
			}
			if !strings.Contains(deferred.Detail, "one "+tc.kind+" value") {
				t.Errorf("the note does not name %s as the resolved type: %s", tc.kind, deferred.Detail)
			}
		})
	}
}
