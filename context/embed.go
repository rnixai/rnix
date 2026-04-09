package context

import (
	"embed"
	"fmt"
)

//go:embed sections/*.txt
var sectionsFS embed.FS

// LoadSection returns the content of an embedded section template file.
// Panics if the section does not exist — embedded resources are compile-time
// constants and a missing section indicates a build or naming error.
func LoadSection(name string) string {
	data, err := sectionsFS.ReadFile("sections/" + name + ".txt")
	if err != nil {
		panic(fmt.Sprintf("LoadSection(%q): embedded section missing: %v", name, err))
	}
	return string(data)
}
