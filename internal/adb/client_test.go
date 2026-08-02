package adb

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewRejectsMissingADB(t *testing.T) {
	_, err := New(Config{ADBPath: "/does/not/exist/adb"})
	if !IsCode(err, ErrADBNotFound) {
		t.Fatalf("got %v, want adb_not_found", err)
	}
}

func TestCommandTimeout(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `sleep 2`), CommandTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Devices(context.Background())
	if !IsCode(err, ErrCommandTimeout) {
		t.Fatalf("got %v, want command_timeout", err)
	}
}

func TestCommandCancellation(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `sleep 2`), CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Devices(ctx)
	if !IsCode(err, ErrCommandCanceled) {
		t.Fatalf("got %v, want command_canceled", err)
	}
}

func TestClassifyCommandFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
		code   ErrorCode
	}{
		{name: "unauthorized", output: "error: device unauthorized", code: ErrDeviceUnauthorized},
		{name: "offline", output: "error: device offline", code: ErrDeviceOffline},
		{name: "disconnected", output: "error: device 'serial' not found", code: ErrDeviceDisconnected},
		{name: "other", output: "error: cannot connect to daemon", code: ErrADBUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := classifyCommandFailure("devices -l", []byte(test.output), errors.New("exit status 1"))
			if err.Code != test.code {
				t.Fatalf("got %s, want %s", err.Code, test.code)
			}
		})
	}
}
