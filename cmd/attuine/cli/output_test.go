package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"name": "test"}
	if err := printJSONTo(&buf, data); err != nil {
		t.Fatalf("printJSONTo() error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("name = %q, want %q", result["name"], "test")
	}
}

func TestPrintTextLines(t *testing.T) {
	var buf bytes.Buffer
	printTextTo(&buf, []string{"line1", "line2"})
	got := buf.String()
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Errorf("output = %q, want lines line1 and line2", got)
	}
}
