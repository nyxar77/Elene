package adb

import (
	"context"
	"strings"
)

type DeviceState string

const (
	DeviceOnline       DeviceState = "online"
	DeviceOffline      DeviceState = "offline"
	DeviceUnauthorized DeviceState = "unauthorized"
)

type Device struct {
	Serial         string
	State          DeviceState
	Product        string
	Model          string
	TransportID    string
	Manufacturer   string
	AndroidVersion string
	SDK            int
	ABIs           []string
}

func ParseDevices(output string) ([]Device, error) {
	var devices []Device
	foundHeader := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "List of devices attached" {
			foundHeader = true
			continue
		}
		if !foundHeader || line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		state := DeviceState(fields[1])
		// ADB calls an authorized device "device". Elene exposes the clearer
		// domain state "online" instead.
		if state == "device" {
			state = DeviceOnline
		}
		if state != DeviceOnline && state != DeviceOffline && state != DeviceUnauthorized {
			continue
		}

		device := Device{Serial: fields[0], State: state}
		for _, field := range fields[2:] {
			key, value, ok := strings.Cut(field, ":")
			if !ok {
				continue
			}
			switch key {
			case "product":
				device.Product = value
			case "model":
				device.Model = value
			case "transport_id":
				device.TransportID = value
			}
		}
		devices = append(devices, device)
	}
	if !foundHeader {
		return nil, &Error{Code: ErrMalformedOutput, Operation: "parse adb devices", Detail: "adb did not return a device list"}
	}
	return devices, nil
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	output, err := c.run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return ParseDevices(string(output))
}

func (c *Client) FindDevice(ctx context.Context, serial string) (Device, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return Device{}, &Error{Code: ErrDeviceNotFound, Operation: "select device", Detail: "device serial is required"}
	}

	devices, err := c.Devices(ctx)
	if err != nil {
		return Device{}, err
	}
	for _, device := range devices {
		if device.Serial != serial {
			continue
		}
		switch device.State {
		case DeviceUnauthorized:
			return Device{}, &Error{Code: ErrDeviceUnauthorized, Operation: "select device", Detail: "the device has not authorized this computer"}
		case DeviceOffline:
			return Device{}, &Error{Code: ErrDeviceOffline, Operation: "select device", Detail: "the device is offline"}
		default:
			return device, nil
		}
	}
	return Device{}, &Error{Code: ErrDeviceNotFound, Operation: "select device", Detail: "device was not found: " + serial}
}
