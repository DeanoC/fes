# Canonical libmister-runtime Repository Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Create an independently buildable `libmister-runtime` repository with one lifecycle API, the useful native implementation, one four-operation local daemon, preserved source history, and an honest software-only baseline.

**Architecture:** Extract the useful lifecycle and native-hardware lineage from `Main_MiSTer` commit `346ba9f`, preserve it in the new repository's Git history, then reduce the active implementation to one lifecycle plus narrow FPGA-manager, MMIO, SPI, and core-loader primitives. The daemon is a thin newline-JSON transport over that lifecycle. Production construction stays unavailable until the later bootable-native milestone.

**Tech Stack:** C++14, POSIX Unix sockets, GNU Make, pthreads, host GCC/Clang sanitizers, Arm GNU 10.2 cross-toolchain, Git, GitHub CLI

**Spec:** `../specs/2026-08-31-libmister-runtime-design.md`

## Global constraints

- This plan implements migration milestone 1 only: the canonical standalone repository and software baseline.
- Do not modify FogCast, Main_MiSTer, misteross, target images, init scripts, or the designated Pi.
- The source lineage is `Main_MiSTer` commit `346ba9f6c6d00fee446b52659229cded9bf16d8c`.
- Preserve useful authorship and file evolution through filtered Git history; do not import the Main application tree.
- The active tree has one library name, `libmister-runtime.a`, and one executable name, `mister-runtime`.
- The active tree has no Stage C0, POC, `fogcast-runtime`, native personality, ABI-v1, public V2, historic `HardwareBroker`, coordinator, fence, replay, ownership database, or compatibility-Main path.
- The daemon protocol is local-only protocol 1 over `/run/mister-runtime.sock`, with one newline-terminated JSON request and response per connection.
- The only operations are `status`, `launch`, `load_development_rbf`, and `stop`.
- There is no authentication, TLS, request ledger, replay protection, persistence, automatic retry, Main fallback, or session preservation.
- The production profile registry is empty and production native construction is explicitly unavailable in this milestone.
- Documentation must state **software-tested only** and **zero hardware-supported systems**.
- Use GPL-3.0-or-later for new files. Preserve a source file's stricter notice if a later milestone ports it.
- Keep C++14 and GNU Make for the extraction. Do not combine this work with a language/build-system modernization.
- Use test-first semantic changes and one focused commit per task.

## Scope and source finding

The experimental tip is not a passing or production-ready baseline. Its production profile lookup returns no profiles, production native construction returns unsupported, and its old daemon requires a durable coordinator, operation ledger, backend fence, ownership epochs, replay tracking, and peer-policy machinery rejected by the approved design. A clean archive of `346ba9f` also fails its aggregate host build because the old daemon protocol header uses `uint64_t` without including its definition.

The useful part is the implementation knowledge: FPGA programming, MMIO addresses, SPI/core command order, bounded transfer behavior, and the final callback-deadline fix. Milestone 1 rewrites that knowledge behind narrow interfaces. Broker leases, typed authority views, containment/recovery frameworks, A/V, input, saves, offload/scheduler adapters, the experimental `fogcast/` daemon, legacy implementations, obsolete `mister_runtime_linux_v2`, and synthetic production authority remain historical only.

The later bootable-native plan owns production construction, idle-RBF packaging, image integration, and physical-Pi acceptance. The later first-system plan owns the first real profile and end-to-end game evidence. This plan must not imply either has happened.

## Final active layout

~~~text
libmister-runtime/
├── .gitignore
├── AGENTS.md
├── ARCHITECTURE.md
├── DEVELOPMENT.md
├── LICENSE
├── Makefile
├── README.md
├── docs/
│   ├── design/2026-08-31-native-runtime-design.md
│   ├── provenance/main-mister-346ba9f.sha256
│   └── support-matrix.md
├── include/libmister-runtime/runtime.h
├── scripts/
│   ├── check-active-tree.sh
│   └── check-history.sh
├── src/
│   ├── profile.cpp
│   ├── runtime.cpp
│   ├── native/
│   │   ├── artifacts.cpp
│   │   ├── artifacts.hpp
│   │   ├── core_loader.cpp
│   │   ├── core_loader.hpp
│   │   ├── hardware.cpp
│   │   ├── hardware.hpp
│   │   └── linux/
│   │       ├── fpga_manager.cpp
│   │       ├── fpga_manager.hpp
│   │       ├── mmio.cpp
│   │       ├── mmio.hpp
│   │       ├── spi.cpp
│   │       └── spi.hpp
│   ├── daemon/
│   │   ├── controller.cpp
│   │   ├── controller.hpp
│   │   ├── json.cpp
│   │   ├── json.hpp
│   │   ├── main.cpp
│   │   ├── protocol.cpp
│   │   ├── protocol.hpp
│   │   ├── server.cpp
│   │   └── server.hpp
│   └── linux/
│       ├── production_hardware.cpp
│       ├── production_hardware.hpp
│       ├── stderr_log.cpp
│       └── stderr_log.hpp
└── tests/
    ├── active_tree_test.sh
    ├── fixtures/protocol-v1.jsonl
    ├── integration/daemon_server_test.cpp
    ├── support/
    │   ├── fake_hardware.cpp
    │   ├── fake_hardware.hpp
    │   ├── fake_mmio.cpp
    │   ├── fake_mmio.hpp
    │   ├── fake_spi.cpp
    │   ├── fake_spi.hpp
    │   ├── capture_log.cpp
    │   ├── capture_log.hpp
    │   └── test_profiles.hpp
    └── unit/
        ├── artifacts_test.cpp
        ├── core_loader_test.cpp
        ├── fpga_manager_test.cpp
        ├── mmio_test.cpp
        ├── native_hardware_test.cpp
        ├── profile_test.cpp
        ├── protocol_test.cpp
        ├── runtime_test.cpp
        └── spi_test.cpp
~~~

The boundaries are fixed:

- `include/libmister-runtime/runtime.h` is the sole lifecycle, request, status, profile, and hardware-interface contract.
- `src/profile.cpp` validates profiles and converts semantic media/settings into prepared native requests.
- `src/runtime.cpp` owns five states, one-operation admission, one cleanup attempt, and in-memory status.
- `src/native/**` owns only the narrow hardware primitives needed by the next milestone; it does not own public launch policy, recovery authority, or wire protocol.
- `src/linux/production_hardware.*` is the only production construction point. In milestone 1 it fails clearly rather than choosing fake hardware or Main.
- `src/linux/stderr_log.*` is the production log sink; no logging framework or
  durable log state exists.
- `src/daemon/json.*` owns bounded JSON syntax only.
- `src/daemon/protocol.*` owns protocol-1 schema validation and response shape.
- `src/daemon/controller.*` maps the four wire operations to `Runtime`.
- `src/daemon/server.*` owns Unix-socket framing and per-connection execution.
- `tests/support` contains the only fake hardware and synthetic profiles.

## Protocol 1 contract fixed by this plan

The maximum request line, including its newline, is 65,536 bytes. JSON nesting is limited to four object levels. Arrays, floating-point numbers, duplicate keys, and unknown fields are rejected. Strings must be valid UTF-8; standard JSON escapes are accepted.

Identifiers are 1-32 lower-case ASCII letters, digits, underscores, or hyphens. A path is 1-4095 bytes and begins with `/`. Media and setting names use the identifier rule. Setting values are strings of at most 64 UTF-8 bytes; the selected profile supplies the allowed values.

