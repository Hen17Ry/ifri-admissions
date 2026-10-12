package pdfcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"
)

var ErrInvalid = errors.New("invalid PDF")
var ErrEncrypted = errors.New("encrypted PDF")
var ErrActive = errors.New("active PDF content")

type Inspector interface {
	Check(context.Context, string) error
}
type QPDF struct{}
type cappedOutput struct {
	bytes.Buffer
	max int
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > w.max {
		return 0, ErrInvalid
	}
	return w.Buffer.Write(p)
}
func (QPDF) Check(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Exit 0 = encrypted; exit 2 = not encrypted. Errors still rejected by --check.
	err := exec.CommandContext(ctx, "qpdf", "--is-encrypted", path).Run()
	if err == nil {
		return ErrEncrypted
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		return ErrInvalid
	}
	cmd := exec.CommandContext(ctx, "qpdf", "--check", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return ErrInvalid
	}
	// Inspect parsed object dictionaries, including object streams and decoded names.
	output := &cappedOutput{max: 8 << 20}
	cmd = exec.CommandContext(ctx, "qpdf", "--json", "--json-key=qpdf", path)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return ErrInvalid
	}
	var data any
	if json.Unmarshal(output.Bytes(), &data) != nil {
		return ErrInvalid
	}
	forbidden := map[string]bool{"/JavaScript": true, "/JS": true, "/Launch": true, "/EmbeddedFile": true, "/EmbeddedFiles": true, "/RichMedia": true, "/OpenAction": true, "/AA": true, "/SubmitForm": true, "/ImportData": true}
	var walk func(any) bool
	walk = func(value any) bool {
		switch v := value.(type) {
		case string:
			return forbidden[v]
		case []any:
			for _, child := range v {
				if walk(child) {
					return true
				}
			}
		case map[string]any:
			for key, child := range v {
				if forbidden[key] || walk(child) {
					return true
				}
			}
		}
		return false
	}
	if walk(data) {
		return ErrActive
	}
	return nil
}
