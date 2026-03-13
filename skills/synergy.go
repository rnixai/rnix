package skills

import "sort"

// DetectSynergies detects synergy matches among loaded skills.
// Returns a deduplicated, sorted list of emergent instructions.
func DetectSynergies(skills []*SkillInfo) []string {
	if len(skills) == 0 {
		return nil
	}

	// Build loaded skill name set
	loaded := make(map[string]struct{}, len(skills))
	for _, s := range skills {
		loaded[s.Manifest.Name] = struct{}{}
	}

	// Collect matched instructions with dedup
	seen := make(map[string]struct{})
	var result []string
	for _, s := range skills {
		for _, decl := range s.Manifest.Synergies {
			if decl.Instruction == "" {
				continue
			}
			if _, ok := loaded[decl.With]; ok {
				if _, dup := seen[decl.Instruction]; !dup {
					seen[decl.Instruction] = struct{}{}
					result = append(result, decl.Instruction)
				}
			}
		}
	}

	sort.Strings(result)
	return result
}
