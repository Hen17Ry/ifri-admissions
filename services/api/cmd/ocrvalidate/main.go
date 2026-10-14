// Command ocrvalidate runs the API's independent OCR result validation on worker results saved
// by the private corpus harness. It prints only document identifiers and static reasons.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"ifri-admissions/services/api/internal/modules/analysis"
)

type saved struct {
	Result        analysis.Result     `json:"result"`
	Expected      []analysis.Expected `json:"expected"`
	Qualification string              `json:"qualification"`
}

type verdict struct {
	Document string `json:"document"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ocrvalidate <directory of saved worker results>")
		os.Exit(2)
	}
	threshold := 0.85
	if raw := os.Getenv("OCR_MIN_CONFIDENCE"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "OCR_MIN_CONFIDENCE invalid")
			os.Exit(2)
		}
		threshold = value
	}
	files, err := filepath.Glob(filepath.Join(os.Args[1], "*.json"))
	if err != nil {
		os.Exit(2)
	}
	sort.Strings(files)
	verdicts := []verdict{}
	for _, file := range files {
		document := filepath.Base(file[:len(file)-len(".json")])
		var item saved
		data, err := os.ReadFile(file)
		if err == nil {
			err = json.Unmarshal(data, &item)
		}
		if err != nil {
			verdicts = append(verdicts, verdict{Document: document, Reason: "unreadable saved result"})
			continue
		}
		v := verdict{Document: document, Accepted: true}
		if err := analysis.Validate(item.Result, item.Expected, threshold, item.Qualification); err != nil {
			v = verdict{Document: document, Reason: err.Error()}
		}
		verdicts = append(verdicts, v)
	}
	_ = json.NewEncoder(os.Stdout).Encode(verdicts)
}
