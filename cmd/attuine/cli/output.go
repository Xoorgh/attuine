package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// printJSON writes v as indented JSON to stdout.
func printJSON(v any) error {
	return printJSONTo(os.Stdout, v)
}

// printJSONTo writes v as indented JSON to w.
func printJSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printText writes lines to stdout.
func printText(lines []string) {
	printTextTo(os.Stdout, lines)
}

// printTextTo writes lines to w.
func printTextTo(w io.Writer, lines []string) {
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}
