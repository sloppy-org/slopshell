# Delegation Routing v2

Date: 2026-03-03

This document defines Tabura's post-Qwen routing policy for system-action delegation.

## Why

The local router (Qwen profile) is fast but brittle for current-information tasks and tool-heavy flows. v2 hardens this by enforcing deterministic delegation rules in backend execution, not only prompt guidance.

## Policy

- Domain detection: `current_info`, `coding`, `general`
- Complexity detection: `simple`, `complex`
- Route matrix:
  - simple `current_info` -> `spark` + `low`
  - complex `coding` -> `codex` + `high`
  - simple `coding` -> `codex` + `low`
  - complex `general` -> `gpt` + `high`
  - simple `general` -> `gpt` + `low`
- Safety override for `current_info`:
  - force `delegate`
  - block `shell`
  - inject route metadata into action payloads (`route_domain`, `route_complexity`, `route_reason`, `policy_version`)

## Delegate effort contract

`delegate_to_model` accepts only `reasoning_effort` at the MCP boundary.

This value is normalized and mapped to Codex app-server `effort` params at thread/turn start.

## Design notes from external agent systems

Applied best-practice patterns inspired by current public docs:

- Claude Code subagents: specialized agents with focused context and explicit tool boundaries.
- Gemini CLI: explicit model/provider selection, strong tool integration (including MCP), and grounding/search-oriented workflows.
- Codex/OpenAI model docs: explicit reasoning effort control per model family.

References:

- Anthropic Claude Code subagents: https://docs.anthropic.com/en/docs/claude-code/sub-agents
- Gemini CLI README: https://github.com/google-gemini/gemini-cli
- Gemini CLI docs index: https://github.com/google-gemini/gemini-cli/blob/main/docs/index.md
- Google Gemini CLI announcement: https://blog.google/technology/developers/introducing-gemini-cli-open-source-ai-agent/
- OpenAI model docs (reasoning effort support): https://platform.openai.com/docs/models/gpt-5-codex
- OpenAI API reference (`reasoning_effort`): https://platform.openai.com/docs/api-reference/chat/create
