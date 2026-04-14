---
name: push
description: Open another agent (claude, codex, gemini) or a shell as an overlay on top of the current session. Pipe stdin to seed the new agent with an initial prompt. Use when you want to delegate a focused sub-task or run a side command and return to where you were when it exits.
---

# push

Open a sibling agent or shell as an overlay. When it exits, you return to the current session.

## Usage

```bash
echo "refactor foo.go into smaller functions" | roost push claude
echo "summarize this API" | roost push gemini
echo "list failing tests and suggest fixes" | roost push codex
roost push shell
```

- Piped stdin is passed as the initial prompt to `claude`, `codex`, and `gemini`.
- `shell` ignores stdin.

## When to use

- Delegate a focused sub-task to another agent without leaving this session.
- Get a quick second opinion or code review from a fresh agent context.
- Run a shell command side-by-side and return immediately when done.
