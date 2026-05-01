package intentdriver

import "embed"

//go:embed prompts/*.txt
var promptsFS embed.FS

func loadPrompt(name string) string {
	data, err := promptsFS.ReadFile("prompts/" + name + ".txt")
	if err != nil {
		panic("intentdriver: missing embedded prompt: " + name + ".txt")
	}
	return string(data)
}
