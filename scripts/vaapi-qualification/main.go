// vaapi-qualification prints a candidate host qualification record.
// It does not test hardware, qualify output quality, or install the record.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/danrichardson/sqzarr/internal/transcoder"
)

func main() {
	fmt.Fprintln(os.Stderr, "Candidate only: install only after reviewed low-power VBR correctness, quality and sustained tests on this runtime.")
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(transcoder.NewLowPowerQualification()); err != nil {
		fmt.Fprintln(os.Stderr, "Could not encode qualification record:", err)
		os.Exit(1)
	}
}