Requests have these exact shapes:

~~~json
{"protocol":1,"operation":"status"}
{"protocol":1,"operation":"launch","system":"test_cart","rbf":"/tmp/test.rbf","media":{"cartridge":"/tmp/game.bin"},"settings":{"region":"auto"}}
{"protocol":1,"operation":"load_development_rbf","rbf":"/tmp/development.rbf"}
{"protocol":1,"operation":"stop"}
~~~

Every response has these exact top-level keys in this encoded order:

~~~json
{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-0123456789ab"}
~~~

`execution` is `none`, `game`, or `development`. `state` is `idle`, `starting`, `running_game`, `running_development`, or `reboot_required`. A failed response replaces `error: null` with:

~~~json
{"code":"invalid_request","message":"request contains an unknown field"}
~~~

The only error codes are `invalid_request`, `unsupported_protocol`, `unknown_system`, `missing_media`, `busy`, `program_failed`, `core_mismatch`, `io_failed`, and `idle_failed`.

There are no request IDs. If a client loses a response after dispatch, it opens a new connection and calls `status`.

---

### Task 1: Extract only the useful history and record provenance

**Files:**

- Create repository: `/home/deano/fes/libmister-runtime`
- Import history for:
  - `runtime/mister_runtime.h`
  - `runtime/mister_runtime_internal.hpp`
  - `runtime/mister_runtime_v2.cpp`
  - `runtime/native/**`
  - `tests/mister_runtime_v2_*`
  - `tests/native_*`
  - `tests/include/**`
- Create: `LICENSE`
- Create: `DEVELOPMENT.md`
- Create: `docs/provenance/main-mister-346ba9f.sha256`
- Create: `docs/provenance/imported-tip`
- Create: `scripts/check-history.sh`

Do not import `fogcast/**`, `runtime/mister_runtime.cpp`, `runtime/mister_runtime_legacy.cpp`, `runtime/mister_runtime_linux_v2.*`, Main application files, old releases, old CI, or the old root/tests Makefiles.

Run Steps 1-8 in one shell so the validated `source_sha`, temporary path,
manifest path, and rewritten `imported_tip` cannot drift between commands.

- [ ] **Step 1: Verify the exact source and unused destination**

~~~bash
set -euo pipefail
source_repo=/home/deano/fes/Main_MiSTer
source_sha=346ba9f6c6d00fee446b52659229cded9bf16d8c
test "$(git -C "$source_repo" rev-parse "$source_sha")" = "$source_sha"
test ! -e /home/deano/fes/libmister-runtime
git -C "$source_repo" status --short --branch
~~~

Expected: the SHA matches, the destination does not exist, and the source checkout is observed but never switched or edited.

- [ ] **Step 2: Make a fresh source clone and explicit allowlist**

~~~bash
mkdir -p /home/deano/.cache/fogcast-tmp
runtime_import_dir=$(mktemp -d \
  /home/deano/.cache/fogcast-tmp/libmister-runtime-import.XXXXXX)
git clone --filter=blob:none --no-checkout --single-branch \
  --branch codex/task9-main-eol-canonicalization-20260816 \
  https://github.com/DeanoC/Main_MiSTer.git "$runtime_import_dir"
git -C "$runtime_import_dir" branch extract-runtime "$source_sha"
git -C "$runtime_import_dir" checkout --quiet extract-runtime
test "$(git -C "$runtime_import_dir" rev-parse HEAD)" = "$source_sha"
allowlist_tmp=/home/deano/.cache/fogcast-tmp/libmister-runtime-allowlist.txt
git -C "$runtime_import_dir" ls-tree -r --name-only "$source_sha" |
awk '
  $0 == "runtime/mister_runtime.h" ||
  $0 == "runtime/mister_runtime_internal.hpp" ||
  $0 == "runtime/mister_runtime_v2.cpp" ||
  index($0, "runtime/native/") == 1 ||
  $0 ~ /^tests\/mister_runtime_v2_[^/]*$/ ||
  $0 ~ /^tests\/native_[^/]*$/ ||
  index($0, "tests/include/") == 1
' | LC_ALL=C sort -u >"$allowlist_tmp"
test -s "$allowlist_tmp"
allowlist_count=$(wc -l <"$allowlist_tmp")
test "$allowlist_count" -eq 83
~~~

Expected: `extract-runtime` starts at the approved source SHA and the allowlist
contains only concrete file paths—no Git pathspec magic.

- [ ] **Step 3: Generate the source-blob SHA-256 manifest**

~~~bash
manifest_tmp=/home/deano/.cache/fogcast-tmp/main-mister-346ba9f.sha256
while IFS= read -r path
do
  oid=$(git -C "$runtime_import_dir" rev-parse "$source_sha:$path")
  digest=$(git -C "$runtime_import_dir" cat-file blob "$oid" |
    sha256sum | cut -d' ' -f1)
  printf '%s  %s\n' "$digest" "$path"
done <"$allowlist_tmp" >"$manifest_tmp"
test -s "$manifest_tmp"
~~~

Do not use working-tree timestamps or Git blob IDs as the content digest.

- [ ] **Step 4: Filter and import the selected branch**

~~~bash
export allowlist_tmp
FILTER_BRANCH_SQUELCH_WARNING=1 \
git -C "$runtime_import_dir" filter-branch --force --prune-empty \
  --index-filter '
    git rm -r --cached --ignore-unmatch . >/dev/null
    git reset -q "$GIT_COMMIT" --pathspec-from-file="$allowlist_tmp"
  ' -- extract-runtime
git -C "$runtime_import_dir" for-each-ref \
  --format='delete %(refname)' refs/original |
  git -C "$runtime_import_dir" update-ref --stdin
git -C "$runtime_import_dir" branch --delete --force \
  codex/task9-main-eol-canonicalization-20260816
git -C "$runtime_import_dir" remote remove origin
git -C "$runtime_import_dir" ls-tree -r --name-only extract-runtime |
  cmp "$allowlist_tmp" -
filtered_commit_count=$(git -C "$runtime_import_dir" \
  rev-list --count extract-runtime)
test "$filtered_commit_count" -eq 35
git init --initial-branch=main /home/deano/fes/libmister-runtime
git -C "$runtime_import_dir" fast-export --signed-tags=strip \
  --refspec='refs/heads/extract-runtime:refs/heads/main' \
  extract-runtime |
  git -C /home/deano/fes/libmister-runtime fast-import --quiet
git -C /home/deano/fes/libmister-runtime reset --hard --quiet main
imported_tip=$(git -C /home/deano/fes/libmister-runtime rev-parse HEAD)
git -C /home/deano/fes/libmister-runtime reflog expire --expire=now --all
git -C /home/deano/fes/libmister-runtime gc --prune=now
~~~

Expected: commit IDs are rewritten, but relevant authors, dates, messages, and per-file evolution are retained. Empty Main-only commits are pruned. No source remote or tag is copied.

- [ ] **Step 5: Add the extraction audit before any code changes**

Add a fresh standard GPL-3.0-or-later `LICENSE`. Install the generated manifest:

~~~bash
mkdir -p docs/provenance
install -m 0644 "$manifest_tmp" \
  docs/provenance/main-mister-346ba9f.sha256
printf '%s\n' "$imported_tip" >docs/provenance/imported-tip
~~~

Add an extraction appendix to `DEVELOPMENT.md` containing:

- source repository URL;
- source branch;
- original SHA `346ba9f6c6d00fee446b52659229cded9bf16d8c`;
- the exact allowlist above;
- the actual value of `imported_tip`; and
- an explanation that filtered SHAs differ while authorship and file evolution are retained.

Create `scripts/check-history.sh`. It reads the recorded rewritten tip, verifies
that it is an ancestor of `HEAD`, and derives the exact allowed source paths
from the manifest. For every commit reachable from the imported tip, every
tree path must be an exact member of that source allowlist. The tip's tree path
list must exactly equal the manifest path list, and every tip blob must match
its recorded SHA-256.

Do not apply source-history path rules to later standalone commits; otherwise
the valid new `src/daemon/main.cpp` would be mistaken for upstream Main. Mark
the script executable.

- [ ] **Step 6: Run the provenance gate**

~~~bash
cd /home/deano/fes/libmister-runtime
scripts/check-history.sh
git log --follow --oneline -- runtime/native/native_lifecycle.cpp | tail -20
git log --follow --oneline -- runtime/native/linux/native_linux_v2_context.cpp | tail -20
git remote -v
git status --short --branch
~~~

Expected: audit passes, representative history reaches the imported authored lineage, no remote exists, and only the new provenance files are uncommitted.

Also run `git for-each-ref` and confirm that `refs/heads/main` is the only ref.

- [ ] **Step 7: Commit the audited repository seed**

~~~bash
git add LICENSE DEVELOPMENT.md docs/provenance scripts/check-history.sh
git diff --cached --check
git commit -m "chore: establish extracted runtime provenance"
~~~

- [ ] **Step 8: Remove only the validated temporary clone**

~~~bash
case "$runtime_import_dir" in
  /home/deano/.cache/fogcast-tmp/libmister-runtime-import.*)
    find "$runtime_import_dir" -depth -delete
    ;;
  *) exit 1 ;;
