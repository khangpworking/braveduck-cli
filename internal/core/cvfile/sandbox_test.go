package cvfile

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// bombPDF is a few MB of PDF whose page content inflates to inflated bytes.
func bombPDF(inflated int) []byte {
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	chunk := bytes.Repeat([]byte("BT (A) Tj ET\n"), 1<<16)
	for n := 0; n < inflated; n += len(chunk) {
		w.Write(chunk)
	}
	w.Close()
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := []int{}
	obj := func(s string) { offs = append(offs, b.Len()); b.WriteString(s) }
	obj("1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n")
	obj("2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n")
	obj("3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n")
	obj(fmt.Sprintf("4 0 obj<</Length %d/Filter/FlateDecode>>stream\n", z.Len()))
	b.Write(z.Bytes())
	b.WriteString("\nendstream endobj\n")
	obj("5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, x)
	return b.Bytes()
}

// A PDF bomb is refused, and only the child process pays for it.
func TestPDFBombIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 4 MB PDF that unpacks to 2 GB")
	}
	if raceEnabled {
		t.Skip("race builds can't cap the child's address space")
	}
	data := bombPDF(2 << 30)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := Text(context.Background(), PDF, data)
	if !errors.Is(err, ErrTooHeavy) {
		t.Fatalf("bomb = %v, want ErrTooHeavy", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 64<<20 {
		t.Errorf("this process's heap grew %d MB reading it", grew>>20)
	}
	t.Logf("refused in %v", time.Since(start).Round(time.Millisecond))
}
