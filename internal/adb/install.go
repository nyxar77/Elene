package adb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (c *Client) Install(ctx context.Context, serial, apkPath string) error {
	if strings.Contains(apkPath, "..") {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: "path traversal sequences are not allowed"}
	}
	cleanPath := filepath.Clean(apkPath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: fmt.Sprintf("APK path is not readable: %s", cleanPath), Err: err}
	}
	if !info.Mode().IsRegular() {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: "APK path is not a regular file"}
	}
	if strings.ToLower(filepath.Ext(cleanPath)) != ".apk" {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: "only .apk files are supported"}
	}

	device, err := c.FindDevice(ctx, serial)
	if err != nil {
		return err
	}
	if _, err := c.run(ctx, "-s", device.Serial, "install", "-r", cleanPath); err != nil {
		if IsCode(err, ErrDeviceUnauthorized) || IsCode(err, ErrDeviceOffline) || IsCode(err, ErrDeviceDisconnected) || IsCode(err, ErrCommandCanceled) || IsCode(err, ErrCommandTimeout) {
			return err
		}
		return &Error{Code: ErrInstallFailed, Operation: "install APK", Detail: "ADB could not install the APK", Err: err}
	}
	return nil
}