esac
~~~

---

### Task 2: Establish canonical layout and one build

**Files:**

- Move: `runtime/mister_runtime.h` → `include/libmister-runtime/runtime.h`
- Move: `runtime/mister_runtime_internal.hpp` → `src/runtime_internal.hpp`
- Move: `runtime/mister_runtime_v2.cpp` → `src/runtime.cpp`
- Move: `runtime/native/**` → `src/native/**`
- Then move file pairs only:
  - `src/native/native_core_protocol.*` → `src/native/core_loader.*`
  - `src/native/linux/native_core_protocol_io_adapter.*` → `src/native/linux/spi.*`
  - `src/native/linux/native_mmio_adapter.*` → `src/native/linux/mmio.*`
  - `src/native/linux/native_fpga_programmer.*` → `src/native/linux/fpga_manager.*`
- Move selected runtime/native tests into `tests/unit/**`
- Then move focused test files only:
  - `native_core_protocol_test.cpp` → `tests/unit/core_loader_test.cpp`
  - `native_linux_artifact_programmer_test.cpp` → `tests/unit/fpga_manager_test.cpp`
  - `native_linux_mmio_containment_test.cpp` → `tests/unit/mmio_test.cpp`
  - `native_linux_core_protocol_io_test.cpp` → `tests/unit/spi_test.cpp`
- Move: `tests/include/**` → `tests/support/include/**`
- Create: `.gitignore`
- Create: `Makefile`

This task is mechanical. Do not redesign lifecycle behavior or delete tested native modules here; that semantic work belongs in Task 3 so `git log --follow` stays useful.

- [ ] **Step 1: Move imported files with Git-aware renames**

Create the destination directories, then use `git mv` for the paths above. Organize native tests under `tests/unit/native/`, runtime lifecycle tests under `tests/unit/`, and Linux stubs under `tests/support/include/`.

Delete the imported `src/native/Makefile`; it must never be the canonical build input.

- [ ] **Step 2: Update paths without disguising obsolete architecture**

Use `git mv` for the four retained source pairs and four focused tests listed
above, then use narrowly scoped edits to update only:

- include paths from `runtime/...` to `libmister-runtime/runtime.h`,
  `runtime_internal.hpp`, or the new file paths as appropriate.

Do not yet rename the classes inside those four files, and do not rename
`HardwareBroker`, recovery/authority classes, or V2 types to make them appear
current. Task 3 rewrites the four retained files and deletes the obsolete
active paths. This intermediate mechanical commit may still contain historic
names; milestone HEAD may not.

- [ ] **Step 3: Add the single deterministic build**

Before the first build, create `.gitignore` containing exactly:

~~~gitignore
/build/
~~~

Create a root `Makefile` with:

- `CXX ?= c++`, `AR ?= ar`;
- `-std=c++14 -Wall -Wextra -Werror -pthread -MMD -MP`;
- includes for `include`, `src`, and `tests/support/include` only where tests need stubs;
- an explicit `LIB_SOURCES` containing `src/runtime.cpp`, later
  `src/profile.cpp`, `src/native/*.cpp`, `src/native/linux/*.cpp`, and
  `src/linux/production_hardware.cpp` when those files exist;
- daemon sources excluded from `LIB_SOURCES` and compiled separately when
  Tasks 4-5 add them;
- exactly one deterministic archive, `build/libmister-runtime.a`, using `ZERO_AR_DATE=1` and `ar rcsD`;
- no relocatable personality object and no second archive;
- dependency files included so header changes rebuild dependents;
- `clean` removing only the repository-local `build` directory.

Add a small archive audit target that compares `ar t build/libmister-runtime.a` with the sorted production object list and rejects duplicate members.

- [ ] **Step 4: Prove the mechanical move builds**

Adapt only enough imported tests/includes for the renamed tree, then run:

~~~bash
make clean
make all
make archive-audit
git diff --check
git status --short
~~~

Expected: the existing selected implementation compiles from its canonical paths into only `build/libmister-runtime.a`.

- [ ] **Step 5: Commit the canonical move separately**

~~~bash
git add -A
git diff --cached --check
git commit -m "refactor: establish canonical runtime layout"
~~~

---

### Task 3: Replace compatibility generations with one lifecycle API

**Files:**

- Rewrite: `include/libmister-runtime/runtime.h`
- Rewrite: `src/runtime.cpp`
- Delete after extracting any useful test intent: `src/runtime_internal.hpp`
- Create: `src/profile.cpp`
- Create: `src/native/artifacts.cpp`
- Create: `src/native/artifacts.hpp`
- Create: `src/native/hardware.cpp`
- Create: `src/native/hardware.hpp`
- Rewrite: `src/native/core_loader.*`
- Rewrite: `src/native/linux/spi.*`
- Rewrite: `src/native/linux/mmio.*`
- Rewrite: `src/native/linux/fpga_manager.*`
- Delete after extracting test vectors: all other imported `src/native/**`
- Create: `src/linux/production_hardware.cpp`
- Create: `src/linux/production_hardware.hpp`
- Create: `tests/support/fake_hardware.*`
- Create: `tests/support/fake_mmio.*`
- Create: `tests/support/fake_spi.*`
- Create: `tests/support/capture_log.*`
- Create: `tests/support/test_profiles.hpp`
- Create: `tests/unit/profile_test.cpp`
- Create: `tests/unit/artifacts_test.cpp`
- Create: `tests/unit/native_hardware_test.cpp`
- Rewrite: `tests/unit/runtime_test.cpp`
- Rewrite focused imported tests in place as
  `tests/unit/core_loader_test.cpp`, `tests/unit/fpga_manager_test.cpp`,
  `tests/unit/mmio_test.cpp`, and `tests/unit/spi_test.cpp`
