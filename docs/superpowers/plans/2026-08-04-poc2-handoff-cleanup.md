# FogCast POC2 Handoff Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (recommended) to execute this plan task-by-task.

**Goal:** Make the repository self-describing for a fresh agent taking over after POC2.

**Architecture:** Establish one canonical handoff document, then make the README and deployment runbook point to it and agree with the observed MiSTer/exFAT environment. Add a bounded POC3 roadmap without changing runtime scope.

**Tech Stack:** Markdown documentation, existing Go/Buildroot runbooks, shell commands already present in the repository.

## Global Constraints

- Keep POC1 historical records intact.
- Do not claim the interrupted-upload HIL gate passed; record it as the sole remaining acceptance limitation.
- Do not include credentials, bearer tokens, or private local paths containing secrets.
- Do not add runtime dependencies or alter the public API as part of documentation cleanup.

### Task 1: Canonical handoff

**Files:**
- Create: `docs/POC2-HANDOFF.md`

- [x] Record goals, architecture, exact commands, target assumptions, evidence, known limitations, and next-stage entry points.
- [x] Verify every command and path against the repository before finalizing.

### Task 2: Public README orientation

**Files:**
- Modify: `README.md`

- [x] Link the canonical handoff first.
- [x] Summarize truthful POC1/POC2 status and the narrow verification commands.

### Task 3: Deployment and recovery runbook

**Files:**
- Modify: `docs/runbooks/poc2-deploy.md`

- [x] Document exFAT publication compatibility and the safe development deployment path.
- [x] Document cache reset, tunnel/reboot sequencing, and rollback boundaries.

### Task 4: POC3 roadmap

**Files:**
- Create: `docs/POC3-ROADMAP.md`

- [x] Define the next stage as host library/productization work, with explicit exclusions for streaming and extra systems.
- [x] List measurable milestones and prerequisites for a fresh agent.

### Task 5: Verify and review

- [x] Search for contradictory POC2 status or stale names.
- [x] Run Markdown/link-oriented repository checks available locally.
- [x] Inspect `git diff` and `git status`; do not commit until the user explicitly requests it.
