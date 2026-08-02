package adb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUsesSeparateADBArguments(t *testing.T) {
	apkPath := filepath.Join(t.TempDir(), "application;not-a-command.apk")
	if err := os.WriteFile(apkPath, []byte("APK"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{ADBPath: fakeADB(t, `
case "$1 $2 $3 $4" in
  "devices -l  ") printf 'List of devices attached\nserial1\tdevice\n' ;;
  "-s serial1 install -r")
    if [ "$5" = "$EXPECTED_APK" ]; then
      printf 'Success\n'
    else
      printf 'unexpected APK argument: %s\n' "$5" >&2
      exit 1
    fi ;;
esac`)})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_APK", apkPath)

	if err := client.Install(context.Background(), "serial1", apkPath); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRejectsInvalidAPK(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `exit 1`)})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(t.TempDir(), "missing.apk")},
		{name: "directory", path: t.TempDir()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := client.Install(context.Background(), "serial1", test.path)
			if !IsCode(err, ErrInvalidAPK) {
				t.Fatalf("got %v, want invalid_apk", err)
			}
		})
	}

	notAPK := filepath.Join(t.TempDir(), "application.zip")
	if err := os.WriteFile(notAPK, []byte("ZIP"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = client.Install(context.Background(), "serial1", notAPK)
	if !IsCode(err, ErrInvalidAPK) {
		t.Fatalf("got %v, want invalid_apk", err)
	}
}

func TestInstallPreservesDeviceDisconnection(t *testing.T) {
	apkPath := filepath.Join(t.TempDir(), "application.apk")
	if err := os.WriteFile(apkPath, []byte("APK"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{ADBPath: fakeADB(t, `
case "$1 $2 $3 $4" in
  "devices -l  ") printf 'List of devices attached\nserial1\tdevice\n' ;;
  "-s serial1 install -r") printf "error: device 'serial1' not found\n" >&2; exit 1 ;;
esac`)})
	if err != nil {
		t.Fatal(err)
	}

	err = client.Install(context.Background(), "serial1", apkPath)
	if !IsCode(err, ErrDeviceDisconnected) {
		t.Fatalf("got %v, want device_disconnected", err)
	}
}
