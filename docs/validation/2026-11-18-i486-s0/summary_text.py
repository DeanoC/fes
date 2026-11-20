"""summary.md's text, from the reports beside it (summarize.py summary).

Every figure comes from a report file. The prose between them says what a
figure is of and what it does not show; process notes are in NOTES.
"""
from __future__ import annotations

NOTES = '''\
## How the runs departed from the method

- **`method.md` was amended three times;** its own text says what changed,
  and quotes Deano's answers. The third amendment followed Lander's review
  of `da4aee953`, and every figure for rules 1 to 5 here is from runs made
  after it. Its RTL stop list for the new table was measured just before the
  amendment's text was written, to produce the committed list; rule 4's
  pass rules did not change.
- **Lander's two fix commits were reviewed and partly taken.** Kept: the
  case table's full XADD, CMPXCHG and TR3-TR7 matrix, and the 486 CR0 and
  EFLAGS masks. Replaced: CR4 in the oracle's state (Deano then decided
  there is no CR4), probe and generator changes that ran only in modes the
  level-4 oracle refuses, new probes added to the level-3 list (which would
  have changed the level-3 case counts), and the census equivalence audit
  (the census was rerun instead).
- **The case table changed after its first runs,** in three ways, none to
  make a row pass: every row's source was reread against the two Intel
  documents and its citation corrected, which moved one row
  (`wp-set-supervisor-readonly`) from an Intel source to "Bochs" because
  the manual speaks only of user-level pages; and two rows (`wp-set-writable`,
  `ac-cpl0`) gained a read-back of the bit they set, because without it the
  RTL agreed with nothing to tell the bit was missing. The first runs' 48 of
  48 was on the earlier table; the figures here are the final table's.
- **Main was merged in after the first full set of runs** (the x87
  transcendentals, #1959, and the dev86 mirror, #1978; no conflicts). Rules
  1 to 4 and the boot were then run again, against the new merge base and
  at the merge commit; after the review they were run again (above). The
  CACHECHK runs were not repeated after the merge: its section says which
  commit it ran at. The pre-merge comparison (merge base `5e592f003`, head
  `75bd225a2`) also passed; it is in this file's history.
- **The level-3 lockstep at the first merge base was run twice.** The first had
  `--corpus` naming the corpus; the head run found the same corpus at its
  default path. The base was rerun the head's way so the arguments are the
  same, and that run is the one compared.
- **The first start of the merge-base runs was wrong and was discarded.**
  It lacked the toolchain cache variable, so its lockstep, test386 and
  vectors steps exited at once, but its boot step ran on, and a second
  start then booted in the same directory. Both were stopped and the three
  unfinished suites rerun once. The merge-base lockstep report is from the
  second start, which ran it alone.
- **`test_fes_i386_vectors.py` ran without the toolchain cache variable,**
  which it refuses, and with the corpus linked at its default path rather
  than by `--corpus`, which `test_fes_i386_test386.py`'s cross-check needs.
- **The census and the CACHECHK runs were made on chip-30x0-ai-dev,** from
  a `git archive` of the code commit, because Powerboat was loaded. The
  seven drives were copied there for the runs and deleted there, with the
  trees, when each census ended.
- **The census was run twice to the end.** The first, at `75bd225a2`
  before the main merge and the review, also gave 54 of 54 equal; it is in
  this file's history. Before it, three starts were discarded with nothing
  read: one from an uncommitted tree and one from the patch-route commit
  `f2027af82`, both stopped while building, and one from `75bd225a2` whose
  first runs exited at once because codeberg.org (dev86's source) did not
  resolve; for that run the pinned dev86 source was copied from Powerboat's
  checkout of the same revision. The census reported here is the second,
  run after the review at `7e813da46` with dev86 fetched as main now does;
  a start at `e9d9d8a2f` was stopped with nothing read when the CR4 gate
  moved from a patch file into the provisioner.
- **The CACHECHK trace used a scratch change to `machine_tb.cpp`** in a
  second copy of the tree, which keeps the last 3,000 oracle steps and
  prints them at the first exception 0. It is not committed. The census
  tree does not have it.
'''