- Delete after extracting focused cases: all other imported native tests and
  `tests/support/include/**`
- Create: `scripts/check-active-tree.sh`
- Create: `tests/active_tree_test.sh`
- Modify: `Makefile`

**Produces:** one unsuffixed C++14 source API, five lifecycle states, one mutation admission lock, one cleanup attempt, four narrow native primitives, no production profiles, and no fake code in production.

- [ ] **Step 1: Write profile tests before changing the API**

Create two test-only profiles:

~~~cpp
inline mister::Profile CartProfile() {
  mister::Profile p;
  p.system = "test_cart";
  p.expected_core = "TESTCART";
  p.media.push_back({"cartridge", 1, true});
  p.settings.push_back({"region", {"auto", "pal", "ntsc"}});
  return p;
}

inline mister::Profile BiosProfile() {
  mister::Profile p;
  p.system = "test_bios";
  p.expected_core = "TESTBIOS";
  p.media.push_back({"bios", 0, true});
  p.media.push_back({"cartridge", 2, true});
  return p;
}
~~~

Test duplicate systems, duplicate roles/indices, empty core, semantic role-to-index mapping, missing required media, unknown roles/settings/values, absolute paths, and an empty production registry.

- [ ] **Step 2: Write lifecycle tests against one fake hardware interface**

The test-only fake implements:

~~~cpp
class FakeHardware final : public mister::Hardware {
public:
  mister::HardwareResult LoadIdle() override;
  mister::HardwareResult Launch(const mister::PreparedLaunch&) override;
  mister::HardwareResult LoadDevelopmentRBF(const std::string&) override;
  void BlockLaunch();
  void WaitUntilLaunchEntered();
  void ReleaseLaunch();
};
~~~

Add separate tests for:

1. `Start` loads idle once and reaches `idle`;
2. failed initial idle load reaches `reboot_required`/`idle_failed`;
3. validation occurs before hardware mutation;
4. blocked launch publishes `starting`;
5. concurrent mutation returns `busy` immediately and is not queued;
6. `status` remains readable during a blocked mutation;
7. successful game launch records system and observed core;
8. wrong observed core performs one cleanup and reports `core_mismatch`;
9. pre-mutation failure performs no cleanup;
10. post-mutation failure performs exactly one cleanup;
11. cleanup success returns to idle while preserving the primary error;
12. cleanup failure reports `idle_failed` and `reboot_required`;
13. development load records no fake game/system/core identity;
14. stop from either running state loads idle once;
15. stop from idle is idempotent; and
16. stop from `reboot_required` returns `idle_failed` without a hardware call;
17. launch/development from either running state returns `busy` without a
    hardware call;
18. launch/development from `reboot_required` returns `busy` without a
    hardware call; and
19. failed Start or Stop never invokes a second cleanup call;
20. a successful launch logs the requested system, expected and confirmed
    core, and each phase;
21. validation failure logs its direct error without hardware work;
22. post-mutation failure plus successful cleanup logs and returns the primary
    failure; and
23. failed cleanup logs both the primary and idle failure, while development
    logging never manufactures a game, system, or core identity.

Use a real second thread for the busy/status checks.

- [ ] **Step 3: Confirm the new contract tests fail**

~~~bash
make build/tests/unit/profile_test build/tests/unit/runtime_test
~~~

Expected: fail because the unsuffixed canonical API is not implemented.

- [ ] **Step 4: Define the sole public API**

Replace the old mixed v1/v2 header with these unsuffixed C++14 types and no compatibility aliases:

~~~cpp
#pragma once

#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace mister {

enum class ErrorCode {
  none, invalid_request, unsupported_protocol, unknown_system, missing_media,
  busy, program_failed, core_mismatch, io_failed, idle_failed,
};
enum class State {
  idle, starting, running_game, running_development, reboot_required,
};
enum class Execution { none, game, development };

struct Error {
  ErrorCode code = ErrorCode::none;
  std::string message;
  bool ok() const { return code == ErrorCode::none; }
};
struct LogRecord {
  std::string operation;
  std::string system;
  std::string core;
  std::string phase;
  Error error;
};
class LogSink {
public:
  virtual ~LogSink() {}
  virtual void Write(const LogRecord&) = 0;
};
struct Media { std::string role; std::string path; };
struct Setting { std::string name; std::string value; };
struct Launch {
  std::string system;
  std::string rbf;
  std::vector<Media> media;
  std::vector<Setting> settings;
};
struct Status {
  State state = State::starting;
  Execution execution = Execution::none;
  std::string system;
  std::string core;
  Error error;
};
struct MediaRule {
  std::string role;
  std::uint8_t index = 0;
  bool required = false;
};
struct SettingRule {
  std::string name;
  std::vector<std::string> allowed_values;
};
struct Profile {
  std::string system;
  std::string expected_core;
  std::vector<MediaRule> media;
  std::vector<SettingRule> settings;
};
struct PreparedMedia { std::uint8_t index = 0; std::string path; };
struct PreparedLaunch {
  std::string system;
  std::string expected_core;
  std::string rbf;
  std::vector<PreparedMedia> media;
  std::vector<Setting> settings;
};

class Profiles {
public:
  Error Add(Profile);
  Error Prepare(const Launch&, PreparedLaunch*) const;
  bool empty() const;
private:
  std::vector<Profile> profiles_;
};

struct HardwareResult {
  Error error;
  bool mutation_attempted = false;
  std::string observed_core;
};
class Hardware {
public:
  virtual ~Hardware() {}
  virtual HardwareResult LoadIdle() = 0;
  virtual HardwareResult Launch(const PreparedLaunch&) = 0;
  virtual HardwareResult LoadDevelopmentRBF(const std::string&) = 0;
};

class Runtime {
public:
  Runtime(Hardware&, const Profiles&, LogSink&);
  ~Runtime();
  Runtime(const Runtime&) = delete;
  Runtime& operator=(const Runtime&) = delete;
  Error Start();
  Status status() const;
  Error LaunchGame(const Launch&);
  Error LoadDevelopmentRBF(const std::string&);
  Error Stop();
private:
  class Impl;
  std::unique_ptr<Impl> impl_;
};

const char* ErrorCodeName(ErrorCode);
const char* StateName(State);
const char* ExecutionName(Execution);
}
~~~

This is deliberately a source-level C++ API. The Unix socket is the external integration boundary; there is no current consumer that justifies preserving two C ABI generations.

`Hardware`, `Profiles`, and `LogSink` are non-owning constructor references and
must outlive `Runtime`. The same log sink is passed to `NativeHardware`; it is
a small event destination, not an audit/security subsystem.

- [ ] **Step 5: Implement profile validation and artifact preflight**

Use these bounds:

- identifiers: 1-32 lower-case ASCII letters, digits, underscore, or hyphen;
- expected core: 1-64 printable ASCII bytes;
- RBF/media path: 1-4095 bytes, starting with `/`;
- setting value: 1-64 valid UTF-8 bytes;
- at most 8 media rules and 16 setting rules per profile;
- no duplicate system, role, numeric index, setting name, or allowed value.

`Profiles::Prepare` validates the entire request before assigning the output or calling hardware. It maps semantic media roles to profile-owned numeric indices. It does not accept game IDs, content digests, fixed cache roots, or catalogue aliases.

