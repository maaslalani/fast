package main

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
)

func TestUploadReader(t *testing.T) {
	var total atomic.Int64
	reader := uploadReader{remaining: 5, total: &total}
	buffer := []byte{1, 1, 1}

	if n, err := reader.Read(buffer); n != 3 || err != nil {
		t.Fatalf("first read = (%d, %v), want (3, nil)", n, err)
	}
	if want := []byte{0, 0, 0}; !bytes.Equal(buffer, want) {
		t.Fatalf("first read = %v, want %v", buffer, want)
	}
	if n, err := reader.Read(buffer); n != 2 || err != nil {
		t.Fatalf("second read = (%d, %v), want (2, nil)", n, err)
	}
	if n, err := reader.Read(buffer); n != 0 || err != io.EOF {
		t.Fatalf("final read = (%d, %v), want (0, EOF)", n, err)
	}
	if got := total.Load(); got != 5 {
		t.Fatalf("total = %d, want 5", got)
	}
}
