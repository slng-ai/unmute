//go:build rig

// The rig: the local telephony planes exercised end to end, with a real
// container runtime and no credentials at all.
//
// Behind a build tag because it needs a container runtime, takes minutes, and
// places calls. `make rig` runs it; the default suite never does, and the PR
// gate never does. It is deliberately separate from `make smoke`, which needs
// provider credentials, because being credential-free is this check's whole
// value: an author, a CI job, or a reviewer can run it on a machine with no
// accounts on it and get an answer about whether the product works.
package cli

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// requireContainerRuntime skips with a sentence that says what to install,
// rather than failing. A missing runtime is not a defect in the product, and a
// red suite that means "this laptop has no Docker" trains people to ignore red.
func requireContainerRuntime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("no container runtime: %s. The rig brings up a SIP plane in containers", composeInstallHint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, "docker", "compose", "version")
	if output, err := check.CombinedOutput(); err != nil {
		t.Skipf("docker is installed but not usable here: %v (%s). Start the daemon, then re-run `make rig`", err, output)
	}
}

// TestRigPreconditions is the whole rig for now: it proves the target runs, the
// tag compiles, and the skip reads well on a machine with no runtime. The plane
// checks land on top of it, one per user story.
func TestRigPreconditions(t *testing.T) {
	requireContainerRuntime(t)
	// The fixtures the plane plays into a call. A rig run that cannot find them
	// would otherwise fail later as silence, which is the hardest failure of
	// all to read.
	for _, name := range []string{"caller.wav", "destination.wav"} {
		samples, rate, err := readWAV(fixturePath(name))
		if err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		if rate != callAudioRate {
			t.Errorf("fixture %s is %d Hz, and a carrier stream is %d Hz", name, rate, callAudioRate)
		}
		loud := 0
		for _, sample := range samples {
			if sample > 512 || sample < -512 {
				loud++
			}
		}
		// Silence in equals silence out, and a silent fixture makes every
		// later assertion about audio meaningless.
		if loud*4 < len(samples) {
			t.Errorf("fixture %s is mostly silent: %d of %d samples carry any level", name, loud, len(samples))
		}
	}
}
