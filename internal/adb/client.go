// Package adb provides the small, argument-safe ADB boundary used by Elene.
package adb

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ErrorCode string

const (
	ErrADBNotFound        ErrorCode = "adb_not_found"
	ErrADBUnavailable     ErrorCode = "adb_unavailable"
	ErrCommandCanceled    ErrorCode = "command_canceled"
	ErrCommandTimeout     ErrorCode = "command_timeout"
	ErrMalformedOutput    ErrorCode = "malformed_adb_output"
	ErrDeviceNotFound     ErrorCode = "device_not_found"
	ErrDeviceUnauthorized ErrorCode = "device_unauthorized"
	ErrDeviceOffline      ErrorCode = "device_offline"
	ErrDeviceDisconnected ErrorCode = "device_disconnected"
	ErrInvalidAPK         ErrorCode = "invalid_apk"
	ErrInstallFailed      ErrorCode = "install_failed"
)

type Error struct {
	Code      ErrorCode
	Operation string
	Detail    string
	Err       error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

type Config struct {
	ADBPath        string
	CommandTimeout time.Duration
}

type Client struct {
	adbPath        string
	commandTimeout time.Duration
}

func New(cfg Config) (*Client, error) {
	path := strings.TrimSpace(cfg.ADBPath)
	if path == "" {
		var err error
		path, err = exec.LookPath("adb")
		if err != nil {
			return nil, &Error{Code: ErrADBNotFound, Operation: "locate adb", Detail: "adb was not found in PATH", Err: err}
		}
	} else if _, err := exec.LookPath(path); err != nil {
		return nil, &Error{Code: ErrADBNotFound, Operation: "locate adb", Detail: fmt.Sprintf("adb executable %q was not found", path), Err: err}
	}

	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 15 * time.Second
	}
	return &Client{adbPath: path, commandTimeout: cfg.CommandTimeout}, nil
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	if c == nil || c.adbPath == "" {
		return nil, &Error{Code: ErrADBUnavailable, Operation: "run adb", Detail: "adb client is not configured"}
	}

	commandCtx, cancel := context.WithTimeout(ctx, c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, c.adbPath, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}

	operation := strings.Join(args, " ")
	switch {
	case errors.Is(commandCtx.Err(), context.Canceled):
		return nil, &Error{Code: ErrCommandCanceled, Operation: operation, Detail: "adb command was canceled", Err: commandCtx.Err()}
	case errors.Is(commandCtx.Err(), context.DeadlineExceeded):
		return nil, &Error{Code: ErrCommandTimeout, Operation: operation, Detail: "adb command timed out", Err: commandCtx.Err()}
	default:
		return nil, classifyCommandFailure(operation, output, err)
	}
}

func classifyCommandFailure(operation string, output []byte, err error) *Error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}

	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "device unauthorized"):
		return &Error{Code: ErrDeviceUnauthorized, Operation: operation, Detail: "the device has not authorized this computer", Err: err}
	case strings.Contains(lower, "device offline"):
		return &Error{Code: ErrDeviceOffline, Operation: operation, Detail: "the device is offline", Err: err}
	case strings.Contains(lower, "no devices/emulators found"),
		strings.Contains(lower, "not found") && strings.Contains(lower, "device"):
		return &Error{Code: ErrDeviceDisconnected, Operation: operation, Detail: "the device is no longer connected", Err: err}
	default:
		return &Error{Code: ErrADBUnavailable, Operation: operation, Detail: detail, Err: err}
	}
}

func IsCode(err error, code ErrorCode) bool {
	var adbErr *Error
	return errors.As(err, &adbErr) && adbErr.Code == code
}
