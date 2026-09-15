package spec

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/google/jsonschema-go/jsonschema"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// Manifest is a portable organization contract. Nil lists leave a choice unrestricted.
type Manifest struct {
	Name      string           `json:"manifest" yaml:"manifest"`
	Version   int              `json:"version" yaml:"version"`
	Models    ManifestModels   `json:"models,omitzero" yaml:"models,omitzero"`
	Languages *ManifestAllow   `json:"languages,omitempty" yaml:"languages,omitempty"`
	Regions   *ManifestRegions `json:"regions,omitempty" yaml:"regions,omitempty"`
	Targets   *ManifestAllow   `json:"targets,omitempty" yaml:"targets,omitempty"`
	Tools     *ManifestTools   `json:"tools,omitempty" yaml:"tools,omitempty"`
	Tracing   *ManifestAllow   `json:"tracing,omitempty" yaml:"tracing,omitempty"`
}
type ManifestAllow struct {
	Allow []string `json:"allow" yaml:"allow"`
}
type ManifestModels struct {
	Listen []ManifestModel `json:"listen,omitzero" yaml:"listen,omitzero"`
	Speak  []ManifestModel `json:"speak,omitzero" yaml:"speak,omitzero"`
	Think  []ManifestModel `json:"think,omitzero" yaml:"think,omitzero"`
}

// A nil Allow permits every model from this provider; an explicit empty list permits none.
type ManifestModel struct {
	Provider string   `json:"provider" yaml:"provider"`
	Allow    []string `json:"allow,omitzero" yaml:"allow,omitzero"`
}
type ManifestRegions struct {
	Models      []ManifestModelRegion      `json:"models,omitempty" yaml:"models,omitempty"`
	Deployments []ManifestDeploymentRegion `json:"deployments,omitempty" yaml:"deployments,omitempty"`
}
type ManifestModelRegion struct {
	Role     string   `json:"role" yaml:"role"`
	Provider string   `json:"provider" yaml:"provider"`
	Allow    []string `json:"allow" yaml:"allow"`
}
type ManifestDeploymentRegion struct {
	Provider string   `json:"provider" yaml:"provider"`
	Allow    []string `json:"allow" yaml:"allow"`
}
type ManifestTools struct {
	Kinds   *ManifestAllow `json:"kinds,omitempty" yaml:"kinds,omitempty"`
	Names   *ManifestAllow `json:"names,omitempty" yaml:"names,omitempty"`
	Builtin *ManifestAllow `json:"builtin,omitempty" yaml:"builtin,omitempty"`
	Slng    *ManifestAllow `json:"slng,omitempty" yaml:"slng,omitempty"`
}

// ManifestSchema derives the contract's public shape from the same Go types the loader uses.
func ManifestSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[Manifest](nil)
	if err != nil {
		return nil, err
	}
	manifestSchemaNoNull(schema)
	schema.Properties["version"].Minimum = ptr(float64(1))
	schema.Properties["manifest"].Pattern = `\S`
	return schema, nil
}

// Reflection permits null pointers and slices; the authoring contract does not.
// Keep the generated shape, removing only the null alternatives.
func manifestSchemaNoNull(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if schema.Type == "null" {
		schema.Type = ""
		schema.Not = &jsonschema.Schema{}
	}
	schema.Types = slices.DeleteFunc(schema.Types, func(value string) bool { return value == "null" })
	for _, properties := range []map[string]*jsonschema.Schema{schema.Properties, schema.Defs, schema.Definitions} {
		for _, child := range properties {
			manifestSchemaNoNull(child)
		}
	}
	manifestSchemaNoNull(schema.Items)
	for _, children := range [][]*jsonschema.Schema{schema.AllOf, schema.AnyOf, schema.OneOf, schema.PrefixItems, schema.ItemsArray} {
		for _, child := range children {
			manifestSchemaNoNull(child)
		}
	}
}

