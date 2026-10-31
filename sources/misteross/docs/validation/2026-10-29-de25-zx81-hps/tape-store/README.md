The board-local tape store preserves the portable module's arbitrary address
and byte-enable behavior, one-cycle old-data read, and high-byte suppression
at `0x3fff`. Adjacent byte writes select opposite even/odd banks. Each bank
uses an explicit common-clock simple-dual-port M20K with mixed-port `OLD_DATA`.
The ordinary simulation branch contains the complete portable source verbatim.

The actual supplied `altera_mf.v` differential passed 155660 cycles and 147468
defined-byte comparisons against both the portable source and the board's
behavioral branch. It covers every address with enables01/10/11, odd writes,
low/high collisions, terminal suppression, randomized inputs and two complete
16KiB readbacks. Unwritten portable bytes remain undefined and are excluded
from equality checks. The supplied generic model logs an `AUTO` fallback for
the unrecognized Agilex5 family; its passing result establishes functional
behavior, while the separate synthesis supplies the mapping evidence.

`synthesis-02/` retains the successful Quartus26.3 isolated synthesis: 59ALMs,
five logic registers, zero MLAB bits, and 131072 block-memory bits in two
8192x8 SimpleDualPort M20K instances. Its report retains `OLD_DATA`. The
standalone project omits board reset-release IP and reports warning20759;
it is a memory-mapping diagnostic, not a programmable board image or timing
acceptance. Root ran this synthesis serially; the implementation agent ran
only the host simulation and prepared the project.

`synthesis-failed-01/` retains the rejected first project: error272006 for an
unused `clock1` tied low. The accepted source ties it to its default high,
matching the existing board RAM convention. These records supplement the
earlier failed full-board producer; they do not replace its generated audit.

From the task worktree root, using the actual vendor model and Icarus12:

```sh
python3 -B sources/misteross/boards/de25nano/zx81-hps/test_tape_store.py simulate \
  --vendor-model /data/altera_pro/26.3/ip/altera/subsystems/hssi_ss/example_project/common_f/altera_mf.v
python3 -B sources/misteross/boards/de25nano/zx81-hps/test_tape_store.py project \
  --output out/de25-tape-store-replay
```

The runner also accepts explicit `--iverilog`, `--iverilog-base` and `--vvp`
paths for a private extraction. `toolchain.json` records the Debian package
and executable hashes; `vendor-model/receipt.json` records the exact command,
model hash, source hashes and warnings. Run the prepared project's synthesis
command from its `README.txt` only when Quartus is available for serial use.
`SHA256SUMS` binds the retained files.