Create these internal RAII artifact contracts in `src/native/artifacts.hpp`:

~~~cpp
class Artifact {
public:
  Artifact();
  ~Artifact();
  Artifact(Artifact&&) noexcept;
  Artifact& operator=(Artifact&&) noexcept;
  Artifact(const Artifact&) = delete;
  Artifact& operator=(const Artifact&) = delete;
  int fd() const;
  std::uint64_t size() const;
  const std::string& path() const;
private:
  friend class PosixArtifactOpener;
  int fd_ = -1;
  std::uint64_t size_ = 0;
  std::string path_;
};
struct OpenedMedia { std::uint8_t index = 0; Artifact artifact; };
struct ArtifactSet { Artifact rbf; std::vector<OpenedMedia> media; };

class ArtifactOpener {
public:
  virtual ~ArtifactOpener() {}
  virtual Error Open(const std::string& path, std::uint64_t maximum_size,
                     Artifact* artifact) = 0;
};
class PosixArtifactOpener final : public ArtifactOpener {
public:
  Error Open(const std::string&, std::uint64_t, Artifact*) override;
};

Error OpenLaunchArtifacts(const PreparedLaunch&, ArtifactOpener&, ArtifactSet*);
Error OpenRBFArtifact(const std::string&, ArtifactOpener&, Artifact*);
~~~

An output is assigned only after the complete open succeeds.
`maximum_size == 0` means no profile-specific maximum; the RBF call always
uses 32 MiB. `Artifact` is the sole descriptor owner, closes on destruction,
and is never reopened by a lower layer. Before the first FPGA/MMIO/SPI write,
production launch and development-load code must:

1. open every RBF and media path read-only;
2. verify each descriptor is a readable regular file with a positive size;
3. reject an RBF larger than 32 MiB;
4. retain all descriptors until the operation finishes; and
5. pass opened descriptors and sizes—not paths—to `FpgaManager` and
   `CoreLoader`.

Open and validate the complete set before any hardware mutation. Profiles may
add media-specific size limits later; milestone 1 imposes only positive,
representable regular-file size for media. Keep the OS-opener seam small and
injectable so `artifacts_test.cpp` covers missing, directory, zero-length,
unreadable/open-error, oversize-RBF, successful multi-file preflight, and
descriptor cleanup. Every rejection must prove zero fake hardware mutation.

- [ ] **Step 6: Implement the five-state lifecycle**

`Runtime::Impl` contains one mutex, the `Hardware` and `Profiles` references, current `Status`, `busy`, and `started`. Mutation flow is:

1. lock and return `busy` immediately if another mutation is active;
2. set `busy` to reserve admission, without changing observable execution;
3. validate the request and current state, clearing `busy` on rejection;
4. set `starting` and the intended execution identity;
5. release the mutex for every hardware call;
6. for post-mutation failure, keep `busy` set while performing the one idle
   cleanup outside the mutex;
7. commit the final status under the mutex; and
8. clear `busy` before returning.

Finish all `Profiles::Add` calls before constructing `Runtime`; the immutable
registry is shared by const reference for the runtime lifetime.

Use this complete admission/transition table:

| Operation | Admitted state | Hardware calls | Success | Failure |
|---|---|---|---|---|
| `status` | any | none | returns current status | n/a |
| `Start` | not previously started | `LoadIdle` once | `idle` | `idle_failed`, `reboot_required` |
| second `Start` | any started state | none | n/a | `busy`, state unchanged |
| game/development launch | `idle` only | corresponding launch | matching running state | launch rules below |
| game/development launch | running or `reboot_required` | none | n/a | `busy`, state unchanged |
| `Stop` | `idle` | none | `idle` | n/a |
| `Stop` | either running state | `LoadIdle` once | `idle` | `idle_failed`, `reboot_required` |
| `Stop` | `reboot_required` | none | n/a | `idle_failed`, state unchanged |

While any admitted mutation is active, every other mutation returns `busy`
without validation or hardware work; `status` remains readable and observes
`starting`.

The cleanup rule applies only to game/development launch. If launch failure
reports `mutation_attempted == false`, return to idle with the primary error
and do not clean up. For a post-mutation launch failure or observed-core
mismatch, call `LoadIdle` exactly once. Cleanup success returns to idle while
preserving the primary error; cleanup failure reports `idle_failed` and
`reboot_required`. `Start` and `Stop` already are the one idle attempt, so
their failure is never followed by another call. Never loop, retry, sleep,
persist state, or fall back to Main.

- [ ] **Step 7: Extract four narrow native primitives, then delete the framework**

The four file pairs were moved without semantic rewriting in Task 2, so keep
their history followable by changing them in place here. Freeze these internal
interfaces before porting behavior:

~~~cpp
class Clock {
public:
  virtual ~Clock() {}
  virtual std::uint64_t NowMs() const = 0;
};
struct NativeTimeouts {
  std::uint32_t program_ms = 30000;
  std::uint32_t core_io_ms = 10000;
};
struct NativeResult {
  Error error;
  bool mutation_attempted = false;
};

class Mmio {
public:
  virtual ~Mmio() {}
  virtual Error Read32(std::uint32_t offset, std::uint32_t*) = 0;
  virtual Error Write32(std::uint32_t offset, std::uint32_t value) = 0;
};
class Spi {
public:
  virtual ~Spi() {}
  virtual Error Exchange(std::uint8_t target,
                         const std::vector<std::uint16_t>& request,
                         std::vector<std::uint16_t>* response,
                         std::uint64_t absolute_deadline_ms) = 0;
};
class FpgaManager {
public:
  virtual ~FpgaManager() {}
  virtual NativeResult Program(const Artifact&,
                               std::uint64_t absolute_deadline_ms) = 0;
};
class CoreLoader {
public:
  explicit CoreLoader(Spi&);
  Error Probe(std::string* observed_core,
              std::uint64_t absolute_deadline_ms);
  Error Configure(const std::vector<Setting>&,
                  std::uint64_t absolute_deadline_ms);
  Error Attach(std::uint8_t index, const Artifact&,
               std::uint64_t absolute_deadline_ms);
};
~~~

The concrete classes are `LinuxMmio`, `LinuxSpi`, and `LinuxFpgaManager` in the
corresponding Linux headers. Raw OS-call seams used by their unit tests remain
private/test-only and must not escape as production archive symbols. Compute
saturating absolute monotonic deadlines from `Clock`; never reset a deadline
inside a polling or transfer loop. A deadline failure returns the operation's
direct error with message `deadline exceeded`: `program_failed` for FPGA
programming and `io_failed` for SPI/core work. It does not add a new error
enumerator.

Retain only these behaviors:

- `CoreLoader`: exact tested core-name/status/file-transfer command ordering,
  driven by a small `Spi` interface and already-opened media artifacts;
- `Spi`: select/write/strobe/ACK/deselect exchange and core probing, with
  injected register operations for tests;
- `Mmio`: narrowly scoped mapping, register read/write, bridge control, and
  cleanup, with injected OS operations;
- `FpgaManager`: the bounded 4 KiB streaming loop and final
  config/init/user-mode checks, consuming an already-opened RBF rather
  than reopening caller paths.

Create `NativeHardware` with this exact ownership boundary:

