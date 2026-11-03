package adb

import (
	"strings"
	"testing"
	"time"
)

func TestParsePackages(t *testing.T) {
	got, err := parsePackages("package:org.example.z versionCode:100\r\npackage:com.example.app versionCode:200\npackage:com.example.app\n")
	if err != nil || len(got) != 2 || got[0].ID != "com.example.app" || got[0].VersionCode != 200 || got[1].VersionCode != 100 {
		t.Fatal(got, err)
	}
	for _, input := range []string{"Error: permission denied", "package:../../tmp", "package:org.example;bad"} {
		if _, err := parsePackages(input); err == nil {
			t.Fatal(input)
		}
	}
}
func TestPackagesUsesSelectedDevice(t *testing.T) {
	path := fakeADB(t, `if [ "$1" = devices ]; then
printf 'List of devices attached\nphone device\n'
elif [ "$1" = -s ] && [ "$2" = phone ] && [ "$3" = shell ] && [ "$4" = pm ] && [ "$5" = list ] && [ "$6" = packages ] && [ "$7" = -3 ]; then
printf 'package:org.example.app\n'
else
exit 1
fi`)
	client, err := New(Config{ADBPath: path, CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	apps, err := client.Packages(t.Context(), "phone")
	if err != nil || len(apps) != 1 || !strings.HasPrefix(apps[0].ID, "org.") {
		t.Fatal(apps, err)
	}
}
