package context

import "embed"

//go:embed sections/*.txt
var sectionsFS embed.FS

// LoadSection returns the content of a section template file.
func LoadSection(name string) string {
	data, err := sectionsFS.ReadFile("sections/" + name + ".txt")
	if err != nil {
		return ""
	}
	return string(data)
}
