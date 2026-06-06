package main

import "slices"

// resolveChildSpawnRouting applies F3 provider/model routing for an intent child
// node (rnix-eval mcp/hello-mcp 401 finding). Rules, in order:
//   - inherit the caller's (orchestrator's) provider/model when the node leaves
//     them unset, so a child doesn't fall back to the project default provider
//     with a bare model name;
//   - when the resolved model names a known provider (decompose LLMs frequently
//     put "deepseek"/"claude" in the model field), treat it as a provider
//     selection and clear the model so that provider's default model is used —
//     instead of sending a bogus model name to the wrong provider and getting a
//     remote 401.
//
// callerProvider/callerModel are empty when there is no caller process.
func resolveChildSpawnRouting(nodeProvider, nodeModel, callerProvider, callerModel string, knownProviders []string) (provider, model string) {
	provider, model = nodeProvider, nodeModel
	if provider == "" {
		provider = callerProvider
	}
	if model == "" {
		model = callerModel
	}
	if model != "" && slices.Contains(knownProviders, model) {
		provider = model
		model = ""
	}
	return provider, model
}
