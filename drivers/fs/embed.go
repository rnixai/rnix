package fs

import "embed"

//go:embed prompts/*.txt
var promptsFS embed.FS

// loadPrompt returns the content of a prompt template file.
func loadPrompt(name string) string {
	data, err := promptsFS.ReadFile("prompts/" + name + ".txt")
	if err != nil {
		// Embedded file missing is a build-time error, so this should never happen.
		return ""
	}
	return string(data)
}
