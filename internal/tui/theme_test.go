package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTheme(t *testing.T) {
	theme := defaultTheme()
	if theme.Accent != "62" {
		t.Errorf("expected accent '62', got %q", theme.Accent)
	}
	if theme.Muted != "240" {
		t.Errorf("expected muted '240', got %q", theme.Muted)
	}
	if theme.Ok != "42" {
		t.Errorf("expected ok '42', got %q", theme.Ok)
	}
}

func TestLoadThemeMissingFile(t *testing.T) {
	theme := loadThemeFrom("/nonexistent/path/theme.toml")
	want := defaultTheme()
	if theme != want {
		t.Errorf("missing file should return defaults, got %+v", theme)
	}
}

func TestLoadThemeFullFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.toml")
	content := `
accent    = "#ff0000"
muted     = "#111111"
text      = "#222222"
highlight = "#333333"
ok        = "#00ff00"
warn      = "#ffff00"
error     = "#ff0000"
`
	os.WriteFile(path, []byte(content), 0o644)

	theme := loadThemeFrom(path)
	if theme.Accent != "#ff0000" {
		t.Errorf("expected accent '#ff0000', got %q", theme.Accent)
	}
	if theme.Ok != "#00ff00" {
		t.Errorf("expected ok '#00ff00', got %q", theme.Ok)
	}
}

func TestLoadThemePartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.toml")
	content := `accent = "#abcdef"`
	os.WriteFile(path, []byte(content), 0o644)

	theme := loadThemeFrom(path)
	if theme.Accent != "#abcdef" {
		t.Errorf("expected accent '#abcdef', got %q", theme.Accent)
	}
	if theme.Muted != "240" {
		t.Errorf("expected muted to remain default '240', got %q", theme.Muted)
	}
}

func TestLoadThemeInvalidToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.toml")
	os.WriteFile(path, []byte("not valid [[[ toml"), 0o644)

	theme := loadThemeFrom(path)
	want := defaultTheme()
	if theme != want {
		t.Errorf("invalid TOML should return defaults, got %+v", theme)
	}
}
