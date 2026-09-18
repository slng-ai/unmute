package generate

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/skill"
)

// TestDeploySkillListsEveryFieldAPushReplaces holds the unmute-deploy skill's
// answer to "what will I lose" against the body a push actually sends.
//
// A push replaces rather than merges, so the fields in slngBody are exactly the
// ones overwritten on every push, and everything else on the live agent (idle
// nudges, noise cancellation, trunk bindings) survives because no package can
// express it. That distinction is the whole reason somebody opens the skill
// before deploying, and it is the kind of list that rots silently: adding a
// field to slngBody starts overwriting a setting the skill still promises is
// safe, and a reader finds out on a live agent.
func TestDeploySkillListsEveryFieldAPushReplaces(t *testing.T) {
	const heading = "## What a push replaces, and what it leaves alone"

	files, err := skill.New("test").Files(skill.DeployCanonical)
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := files["SKILL.md"]
	if !ok {
		t.Fatal("the unmute-deploy bundle has no SKILL.md")
	}

	_, after, found := strings.Cut(string(doc), heading)
	if !found {
		t.Fatalf("the unmute-deploy SKILL.md has no %q section; this test reads the first fenced block under it", heading)
	}
	fence := regexp.MustCompile("(?s)```text\n(.*?)```").FindStringSubmatch(after)
	if fence == nil {
		t.Fatalf("no ```text block under %q", heading)
	}
	documented := strings.FieldsFunc(fence[1], func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z')
	})
	slices.Sort(documented)

	var emitted []string
	for _, field := range reflect.VisibleFields(reflect.TypeFor[slngBody]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("slngBody.%s has no json tag: the deploy skill lists the wire names", field.Name)
		}
		emitted = append(emitted, name)
	}
	slices.Sort(emitted)

	for _, name := range emitted {
		if !slices.Contains(documented, name) {
			t.Errorf("a push overwrites %q, and the unmute-deploy skill does not list it: a reader is told that setting survives", name)
		}
	}
	for _, name := range documented {
		if !slices.Contains(emitted, name) {
			t.Errorf("the unmute-deploy skill says a push replaces %q, and nothing in the compiled body carries it", name)
		}
	}
}
