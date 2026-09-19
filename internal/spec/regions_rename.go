package spec

// Rename replaces the declared regions with names, keeping the swaps of every
// region whose name survives.
//
// The interactive console edits `deployment_region` as a comma-separated list
// of names, because a nested swap list is not a thing to type into one field.
// Without this, opening that field to add a region would silently drop every
// other region's model swaps, and `maintain` rewrites targets.yaml from the
// same struct, so the loss would reach the author's file.
//
// A name that was not there before arrives with no swaps, which is what it
// means: a new region runs the models as written.
func (r Regions) Rename(names []string) Regions {
	if len(names) == 0 {
		return nil
	}
	swaps := make(map[string][]Pair, len(r))
	for _, region := range r {
		swaps[region.Name] = region.Swaps
	}
	renamed := make(Regions, 0, len(names))
	for _, name := range names {
		renamed = append(renamed, Region{Name: name, Swaps: swaps[name]})
	}
	return renamed
}