DEPARTURES = '''\
## The plan's slice 0, and what was added

Every item of the plan record's slice 0 definition is built: the level-4
oracle beside level 3, the CR0 and EFLAGS masks of a 486 in the `i486`
mode, the #UD probes a 486 keeps run in that mode, the census keys, and the
machine's level option. For CR4, Deano decided after the plan that the 486
identity has no VME, so it has no CR4: MOV CR4 is #UD on the level-4 oracle
by the level-4 provisioner's edit to Bochs's opcode table, and the oracle's
state has no CR4.

Added beyond the plan: the adapter sets a 486's reset EDX and DR6, which
Bochs leaves as the 386's.
'''

NOT_RUN = '''\
## What was not run

- No seal, no D12 netlist and RBF comparison, and no kit.
- No netlist lockstep for the `i486` mode: it runs the RTL only.
- The CACHECHK trace was not repeated after main was merged or after the
  review; the census's own CACHECHK run at the reviewed commit is reported
  in its section.
- `wfw-msdos` and the three Linux drives were not run at level 4; the plan
  did not run them either.
- CACHECHK's timing loop was not traced, only the divide and its operands.
- Nothing ran with VME included, or on a 486 without CPUID.
- The other modes (corpus, random, protected-mode and paging programs) do
  not run against the level-4 oracle: the RTL has no 486 identity for them.
'''


def yes(value) -> str:
    return 'yes' if value else '**no**'


def carried(kind: str, commit: str, head: str, inputs_changed) -> str:
    """Whether a run made at commit stands for head: no input of it changed since."""
    if commit == head:
        return f'at the record\'s commit `{head[:9]}`'
    changed = inputs_changed(kind, commit, head)
    if changed:
        return (f'at `{commit[:9]}`, **before main was merged in: {len(changed)} files it reads differ at '
                f'`{head[:9]}`**, so it is a result of that commit and not of this one (the first of them: '
                + ', '.join(f'`{path.rsplit("/", 1)[-1]}`' for path in changed[:6]) + ')')
    return f'at `{commit[:9]}`; none of its inputs differs at `{head[:9]}`'


