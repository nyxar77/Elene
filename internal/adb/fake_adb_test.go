package adb

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeADB(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
