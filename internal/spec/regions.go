package spec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// Region is one place the platform deploys the agent, and the model names that
// place swaps.
//
// A bare region runs agent.yaml as written. A region that carries swaps runs the
// same agent and the same prompt with different models behind the same names:
//
//	deployment_region:
//	  - us-west
//	  - eu-north:
//	      - transcriber: transcriber_fr
//	      - voice: voice_fr
//
// The swap names two entries of the package's own model palette, so a regional
// model is declared once, beside its default, and the target file says only
// which one each region reads. The alternative, writing the model out again
// under the region, would put a vendor and a model id in targets.yaml, which is
// the half of a package meant to stay portable.
type Region struct {
	Name string
	// Swaps is `- default_name: replacement_name`, read left to right. Both
	// sides name an entry in agent.yaml's models; ir.Build refuses a name it
	// cannot find and a replacement of a different kind.
	Swaps []Pair
}

// Regions is what an author may write for `deployment_region`: one region as a
// bare scalar, or several as a list (SCHEMA N32). Only the authoring surface is
// one-or-many; the resolved IR always holds a list.
type Regions []Region

// Names is the declared regions in order, for the callers that want placement
// and not the swaps.
func (r Regions) Names() []string {
	if len(r) == 0 {
		return nil
	}
	names := make([]string, 0, len(r))
	for _, region := range r {
		names = append(names, region.Name)
	}
	return names
}

// UnmarshalYAML accepts both shapes. goccy's InterfaceUnmarshaler is the
// deliberate choice here: its callback re-enters the decoder, so a value that is
// neither shape (a mapping, a nested list) fails with goccy's own line and
// column rather than a sentence of ours with no position. An item of the list
// form refuses itself, through Region below, which knows its line.
func (r *Regions) UnmarshalYAML(unmarshal func(any) error) error {
	var list []Region
	listErr := unmarshal(&list)
	if listErr == nil {
		*r = list
		return nil
	}
	var one string
	if err := unmarshal(&one); err == nil {
		*r = Regions{{Name: one}}
		return nil
	}
	return listErr
}

// UnmarshalYAML decodes one item of the list form: a bare region name, or a
// one-key mapping from the region to its swaps.
//
// goccy's NodeUnmarshaler is the deliberate choice here, the same one Pair
// makes and for the same reason: the AST node carries the line, and a dropped
// indent is the mistake this catches. Two keys in one item is two regions the
// author meant to write as two items, and a map would have taken it silently.
func (r *Region) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	switch typed := node.(type) {
	case *ast.StringNode:
		r.Name = typed.Value
		return nil
	case *ast.MappingValueNode:
		// A one-key mapping parses as a MappingValueNode rather than a
		// MappingNode, which is the shape every correctly written item has.
		return r.set(typed, line)
	case *ast.MappingNode:
		switch len(typed.Values) {
		case 1:
			return r.set(typed.Values[0], line)
		case 0:
			return &PairError{Line: line, Msg: `an empty deployment_region item. Write it as "- us-west", or "- us-west:" followed by its model swaps`}
		default:
			keys := make([]string, 0, len(typed.Values))
			for _, value := range typed.Values {
				keys = append(keys, value.Key.String())
			}
			return &PairError{Line: line, Msg: fmt.Sprintf(
				"a deployment_region item naming %d regions (%s). One item, one region: give each its own %q line",
				len(keys), strings.Join(keys, ", "), "- ")}
		}
	default:
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"a deployment_region item is a region name, or a region with its model swaps under it, and this item is a %s", node.Type())}
	}
}

// set fills the region from `name: <swaps>`. The value is a sequence of pairs,
// so a swap written as a mapping under the region is refused here rather than
// reaching Build as a shape no swap list can be.
func (r *Region) set(value *ast.MappingValueNode, line int) error {
	r.Name = value.Key.String()
	sequence, ok := value.Value.(*ast.SequenceNode)
	if !ok {
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"region %q holds a %s, and a region's model swaps are a list: %q",
			r.Name, value.Value.Type(), "- transcriber: transcriber_fr")}
	}
	for _, item := range sequence.Values {
		var pair Pair
		if err := pair.UnmarshalYAML(item); err != nil {
			return err
		}
		if _, ok := pair.Value.(string); !ok {
			itemLine := line
			if token := item.GetToken(); token != nil {
				itemLine = token.Position.Line
			}
			return &PairError{Line: itemLine, Msg: fmt.Sprintf(
				"region %q swaps %q for a %T, and a swap names another model entry: %q",
				r.Name, pair.Key, pair.Value, "- "+pair.Key+": "+pair.Key+"_fr")}
		}
		r.Swaps = append(r.Swaps, pair)
	}
	return nil
}

// MarshalYAML writes a single region back as the bare scalar the author wrote,
// so a TUI round-trip does not reshape their file.
func (r Regions) MarshalYAML() (any, error) { return r.authored(), nil }

// MarshalJSON writes the same shape, because the published authoring schema is
// checked against JSON-encoded targets: encoding the Go struct instead would
// publish `{"Name": ...}`, which is not a shape any author writes and not a
// shape the schema describes.
func (r Regions) MarshalJSON() ([]byte, error) { return json.Marshal(r.authored()) }

// authored is the value as it appears in a file: one plain region collapses to
// the bare scalar, and a region with no swaps stays a bare name inside the list.
func (r Regions) authored() any {
	if len(r) == 1 && len(r[0].Swaps) == 0 {
		return r[0].Name
	}
	items := make([]any, 0, len(r))
	for _, region := range r {
		if len(region.Swaps) == 0 {
			items = append(items, region.Name)
			continue
		}
		swaps := make([]any, 0, len(region.Swaps))
		for _, swap := range region.Swaps {
			swaps = append(swaps, map[string]any{swap.Key: swap.Value})
		}
		items = append(items, map[string]any{region.Name: swaps})
	}
	return items
}