~~~cpp
class NativeHardware final : public Hardware {
public:
  NativeHardware(ArtifactOpener&, FpgaManager&, CoreLoader&, Clock&,
                 LogSink&, std::string idle_rbf, NativeTimeouts);
  HardwareResult LoadIdle() override;
  HardwareResult Launch(const PreparedLaunch&) override;
  HardwareResult LoadDevelopmentRBF(const std::string&) override;
private:
  // Non-owning primitive/clock/log references; owned idle path and timeouts.
};
~~~

Launch order is complete artifact preflight, program RBF, probe and compare the
core, configure settings, then attach media in numeric-index order.
Development load is preflight then program only. Idle load is preflight of the
configured idle RBF then program only. There is no background native state;
`Runtime` is the sole operation admission owner. `Artifact` retains sole
descriptor ownership for the complete synchronous call, and all injected
references must outlive `NativeHardware`.

Use this exact result mapping:

| Failure/result | Error | `mutation_attempted` | `observed_core` |
|---|---|---:|---|
| Request/profile shape rejected by `Runtime` | existing direct validation error | n/a; no `Hardware` call | empty |
| Artifact open/stat/type/readability/positive-size/limit failure | `io_failed` | false | empty |
| FPGA failure before its first register/data write | `program_failed` | false | empty |
| FPGA failure after its first register/data write | `program_failed` | true | empty |
| Probe/configure/media I/O or deadline | `io_failed` | true | retained after a successful probe |
| Confirmed core differs from profile | `core_mismatch` | true | actual core |
| Successful game launch | none | true | actual core |
| Successful development or idle load | none | true | empty |

`Runtime` converts any failed `LoadIdle` result in `Start` or `Stop` to
`idle_failed`/`reboot_required` without a second hardware call. Launch cleanup
preserves the primary error when idle succeeds; failed idle cleanup replaces
it with `idle_failed`/`reboot_required` while both failures are logged.

`native_hardware_test.cpp` verifies all artifacts open before programming,
program/probe/configure/media order, sorted media attachment,
development-RBF programming, idle-RBF loading, absolute deadlines,
pre-write versus post-write failure classification, exact error mapping, and
zero writes for every preflight rejection.

Use one `CaptureLog` test sink backed by a mutex-protected
`std::vector<LogRecord>`. The fixed operation values are `start`, `launch`,
`load_development_rbf`, and `stop`; fixed phase values are `validate`,
`starting`, `preflight`, `program`, `probe`, `configure`, `media`, `cleanup`,
`idle`, `running`, and `failure`. `Runtime` logs validation/admission,
observable state transitions, cleanup, and final failure. `NativeHardware`
logs the exact native phase, the confirmed core after probe, and its direct
failure. These are single in-process events only: no credentials, audit trail,
persistence, rotation, or logging framework.

Rewrite the useful command traces and direct failures into the four focused
tests. Do not carry broker leases, capability bundles, authority views,
mutation receipts, allocation-fault machinery, recovery mappings, or private
profile pointer-identity checks into those interfaces.

After the focused tests pass, delete the remaining imported native active
tree: `hardware_broker`, containment, recovery, peripheral sessions,
resources/session-state, A/V, input, save, scheduler/offload, SNES content,
Linux context, and their old tests. Their source remains reachable in the
filtered history for future reference; it is not compiled or copied into a
deprecated directory.

Production profiles remain empty. `src/linux/production_hardware.*` exposes
one clear construction function and returns `io_failed` in milestone 1 because
the image-owned idle path, production profiles, and accepted device
composition do not exist yet. It must never silently choose fake hardware or
Main. Tests may construct `NativeHardware` directly with injected primitives;
the production executable may not.

Use this construction boundary:

~~~cpp
mister::Error CreateProductionHardware(
    mister::LogSink& log,
    std::unique_ptr<mister::Hardware>* hardware);
std::unique_ptr<mister::Hardware> CreateUnavailableHardware(
    const mister::Error& reason);
~~~

The first function leaves `hardware` empty on failure. The second is used only
to keep status serviceable after that explicit failure; every mutation returns
the recorded `io_failed` reason.

Delete the old C99/v2 smoke tests and compatibility-only lifecycle tests after
the canonical profile/runtime tests cover their relevant intent.

- [ ] **Step 8: Add active-tree and production-link guards**

Create executable `scripts/check-active-tree.sh` and `tests/active_tree_test.sh`. Scan only `include/`, `src/`, and `Makefile` for active historic terms so the guard does not match its own patterns:

~~~text
stage[-_ ]?c0
poc[0-9]*
fogcast-runtime
native[-_ ]personality
mister_runtime_linux_v2
NativeLinuxV2
HardwareBroker
OperationLease
CapabilityBundle
AuthorityView
BackendFence
ReplayTracker
native_recovery
native_containment
native_peripheral_session
native_resources
native_audio
native_av_io
native_video
native_input
native_save
native_scheduler
native_offload
native_snes
v2
~~~

The test must also fail if root active paths named `fogcast`, `runtime`, `support`, `lib`, or `releases` exist. It must prove fake hardware/profile objects are absent from production archive members and symbols.

- [ ] **Step 9: Run focused and native verification**

~~~bash
set -euo pipefail
make clean
make test
make sanitize
make tsan
make archive-audit
scripts/check-active-tree.sh
scripts/check-history.sh
git diff --check
~~~

Expected: lifecycle/profile tests, four focused native tests, ASan/UBSan,
relevant TSan tests, archive audit, active-tree guard, and history audit all
pass.

- [ ] **Step 10: Commit the semantic runtime cleanup**

~~~bash
git add -A
git diff --cached --check
git commit -m "refactor: define one native runtime lifecycle"
~~~

---

### Task 4: Implement the exact four-operation protocol

**Files:**

- Create: `src/daemon/json.hpp`
- Create: `src/daemon/json.cpp`
- Create: `src/daemon/protocol.hpp`
- Create: `src/daemon/protocol.cpp`
- Create: `tests/fixtures/protocol-v1.jsonl`
- Create: `tests/unit/protocol_test.cpp`
- Modify: `Makefile`

- [ ] **Step 1: Add golden valid request fixtures**

Put exactly the four request examples from the protocol contract into `tests/fixtures/protocol-v1.jsonl`, one per line. No hello, health, operation status, recover, shutdown, request IDs, digests, expected sequences, or negotiation records are permitted.

- [ ] **Step 2: Write schema tests first**

Test:

- all four valid operations;
- protocol missing, zero, two, string, float, and oversized integer;
- unknown top-level, media, and settings fields;
- missing or extra fields per operation;
- duplicate JSON keys;
- arrays, floats, excessive nesting, invalid UTF-8, invalid escapes, and trailing data;
- identifier/path/value bounds;
- stable response key order, null identity fields, escaped strings, and the complete error-code mapping.

The parser must distinguish syntax/shape failure (`invalid_request`) from a well-formed unsupported protocol (`unsupported_protocol`).

- [ ] **Step 3: Confirm protocol tests fail**

~~~bash
make build/tests/unit/protocol_test
~~~

Expected: fail because the JSON and protocol modules are absent.

- [ ] **Step 4: Implement a bounded syntax parser**

`json.*` owns syntax only. It accepts object, string, integer, boolean, and null values; rejects arrays/floats/duplicate keys; validates UTF-8; enforces four object levels; and never reads beyond 65,536 bytes. It must not know operation names or runtime types.

- [ ] **Step 5: Implement exact request/response schemas**

`protocol.*` defines `Operation`, `Request`, `ParseRequest`, and `EncodeResponse`. Reject every field not listed in the fixed contract. Decode media/settings objects into canonical `Launch` values. Encode all eight response keys in the documented order.