def render(read, inputs_changed, record: str) -> str:
    identity, level3, selfcheck = read('identity.json'), read('level3-compare.json'), read('oracle-selfcheck.json')
    stops, boot, census, cachechk = (read('expected-stops.json'), read('boot.json'), read('census-compare.json'),
                                     read('cachechk.json'))
    out: list[str] = []
    add = out.append
    add('# i486 slice 0: the Bochs oracle and the lockstep at CPU level 4, 18 November 2026\n')
    add('Generated by [`summarize.py`](summarize.py) from the report files in this directory; '
        'do not edit. The rules each result is held to are in [`method.md`](method.md), committed first. '
        'Tracker [#1962](https://github.com/DeanoC/fes/issues/1962), issue '
        '[#1968](https://github.com/DeanoC/fes/issues/1968). No CPU RTL changed, nothing was sealed and no kit '
        'was used.\n')

    # ---- result ----
    add('**Result.**')
    if identity:
        equal = sum(row['equal'] for row in identity['fields'].values())
        files = identity['files']
        add(f"- **Rule 1, the level-3 oracle's identity: {'pass' if identity['pass'] else 'FAIL'}.** "
            f"{equal} of {len(identity['fields'])} identity fields and {sum(row['equal'] for row in files.values())} of "
            f"{len(files)} files they cover are byte for byte the merge base's "
            f"(`{identity['base'][:9]}` against `{identity['head'][:9]}`); the fields that moved are "
            + ', '.join(f'`{name}`' for name, row in identity['fields'].items() if not row['equal'])
            + f", which Deano approved on 18 November 2026. {len(identity['pc386_record_paths_changed'])} of the "
            f"{identity['pc386_record_paths']} files pc386's build record hashes changed, so its `BUILD_ID` moves "
            f"with no RTL change; no seal or D12 comparison was run, under his exemption for this PR.")
    else:
        add('- **Rule 1: not measured.**')
    if level3:
        runs = level3['runs']
        add(f"- **Rule 2, level-3 behaviour: {'pass' if level3['pass'] else 'FAIL'}.** "
            + ', '.join(f"{name} {'equal' if row['equal'] else 'DIFFERENT'}" for name, row in runs.items()) + '.')
    else:
        add('- **Rule 2: not measured.**')
    if selfcheck:
        cases = selfcheck['cases']
        bochs = [row for row in cases if row['source'].startswith('Bochs')]
        reset = selfcheck['reset']
        add(f"- **Rule 3, the oracle's self-check: {sum(row['pass'] for row in cases)} of {len(cases)} cases and "
            f"{sum(row['pass'] for row in reset)} of {len(reset)} reset fields equal their rows.** "
            f"{len(cases) - len(bochs)} rows are held to an Intel document and {len(bochs)} record what Bochs does "
            f"where no document read says. {sum(1 for row in cases if row['divergence'])} rows are where Bochs is "
            f"known not to be a 486: they pin its behaviour and are not a 486's answer.")
    if stops:
        rows = stops['cases']
        agree = [row for row in rows if row['rtl'] == 'agree']
        probes = [row for row in rows if row['label'].startswith('ud-')]
        add(f"- **Rule 4, the RTL's stops: {len(rows) - len(probes)} cases and {len(probes)} #UD probes; "
            f"{len(rows) - len(agree)} stop and {len(agree)} agree; {sum(row['isolated'] for row in rows)} of "
            f"{len(rows)} are isolated** (agree, or first mismatch at or after the probe). The #UD probes that agree: "
            f"{sum(row['rtl'] == 'agree' for row in probes)} of {len(probes)}. Two runs gave the same list: "
            f"{yes(stops['runs_equal'])}. {sum(row['as_predicted'] is False for row in rows)} cases differ from "
            f"`method.md`'s prediction.")
    if boot:
        end = boot['end'] or {}
        add(f"- **Rule 5, the level-4 boot: {'pass' if boot['pass'] else 'FAIL'}.** FreeDOS ends at "
            f"`{end.get('reason')}` after {end.get('steps'):,} steps and {end.get('seconds')} simulated seconds; "
            f"{boot['frames_matched']} of {boot['frames_compared']} compared frames match.")
    else:
        add('- **Rule 5, the level-4 boot: not run.**')
    if census:
        add(f"- **Rule 5, the census: {'pass' if census['pass'] else 'not passed'}.** {census['rows_run']} of "
            f"{census['plan_rows']} of the plan's rows were run again, "
            f"{carried('machine', census['commit'], record, inputs_changed)}; {census['rows_equal']} equal the plan's counts"
            + (f"; {len(census['not_run'])} not run" if census['not_run'] else '') + '.')
    else:
        add('- **Rule 5, the census: still running when this was generated; no row is reported.**')
    if cachechk:
        add(f"- **Rule 6, CACHECHK's \"Divide error\": {cachechk['result']}.** {cachechk['one_line']}")
    else:
        add('- **Rule 6, CACHECHK: its runs were still going when this was generated; nothing is reported.**')
    add('')

    # ---- rule 1 ----
    if identity:
        add('## Rule 1: the level-3 oracle\'s identity\n')
        add("`oracle_identity()`'s fields in `build_fes_pc386_quartus.py`, and the instrumentation header, from the "
            'files as committed at each revision:\n')
        add('| Field | Merge base | Head | Equal |\n| --- | --- | --- | --- |')
        for name, row in identity['fields'].items():
            add(f"| `{name}` | `{row['base'][:16]}` | `{row['head'][:16]}` | {yes(row['equal'])} |")
        add('\nThe files those digests cover:\n')
        add('| File | Equal |\n| --- | --- |')
        for name, row in identity['files'].items():
            add(f"| `{name}` | {yes(row['equal'])} |")
        changed = identity['pc386_record_paths_changed']
        add(f"\npc386's build record hashes its producer's pinned inputs and that producer's import closure: "
            f"{identity['pc386_record_paths']} files. Of the {len(identity['files_changed'])} files this change "
            f"touches, {len(changed)} are among them" + (': ' + ', '.join(f'`{path}`' for path in changed) if changed else '')
            + '. So the record, and the `BUILD_ID` made from it, move, with no RTL change. AGENTS.md\'s D12 rule '
            'asks for a reseal or a netlist and RBF comparison in that case. **Neither was run:** Deano exempted '
            'this PR on 18 November 2026, while pc386 is in progress (`method.md` quotes him).\n')
        add('What this shows: the level-3 Bochs tree is built from the inputs it had, and only the two approved '
            'files move the seal\'s oracle and suite identity. '
            'What it does not show: that the level-4 build cannot disturb a level-3 build on the same host. '
            'That is by construction (a separate tree prefix and output directory; '
            '`tests/test_fes_i486_oracle.py` checks the names against the level-3 sweeps) and was not measured.\n')

    # ---- rule 2 ----
    if level3:
        add('## Rule 2: level-3 behaviour\n')
        runs = level3['runs']
        lockstep = runs['lockstep']
        add(f"Each suite ran in a clean checkout of the merge base (`{level3['commits']['base'][:9]}`) and of this "
            f"branch, {carried('level3', level3['commits']['head'], record, inputs_changed)} (the boot: "
            f"{carried('machine', level3['commits']['head'], record, inputs_changed)}). Same arguments on both sides. "
            f"Exit statuses: base {level3['exit_status']['base']}, head {level3['exit_status']['head']}.\n")
        add(f"**Lockstep** (default suite: {yes(lockstep['default_suite'])}; tracked changes in either checkout: "
            f"{lockstep['tracked_changes']}). Reports equal but for the ignored keys: {yes(lockstep['equal'])}.\n")
        add('| Suite | Runs | Passed | Failed | Trace hashes | Base equals head |\n| --- | ---: | ---: | ---: | --- | --- |')
        for name, head in lockstep['suites']['head'].items():
            base = lockstep['suites']['base'].get(name)
            hashes = head.get('trace_hash') or (f"{head['trace_hashes']} hashes" if 'trace_hashes' in head else 'none')
            add(f"| {name} | {head.get('runs', '')} | {head.get('passed', '')} | {head.get('failed', '')} | "
                f"`{hashes}` | {yes(base == head)} |")
        add('')
        if lockstep['differences']:
            add('Differences:\n')
            out.extend(f'- `{item}`' for item in lockstep['differences'][:40])
            add('')
        vectors, boot3, test386 = runs['vectors'], runs['boot'], runs['test386']
        add(f"**SingleStepTests vectors.** Base {vectors['totals']['base']}, head {vectors['totals']['head']}; "
            f"reports equal: {yes(vectors['equal'])}.\n")
        add(f"**test386.** The runner's {len(test386['head']['lines'])} result lines, the dump's SHA-256 "
            f"(`{test386['head']['dump_sha256'][:16]}`) and the output text's (`{test386['head']['output_sha256'][:16]}`) "
            f"equal the base's: {yes(test386['equal'])}.\n")
        add(f"**Boot.** Base ends {boot3['end']['base']}, head {boot3['end']['head']}; replay base "
            f"`{boot3['replay']['base']}`, head `{boot3['replay']['head']}`; reports equal: {yes(boot3['equal'])}.\n")
        for name in ('vectors', 'boot'):
            if runs[name]['differences']:
                add(f'Differences in {name}:\n')
                out.extend(f'- `{item}`' for item in runs[name]['differences'][:40])
                add('')
        add(f"The comparison ignores these keys wherever they occur: {', '.join('`' + key + '`' for key in level3['ignored_keys'])}. "
            'They are the checkout\'s revision and source list, digests of the oracle and suite identity (rule 1) '
            'and of binaries built from them, and paths.\n')

    # ---- rule 3 ----
    if selfcheck:
        add('## Rule 3: what the level-4 oracle does\n')
        oracle = selfcheck['oracle']
        add(f"Run {carried('suite', selfcheck['commit'], record, inputs_changed)}. "
            f"The oracle: Bochs `{oracle['revision'][:12]}` at CPU level {oracle['cpu_level']}; level-4 provisioner "
            f"`{oracle['provisioner_sha256'][:16]}`, adapter "
            f"sources `{oracle['adapter_sha256'][:16]}`; cases `{selfcheck['cases_sha256'][:16]}`.\n")
        add('**After reset:**\n')
        add('| Field | A 486 | The oracle | Source | Equal |\n| --- | --- | --- | --- | --- |')
        for row in selfcheck['reset']:
            add(f"| {row['field']} | {row['expected']} | {row['oracle']} | {row['source']} | {yes(row['pass'])} |")
        add('\n**The cases.** Each program ran on the oracle alone. "Expected" is the row written before the run; '
            'every field in it was compared.\n')
        add('| Case | What it checks | Expected | Source | Oracle equals it |\n| --- | --- | --- | --- | --- |')
        for row in selfcheck['cases']:
            expected = ', '.join(f'{name} {value}' for name, value in row['expected'].items())
            add(f"| `{row['label']}` | {row['what']} | {expected} | {row['source']} | {yes(row['pass'])} |")
        add('')
        divergent = [row for row in selfcheck['cases'] if row['divergence']]
        add('**Where Bochs\'s level 4 is not a 486.** These rows pin what Bochs does, so that the difference cannot '
            'move unseen; the right-hand column is what a 486 does.\n')
        add('| Case | Bochs | A 486 |\n| --- | --- | --- |')
        for row in divergent:
            expected = ', '.join(f'{name} {value}' for name, value in row['expected'].items())
            add(f"| `{row['label']}` | {expected} | {row['i486']} |")
        add('\nThe whole table of differences, with how each is handled, is in `method.md` and in the oracle '
            '[README](../../../sources/misteross/cores/fes-common/sim/i386/README.md#the-level-4-oracle). '
            'Reset EDX and DR6 are adapted in the adapter and so appear above as equal; the others are not checked '
            'against the oracle.\n')
        add('What this shows: on these programs the oracle gives the documented result. What it does not: that '
            'Bochs is right on anything the cases do not exercise, or on behaviour no document read settles '
            '(the undefined flags, for one). The rows marked "Bochs" show only what the oracle does.\n')

    # ---- rule 4 ----
    if stops:
        add('## Rule 4: where the RTL stops today\n')
        add(f"Run {carried('suite', stops['commit'], record, inputs_changed)}. "
            'The RTL is a 386. Each case ran in lockstep with the level-4 oracle to its end or its first mismatch. '
            '"Probe" is the address of the first instruction that needs a 486; a stop is written as the testbench '
            'prints it, `@<eip>.<field> <oracle> <rtl>`.\n')
        add('| Case | Probe | RTL | Isolated | Predicted | As predicted |\n| --- | --- | --- | --- | --- | --- |')
        for row in stops['cases']:
            add(f"| `{row['label']}` | {row['probe'] or 'none'} | `{row['rtl']}` | {yes(row['isolated'])} | "
                f"{row['predicted'] or 'not predicted'} | "
                f"{'n/a' if row['as_predicted'] is None else yes(row['as_predicted'])} |")
        add('')
        kinds: dict[str, int] = {}
        for row in stops['cases']:
            if row['rtl'] != 'agree':
                field = row['rtl'].split(' ')[1].split('.', 1)[1].split('.')[0]
                kinds[field] = kinds.get(field, 0) + 1
        add('Stops by the first mismatching field: ' + ', '.join(f'{name} {count}' for name, count in sorted(kinds.items())) + '.\n')
        wrong = [row for row in stops['cases'] if row['as_predicted'] is False]
        if wrong:
            no_probe = [row['label'] for row in wrong if row['probe'] is None]
            with_probe = [row['label'] for row in wrong if row['probe'] is not None]
            add(f"**The prediction was wrong for {len(wrong)} cases,** each of which "
                f"{'agrees' if all(row['rtl'] == 'agree' for row in wrong) else 'differs'} where the other was "
                f"predicted. {len(no_probe)} have no probe, so the 486's answer is the 386's and the prediction "
                f"should have listed them" + (': ' + ', '.join(f'`{label}`' for label in no_probe) if no_probe else '')
                + f". {len(with_probe)} have a probe and agree all the same"
                + (': ' + ', '.join(f'`{label}`' for label in with_probe) if with_probe else '')
                + ('; these set a 486 bit whose only effect in the program is a fault that the case is there to '
                   'show does not happen, so a 386 runs them to the same result' if with_probe else '')
                + '. That is a finding about the prediction; the rule is that no case stops before its probe.\n')
        add(f"Failures the runner reported: {stops['failures'] or 'none'}.\n")
        add('The list is committed as '
            '[`i486-expected-stops.txt`](../../../sources/misteross/cores/fes-common/sim/i386/i486-expected-stops.txt), '
            'and `scripts/test_fes_i486_lockstep.py` fails when a run differs from it.\n')
        inputs = stops['machine_reset_inputs']
        add('What this does not show: the reset values. The CPU takes its reset state from inputs the machine '
            'drives, so the lockstep starts every case from a state the bench loads. At this commit '
            '`pc386_system.sv` drives ' + ', '.join(f'`{name}` with `{value}`' for name, value in inputs.items())
            + ', the 386\'s; the oracle\'s are in the table under rule 3. Slice 2 changes them with the identity.\n')

    # ---- rule 5 ----
    add('## Rule 5: the machine on the level-4 oracle\n')
    if boot:
        add(f"**Boot.** `sim_fes_pc386.py --suite boot --cpu-level 4`, {carried('machine', boot['commit'], record, inputs_changed)}: "
            f"end {boot['end']}, exit {boot['exit']}, "
            f"failures {boot['failures'] or 'none'}; VGA replay `{boot['replay_summary']}`.\n")
    else:
        add('**Boot.** Not run.\n')
    if census:
        add(f"**Census.** Run on {census['host']}, {carried('machine', census['commit'], record, inputs_changed)}. "
            f"The plan's `census.jsonl` at `{census['plan_revision'][:9]}` has {census['plan_rows']} rows. "
            f"{census['rows_run']} were run again on this slice's level-4 oracle; compared keys: "
            f"{', '.join('`' + key + '`' for key in census['compared_keys'])}.\n")
        different = [row for row in census['rows'] if row.get('run') and not row.get('equal')]
        add(f"- Equal to the plan's row: {census['rows_equal']}.")
        add(f"- Different: {len(different)}." + ('' if not different else ' Each, with both values:'))
        for row in different:
            add(f"  - `{row['id']}`: " + '; '.join(f"{key}: plan {value['plan']}, this slice {value['slice0']}"
                                                   for key, value in row['differences'].items()))
        add(f"- Not run: {len(census['not_run'])}" + (': ' + ', '.join(f'`{name}`' for name in census['not_run']) if census['not_run'] else '') + '.')
        nonzero = sorted(name for name, status in census['exit_status'].items() if not status.endswith('rc=0'))
        add(f"- Runs whose bench exited non-zero: {len(nonzero)}" + (': ' + ', '.join(f'`{name}`' for name in nonzero)
            + '. Their rows are counted. The plan record has the same for `chkcpu`: it prints and exits, so the '
              'game check sees no screen progress' if nonzero else '') + '.')
        add('\nThe rows are in [`census.jsonl`](census.jsonl): counts only.\n')
    else:
        add('**Census.** Still running when this was generated.\n')

    # ---- rule 6 ----
    add('## Rule 6: CACHECHK\'s "Divide error"\n')
    if cachechk:
        add(f"**{cachechk['result'].capitalize()}.** Run on {cachechk['host']}, "
            f"{carried('machine', cachechk['commit'], record, inputs_changed)}.\n")
        for line in cachechk['findings']:
            add(f'- {line}')
        add('')
    else:
        add('Its runs were still going when this was generated.\n')

    add(DEPARTURES)
    add(NOT_RUN)
    add(NOTES)
    return '\n'.join(out)
