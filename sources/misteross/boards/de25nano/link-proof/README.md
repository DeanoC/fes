# DE25 base-plus-persona composition proof

This isolated diagnostic tests a Quartus-only alternative to FES's Cyclone V
nextpnr CRAM linker. Quartus builds one fixed base and two interchangeable PR
personas for `A5EB013BB23BE4SCS`. The standalone Go target executable validates
the selected shell and ordinary artifact files, then assembles a deterministic
deployment tar containing the base, one persona and their load order. The
result is a **base-plus-persona bundle**, not a merged RBF.

The proof belongs to misteross. It adds no production expansion ABI, core
package, FogCast orchestration or runtime programming backend. No command
loads hardware, accesses MMIO, changes an image or writes flash. Physical PR
loading, CPU control, configuration recovery and exact-artifact board acceptance
remain separate work for the arriving DE25.

The tiny base owns the verified board clock and LED pads, reset release and
static state. One registered request/response socket sits in a reserved
placement/routing region. Personas A and B have different IDs, counter steps
and 256-word M20K ROM contents. The shell stops requests, drains outstanding
responses, holds local reset and clamps outputs while frozen. Its freeze input
is a virtual diagnostic pin; there is no HPS control path. The transition bench
uses the real shell and persona RTL with an explicit mux, so it proves logical
sequencing rather than physical reconfiguration behavior.

Run from this directory, choosing fresh output directories:

```sh
make test
make sim OUTPUT=/new/transition-simulation
make project OUTPUT=/new/pr-project
make build OUTPUT=/new/pr-build QUARTUS_ROOTDIR=/data/altera_pro/26.3
make proof ARTIFACTS=/new/pr-build/artifacts.json OUTPUT=/new/cpu-proof \
  QEMU=/absolute/path/to/qemu-aarch64-static
```

The build uses Quartus Pro 26.3.0 Build 114, the exact SCS part, seed 1 and two
CPU workers. Final builds require committed functional inputs. They retain the
source closure, generated project, IP generation and compile logs, reports,
final root QDB and opaque vendor bitstreams. Both implementation revisions
import the same root partition. PR compatibility checking remains enabled;
the producer checks the imported root QDB and vendor-decoded POF0 region/hash
metadata before emitting the target descriptor. Its compatibility label is
`pfg-info-pof0-match`; the retained `quartus_pfg --compare` warning about absent
owner-root-key fields does not establish additional compatibility. External LEDs and virtual freeze control have
explicit timing classifications; this diagnostic does not establish overall
board timing or electrical acceptance.

The target descriptor binds exact tool, source, root QDB, interface, clock/reset,
region and payload identities. These hashes identify compiler-produced inputs;
they do not decode an Agilex bitstream or independently prove its physical
compatibility. The caller supplies the selected shell ID, base digest and exact
descriptor digest from the trusted producer context. The output directory must
be owned by the invoking user with mode `0700`:

```sh
./compose-aarch64 compose --artifacts /path/to/artifacts.json \
  --expected-shell-id SHELL_SHA256 --expected-base-sha256 BASE_SHA256 \
  --expected-descriptor-sha256 DESCRIPTOR_SHA256 \
  --persona counter_a --output /new/deployment.tar
```

The closed diagnostic schema accepts one of two personas for one region. It
rejects unknown fields, duplicate keys/selections, incompatible bindings,
truncated or altered payloads and unsafe file paths. Existing outputs are
preserved; failed compositions publish no output. Every bundle includes
`deployment.json`, `artifacts.json`, `base.rbf` and `slot0/persona.rbf` in that
order. The plan loads the base before the persona. A future hardware backend
must implement that lifecycle and its freeze/drain/reset contract.

`proof.py` builds static native and Linux AArch64 executables using the local
Go toolchain. It executes both persona selections twice on each architecture,
using QEMU user mode with Cortex-A76 for AArch64. It independently checks the
tar, exact payload bytes, canonical plan and composition identity, requires
native/AArch64 byte equality, and exercises wrong shell/base, foreign root,
replaced descriptor, corrupt persona and overlapping selection rejection on both CPUs. `proof.json`
records tools, executables, source hashes and results. QEMU execution is
software evidence; execution on the physical kit CPU remains untested.
