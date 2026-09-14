package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEverySpeechToSpeechRowSaysWhetherACallWasMade.
//
// A catalogue row carries a Verified date, and a date on its own says nothing
// about what was checked. For the cascade roles that is tolerable: a
// transcriber or a synthesizer is one construction and the offline suite reads
// it. For the two speech-to-speech roles it is not, because the thing they take
// over is the turn, and no offline check exercises a turn. A green suite here
// means the module constructs, not that a caller could talk to it.
//
// So every file carrying a Live or Realtime row states, in prose, that the
// browser call is owed, until somebody makes it and writes down what happened.
// The word is what this reads for, and the paragraph beside it is what a person
// reads. When the call is made, this fails and asks for the result rather than
// silently accepting a row that now claims more than it did.
func TestEverySpeechToSpeechRowSaysWhetherACallWasMade(t *testing.T) {
	files, err := filepath.Glob("catalog_*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if !strings.Contains(source, "Role: Live") && !strings.Contains(source, "Role: Realtime") {
			continue
		}
		checked++
		// OWED in capitals, because it is the word a reader scans a comment
		// block for, and a lowercase "owed" turns up in ordinary prose.
		if !strings.Contains(source, "OWED") {
			t.Errorf("%s carries a speech-to-speech row and never says whether a call was made.\n"+
				"Write what the Verified date covers and what it does not, or, if the call has been made, what happened on it.", path)
		}
	}
	if checked == 0 {
		t.Fatal("no catalogue file carries a Live or Realtime row, so this gate read nothing")
	}
}
