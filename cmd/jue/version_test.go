package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var semverRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

func TestSemverParity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("failed to read root VERSION file: %v", err)
	}
	expected := strings.TrimSpace(string(data))

	if version != expected {
		t.Errorf("version in main.go (%q) does not match VERSION file (%q)", version, expected)
	}

	if version != "2.2.0" {
		t.Errorf("expected default version to be 2.2.0, got %q", version)
	}

	if !semverRegex.MatchString(version) {
		t.Errorf("version %q does not conform to semver format", version)
	}
}
