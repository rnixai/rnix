package rnix

import "embed"

//go:embed lib/agents
var EmbeddedAgents embed.FS

//go:embed lib/skills
var EmbeddedSkills embed.FS