func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := yaml.UnmarshalWithOptions(data, &manifest, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	// YAML null otherwise decodes as a nil list and silently removes a restriction.
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := manifestNoNull(raw, "manifest"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.Name) == "" || manifest.Version < 1 {
		return nil, fmt.Errorf("manifest: set a company name and a positive integer version")
	}
	if err := manifestFields(reflect.ValueOf(manifest), "manifest"); err != nil {
		return nil, err
	}
	if err := manifestKnownValues(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
func manifestNoNull(value any, path string) error {
	switch value := value.(type) {
	case nil:
		return fmt.Errorf("%s: null is not a rule; omit it or write an empty allowlist", path)
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(value)) {
			if err := manifestNoNull(value[key], path+"."+key); err != nil {
				return err
			}
		}
	case map[any]any:
		normalized := map[string]any{}
		for key, item := range value {
			normalized[fmt.Sprint(key)] = item
		}
		return manifestNoNull(normalized, path)
	case []any:
		for i, item := range value {
			if err := manifestNoNull(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
func manifestFields(value reflect.Value, path string) error {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return manifestFields(value.Elem(), path)
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			name := strings.Split(field.Tag.Get("yaml"), ",")[0]
			item := value.Field(i)
			if name == "allow" && item.IsNil() && value.Type() != reflect.TypeFor[ManifestModel]() {
				return fmt.Errorf("%s.allow: write an allowlist, including [] to allow nothing", path)
			}
			if name == "provider" && item.String() == "" {
				return fmt.Errorf("%s.provider: name the provider", path)
			}
			if name == "role" && item.String() != "listen" && item.String() != "speak" && item.String() != "think" {
				return fmt.Errorf("%s.role: choose listen, speak or think", path)
			}
			if err := manifestFields(item, path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice:
		seen := map[string]bool{}
		for i := 0; i < value.Len(); i++ {
			item := value.Index(i)
			key := ""
			if item.Kind() == reflect.String {
				key = item.String()
				if strings.TrimSpace(key) == "" {
					return fmt.Errorf("%s: an allowlist cannot contain an empty value", path)
				}
				if path == "manifest.languages.allow" {
					key = strings.ToLower(key)
				}
			} else {
				key = item.FieldByName("Provider").String()
				if role := item.FieldByName("Role"); role.IsValid() {
					key = role.String() + "/" + key
				}
			}
			if seen[key] {
				return fmt.Errorf("%s: duplicate rule %q", path, key)
			}
			seen[key] = true
			if err := manifestFields(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (p *Package) readManifest() error {
	if p.Agent.Manifest != "" && p.Agent.Manifest != "manifest" {
		return fmt.Errorf("agent.yaml: manifest must link to the package-root file with manifest: manifest")
	}
	data, err := readWithin(p.Root, "manifest")
	if p.Agent.Manifest == "" {
		if err == nil {
			return fmt.Errorf("agent.yaml: a manifest exists; link it with manifest: manifest")
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	p.Manifest, err = ParseManifest(data)
	if err != nil {
		return err
	}
	p.ManifestBytes = data
	p.files["manifest"] = data
	return nil
}

var manifestLanguagePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)

// ManifestToolKinds returns every execution kind a company contract can allow.
func ManifestToolKinds() []string { return slices.Clone(executionBlocks) }

// ManifestTracingProviders is shared by validation and the manifest editor.
func ManifestTracingProviders() []string { return []string{"langfuse", "coval"} }

func manifestKnownValues(m *Manifest) error {
	check := func(path string, rule *ManifestAllow, allowed []string) error {
		if rule == nil {
			return nil
		}
		for _, value := range rule.Allow {
			if !slices.Contains(allowed, value) {
				return fmt.Errorf("manifest.%s: unknown value %q; choose from %v", path, value, allowed)
			}
		}
		return nil
	}
	providers := make([]string, len(targetcap.Providers))
	for i, value := range targetcap.Providers {
		providers[i] = string(value)
	}
	if err := check("targets.allow", m.Targets, providers); err != nil {
		return err
	}
	if err := check("tracing.allow", m.Tracing, ManifestTracingProviders()); err != nil {
		return err
	}
	if m.Tools != nil {
		if err := check("tools.kinds.allow", m.Tools.Kinds, executionBlocks); err != nil {
			return err
		}
	}
	if m.Languages != nil {
		for _, value := range m.Languages.Allow {
			if !manifestLanguagePattern.MatchString(value) {
				return fmt.Errorf("manifest.languages.allow: %q must be a language tag such as en or en-US", value)
			}
		}
	}
	if m.Regions != nil {
		for _, row := range m.Regions.Deployments {
			if !slices.Contains(providers, row.Provider) {
				return fmt.Errorf("manifest.regions.deployments: unknown provider %q; choose from %v", row.Provider, providers)
			}
		}
	}
	return nil
}
