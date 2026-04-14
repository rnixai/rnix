package memory

import "embed"

//go:embed prompts/*.txt
var promptsFS embed.FS

// loadPromptTemplate reads a prompt template from the embedded filesystem.
func loadPromptTemplate(name string) string {
	data, err := promptsFS.ReadFile("prompts/" + name)
	if err != nil {
		return ""
	}
	return string(data)
}

// LoadRecallSummarizePrompt returns the recall summarization prompt template.
// Exported for use by drivers/memory/recall_file.go.
func LoadRecallSummarizePrompt() string {
	return loadPromptTemplate("recall_summarize.txt")
}
