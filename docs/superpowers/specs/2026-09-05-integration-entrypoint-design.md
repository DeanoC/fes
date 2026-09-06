# FES integration entry point

Approved direction: retain independent components and make FES the starting
point for system integration and agents specialising in component work.

FES owns component selection, consistency checks, build orchestration and
integration evidence. FogCast owns the UI, host and network agent; the runtime
owns physical lifecycle; misteross owns FPGA builds; mister-packages owns shared
definitions. Main_MiSTer is a reference, not a production dependency.

Add a native integration profile selecting the merged development-loader and
source-bundle consumer, matching runtime and package definitions. Preserve the
two historical profiles with explicit historical source revisions. Gitlinks
select current component checkouts; historical profile revisions must exist in
those repositories and their runtime locks must agree. Builds stage clean,
standalone clones at the selected revisions.

Use the newer FogCast bundle interface for the integration profile. Keep the
lock overlay only for the historical source profile that requires it. Compare
every admitted bundle's recipe digest with its selected misteross recipe.

Provide one lightweight consistency command to check current gitlinks, runtime
lock, generated Go/C++ consumers and the copied core source pin. Run it in CI.
Agent instructions assign disjoint component worktrees and give the integrator
ownership of parent pins and shared build/hardware operations. Use a short
handoff containing revisions, changes, tests and remaining limitations.

Acceptance: parent regression tests; actual package regeneration comparison;
host compilation; two independent native image passes and packaging checks.
Hardware support remains scoped to dated exact-artifact evidence. Full native
bootable media assembly remains the next separate migration, after artifact
inputs are established; this change does not duplicate the child image builder.
