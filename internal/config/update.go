package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// keySection maps each runtime-editable config key to its TOML section.
// Keys not found in the file are inserted into the correct section.
var keySection = map[string]string{
	"interval_hours":           "scanner",
	"worker_concurrency":       "scanner",
	"root_dirs":                "scanner",
	"paused":                   "scanner",
	"processed_dir_name":       "safety",
	"originals_retention_days": "safety",
	"fail_threshold":           "safety",
	"system_fail_threshold":    "safety",
	"delete_confirm_single":    "safety",
	"encoder":                  "transcoder",
	"hardware_decode":          "transcoder",
	"low_power":                "transcoder",
	"password_hash":            "auth",
	"enabled":                  "plex",
	"base_url":                 "plex",
	"token":                    "plex",
}

// UpdateFile updates specific key=value pairs in a TOML config file in-place.
// All other content (comments, blank lines, other settings) is preserved.
// Missing keys are inserted into the correct TOML section (not appended to the end).
func UpdateFile(path string, updates map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	content := string(data)
	for key, rawValue := range updates {
		re := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*).*$`)
		if re.MatchString(content) {
			content = re.ReplaceAllStringFunc(content, func(match string) string {
				return re.FindStringSubmatch(match)[1] + rawValue
			})
		} else {
			// Key not present — insert it into the correct section.
			section := keySection[key]
			if section == "" {
				// Unknown key: fall back to appending.
				content = strings.TrimRight(content, "\n") + "\n" + key + " = " + rawValue + "\n"
				continue
			}
			// Find the section header and insert after the last key in that section
			// (i.e. just before the next [section] header or end of file).
			sectionRe := regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(section) + `\]`)
			loc := sectionRe.FindStringIndex(content)
			if loc == nil {
				// Section header missing — fall back to appending.
				content = strings.TrimRight(content, "\n") + "\n\n[" + section + "]\n" + key + " = " + rawValue + "\n"
				continue
			}
			// Find the start of the next section after this one.
			rest := content[loc[1]:]
			nextSection := regexp.MustCompile(`(?m)^\[`).FindStringIndex(rest)
			var insertAt int
			if nextSection == nil {
				insertAt = len(content)
			} else {
				insertAt = loc[1] + nextSection[0]
			}
			// Insert before the next section (trim trailing whitespace from the block first).
			block := strings.TrimRight(content[:insertAt], "\n")
			tail := content[insertAt:]
			content = block + "\n" + key + " = " + rawValue + "\n" + tail
		}
	}

	return os.WriteFile(path, []byte(content), 0o644)
}
