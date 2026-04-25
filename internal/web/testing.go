package web

// Helpers exposed only for out-of-package end-to-end tests. Do NOT use these
// in production code; they bypass normal configuration paths and exist so
// tests under cmd/slsh can point a real App at mock LLM/MCP endpoints and
// supply a known CLI token without racing with the runtime's file-backed
// token initialisation.

// TestingOverrideCLIAuth replaces the App's CLI token and token-file path.
// The file itself is managed by the caller.
func TestingOverrideCLIAuth(app *App, token, tokenPath string) {
	if app == nil {
		return
	}
	app.cliToken = token
	app.cliTokenPath = tokenPath
}

// TestingForceLocalAssistantLLM pins the local assistant LLM URL and mode so
// an out-of-package e2e test can drive the local-assistant tool loop against
// a mock /v1/chat/completions endpoint.
func TestingForceLocalAssistantLLM(app *App, llmURL string) {
	if app == nil {
		return
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llmURL
	app.assistantLLMExplicit = true
	app.intentLLMURL = ""
}
