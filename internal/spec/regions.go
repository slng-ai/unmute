package spec

// Regions is what an author may write for `deployment_region`: one region as a
// bare scalar, or several as a list (SCHEMA N32). Only the authoring surface is
// one-or-many; the resolved IR always holds a list.
type Regions []string

// UnmarshalYAML accepts both shapes. goccy's InterfaceUnmarshaler is the
// deliberate choice here: its callback re-enters the decoder, so a value that is
// neither shape (a mapping, a nested list) fails with goccy's own line and
// column rather than a sentence of ours with no position.
func (r *Regions) UnmarshalYAML(unmarshal func(any) error) error {
	var list []string
	listErr := unmarshal(&list)
	if listErr == nil {
		*r = list
		return nil
	}
	var one string
	if err := unmarshal(&one); err == nil {
		*r = Regions{one}
		return nil
	}
	return listErr
}

// MarshalYAML writes a single region back as the bare scalar the author wrote,
// so a TUI round-trip does not reshape their file.
func (r Regions) MarshalYAML() (any, error) {
	if len(r) == 1 {
		return r[0], nil
	}
	return []string(r), nil
}
