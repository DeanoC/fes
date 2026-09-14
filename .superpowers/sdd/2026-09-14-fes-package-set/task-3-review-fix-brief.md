# Task 3 review-fix brief

Resolve the independent review findings against the final Task 3 state:

- reconcile current `docs/README.md`, `docs/component-boundaries.md`,
  `docs/project-map.md` and `docs/fes-zx81.md` with the ordered
  `fes.pong`, `fes.zx81`, `fes.coleco` package-only profile;
- extend the stale-contract regression assertion to cover those current guides;
- preserve historical validation wording where it is explicitly historical;
- do not alter package implementation, component sources, or the parent checkout.

The implementation must rerun the focused parent tests and `git diff --check`
before the final independent review.
