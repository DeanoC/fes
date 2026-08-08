# Documentation rules

Read the repository `AGENTS.md` first. Preserve document class and evidence
boundaries; do not silently rewrite history into a current claim.

## Authority and classification

- `ARCHITECTURE.md` and the active `ROADMAP.md` are normative current direction
  and migration gates, subject to accepted evidence and ADRs.
- `adr/` records accepted decisions; do not change a decision's meaning without
  an explicit replacement/supersession decision.
- `IDEA.md` is product vision, not an implementation or acceptance commitment.
- Results, handoffs, and POC evidence are historical evidence for their stated
  scope. Preserve hashes, limits, and evidence labels; new artifacts need new
  evidence.
- Older POC roadmaps are historical scope, not the active migration plan.
- Runbooks and development guides are current operations/recovery procedures;
  they do not authorize hardware mutation by themselves.

Use the roadmap vocabulary exactly: Designed, Software-tested, Reproducible,
HIL-observed, and Accepted. Identify commands, machine observations, operator
observations, and inferences separately. Do not claim controller symmetry,
latency, production readiness, portability, or hardware behavior beyond the
recorded evidence. Keep secrets out of docs, examples, manifests, and reports.

When adding links, use repository-relative links and verify local targets.
Architecture/migration edits that change contracts, ownership, security,
portability, rollback, or evidence gates require Sol escalation and independent
Vega review under the root instructions.
