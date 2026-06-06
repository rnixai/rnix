package main

import "testing"

// TestResolveChildSpawnRouting verifies F3 (rnix-eval upstream finding): an
// intent child node's provider/model is resolved so a bare model name does not
// route to the wrong provider and 401.
func TestResolveChildSpawnRouting(t *testing.T) {
	known := []string{"opencodego", "deepseek", "claude"}
	tests := []struct {
		name                        string
		nodeProvider, nodeModel     string
		callerProvider, callerModel string
		wantProvider, wantModel     string
	}{
		{
			name:           "inherit both from caller when node unset",
			callerProvider: "opencodego", callerModel: "deepseek-v4-pro",
			wantProvider: "opencodego", wantModel: "deepseek-v4-pro",
		},
		{
			name:      "bare provider-name as model routes to that provider",
			nodeModel: "deepseek", callerProvider: "opencodego", callerModel: "deepseek-v4-flash",
			wantProvider: "deepseek", wantModel: "",
		},
		{
			name:         "explicit node provider+model preserved",
			nodeProvider: "claude", nodeModel: "sonnet",
			callerProvider: "opencodego", callerModel: "flash",
			wantProvider: "claude", wantModel: "sonnet",
		},
		{
			name:         "node provider set, model inherited from caller",
			nodeProvider: "deepseek", callerProvider: "opencodego", callerModel: "deepseek-v4-pro",
			wantProvider: "deepseek", wantModel: "deepseek-v4-pro",
		},
		{
			name:      "no caller, no node values stays empty",
			wantProvider: "", wantModel: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotP, gotM := resolveChildSpawnRouting(tt.nodeProvider, tt.nodeModel, tt.callerProvider, tt.callerModel, known)
			if gotP != tt.wantProvider || gotM != tt.wantModel {
				t.Errorf("resolveChildSpawnRouting = (%q, %q), want (%q, %q)", gotP, gotM, tt.wantProvider, tt.wantModel)
			}
		})
	}
}
