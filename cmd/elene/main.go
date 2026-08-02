package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nyxar77/elene/internal/adb"
)

const commandTimeout = 15 * time.Second

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	adbPath := getenv("ELENE_ADB_PATH")
	if len(args) >= 2 && args[0] == "--adb" {
		adbPath, args = args[1], args[2:]
	}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	command := args[0]
	args = args[1:]
	// Accept the option after the subcommand as well, for interactive use.
	if len(args) >= 2 && args[0] == "--adb" {
		adbPath, args = args[1], args[2:]
	}

	switch command {
	case "devices":
		if len(args) != 0 {
			usage(stderr)
			return 2
		}
	case "inspect-device":
		if len(args) != 1 {
			usage(stderr)
			return 2
		}
	case "install":
		if len(args) != 2 {
			usage(stderr)
			return 2
		}
	default:
		usage(stderr)
		return 2
	}

	client, err := adb.New(adb.Config{ADBPath: adbPath, CommandTimeout: commandTimeout})
	if err != nil {
		printError(stderr, err)
		return 1
	}

	switch command {
	case "devices":
		err = devices(ctx, client, stdout)
	case "inspect-device":
		err = inspectDevice(ctx, client, args[0], stdout)
	case "install":
		err = client.Install(ctx, args[0], args[1])
		if err == nil {
			_, _ = fmt.Fprintln(stdout, "installation succeeded")
		}
	}
	if err != nil {
		printError(stderr, err)
		return 1
	}
	return 0
}

func devices(ctx context.Context, client *adb.Client, output io.Writer) error {
	list, err := client.Devices(ctx)
	if err != nil {
		return err
	}
	for _, device := range list {
		fmt.Fprintf(output, "%s\t%s\t%s\n", device.Serial, device.State, device.Model)
	}
	return nil
}

func inspectDevice(ctx context.Context, client *adb.Client, serial string, output io.Writer) error {
	device, err := client.InspectDevice(ctx, serial)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Serial: %s\nState: %s\nManufacturer: %s\nModel: %s\nAndroid: %s\nSDK: %d\nABIs: %s\n", device.Serial, device.State, device.Manufacturer, device.Model, device.AndroidVersion, device.SDK, strings.Join(device.ABIs, ", "))
	return nil
}

func printError(output io.Writer, err error) {
	var adbErr *adb.Error
	if errors.As(err, &adbErr) {
		fmt.Fprintf(output, "%s: %s\n", adbErr.Code, adbErr.Error())
		return
	}
	_, _ = fmt.Fprintln(output, err)
}

func usage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "usage: elene [--adb PATH] devices | inspect-device SERIAL | install SERIAL PATH_TO_APK")
}
