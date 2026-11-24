#!/usr/bin/env python3
"""Which fes.pcxt-dos record files under cores/fes-pcxt/sim does the seal read?

Classifies every record file under cores/fes-pcxt/sim the way #1984 classified
pc386's record (docs/validation/2026-11-18-1984-pc386-identity-scope), for the
columns refuse_compiler_read() checks:

- yosys_read: the seal's DOS-package elaboration (quartus_fes_pcxt.elaboration
  with the package and DOS profiles), run with the PC/XT lock's Yosys under
  strace. Every regular file opened read-only under the source root.
- quartus_read: the generated project QSF names the file (VERILOG_FILE,
  SDC_FILE or any other path). Declared by the QSF, not traced: no Quartus run.
- inlined_in_qsf: its text appears in the generated QSF.
- generator_code: the seal's Python opens it while it generates compiler input
  (prepare_elaboration_inputs, the elaboration program, the project QSF and
  dos_assets), measured with an audit hook on open().
- proof_read: the two proofs the seal runs as subprocesses
  (tests/test_pcxt_video_mailbox.py, tests/test_pcxt_audio_cdc.py) open it,
  traced with strace.

Hashing a file into the record (capture_sources, source_input_closure) is not a
read for these columns: that is what keeps the file bound to the record.

No Quartus, GPU or kit. Writes OUT only, plus the producer's own scratch under
OUT. Usage, from the repository root:

  python3 -I docs/validation/2026-11-22-pcxt-sim-evidence/classify.py \\
      sources/misteross OUT /abs/path/to/yosys-3a5accbd/bin/yosys
"""
import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import tempfile
import time
from pathlib import Path

root = Path(sys.argv[1]).resolve()
out = Path(sys.argv[2]).resolve()
yosys = Path(sys.argv[3]).resolve()
out.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(root))
os.chdir(root)
from scripts import build_fes_pcxt_quartus as producer  # noqa: E402
from scripts import quartus_fes_pcxt as board  # noqa: E402

SIM = 'cores/fes-pcxt/sim/'
# Files a Verilator simulation alone consumes: lint configuration, testbench
# C++ and the testbench top modules.
CANDIDATE_SUFFIXES = ('.vlt', '.cpp', '.h', '.hpp', '.sv', '.v')


def record_inputs():
    repository, revision = producer._require_clean_source(root, identity_version=2)
    record = producer.create_build_record(
        root, repository, revision, {'quartus': 'selection-only', 'yosys': 'selection-only'},
        execution={'selection_only': True, 'gpu_device': 'lease', 'toolchain': 'quartus'})
    fields = producer._decode_build_record(record) if hasattr(producer, '_decode_build_record') else None
    if fields is None:
        from scripts.export_core_package import _decode_build_record
        fields = _decode_build_record(record)
    return revision, sorted(fields['source_inputs'])


