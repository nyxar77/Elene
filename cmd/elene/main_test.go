package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func fakeADB(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDevicesCommand(t *testing.T) {
	adbPath := fakeADB(t, `printf 'List of devices attached\nserial1\tdevice model:Phone\n'`)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"--adb", adbPath, "devices"}, func(string) string { return "" }, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d: %s", code, stderr.String())
	}
	if stdout.String() != "serial1\tonline\tPhone\n" {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"install"}, func(string) string { return "" }, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected usage output")
	}
}
