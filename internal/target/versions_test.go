package target

import (
	"strings"
	"testing"
)

// The support window is the one recorded home for what framework versions a
// release supports, so these are the checks that keep it honest: the table has
// to be real, its bounds have to be orderable, and the range has to be enforced
// with patch precision rather than the minor-only compare it replaced.

func TestSupportWindowsAreWellFormed(t *testing.T) {
	windows := Windows()
	if len(windows) == 0 {
		t.Fatal("no support windows recorded; every check below would pass vacuously")
	}
	for provider, win := range windows {
		if !EmitsProject(provider) {
			t.Errorf("%s has a support window but no shipped driver", provider)
		}
		if FrameworkPackage(provider) == "" {
			t.Errorf("%s has a support window but no framework package name", provider)
		}
		if _, ok := ParseVersion(win.Floor); !ok {
			t.Errorf("%s floor %q is not a version", provider, win.Floor)
		}
		if _, ok := ParseVersion(win.Ceiling); !ok {
			t.Errorf("%s ceiling %q is not a version", provider, win.Ceiling)
		}
		if win.Floor != win.Ceiling {
			t.Errorf("%s must support one tested version, got %s through %s", provider, win.Floor, win.Ceiling)
		}
		// A ceiling with no verification date is a claim nobody stands behind.
		if win.Verified == "" {
			t.Errorf("%s ceiling %s carries no verification date", provider, win.Ceiling)
		}
		if !exactVersionPattern.MatchString(win.Floor) || !exactVersionPattern.MatchString(win.Ceiling) {
			t.Errorf("%s window bounds must be three numbers, got floor %q ceiling %q", provider, win.Floor, win.Ceiling)
		}
	}
}

// Every shipped code target needs a window, or CheckVersion silently passes
// anything for it.
func TestEveryShippedDriverHasAWindow(t *testing.T) {
	for _, provider := range []Provider{LiveKit, Pipecat} {
		if _, ok := Window(provider); !ok {
			t.Errorf("%s ships a driver but has no support window", provider)
		}
	}
}

func TestCheckVersionAgainstWindow(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		version  string
		wantErr  string // "" means accepted
	}{
		{name: "livekit exact", provider: LiveKit, version: "1.6.10"},
		{name: "pipecat exact", provider: Pipecat, version: "1.7.0"},

		// The bump this feature ships is exactly the case a string compare gets
		// wrong: "1.6.10" sorts below "1.6.4" lexically.
		{name: "patch above ceiling", provider: LiveKit, version: "1.6.11", wantErr: "newer than this unmute supports"},
		{name: "minor above ceiling", provider: LiveKit, version: "1.7.0", wantErr: "newer than this unmute supports"},
		{name: "major above ceiling", provider: Pipecat, version: "2.0.0", wantErr: "newer than this unmute supports"},
		{name: "above ceiling names the fix", provider: Pipecat, version: "1.8.0", wantErr: "a newer unmute may support it"},

		{name: "below pipecat floor", provider: Pipecat, version: "1.6.9", wantErr: "outside the supported range (exactly 1.7.0)"},
		{name: "below livekit floor", provider: LiveKit, version: "1.6.9", wantErr: "outside the supported range (exactly 1.6.10)"},
		{name: "far below floor", provider: LiveKit, version: "0.0.108", wantErr: "outside the supported range"},

		{name: "partial version", provider: LiveKit, version: "1.6", wantErr: "must be three numbers"},
		{name: "major only", provider: LiveKit, version: "1", wantErr: "must be three numbers"},
		{name: "prerelease suffix", provider: LiveKit, version: "1.6.11rc1", wantErr: "must be three numbers"},
		{name: "not a version", provider: LiveKit, version: "latest", wantErr: "not a semantic version"},
		{name: "empty", provider: LiveKit, version: "", wantErr: "requires a framework version"},

		// An unknown provider has no window to be outside of. Every provider in
		// Providers now has a driver, so this case uses a name that is not one.
		{name: "unknown provider", provider: Provider("nothing-here"), version: "9.9.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckVersion(tc.provider, tc.version)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckVersion(%s, %q) = %v, want accepted", tc.provider, tc.version, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckVersion(%s, %q) accepted it, want error containing %q", tc.provider, tc.version, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("CheckVersion(%s, %q) = %q, want it to contain %q", tc.provider, tc.version, err, tc.wantErr)
			}
		})
	}
}

// An out-of-range error has to name the exact supported version, or the author
// has to go looking for what their unmute supports (FR-004).
func TestCheckVersionErrorsNameTheExactVersion(t *testing.T) {
	win, _ := Window(LiveKit)
	for _, version := range []string{"1.4.0", "1.9.9"} {
		err := CheckVersion(LiveKit, version)
		if err == nil {
			t.Fatalf("CheckVersion(livekit, %q) accepted an out-of-range version", version)
		}
		if !strings.Contains(err.Error(), "exactly "+win.Ceiling) {
			t.Errorf("CheckVersion(livekit, %q) = %q, want it to name exactly %s", version, err, win.Ceiling)
		}
	}
}
