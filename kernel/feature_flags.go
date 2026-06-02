package kernel

// FeatureFlags controls which emergent subsystems are active for a process.
// kernel package does not import internal/config; cmd/rnix bridges the two
// via value copy at daemon startup.
type FeatureFlags struct {
	Planning      bool
	Replan        bool
	Specialize    bool
	DiscoverSkill bool
	Spawn         bool
	DiffMemory    bool
	StemMatcher   bool
	Immune        bool
	Compaction    bool
}

// FullFeatureFlags returns a FeatureFlags with all flags enabled (equivalent to ProfileFull).
func FullFeatureFlags() FeatureFlags {
	return FeatureFlags{
		Planning:      true,
		Replan:        true,
		Specialize:    true,
		DiscoverSkill: true,
		Spawn:         true,
		DiffMemory:    true,
		StemMatcher:   true,
		Immune:        true,
		Compaction:    true,
	}
}
