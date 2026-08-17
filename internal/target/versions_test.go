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
		floor, ok := ParseVersion(win.Floor)
		if !ok {
			t.Errorf("%s floor %q is not a version", provider, win.Floor)
			continue
		}
		ceiling, ok := ParseVersion(win.Ceiling)
		if !ok {
			t.Errorf("%s ceiling %q is not a version", provider, win.Ceiling)
			continue
		}
		if floor[0] > ceiling[0] || (floor[0] == ceiling[0] && floor[1] > ceiling[1]) ||
			(floor[0] == ceiling[0] && floor[1] == ceiling[1] && floor[2] > ceiling[2]) {
			t.Errorf("%s window is empty: floor %s is above ceiling %s", provider, win.Floor, win.Ceiling)
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
		{name: "livekit floor", provider: LiveKit, version: "1.5.0"},
		{name: "livekit ceiling", provider: LiveKit, version: "1.6.10"},
		{name: "livekit inside", provider: LiveKit, version: "1.6.4"},
		{name: "pipecat floor", provider: Pipecat, version: "1.5.0"},
		{name: "pipecat ceiling", provider: Pipecat, version: "1.7.0"},
		{name: "pipecat inside", provider: Pipecat, version: "1.6.0"},

		// The bump this feature ships is exactly the case a string compare gets
		// wrong: "1.6.10" sorts below "1.6.4" lexically.
		{name: "patch above ceiling", provider: LiveKit, version: "1.6.11", wantErr: "newer than this unmute supports"},
		{name: "minor above ceiling", provider: LiveKit, version: "1.7.0", wantErr: "newer than this unmute supports"},
		{name: "major above ceiling", provider: Pipecat, version: "2.0.0", wantErr: "newer than this unmute supports"},
		{name: "above ceiling names the fix", provider: Pipecat, version: "1.8.0", wantErr: "a newer unmute may support it"},

		{name: "below floor", provider: Pipecat, version: "1.4.9", wantErr: "outside the supported range"},
		{name: "far below floor", provider: LiveKit, version: "0.0.108", wantErr: "outside the supported range"},

		{name: "partial version", provider: LiveKit, version: "1.6", wantErr: "must be three numbers"},
		{name: "major only", provider: LiveKit, version: "1", wantErr: "must be three numbers"},
		{name: "prerelease suffix", provider: LiveKit, version: "1.6.11rc1", wantErr: "must be three numbers"},
		{name: "not a version", provider: LiveKit, version: "latest", wantErr: "not a semantic version"},
		{name: "empty", provider: LiveKit, version: "", wantErr: "requires a framework version"},

		// A provider with no driver has no window to be outside of.
		{name: "driverless provider", provider: Vapi, version: "9.9.9"},
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

// An out-of-range error has to name the range, or the author has to go looking
// for what their unmute supports (FR-004).
func TestCheckVersionErrorsNameTheRange(t *testing.T) {
	win, _ := Window(LiveKit)
	for _, version := range []string{"1.4.0", "1.9.9"} {
		err := CheckVersion(LiveKit, version)
		if err == nil {
			t.Fatalf("CheckVersion(livekit, %q) accepted an out-of-range version", version)
		}
		if !strings.Contains(err.Error(), win.Floor) || !strings.Contains(err.Error(), win.Ceiling) {
			t.Errorf("CheckVersion(livekit, %q) = %q, want it to name both %s and %s", version, err, win.Floor, win.Ceiling)
		}
	}
}

func TestCheckFeatureFloors(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		version  string
		used     []FrameworkFeature
		wantErr  string
	}{
		{name: "no features", provider: LiveKit, version: "1.5.0"},
		{name: "warm at floor", provider: LiveKit, version: "1.6.0", used: []FrameworkFeature{FeatureWarmTransfer}},
		{name: "warm above floor", provider: LiveKit, version: "1.6.10", used: []FrameworkFeature{FeatureWarmTransfer}},
		{name: "mcp above floor", provider: LiveKit, version: "1.6.4", used: []FrameworkFeature{FeatureMCPTools}},
		{name: "slng livekit at floor", provider: LiveKit, version: "1.6.10", used: []FrameworkFeature{FeatureSLNGResponses}},
		{name: "slng pipecat at floor", provider: Pipecat, version: "1.7.0", used: []FrameworkFeature{FeatureSLNGResponses}},

		// The regression this gate exists for: before the pin became exact, this
		// package compiled green and quietly installed a version it never declared.
		{
			name: "warm below floor", provider: LiveKit, version: "1.5.2",
			used: []FrameworkFeature{FeatureWarmTransfer}, wantErr: "too old for a warm transfer",
		},
		{
			name: "mcp below floor", provider: LiveKit, version: "1.5.0",
			used: []FrameworkFeature{FeatureMCPTools}, wantErr: "too old for an MCP tool source",
		},
		{
			name: "names the package and floor", provider: LiveKit, version: "1.5.0",
			used: []FrameworkFeature{FeatureWarmTransfer}, wantErr: "livekit-agents >=1.6.0",
		},
		{
			name: "slng livekit below floor", provider: LiveKit, version: "1.6.9",
			used: []FrameworkFeature{FeatureSLNGResponses}, wantErr: "livekit-agents >=1.6.10",
		},
		{
			name: "slng pipecat below floor", provider: Pipecat, version: "1.6.9",
			used: []FrameworkFeature{FeatureSLNGResponses}, wantErr: "pipecat-ai >=1.7.0",
		},
		// Pipecat features vary by extra, never by version.
		{name: "pipecat has no floors", provider: Pipecat, version: "1.5.0", used: []FrameworkFeature{FeatureWarmTransfer}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckFeatureFloors(tc.provider, tc.version, tc.used)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckFeatureFloors(%s, %q, %v) = %v, want accepted", tc.provider, tc.version, tc.used, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckFeatureFloors(%s, %q, %v) accepted it, want %q", tc.provider, tc.version, tc.used, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("CheckFeatureFloors(%s, %q, %v) = %q, want it to contain %q", tc.provider, tc.version, tc.used, err, tc.wantErr)
			}
		})
	}
}

// Every feature floor has to sit inside its provider's window, or it names a
// version nobody can declare.
func TestFeatureFloorsSitInsideTheWindow(t *testing.T) {
	for provider, floors := range featureFloors {
		if _, ok := Window(provider); !ok {
			t.Errorf("%s has feature floors but no support window", provider)
			continue
		}
		for feature, floor := range floors {
			if err := CheckVersion(provider, floor); err != nil {
				t.Errorf("%s floor for %s is %s, which its own window rejects: %v", provider, feature, floor, err)
			}
		}
	}
}
