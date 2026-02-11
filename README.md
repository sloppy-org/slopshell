# tabula

Minimal Python prototype for terminal-first Codex sessions with an optional canvas window.

- start in `prompt` mode
- switch to `discussion` when a valid canvas artifact event arrives
- return to `prompt` on `clear_canvas`

`tabula` is the main command. It keeps Codex in your terminal and can launch a separate canvas window when display is available.

## Event bridge

Append JSONL events to `.tabula/canvas-events.jsonl`.

Supported kinds:

- `text_artifact`
- `image_artifact`
- `pdf_artifact`
- `clear_canvas`

## Run

```bash
python -m pip install -e .[test]
python -m pip install -e .[gui]   # optional, for canvas window
tabula --prompt "your task for codex"
```

Default behavior:
- bootstraps protocol files in the project
- launches canvas if display is available (unless `--no-canvas`)
- falls back to headless automatically when no display is found
- hands off to interactive `codex` in your current terminal

Useful options:
- `--project-dir <path>`
- `--mode project|global`
- `--prompt "..."` or positional prompt text
- `--headless`
- `--no-canvas`
- `--poll-ms 250`

## Bootstrap protocol files

Creates/updates:
- `AGENTS.md` protocol block for Codex
- `.tabula/prompt-injection.txt` for extra prompt injection
- `.tabula/artifacts/` artifact directory
- `.tabula/canvas-events.jsonl` bridge file
- `.gitignore` binary-artifact ignore patterns
- runs `git init` if `.git/` does not exist

```bash
tabula bootstrap --project-dir /path/to/project
```

## Example: markdown/pdf as user task (not built-in)

Use a normal prompt to ask Codex to do markdown/pdf work:

```bash
tabula --project-dir /path/to/project \
  --prompt "Create a short markdown note, render PDF, revise once, and emit canvas events."
```

## Validate events only

```bash
tabula check-events --events .tabula/canvas-events.jsonl
```

## Print schema

```bash
tabula schema
```

## Other commands

```bash
tabula canvas --events .tabula/canvas-events.jsonl
tabula bootstrap --project-dir .
tabula check-events --events .tabula/canvas-events.jsonl
tabula schema
```

## Tests

```bash
PYTHONPATH=src python -m pytest
```
