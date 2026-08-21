package cli

import (
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M2: the media a local plane carries is mu-law at 8000 Hz mono in 20 ms
// frames, round-tripped against a checked-in fixture. A stand-in that gets this
// wrong does not fail loudly; it produces a call that connects and carries
// noise, which is the failure this is here to catch.
func TestCallAudioRoundTripsTheFixtureThroughMulaw(t *testing.T) {
	samples, rate, err := readWAV(fixturePath("caller.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if rate != callAudioRate {
		t.Fatalf("fixture is %d Hz, and a carrier stream is %d Hz", rate, callAudioRate)
	}
	if len(samples) == 0 {
		t.Fatal("fixture has no samples")
	}

	payload := pcmToMulaw(samples)
	if len(payload) != len(samples) {
		t.Fatalf("mu-law payload is %d bytes for %d samples; one sample is one byte", len(payload), len(samples))
	}

	frames := mulawFrames(payload)
	if len(frames) != len(samples)/callAudioFrameSamples {
		t.Fatalf("%d samples became %d frames of %d", len(samples), len(frames), callAudioFrameSamples)
	}
	for i, frame := range frames {
		if len(frame) != callAudioFrameSamples {
			t.Fatalf("frame %d is %d bytes, want %d (20 ms at %d Hz)", i, len(frame), callAudioFrameSamples, callAudioRate)
		}
	}

	// One pass is lossy, because mu-law has 255 values and the fixture has
	// thousands. The error it may lose is the quantisation step, not the
	// signal: a mean error above a few hundredths of full scale means the
	// conversion is wrong rather than coarse.
	decoded := mulawToPCM(payload)
	if len(decoded) != len(samples) {
		t.Fatalf("decoded %d samples from %d", len(decoded), len(samples))
	}
	var total, peak int64
	for i, sample := range samples {
		diff := int64(sample) - int64(decoded[i])
		if diff < 0 {
			diff = -diff
		}
		total += diff
		peak = max(peak, diff)
	}
	mean := float64(total) / float64(len(samples)) / 32768.0
	if mean > 0.01 {
		t.Errorf("mean round-trip error is %.4f of full scale, which is distortion rather than quantisation", mean)
	}
	if peak > 1024 {
		t.Errorf("worst round-trip error is %d, larger than a mu-law step at this amplitude", peak)
	}

	// The second pass is exact, which is the property that proves the two
	// halves agree: every decoded value already sits on a mu-law point, so
	// encoding it again has to return the same byte.
	for i, code := range pcmToMulaw(decoded) {
		if code != payload[i] {
			t.Fatalf("sample %d re-encodes to %#02x instead of %#02x; encode and decode disagree", i, code, payload[i])
		}
	}
}

// Silence stays silence in both directions. This is worth its own case because
// mu-law's zero is not the byte zero, so an off-by-one in the bias shows up
// here as a quiet hiss under every call.
func TestMulawKeepsSilenceSilent(t *testing.T) {
	if got := encodeMulaw(0); got != 0xFF {
		t.Errorf("encodeMulaw(0) = %#02x, want 0xff", got)
	}
	if got := decodeMulaw(0xFF); got != 0 {
		t.Errorf("decodeMulaw(0xff) = %d, want 0", got)
	}
	// The byte is stored inverted, so its sign bit reads backwards from what
	// the name suggests: 0x80 is the loudest positive sample and 0x00 the
	// loudest negative one. Worth pinning, because getting it the other way
	// round produces a call that sounds plausible and is phase-inverted.
	if got := decodeMulaw(0x80); got < 32000 {
		t.Errorf("loudest positive code decodes to %d", got)
	}
	if got := decodeMulaw(0x00); got > -32000 {
		t.Errorf("loudest negative code decodes to %d", got)
	}
	// Neither extreme may wrap. The most negative sample is the one input that
	// cannot be negated inside an int16, so it is where 16-bit arithmetic
	// would show up as a loud positive click.
	if got := decodeMulaw(encodeMulaw(-32768)); got > -32000 {
		t.Errorf("the most negative sample round-trips to %d", got)
	}
	if got := decodeMulaw(encodeMulaw(32767)); got < 32000 {
		t.Errorf("the most positive sample round-trips to %d", got)
	}
}

// A wav the plane writes has to be one this reads back, since the recording is
// the evidence a rig run leaves behind.
func TestWAVWriteReadIsExact(t *testing.T) {
	want := []int16{0, 1, -1, 32767, -32768, 12345, -12345}
	path := filepath.Join(t.TempDir(), "leg.wav")
	if err := writeWAV(path, want, callAudioRate); err != nil {
		t.Fatal(err)
	}
	got, rate, err := readWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	if rate != callAudioRate {
		t.Fatalf("rate = %d", rate)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d samples, wrote %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %d, wrote %d", i, got[i], want[i])
		}
	}
}

// A fixture in the wrong shape is refused by name. Silence on a call is the
// symptom this prevents, and "no audio" is a much harder thing to debug than
// "this file is stereo".
func TestReadWAVRefusesWhatItCannotCarry(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name  string
		build func(string) error
		want  string
	}{
		{"not a wav", func(p string) error { return os.WriteFile(p, []byte("this is not audio"), 0o600) }, "RIFF/WAVE"},
		{"stereo", func(p string) error { return writeBrokenWAV(p, 2, 16) }, "want mono"},
		{"8-bit", func(p string) error { return writeBrokenWAV(p, 1, 8) }, "want 16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".wav")
			if err := tc.build(path); err != nil {
				t.Fatal(err)
			}
			_, _, err := readWAV(path)
			if err == nil {
				t.Fatal("a fixture this shape must be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say %q: %v", tc.want, err)
			}
		})
	}
}

// writeBrokenWAV writes a header the reader must refuse: the right container,
// the wrong shape inside it.
func writeBrokenWAV(path string, channels, bits int) error {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], callAudioRate)
	binary.LittleEndian.PutUint32(header[28:32], callAudioRate*uint32(channels*bits/8))
	binary.LittleEndian.PutUint16(header[32:34], uint16(channels*bits/8))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bits))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], 0)
	return os.WriteFile(path, header, 0o600)
}

// TestDumpMulawForOracle is not a gate. It exists so the encoder can be
// compared byte for byte against a system codec once, by hand:
//
//	go test ./internal/cli -run TestDumpMulawForOracle -oracle-dump /tmp/go.ulaw
//
// Nothing runs it in CI, because no CI image has a G.711 encoder to compare to.
func TestDumpMulawForOracle(t *testing.T) {
	if *oracleDump == "" {
		t.Skip("no -oracle-dump path given")
	}
	samples, _, err := readWAV(fixturePath("caller.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(*oracleDump, pcmToMulaw(samples), 0o600); err != nil {
		t.Fatal(err)
	}
}

var oracleDump = flag.String("oracle-dump", "", "write the fixture's mu-law payload here (see TestDumpMulawForOracle)")

// fixturePath locates a checked-in call fixture. Shared with the rig, which
// compiles this file too, so both read the same two files.
func fixturePath(name string) string {
	return filepath.Join("testdata", "fixtures", name)
}
