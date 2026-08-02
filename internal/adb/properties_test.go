package adb

import (
	"context"
	"strings"
	"testing"
)

func TestInspectDevice(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `
case "$1 $2 $3 $4" in
  "devices -l  ") printf 'List of devices attached\nserial1\tdevice product:foo model:Phone transport_id:7\n' ;;
  "-s serial1 shell getprop")
    case "$5" in
      ro.product.manufacturer) printf 'Acme\n' ;;
      ro.product.model) printf 'Phone\n' ;;
      ro.build.version.release) printf '14\n' ;;
      ro.build.version.sdk) printf '34\n' ;;
      ro.product.cpu.abilist) printf 'arm64-v8a, armeabi-v7a\n' ;;
    esac ;;
esac`)})
	if err != nil {
		t.Fatal(err)
	}

	device, err := client.InspectDevice(context.Background(), "serial1")
	if err != nil {
		t.Fatal(err)
	}
	if device.Manufacturer != "Acme" || device.SDK != 34 || strings.Join(device.ABIs, ",") != "arm64-v8a,armeabi-v7a" {
		t.Fatalf("unexpected device: %+v", device)
	}
}

func TestInspectDeviceRejectsInvalidSDK(t *testing.T) {
	client, err := New(Config{ADBPath: fakeADB(t, `
case "$1 $2" in
  "devices -l") printf 'List of devices attached\nserial1\tdevice\n' ;;
  "-s serial1") printf 'not-a-number\n' ;;
esac`)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.InspectDevice(context.Background(), "serial1")
	if !IsCode(err, ErrMalformedOutput) {
		t.Fatalf("got %v, want malformed_adb_output", err)
	}
}

func TestSplitABIs(t *testing.T) {
	abis := splitABIs("arm64-v8a, , armeabi-v7a")
	if strings.Join(abis, ",") != "arm64-v8a,armeabi-v7a" {
		t.Fatalf("got %v", abis)
	}
}
