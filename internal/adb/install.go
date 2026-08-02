package adb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (c *Client) Install(ctx context.Context, serial, apkPath string) error {
	info, err := os.Stat(apkPath)
	if err != nil {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: fmt.Sprintf("APK path is not readable: %s", apkPath), Err: err}
	}
	if !info.Mode().IsRegular() {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: "APK path is not a regular file"}
	}
	if strings.ToLower(filepath.Ext(apkPath)) != ".apk" {
		return &Error{Code: ErrInvalidAPK, Operation: "validate APK", Detail: "only .apk files are supported"}
	}

	device, err := c.FindDevice(ctx, serial)
	if err != nil {
		return err
	}
	if _, err := c.run(ctx, "-s", device.Serial, "install", "-r", apkPath); err != nil {
		if IsCode(err, ErrDeviceUnauthorized) || IsCode(err, ErrDeviceOffline) || IsCode(err, ErrDeviceDisconnected) || IsCode(err, ErrCommandCanceled) || IsCode(err, ErrCommandTimeout) {
			return err
		}
		return &Error{Code: ErrInstallFailed, Operation: "install APK", Detail: "ADB could not install the APK", Err: err}
	}
	return nil
}
