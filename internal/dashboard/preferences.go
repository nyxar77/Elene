package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// StrictInt parses integer values and strictly rejects floats or decimal representations (e.g. 19.5 or 18.0).
type StrictInt int

func (s *StrictInt) UnmarshalJSON(b []byte) error {
	str := strings.TrimSpace(string(b))
	if strings.Contains(str, ".") || strings.Contains(str, "e") || strings.Contains(str, "E") {
		return fmt.Errorf("must be a whole integer, decimal numbers are not accepted: %s", str)
	}
	val, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid integer: %s", str)
	}
	*s = StrictInt(val)
	return nil
}

func (s StrictInt) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(s))), nil
}

type Preferences struct {
	Theme              string    `json:"theme"`
	Accent             string    `json:"accent"`
	Contrast           string    `json:"contrast"`
	Interface          string    `json:"interface"`
	InstallStrategy    string    `json:"installStrategy"`
	MaxCachedAPKs      StrictInt `json:"maxCachedAPKs"`
	CacheRetentionDays StrictInt `json:"cacheRetentionDays"`
}

type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var themes = []Choice{
	{Value: "light", Label: "Elene · Light"},
	{Value: "dark", Label: "Elene · Dark"},
	{Value: "dracula", Label: "Dracula"},
	{Value: "latte", Label: "Catppuccin · Latte"},
	{Value: "frappe", Label: "Catppuccin · Frappé"},
	{Value: "macchiato", Label: "Catppuccin · Macchiato"},
	{Value: "mocha", Label: "Catppuccin · Mocha"},
	{Value: "gruvbox-light", Label: "Gruvbox · Light"},
	{Value: "gruvbox-dark", Label: "Gruvbox · Dark"},
}

var accents = []string{
	"rosewater", "flamingo", "pink", "mauve", "red", "maroon",
	"peach", "yellow", "green", "teal", "sky", "sapphire",
	"blue", "lavender",
}

func defaultPreferences() Preferences {
	return Preferences{
		Theme:              "light",
		Accent:             "mauve",
		Contrast:           "medium",
		Interface:          "enhanced",
		InstallStrategy:    "auto",
		MaxCachedAPKs:      10,
		CacheRetentionDays: 14,
	}
}

func (p Preferences) validate() error {
	hasTheme := slices.ContainsFunc(themes, func(c Choice) bool {
		return c.Value == p.Theme
	})
	if !hasTheme {
		return fmt.Errorf("invalid appearance theme: %q", p.Theme)
	}

	if !slices.Contains(accents, p.Accent) {
		return fmt.Errorf("invalid appearance accent: %q", p.Accent)
	}

	validContrasts := []string{"soft", "medium", "hard"}
	if !slices.Contains(validContrasts, p.Contrast) {
		return fmt.Errorf("invalid appearance contrast: %q", p.Contrast)
	}

	validInterfaces := []string{"enhanced", "basic"}
	if !slices.Contains(validInterfaces, p.Interface) {
		return fmt.Errorf("invalid interface mode: %q", p.Interface)
	}

	validStrategies := []string{"auto", "download_and_install", "download_only"}
	if p.InstallStrategy != "" && !slices.Contains(validStrategies, p.InstallStrategy) {
		return fmt.Errorf("invalid install strategy: %q", p.InstallStrategy)
	}

	if p.MaxCachedAPKs != 0 && (p.MaxCachedAPKs < 5 || p.MaxCachedAPKs > 20) {
		return fmt.Errorf("maximum cached APKs must be between 5 and 20, got %d", p.MaxCachedAPKs)
	}

	if p.CacheRetentionDays != 0 && (p.CacheRetentionDays < 1 || p.CacheRetentionDays > 30) {
		return fmt.Errorf("cache retention duration must be between 1 and 30 days, got %d", p.CacheRetentionDays)
	}

	return nil
}

func loadPreferences(path string) (Preferences, error) {
	p := defaultPreferences()
	if path == "" {
		return p, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}

	if err = json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("read settings: %w", err)
	}

	if p.InstallStrategy == "" {
		p.InstallStrategy = "auto"
	}
	if p.MaxCachedAPKs == 0 {
		p.MaxCachedAPKs = 10
	}
	if p.CacheRetentionDays == 0 {
		p.CacheRetentionDays = 14
	}

	return p, p.validate()
}

// savePreferences replaces the file atomically so an interrupted save leaves the previous settings intact.
func savePreferences(path string, p Preferences) error {
	if err := p.validate(); err != nil {
		return err
	}
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()

	if _, err = file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}
