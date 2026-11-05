package cvfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// PDFs are read in a child braveduck process with an OS memory limit and a
// time limit: a small PDF can inflate to gigabytes ("PDF bomb"), and the
// parser has no limits of its own. If the child runs out of memory or time,
// only it dies and the upload is refused. (A .docx is capped while it
// unpacks; HTML and Markdown are read as uploaded.)

// childEnv makes a braveduck process act as the PDF-reading child.
const childEnv = "BRAVEDUCK_PDF_TEXT_CHILD"

// Child limits.
const (
	childMemory  = 1 << 30 // address space, bytes
	childTime    = 30 * time.Second
	childMaxText = 4 << 20 // bytes of text read back
)

// ErrTooHeavy means the PDF needed more memory or time than allowed.
var ErrTooHeavy = errors.New("this PDF is too heavy to read (it unpacks to far more than its size); export it again, or upload a .docx")

// IsChild reports whether this process was started to read one PDF; main
// then calls RunChild and exits.
func IsChild() bool { return os.Getenv(childEnv) == "1" }

// RunChild reads a PDF on stdin and writes its raw text to stdout, under
// the memory limit. It returns the process exit code.
func RunChild() int {
	limitMemory(childMemory)
	data, err := io.ReadAll(io.LimitReader(os.Stdin, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		fmt.Fprintln(os.Stderr, "bad input")
		return 2
	}
	text, err := pdfText(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_, _ = io.WriteString(os.Stdout, text)
	return 0
}

// pdfTextSandboxed runs pdfText in the child process.
func pdfTextSandboxed(ctx context.Context, data []byte) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, childTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), childEnv+"=1")
	cmd.Stdin = bytes.NewReader(data)
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, n: childMaxText}
	cmd.Stderr = &limitedWriter{w: &errOut, n: 4 << 10}
	err = cmd.Run()
	switch {
	case ctx.Err() != nil:
		return "", ErrTooHeavy
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", errors.New(string(bytes.TrimSpace(errOut.Bytes()))) // the parser's own message
		}
		return "", ErrTooHeavy // killed: out of memory
	}
	return out.String(), nil
}

// limitedWriter keeps the first n bytes and drops the rest.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
