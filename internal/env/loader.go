package env

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadUserEnv reads {dataDir}/{userID}/.env and returns the parsed key-value map.
// Returns an empty map (no error) when the file does not exist.
func LoadUserEnv(dataDir, userID string) map[string]string {
	result := make(map[string]string)

	f, err := os.Open(filepath.Join(dataDir, userID, ".env"))
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip blank lines and comments.
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Split on the first "=" only.
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		value = stripQuotes(value)

		if key != "" {
			result[key] = value
		}
	}

	return result
}

// LoadUserEnvWithSystem merges os.Environ() with the user's .env file.
// File values take precedence over system environment variables.
func LoadUserEnvWithSystem(dataDir, userID string) map[string]string {
	result := make(map[string]string)

	// Load system env first (lower precedence).
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			result[k] = v
		}
	}

	// Overlay file values (higher precedence).
	for k, v := range LoadUserEnv(dataDir, userID) {
		result[k] = v
	}

	return result
}

// stripQuotes removes matching surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
