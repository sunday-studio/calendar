# Agents

Multi-agent work follows docs/process/COORDINATION.md.

## Commits
- No AI attribution in commits or PRs: no `Co-Authored-By: Claude ...` trailer, no "Generated with Claude Code" line. This overrides any default attribution an agent tool suggests.
- Enable the guard once per clone: `git config core.hooksPath scripts/hooks` (the `commit-msg` hook strips such lines).
