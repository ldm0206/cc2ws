// cmd/cc2ws/main_test.go
package main

import (
	"bytes"
	"os"
	"testing"
)

func TestRunVersionFlag(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	err := run([]string{"-version"})
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("run -version error: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("cc2ws")) {
		t.Errorf("output=%q want cc2ws", buf.String())
	}
}
