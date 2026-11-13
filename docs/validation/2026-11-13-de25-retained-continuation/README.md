# Retained native configuration continuation prototype

The preceding [format-11 hardware trial](../2026-11-13-de25-command-completion-physical/README.md)
qualifies retained-source retirement for its selected artifact. It deliberately
parks before native configuration completion. Format 12 connects a successful
retirement to the existing native return path; this is a new software prototype,
without matched-image or hardware acceptance yet.

`nano_source_continue` consumes one attempt on the private retired engine and
checks its actual source binding, trusted callback table, completed command
receipt and disabled controller state. It accepts no copied retirement receipt
as authority. Fresh ownership/callback checks bracket one complete retained
table validation and two 24-register final views. A final monotonic counter and
owner check remain under the unchanged original one-second retirement origin;
the source supplier also enforces its original 20-second bound. No write,
command store, bypass, reset, buffer release or activation callback is invoked.
Retirement's original observations remain separate from continuation clocks.

Only successful live continuation returns from `fes_nano_sdm_source_finish`.
The existing dispatcher then runs `fes_nano_configuration_after`, including the
full source check and configuration-completion ownership checks. An informational
`FES_NANO_INPUT_CONTINUE` record establishes return readiness only. Failed
retirement/continuation admission parks with retained resources and no retry;
its stop record emits existing saved error observations without extra MMIO.
Failures in the subsequent native configuration checks follow their existing
error path and block activation. Continuation readiness cannot stand in for
native completion, endpoint identity or a visible menu.

The explicit SDK `--retain-configuration` option selects
`NewRetainedProgramArena`. Its first source and metadata remain held after
successful completion; another program reservation or initial-tape publication
rejects before writes, barriers or firmware calls. The selection propagates
through SDK, entry and cold-profile identity. Format 12 rejects an unretained
arena and all previous contracts. Other profiles retain their existing arena
selection; Linux build/image defaults and shared hardware ABI are unchanged.

The [focused checks](checks.json) cover live callback/source/generation changes,
failed/partial retirement, consumed replay, read failures, backward/expired
clocks, actual joined vendor transfer and native configuration completion,
changed native source/owner/completion callbacks, and strict wire records.
A continuation record cannot promote failed source or retirement, and Park-only,
partial, duplicate and reordered records cannot claim continuation. The observer
always reports native activation and memory reuse as unqualified.

The full native fabric race suite passes, including retained source/metadata
preservation after successful and failed completion and rejected tape reuse.
Six portable AArch64 units compile with strict pinned flags; the largest
individual frame is 384 bytes. That is not a linked BL31 or whole EL3 stack
proof. Repository consistency verifies 69 consumers and 45 fixture copies.
Initial development failures remain in separate logs: a copied cost-mode macro
in two non-cost fixtures, a continuation fixture timestamp earlier than its
retirement, and phase-6 cost accounting that previously included a post-Park
retry probe. The corrected accounting keeps the full final table scan and
compares unchanged earlier phases; no firmware check is removed for a test.

The [source manifest](source-manifest.json) binds these implementation bytes.
Full affected testing and a fresh matched SDK/entry/BL31/SPL/U-Boot/composition
build are next. Physical continuation must separately establish normal native
completion and the selected menu/endpoint before it can count as a hardware
pass. The latest verified kit state remains the original baseline at final
Park from the format-11 trial; this work makes no board/device/card changes.

This one-generation prototype retains all memory. Repeatable core loading,
independent interconnect drain, safe arena reuse, broader devices/packages and
DE10-equivalent normal appliance behavior remain open. The active goal is not
complete.
