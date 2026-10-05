package spec

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// StateFile is the file a package declares its call state in, beside
// agent.yaml.
const StateFile = stateschema.FileName

// readState reads state.py's bytes when the package has one. Absent is not an
// error: a package with no state declares none.
func (p *Package) readState() error {
	content, err := os.ReadFile(filepath.Join(p.Root, StateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	p.StateSource = content
	return nil
}

// ReadState asks reader what state.py declares and keeps the answer on the
// package, where ir.Build reads it. Load does not do this itself, because the
// answer comes from Python: a command reads it through uv, and a test through a
// recorded report, so Load stays free of both.
func (p *Package) ReadState(ctx context.Context, reader stateschema.Reader) error {
	if p.StateSource == nil {
		return nil
	}
	model, err := reader.Read(ctx, p.Root, p.StateSource)
	if err != nil {
		return err
	}
	p.State = model
	return nil
}
