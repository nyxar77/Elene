package adb

import (
	"context"
	"testing"
)

func TestParseDevices(t *testing.T) {
	output := "* daemon started successfully\n" +
		"List of devices attached\n" +
		"R58M123456A\tdevice product:panther model:Pixel_7 transport_id:1\n" +
		"emulator-5554\toffline transport_id:2\n" +
		"ABC123\tunauthorized usb:1-2 transport_id:3\n" +
		"bad\tunknown\n"
	devices, err := ParseDevices(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 {
		t.Fatalf("got %d devices, want 3", len(devices))
	}
	if devices[0].Model != "Pixel_7" || devices[0].Product != "panther" || devices[0].TransportID != "1" {
		t.Fatalf("parsed metadata incorrectly: %+v", devices[0])
	}
	if devices[1].State != DeviceOffline || devices[2].State != DeviceUnauthorized {
		t.Fatalf("parsed states incorrectly: %+v", devices)
	}
}

func TestParseDevicesRejectsMalformedOutput(t *testing.T) {
	_, err := ParseDevices("daemon is unavailable\n")
	if !IsCode(err, ErrMalformedOutput) {
		t.Fatalf("got %v, want malformed_adb_output", err)
	}
}

func TestFindDeviceReturnsStateErrors(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `
printf 'List of devices attached\nunauthorized\tunauthorized\noffline\toffline\n'`)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.FindDevice(context.Background(), "unauthorized")
	if !IsCode(err, ErrDeviceUnauthorized) {
		t.Fatalf("got %v, want device_unauthorized", err)
	}
	_, err = client.FindDevice(context.Background(), "offline")
	if !IsCode(err, ErrDeviceOffline) {
		t.Fatalf("got %v, want device_offline", err)
	}
}
