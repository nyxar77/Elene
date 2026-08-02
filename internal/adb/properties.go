package adb

import (
	"context"
	"strconv"
	"strings"
)

type deviceProperty struct {
	name string
	key  string
}

var inspectedProperties = []deviceProperty{
	{name: "manufacturer", key: "ro.product.manufacturer"},
	{name: "model", key: "ro.product.model"},
	{name: "android", key: "ro.build.version.release"},
	{name: "sdk", key: "ro.build.version.sdk"},
	{name: "abis", key: "ro.product.cpu.abilist"},
}

func (c *Client) InspectDevice(ctx context.Context, serial string) (Device, error) {
	device, err := c.FindDevice(ctx, serial)
	if err != nil {
		return Device{}, err
	}

	for _, property := range inspectedProperties {
		output, runErr := c.run(ctx, "-s", device.Serial, "shell", "getprop", property.key)
		if runErr != nil {
			return Device{}, runErr
		}
		value := strings.TrimSpace(string(output))

		switch property.name {
		case "manufacturer":
			device.Manufacturer = value
		case "model":
			device.Model = value
		case "android":
			device.AndroidVersion = value
		case "sdk":
			sdk, parseErr := strconv.Atoi(value)
			if parseErr != nil || sdk < 1 {
				return Device{}, &Error{Code: ErrMalformedOutput, Operation: "read device SDK", Detail: "adb returned an invalid Android SDK value"}
			}
			device.SDK = sdk
		case "abis":
			device.ABIs = splitABIs(value)
		}
	}
	return device, nil
}

func splitABIs(value string) []string {
	var abis []string
	for _, abi := range strings.Split(value, ",") {
		if abi = strings.TrimSpace(abi); abi != "" {
			abis = append(abis, abi)
		}
	}
	return abis
}