Response `ok` reports the current request result. `Status::error` is the last lifecycle error and may remain non-null after a later successful `status`; document and test that distinction rather than clearing evidence implicitly.

- [ ] **Step 6: Run protocol gates**

~~~bash
make test
make sanitize
git diff --check
~~~

- [ ] **Step 7: Commit the fresh protocol**

~~~bash
git add src/daemon/json.* src/daemon/protocol.* \
  tests/fixtures/protocol-v1.jsonl tests/unit/protocol_test.cpp Makefile
git diff --cached --check
git commit -m "feat: define the local runtime protocol"
~~~

---

### Task 5: Add the thin Unix-socket daemon

**Files:**

- Create: `src/daemon/controller.hpp`
- Create: `src/daemon/controller.cpp`
- Create: `src/daemon/server.hpp`
- Create: `src/daemon/server.cpp`
- Create: `src/daemon/main.cpp`
- Create: `src/linux/stderr_log.hpp`
- Create: `src/linux/stderr_log.cpp`
- Create: `tests/integration/daemon_server_test.cpp`
- Modify: `Makefile`

- [ ] **Step 1: Write controller/socket tests first**

Use a temporary socket path, test profiles, and `FakeHardware`. Cover:

1. startup/status returns idle;
2. one request receives one newline response and EOF;
3. a second request on the same connection is never processed;
4. EOF before newline and a line over 65,536 bytes return `invalid_request` then close;
5. launch, development load, and stop identities/states;
6. status from another connection observes `starting`;
7. concurrent mutation returns `busy`;
8. cleanup failure returns `reboot_required`/`idle_failed`;
9. destroying/reconstructing runtime does not preserve a game;
10. losing a launch response is reconciled by a later status call; and
11. test `RequestStop` closes the listener, waits for active connections, and
    removes only its own socket path;
12. a second server refuses to steal a path with a live listener; and
13. a confirmed stale socket is removed and rebound exactly once;
14. a launch through the socket emits the same requested/confirmed identity
    and phase records as the direct runtime call; and
15. development load emits no synthetic game, system, or core identity.

- [ ] **Step 2: Confirm the integration test fails**

~~~bash
make build/tests/integration/daemon_server_test
~~~

- [ ] **Step 3: Implement the controller without policy**

`Controller::Handle(line)` parses once, calls exactly one of `status`, `LaunchGame`, `LoadDevelopmentRBF`, or `Stop`, then encodes the observed state once. It does not retry, persist, assign IDs, translate to Main, or manufacture identities.

For a parse failure, copy `runtime.status()`, replace only the response copy's error with the parse error, encode `ok:false`, and do not mutate runtime state.

- [ ] **Step 4: Implement bounded concurrent socket service**

`Server`:

- creates `AF_UNIX/SOCK_STREAM` at its injected path;
- first attempts `bind` without unlinking anything;
- on `EADDRINUSE`, connects to the existing path: a successful connection
  means a live owner and startup fails;
- only when connect proves no listener and `lstat` proves the existing entry
  is a Unix socket, unlinks that exact stale socket and retries `bind` once;
- refuses to unlink a regular file, directory, unknown entry, or a socket that
  becomes live during the retry;
- listens with backlog 8;
- processes one bounded newline request and one response per connection;
- starts one detached `std::thread` per accepted connection;
- increments a mutex-protected active-connection count before detach;
- decrements it through a scope guard after shutdown/close;
- waits on a condition variable for active count zero before `Serve` returns;
- handles short writes;
- unlinks only its exact socket path; and
- performs no peer-credential check or security/state-file setup.

`RequestStop` exists for controlled tests and closes/shuts down the listening
socket. An incidental `EINTR` retries `accept` unless `RequestStop` was set.
There is no polling or sleep loop.

- [ ] **Step 5: Compose honest production startup**

Implement `StderrLogSink` as a direct, mutex-protected `LogSink`. It writes one
line per record to standard error in this stable form:

~~~text
mister-runtime operation=<value> phase=<value> system=<value-or-> core=<value-or-> error=<code> message=<value-or->
~~~

Escape control characters so a record cannot create extra lines. Do not add a
logger dependency, timestamp service, file sink, rotation, persistence, or
security/audit semantics. Unit/integration coverage proves a record is one
line and every record field survives escaping. `main.cpp` passes this one sink to
both `Runtime` and production hardware.

`main.cpp`:

1. leaves `SIGINT`/`SIGTERM` at their normal process-termination behavior;
2. relies on exact stale-socket removal at the next start rather than adding a
   signal thread, self-pipe, or unsafe handler;
3. creates the empty production profile registry;
4. constructs `StderrLogSink` and asks `production_hardware` for the native
   implementation, logging a `failure` record if construction is unavailable;
5. when construction is unavailable, uses an explicit unavailable object whose methods return `io_failed`—never fake/Main;
6. calls `Runtime::Start` once, logs any direct startup failure, and retains
   honest `reboot_required` status;
7. serves only `/run/mister-runtime.sock`;
8. embeds `MISTER_RUNTIME_VERSION`; and
9. returns non-zero only for socket setup/serve failure, not because milestone-2 hardware construction is deliberately unavailable.

- [ ] **Step 6: Extend the one build**

Build `build/mister-runtime` from daemon objects plus
`build/libmister-runtime.a`. On the GNU host/cross link, surround the archive
with `--whole-archive`/`--no-whole-archive` so every production native member
participates in final link closure. Do not create a daemon library or second
runtime archive. Default version is `git-<12-char HEAD>` with `-dirty` when
applicable. Ensure fake objects appear only in test link commands.

- [ ] **Step 7: Run daemon and link checks**

~~~bash
set -euo pipefail
make clean
make test
make sanitize
make tsan
make all
nm -C build/mister-runtime >build/mister-runtime.symbols
if rg 'FakeHardware|test_support' build/mister-runtime.symbols
then
  exit 1
fi
nm -u -C build/mister-runtime >build/mister-runtime.undefined
if rg '(^|[^[:alnum:]_])(fpga_load_rbf|user_io_|scheduler_|offload_)' \
  build/mister-runtime.undefined
then
  exit 1
fi
git diff --check
~~~

Expected: all checks pass. `set -e` makes either standalone `nm` failure fatal;
the searches cannot mistake a failed symbol dump for an empty result.

- [ ] **Step 8: Commit the daemon**

~~~bash
git add src/daemon src/linux/stderr_log.* \
  tests/integration/daemon_server_test.cpp Makefile
git diff --cached --check
git commit -m "feat: add the local runtime daemon"
~~~

---

### Task 6: Document, cross-build, and publish the software baseline

**Files:**

- Create: `README.md`
- Create: `ARCHITECTURE.md`
- Expand: `DEVELOPMENT.md`
- Create: `AGENTS.md`
- Create: `docs/design/2026-08-31-native-runtime-design.md`
- Create: `docs/support-matrix.md`
- Modify: `scripts/check-active-tree.sh`
- Modify: `Makefile`

- [ ] **Step 1: Add and run the pinned target build**

Add:

~~~make
TARGET_CXX ?= arm-none-linux-gnueabihf-g++
TARGET_AR ?= arm-none-linux-gnueabihf-ar

.PHONY: target
target:
	$(MAKE) BUILD_DIR=build/target CXX="$(TARGET_CXX)" \
		AR="$(TARGET_AR)" all
