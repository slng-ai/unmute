package cli

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

// Call audio for the local planes: wav in, wav out, and the G.711 mu-law
// conversion in between. A carrier's media stream is mu-law at 8000 Hz mono in
// 20 ms frames, so a local stand-in that speaks anything else is not standing
// in for a carrier (research R7).
//
// Standard library only. This is a few hundred lines of arithmetic that has not
// changed since 1972, and a dependency for it would be a dependency to audit.

const (
	// wavHeaderSize is a canonical PCM wav header. A file exactly this size
	// carries a header and no audio at all, which is how "recorded nothing" is
	// told apart from "recorded silence".
	wavHeaderSize = 44
	// callAudioRate is the sample rate every carrier media stream uses.
	callAudioRate = 8000
	// callAudioFrameSamples is 20 ms at that rate, which is one frame. In
	// mu-law one sample is one byte, so it is also the frame size in bytes.
	callAudioFrameSamples = callAudioRate / 50
)

// mu-law constants from G.711. The bias is added before the exponent search and
// removed after, and the clip is the largest magnitude the 14-bit code can
// carry, so louder samples flatten rather than wrap.
//
// Checked against a system encoder once, on the caller fixture: 97.9% of bytes
// identical to `afconvert -d ulaw`, and every difference exactly one code step,
// because this truncates to 14 bits where that one rounds. Both are valid
// G.711 and the difference is one quantisation step, so this follows the
// reference implementation rather than chasing a particular vendor's rounding.
const (
	mulawBias = 0x84
	mulawClip = 8159
)

// mulawSegmentEnd is the top of each mu-law segment, on the 14-bit scale the
// encoder works on.
var mulawSegmentEnd = [8]int32{0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF}

// encodeMulaw converts one 16-bit sample to one mu-law byte.
func encodeMulaw(sample int16) byte {
	// The code is 14-bit, so the sample loses its two quietest bits. int32
	// throughout, because negating the most negative int16 does not fit in one.
	value := int32(sample) >> 2
	mask := byte(0xFF)
	if value < 0 {
		value = -value
		mask = 0x7F
	}
	if value > mulawClip {
		value = mulawClip
	}
	value += mulawBias >> 2

	segment := 0
	for segment < len(mulawSegmentEnd) && value > mulawSegmentEnd[segment] {
		segment++
	}
	if segment >= len(mulawSegmentEnd) {
		return 0x7F ^ mask
	}
	code := byte(segment<<4) | byte((value>>(segment+1))&0x0F)
	return code ^ mask
}

// decodeMulaw converts one mu-law byte back to a 16-bit sample. The result is
// one of 255 values, which is what makes the conversion lossy in one direction
// and exact in the other.
func decodeMulaw(code byte) int16 {
	code = ^code
	value := (int32(code&0x0F) << 3) + mulawBias
	value <<= (code & 0x70) >> 4
	if code&0x80 != 0 {
		return int16(mulawBias - value)
	}
	return int16(value - mulawBias)
}

func pcmToMulaw(samples []int16) []byte {
	payload := make([]byte, len(samples))
	for i, sample := range samples {
		payload[i] = encodeMulaw(sample)
	}
	return payload
}

func mulawToPCM(payload []byte) []int16 {
	samples := make([]int16, len(payload))
	for i, code := range payload {
		samples[i] = decodeMulaw(code)
	}
	return samples
}

// mulawFrames splits a mu-law payload into the 20 ms frames a media stream
// carries. A short tail is returned as its own frame rather than padded: the
// caller knows whether silence or nothing is the right ending, and this does
// not.
func mulawFrames(payload []byte) [][]byte {
	if len(payload) == 0 {
		return nil
	}
	frames := make([][]byte, 0, (len(payload)+callAudioFrameSamples-1)/callAudioFrameSamples)
	for start := 0; start < len(payload); start += callAudioFrameSamples {
		end := min(start+callAudioFrameSamples, len(payload))
		frames = append(frames, payload[start:end])
	}
	return frames
}

// readWAV reads a 16-bit mono PCM wav file. Anything else is refused by name,
// because a fixture in the wrong format is the kind of thing that otherwise
// shows up as silence on a call.
func readWAV(path string) (samples []int16, rate int, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("%s: not a RIFF/WAVE file", path)
	}
	var channels, bits int
	var data []byte
	// Chunks are 8 bytes of header then a padded body, in any order, so both
	// the format and the samples are found by walking rather than by offset.
	for offset := 12; offset+8 <= len(raw); {
		id := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		body := offset + 8
		if size < 0 || body+size > len(raw) {
			return nil, 0, fmt.Errorf("%s: chunk %q runs past the end of the file", path, id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("%s: format chunk is %d bytes, want at least 16", path, size)
			}
			format := binary.LittleEndian.Uint16(raw[body : body+2])
			if format != 1 {
				return nil, 0, fmt.Errorf("%s: audio format %d is not uncompressed PCM", path, format)
			}
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			rate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
		case "data":
			data = raw[body : body+size]
		}
		offset = body + size + size%2
	}
	switch {
	case rate == 0:
		return nil, 0, fmt.Errorf("%s: no format chunk", path)
	case data == nil:
		return nil, 0, fmt.Errorf("%s: no data chunk", path)
	case channels != 1:
		return nil, 0, fmt.Errorf("%s: %d channels, want mono", path, channels)
	case bits != 16:
		return nil, 0, fmt.Errorf("%s: %d-bit samples, want 16", path, bits)
	}
	samples = make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[2*i : 2*i+2]))
	}
	return samples, rate, nil
}

// writeWAV writes 16-bit mono PCM, which is what every tool that opens a
// recording expects and what readWAV accepts.
func writeWAV(path string, samples []int16, rate int) error {
	if rate <= 0 {
		return errors.New("writeWAV: sample rate must be positive")
	}
	const headerSize = wavHeaderSize
	body := 2 * len(samples)
	out := make([]byte, headerSize+body)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(headerSize-8+body))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // uncompressed PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(rate*2)) // bytes per second
	binary.LittleEndian.PutUint16(out[32:34], 2)              // bytes per frame
	binary.LittleEndian.PutUint16(out[34:36], 16)             // bits per sample
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(body))
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[headerSize+2*i:headerSize+2*i+2], uint16(sample))
	}
	return os.WriteFile(path, out, 0o600)
}

// callFixtureSample is the caller endpoint's audio, one sample at a time: three
// harmonics under a syllable envelope. Synthesised rather than recorded, because
// a real voice sample is somebody's copyright and a redistribution question,
// and because this is deterministic, so a recording of it can be compared
// against what was sent.
//
// Not a pure tone on purpose. A single sine survives every kind of broken
// resampling and codec mismatch looking exactly like itself, so it would prove
// far less than it appears to; a shaped signal with harmonics does not.
func callFixtureSample(index int) int16 {
	seconds := float64(index) / float64(callAudioRate)
	// Roughly 2.7 syllables a second, never reaching silence, so a recording
	// that went quiet means the audio stopped rather than the envelope dipping.
	envelope := 0.35 + 0.65*math.Abs(math.Sin(2*math.Pi*2.7*seconds))
	value := 0.55*math.Sin(2*math.Pi*210*seconds) +
		0.28*math.Sin(2*math.Pi*420*seconds) +
		0.12*math.Sin(2*math.Pi*830*seconds)
	scaled := value * envelope * 20000
	if scaled > 32767 {
		scaled = 32767
	}
	if scaled < -32768 {
		scaled = -32768
	}
	return int16(scaled)
}
