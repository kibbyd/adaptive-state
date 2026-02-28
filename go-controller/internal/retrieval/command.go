package retrieval

import (
	"strings"
)

// #region command-detection
// IsDirectCommand returns true if the prompt is a direct tool command or short imperative
// that should skip evidence retrieval entirely.
func IsDirectCommand(prompt string) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if lower == "" {
		return false
	}

	// Tool command prefixes
	prefixes := []string{
		"list files",
		"read ",
		"write ",
		"search for ",
		"show files",
		"show me the files",
		"open ",
		"create ",
		"delete ",
		"remove ",
		"save ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Short imperative phrases (1-3 words, no question mark)
	if strings.Contains(lower, "?") {
		return false
	}
	words := strings.Fields(lower)
	if len(words) >= 1 && len(words) <= 3 {
		imperatives := map[string]bool{
			"list": true, "read": true, "show": true, "run": true,
			"write": true, "save": true, "stop": true, "start": true,
			"help": true, "clear": true, "reset": true, "quit": true,
			"exit": true,
		}
		if imperatives[words[0]] {
			return true
		}
	}

	return false
}

// #endregion command-detection
