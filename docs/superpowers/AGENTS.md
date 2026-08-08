# Design records and plans

Read `../../AGENTS.md` and `../AGENTS.md` first. Treat a design/specification
as current only when its status and the canonical `ARCHITECTURE.md`, `ROADMAP.md`,
and relevant ADR say it is current. Older specs, plans, task briefs, checklists,
and handoffs are historical context/evidence unless explicitly adopted.

Never blindly execute an old checklist. First confirm scope, authority,
dependencies, safety gates, and whether its evidence/status remains applicable.
Do not convert a planned action into an Accepted claim, or use software evidence
as HIL evidence. Preserve provenance and links when amending historical records;
write a new dated decision or plan for new work instead of silently changing an
old commitment.

Plans must name owners, disjoint writable files/worktrees, verification,
review, rollback implications, and a next safe action. Architecture, lifecycle,
hardware, security, portability, protocol, or evidence-boundary changes require
Sol escalation and independent Vega review. No plan authorizes commit, push,
deployment, or hardware mutation.
