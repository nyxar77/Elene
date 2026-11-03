package adb

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var packageNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

type Package struct {
	ID          string `json:"id"`
	VersionCode int64  `json:"versionCode,omitempty"`
}

// Packages lists third-party packages, including version codes if supported by the Android device.
func (c *Client) Packages(ctx context.Context, serial string) ([]Package, error) {
	device, err := c.FindDevice(ctx, serial)
	if err != nil {
		return nil, err
	}

	// Try querying with --show-versioncode first (supported on Android 8.0+)
	output, err := c.run(ctx, "-s", device.Serial, "shell", "pm", "list", "packages", "--show-versioncode", "-3")
	if err != nil {
		// Fallback to standard pm list packages -3
		output, err = c.run(ctx, "-s", device.Serial, "shell", "pm", "list", "packages", "-3")
		if err != nil {
			return nil, err
		}
	}

	return parsePackages(string(output))
}

func parsePackages(output string) ([]Package, error) {
	packages := []Package{}
	seen := map[string]bool{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line, ok := strings.CutPrefix(line, "package:")
		if !ok {
			return nil, &Error{
				Code:      ErrMalformedOutput,
				Operation: "list installed apps",
				Detail:    "ADB returned an invalid package list",
			}
		}

		// Line can be: "com.example.app" or "com.example.app versionCode:12345"
		var name string
		var versionCode int64

		fields := strings.Fields(line)
		if len(fields) >= 1 {
			name = fields[0]
		}
		if len(fields) >= 2 && strings.HasPrefix(fields[1], "versionCode:") {
			vcStr := strings.TrimPrefix(fields[1], "versionCode:")
			if vc, parseErr := strconv.ParseInt(vcStr, 10, 64); parseErr == nil {
				versionCode = vc
			}
		}

		if !packageNamePattern.MatchString(name) {
			return nil, &Error{
				Code:      ErrMalformedOutput,
				Operation: "list installed apps",
				Detail:    "ADB returned an invalid package list",
			}
		}

		if !seen[name] {
			packages = append(packages, Package{
				ID:          name,
				VersionCode: versionCode,
			})
			seen[name] = true
		}
	}

	slices.SortFunc(packages, func(a, b Package) int {
		return strings.Compare(a.ID, b.ID)
	})

	return packages, nil
}
