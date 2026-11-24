"""The DE25's i486DX CPU (#2135, plan docs/validation/2026-11-22-i486dx-core-plan).

The DX is fes_i386_cpu with the 387 on, the 486 identity, and round 2's cuts
(docs/validation/2026-11-23-i486dx-cuts): both L1s at 8 KiB 2-way, and no load
forwarding. It is DE25 only. Nothing here is read by a DE10 build: the DE10's
CPU closure is scripts/yosys_netlist.py's CPU_SOURCES, and this tree is
outside it, so the DE10 netlist and BUILD_ID do not move (the plan's "Fork
and sync").

Two kinds of difference from the integer core, as the plan sets out:
- **Forked** modules, rewritten: fes_i486dx_l1i.sv and fes_i486dx_l1d.sv, the
  L1s at two ways. Each names its upstream file, and FORKS pins the upstream
  bytes it was forked from, so an upstream change fails test_de25_i486dx.py
  until the fork is synced or the change is recorded as not applicable.
- **Derived**: fes_i486dx_cpu is fes_i386_cpu with anchored edits (CPU_EDITS):
  the module renamed so no tool sees two fes_i386_cpu modules, the two L1
  instances retargeted to the forks, and load forwarding off. An anchor that
  moves fails the build.

The decoder, the microcode, the x87 unit and every other CPU file are shared
verbatim. The 387 and the 486 identity are pc386_system's own top parameters
(X87_PRESENT, I486), set by the DE25 producer (../build.py).
"""
from pathlib import Path
import hashlib

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]  # sources/misteross
BOARD = "boards/de25nano/pc386/i486dx/"
I386 = "cores/fes-common/rtl/i386/"
TOP = "fes_i486dx_cpu"
UPSTREAM_TOP = "fes_i386_cpu"
CPU_UPSTREAM = I386 + "fes_i386_cpu.sv"
CPU_FILE = TOP + ".sv"

# Each fork: the upstream file, the fork, and the upstream's bytes when it was
# forked (at 499c3c3ea1f0, the last commit to touch either L1).
FORKS = {
    I386 + "fes_i386_l1i.sv": (BOARD + "fes_i486dx_l1i.sv",
                               "f839aab032854e8304a98ea6681ea4b3affaec5ba2fed552660a0010a18822e7"),
    I386 + "fes_i386_l1d.sv": (BOARD + "fes_i486dx_l1d.sv",
                               "6affddd2733a0bb1a957518fb46f030466abcbcf595ac462323ec5237111ab6b"),
}

LDFWD_BEFORE = (b"        d_out.ldfwd = u.cls == fes_i386_pkg::UOP_ALU && (u.op == 5'(fes_i386_pkg::ALU_MOV)"
                b" || u.op == 5'(fes_i386_pkg::ALU_MOVZX))\n"
                b"                   && mkind == fes_i386_pkg::MEM_LOAD && !u.mem.in_a"
                b" && u.size == fes_i386_pkg::SIZE_32 && !u.dst.hi8;\n")
LDFWD_AFTER = (b"        // i486DX: no load forwarding (#2146 decision 3: 360 ALMs for 1.2% of\n"
               b"        // DOOM's CPI), so no uop is fes_i386_dfwd's LOAD kind.\n"
               b"        d_out.ldfwd = 1'b0;\n")
HEADER_BEFORE = b"// SPDX-License-Identifier: MIT\n"
HEADER_AFTER = (b"// SPDX-License-Identifier: MIT\n"
                b"// GENERATED: fes_i486dx_cpu, the DE25's i486DX, is fes_i386_cpu with the edits\n"
                b"// boards/de25nano/pc386/i486dx/derive.py makes (the L1s at two ways, no load\n"
                b"// forwarding). Edit fes_i386_cpu.sv or derive.py, not this file.\n")
RENAME = (b"module fes_i386_cpu #(", b"module fes_i486dx_cpu #(", 1)
# (before, after, count in the upstream file).
CPU_EDITS = (
    (HEADER_BEFORE, HEADER_AFTER, 1),
    RENAME,
    (b"    fes_i386_l1i l1i (\n", b"    fes_i486dx_l1i l1i (\n", 1),
    (b"    fes_i386_l1d l1d (\n", b"    fes_i486dx_l1d l1d (\n", 1),
    (LDFWD_BEFORE, LDFWD_AFTER, 1),
)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def derive_cpu(source, *, rename=True):
    """fes_i486dx_cpu from fes_i386_cpu's bytes, and a record of the edits. With rename False, the
    benches' form: every edit but the module's name, so a bench that reads the CPU's hierarchy by
    fes_i386_cpu's name (lockstep_tb, machine_tb) runs the DX unchanged. The synthesis form is
    always renamed, since there the integer core's files are never read beside it."""
    result = source
    for before, after, count in CPU_EDITS if rename else tuple(edit for edit in CPU_EDITS if edit != RENAME):
        if source.count(before) != count:
            raise ValueError("fes_i386_cpu changed under an i486DX derivation anchor: " + before[:60].decode())
        result = result.replace(before, after)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "changes": [{"before": b.decode(), "after": a.decode(), "count": n} for b, a, n in CPU_EDITS]}


def check_forks(root=ROOT):
    """Each fork's upstream still has the bytes it was forked from."""
    stale = [upstream for upstream, (_, pinned) in FORKS.items() if sha((Path(root) / upstream).read_bytes()) != pinned]
    if stale:
        raise ValueError("i486DX forks behind their upstream (sync the fork, then its FORKS pin): " + ", ".join(stale))


def cpu_sources(cpu_sources, derived_cpu):
    """The DX's CPU sources in read order: cpu_sources (yosys_netlist.CPU_SOURCES,
    paths under sources/misteross) with each forked file and fes_i386_cpu replaced.
    derived_cpu is where fes_i486dx_cpu.sv was written."""
    swap = {upstream: fork for upstream, (fork, _) in FORKS.items()}
    swap[CPU_UPSTREAM] = str(derived_cpu)
    if not set(swap) <= set(cpu_sources):
        raise ValueError("the CPU source list no longer names every file the i486DX replaces")
    return tuple(swap.get(path, path) for path in cpu_sources)


def write_cpu(output, root=ROOT, *, rename=True):
    """Write fes_i486dx_cpu.sv into output; its path and the derivation's record."""
    check_forks(root)
    data, record = derive_cpu((Path(root) / CPU_UPSTREAM).read_bytes(), rename=rename)
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    (output / CPU_FILE).write_bytes(data)
    return output / CPU_FILE, record


def sim_sources(output, root=ROOT, *, rename=False):
    """For a bench: the DX's CPU sources as absolute paths, fes_i486dx_cpu.sv written into output, by
    default in the benches' form (derive_cpu rename False: the top keeps fes_i386_cpu's name)."""
    from scripts import yosys_netlist  # the caller has sources/misteross on its path
    cpu, _ = write_cpu(output, root, rename=rename)
    return tuple(Path(path) if Path(path).is_absolute() else Path(root) / path
                 for path in cpu_sources(yosys_netlist.CPU_SOURCES, cpu))