~~~

Install the existing project toolchain only if absent:

~~~bash
toolchain_cache=/home/deano/.cache/toolchains
toolchain_name=gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf
toolchain_archive="$toolchain_cache/$toolchain_name.tar.xz"
toolchain_url=https://developer.arm.com/-/media/Files/downloads/gnu-a/10.2-2020.11/binrel/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz
mkdir -p "$toolchain_cache"
if ! test -x "$toolchain_cache/$toolchain_name/bin/arm-none-linux-gnueabihf-g++"
then
  curl -fL "$toolchain_url" -o "$toolchain_archive"
  tar -C "$toolchain_cache" -xf "$toolchain_archive"
fi
export RUNTIME_TARGET_BIN="$toolchain_cache/$toolchain_name/bin"
make target \
  TARGET_CXX="$RUNTIME_TARGET_BIN/arm-none-linux-gnueabihf-g++" \
  TARGET_AR="$RUNTIME_TARGET_BIN/arm-none-linux-gnueabihf-ar"
file build/target/libmister-runtime.a build/target/mister-runtime
~~~

Expected: archive and executable are 32-bit Arm EABI hard-float artifacts. The pinned compiler is host setup, not a repository dependency; do not add attestation machinery.

- [ ] **Step 2: Complete archive/link/dependency guards**

Extend `check-active-tree.sh` to assert:

1. one archive named `build/libmister-runtime.a` and one executable named `build/mister-runtime` exist;
2. no built output contains historic fogcast/personality/stage/POC/broker/coordinator/fence/replay/V2 names;
3. archive members exactly match the production source manifest with no fake/test objects;
4. the executable contains no Main mutation or fake symbols;
5. touching a public/native header rebuilds all dependent objects; and
6. two clean archive builds have identical SHA-256 values.

For symbol checks, run `nm` as its own required-success command into a file,
then search that file. Do not place `nm` on the conditional side of a pipeline.

- [ ] **Step 3: Write root documentation from current truth**

`README.md` leads with purpose and current status, explicitly stating:

~~~markdown
## Current status

Software-tested only. Production native construction is not yet available.
Hardware-supported systems: 0.
~~~

It gives the shortest host build/test commands and links to the single support matrix, architecture, and development guide. It must not claim that a native image, idle RBF, FogCast backend, production profile, or Pi acceptance exists.

`ARCHITECTURE.md` explains library/daemon/native boundaries, lifecycle, profile ownership, four-operation protocol, one-operation admission, single cleanup, and why status reconciles a lost response.

`DEVELOPMENT.md` keeps extraction provenance and adds host build, sanitizers,
Arm build, a clearly marked not-yet-available image-install section,
physical-evidence format, and rollback principles.

- [ ] **Step 4: Add one canonical support matrix and design snapshot**

`docs/support-matrix.md` contains the 16 approved IDs exactly once:
`megadrive`, `snes`, `nes`, `sms`, `gb`, `gbc`, `gba`, `pce`, `gg`, `a2600`,
`a7800`, `coleco`, `lynx`, `ws`, `wsc`, and `intv`. Every row starts
`not implemented`, `software: no`, `hardware: no`, with no synthetic fixture
presented as support. Other docs link to it.

Copy the approved design into `docs/design/2026-08-31-native-runtime-design.md`
and identify FogCast commit `6da6fb9` as the source snapshot. This prevents
the standalone repository depending forever on a sibling absolute path.

- [ ] **Step 5: Add the repository policy**

`AGENTS.md` requires:

- read `README.md` and `ARCHITECTURE.md` before changing execution;
- update docs/support status in the same commit as behavior;
- one active lifecycle, daemon, profile table, and Linux construction path;
- Git history instead of active deprecated copies;
- no POC/stage/temporary active names;
- no ownership database, authentication, security framework, failover, retry framework, or recovery coordinator without a demonstrated current need and explicit approval;
- no hardware-working claim without dated physical-Pi evidence;
- real hardware tests for FPGA/input/video/save/lifecycle changes;
- preserve unrelated user work; and
- no commit, push, or PR without explicit user authorization.

- [ ] **Step 6: Run the complete local milestone gate**

~~~bash
set -euo pipefail
runtime_target_bin=/home/deano/.cache/toolchains/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin
test -x "$runtime_target_bin/arm-none-linux-gnueabihf-g++"
test -x "$runtime_target_bin/arm-none-linux-gnueabihf-ar"
make clean
make all
make test
make sanitize
make tsan
make archive-audit
scripts/check-active-tree.sh
scripts/check-history.sh
make target \
  TARGET_CXX="$runtime_target_bin/arm-none-linux-gnueabihf-g++" \
  TARGET_AR="$runtime_target_bin/arm-none-linux-gnueabihf-ar"
git diff --check
git status --short
~~~

Expected: all checks pass and status lists only Task 6 changes before commit.

- [ ] **Step 7: Commit the documented software baseline**

~~~bash
git add README.md ARCHITECTURE.md DEVELOPMENT.md AGENTS.md docs Makefile \
  scripts/check-active-tree.sh
git diff --cached --check
git commit -m "docs: establish the standalone runtime baseline"
git status --short --branch
~~~

Expected: clean local `main`.

- [ ] **Step 8: Pause for explicit publishing authorization**

Do not create or push a remote as part of the local gate. Show the commit list, verification summary, and proposed private repository URL to the user. Obtain explicit authorization to create/push `DeanoC/libmister-runtime`.

After approval only:

~~~bash
gh repo create DeanoC/libmister-runtime --private \
  --description "Native MiSTer runtime and local control daemon" \
  --source /home/deano/fes/libmister-runtime \
  --remote origin \
  --push
git -C /home/deano/fes/libmister-runtime status --short --branch
gh repo view DeanoC/libmister-runtime \
  --json nameWithOwner,visibility,defaultBranchRef,url
~~~

Expected: private repository, `main` default branch, local tracking `origin/main`, and clean worktree.

## Milestone 1 completion gate

Do not describe milestone 1 as complete until all are true:

1. The new repository clones and builds without Main_MiSTer, FogCast, or misteross.
2. Reachable history contains only the allowlisted lineage; provenance records the original SHA, rewritten tip, and source-blob SHA-256 manifest.
3. HEAD contains one archive, one executable, one lifecycle API, one profile model, and the four narrow native primitives.
4. Historic active names, legacy/v2 APIs, obsolete Linux scaffold, old daemon, and coordinator/replay/security/failover paths are absent.
5. Protocol 1 implements exactly four operations and the fixed schema; status is the only lost-response reconciliation mechanism.
6. Production cannot select fake/synthetic profiles and reports native construction unavailable.
7. Host tests, relevant sanitizers, Arm cross-build, deterministic archive, dependency invalidation, and final link closure pass.
8. Root docs state software-only and zero hardware-supported systems.
9. FogCast, Main_MiSTer, misteross, image construction, and the Pi remain unchanged.

## Explicitly deferred

- Native image build/service ordering and idle-RBF packaging.
- FogCast agent integration with `/run/mister-runtime.sock`.
- Real FPGA-manager/MMIO production construction.
- Mega Drive, SNES, or any other production system profile.
- Physical-Pi launch, visible capture, input, save, stop, and reboot evidence.
- Switching the disposable kit's everyday image.

These become later plans after this repository baseline is reviewed. They must not be pulled into implementation merely because scaffolding exists.
