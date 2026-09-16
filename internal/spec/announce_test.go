package spec

import (
	"reflect"
	"testing"

	"github.com/goccy/go-yaml"
)

// `announce:` takes one sentence or a list of alternatives. Both shapes decode
// to the same type, so nothing downstream asks which one the author wrote.
func TestAnnounceDecodesAScalarAndAList(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want Announce
	}{
		{"scalar", "announce: One moment.\n", Announce{"One moment."}},
		{"list", "announce:\n  - One moment.\n  - Let me look.\n", Announce{"One moment.", "Let me look."}},
		{"absent", "other: 1\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Announce Announce `yaml:"announce"`
			}
			if err := yaml.Unmarshal([]byte(tc.yaml), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(got.Announce, tc.want) {
				t.Errorf("announce = %#v, want %#v", got.Announce, tc.want)
			}
		})
	}
}

// A mapping is neither shape, and the message has to carry a position: goccy's
// own error does, and a sentence of ours would not.
func TestAnnounceRefusesAMapping(t *testing.T) {
	var got struct {
		Announce Announce `yaml:"announce"`
	}
	if err := yaml.Unmarshal([]byte("announce:\n  text: One moment.\n"), &got); err == nil {
		t.Fatal("a mapping decoded as an announcement")
	}
}

// The round trip `unmute maintain` runs must not reshape a file: one sentence
// goes back as the bare scalar it was written as, alternatives stay a list.
// Without this the TUI rewrites every scalar in the tree as a one-item list.
func TestAnnounceWritesBackTheShapeItWasWrittenIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value Announce
		want  string
	}{
		{"scalar", Announce{"One moment."}, "One moment.\n"},
		{"list", Announce{"One moment.", "Let me look."}, "- One moment.\n- Let me look.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := yaml.Marshal(tc.value)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if string(out) != tc.want {
				t.Errorf("encoded %q, want %q", out, tc.want)
			}
		})
	}
}

// Settled is the whole default resolution, so every driver reads a list with no
// blanks in it and none has to decide what " " means.
func TestAnnounceSettledTrimsAndDropsBlanks(t *testing.T) {
	got := Announce{"  One moment.  ", "   ", "", "Let me look."}.Settled()
	want := []string{"One moment.", "Let me look."}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Settled() = %#v, want %#v", got, want)
	}
	blank := Announce{"  ", ""}
	if blank.Settled() != nil {
		t.Error("an all-blank announcement settled to something")
	}
}