def traced_opens(command, cwd, env=None, name='trace'):
    """Regular files under root that command opened read-only (strace)."""
    trace = Path(tempfile.mkdtemp(prefix=name + '-', dir=out))
    run = subprocess.run(['strace', '-f', '-ff', '-qq', '-e', 'trace=open,openat,openat2',
                          '-e', 'status=successful', '-y', '-o', str(trace / 'open'), *command],
                         cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    opened = set()
    for f in trace.glob('open.*'):
        for line in f.read_text(errors='replace').splitlines():
            m = re.search(r'= \d+<(.*)>$', line)
            if not m or 'O_WRONLY' in line or 'O_RDWR' in line or 'O_DIRECTORY' in line:
                continue
            p = Path(m.group(1))
            if p.is_relative_to(root) and p.is_file():
                opened.add(p.relative_to(root).as_posix())
    return run.returncode, run.stdout, opened


res = {'host': platform.node(), 'yosys': str(yosys), 'yosys_version': subprocess.check_output(
    [str(yosys), '-V'], text=True).strip(), 'yosys_sha256': hashlib.sha256(yosys.read_bytes()).hexdigest()}
res['revision'], inputs = record_inputs()
res['record_files'] = len(inputs)
sim_files = [p for p in inputs if p.startswith(SIM)]
candidates = [p for p in sim_files if p.endswith(CANDIDATE_SUFFIXES)]
res['sim_files_not_candidates'] = sorted(set(sim_files) - set(candidates))

# Python generation, with an audit hook on open().
generator_opened = set()


def audit(event, args):
    if event == 'open' and args and isinstance(args[0], (str, bytes, os.PathLike)):
        p = Path(os.fsdecode(args[0]))
        p = p if p.is_absolute() else Path.cwd() / p
        try:
            p = p.resolve()
        except OSError:
            return
        if p.is_relative_to(root) and p.is_file():
            generator_opened.add(p.relative_to(root).as_posix())


work = out / 'elaboration'
work.mkdir(exist_ok=True)
library = yosys.parent.parent / 'share/yosys/intel_alm/common/megafunction_bb.v'
sys.addaudithook(audit)
dos_inputs = board.prepare_elaboration_inputs(root, work, dos_profile=True)
program = board.elaboration(root, work, library, package_profile=True, build_id='0' * 32, dos_profile=True)
qsf = board.canonical_project_qsf(root, 1, dos_profile=True)
producer.dos_assets(root)
audit_done = set(generator_opened)
generator_opened.clear()
(work / 'elaborate.ys').write_text(program)
(out / 'top.qsf').write_text(qsf)

t = time.time()
rc, log, yosys_opened = traced_opens([str(yosys), '-q', '-l', str(out / 'yosys.log'), '-s', 'elaborate.ys'],
                                     work, name='yosys')
res['yosys_exit'], res['yosys_seconds'] = rc, round(time.time() - t)
res['netlist_sha256'] = hashlib.sha256((work / 'elaborated.v').read_bytes()).hexdigest() \
    if (work / 'elaborated.v').exists() else None

env = dict(os.environ, YOSYS=str(yosys), YOSYS_ABC=str(yosys.with_name('yosys-abc')))
proof_opened, res['proofs'] = set(), {}
for proof in ('tests/test_pcxt_video_mailbox.py', 'tests/test_pcxt_audio_cdc.py'):
    t = time.time()
    rc, log, opened = traced_opens([sys.executable, proof, '-v'], root, env, name=Path(proof).stem)
    res['proofs'][proof] = {'exit': rc, 'seconds': round(time.time() - t),
                            'summary': [l for l in log.splitlines() if l.startswith(('Ran ', 'OK', 'FAILED'))]}
    proof_opened |= opened

rows = []
for path in candidates:
    name = Path(path).name
    text = (root / path).read_text(errors='replace')
    rows.append({'path': path,
                 'yosys_read': path in yosys_opened,
                 'quartus_read': name in qsf,
                 'inlined_in_qsf': bool(text.strip()) and text.strip() in qsf,
                 'generator_code': path in audit_done,
                 'proof_read': path in proof_opened})
res['candidates'] = rows
res['evidence'] = [r['path'] for r in rows if not any(v for k, v in r.items() if k != 'path')]
res['dropped'] = [r for r in rows if any(v for k, v in r.items() if k != 'path')]
res['seal_reads_under_sim'] = {'yosys': sorted(p for p in yosys_opened if p.startswith(SIM)),
                               'generator': sorted(p for p in audit_done if p.startswith(SIM)),
                               'proofs': sorted(p for p in proof_opened if p.startswith(SIM))}
res['dos_inputs'] = dos_inputs
res['yosys_opened_under_root'] = sorted(yosys_opened)
res['generator_opened_under_root'] = sorted(audit_done)
text = json.dumps(res, indent=1) + '\n'
(out / 'classification.json').write_text(text)
print(json.dumps({k: res[k] for k in ('revision', 'record_files', 'yosys_exit', 'proofs', 'evidence',
                                      'dropped', 'seal_reads_under_sim', 'sim_files_not_candidates')}, indent=1))
