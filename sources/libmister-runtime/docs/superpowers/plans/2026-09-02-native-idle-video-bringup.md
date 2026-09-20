# Native Idle Video Bring-up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make native idle publish success only after the locked menu core produces a verified 1280x720@60 HDMI signal through the ADV7513.

**Architecture:** Add one immutable menu video recipe, a small Linux I2C byte-register adapter, and a `MenuVideoBringup` state machine composed from the existing core-loader, SPI, clock, and logging boundaries. `NativeHardware::LoadIdle()` alone invokes that component after FPGA programming; game and development flows remain unchanged, and every post-program failure uses the existing `idle_failed`/`reboot_required` contract.

**Tech Stack:** C++14, GNU Make, Linux i2c-dev ioctls, the existing libmister-runtime SPI/core/clock/logging abstractions, shell acceptance tooling in FogCast, and the pinned GNU Arm 10.2 EABI5 hard-float toolchain.

**Spec:** `docs/design/2026-09-01-native-idle-video-bringup-design.md`

## Global Constraints

- Preserve one runtime, one daemon, one production construction path, one profile table, and the existing protocol/state model.
- The only production mode is immutable `menu_720p60`: 1280/110/40/220 by 720/5/5/20, 74.25 MHz, CTA VIC 4, no pixel repetition.
- The menu core identity must be exactly `MENU`; missing, malformed, or different identities fail before any video recipe write.
- Scan only `/dev/i2c-0`, `/dev/i2c-1`, and `/dev/i2c-2`; select ADV7513 address `0x39`; keep the first descriptor whose register `0x41` read succeeds.
- Use one caller-owned absolute deadline for the whole video operation. Link polling may repeat only the `0x42` read and must stop at that deadline.
- Require `(adv_status_42 & 0x60) == 0x60`; do not add EDID, alternate modes, fallback, retries of mutation, background monitoring, or Main startup.
- Add no external I2C library or shell helper. Linux production uses direct kernel i2c-dev operations; non-Linux host builds remain supported and fail production I2C access clearly at runtime.
- Do not copy Main subsystems or carry a mode/PLL search algorithm. Literal production data is provenance-checked against Main commit `cc5eb4bfc4cb2887dd6ab8364bff7010d7c6978c`.
- Keep production profiles empty and the support matrix at zero game systems. Native game and development-video support remain unclaimed.
- Fake SPI/I2C/hardware implementations stay under `tests/support` and cannot be linked into the production archive or daemon.
- A physical support claim requires a merged runtime, merged FogCast pin, two identical native images, the unchanged native lifecycle smoke, five direct HDMI frames, and fresh legacy/Sonic restoration evidence.
- On any physical failure: preserve exact evidence, restore fresh legacy dev/Menu, stop the acceptance sequence, and make no pass claim.

## File map

- `src/native/video_recipe.{hpp,cpp}`: immutable menu timing words, ADV7513 register tables, identity, and Main provenance SHA.
- `src/native/linux/i2c.{hpp,cpp}`: narrow stateful I2C interface plus the Linux `/dev/i2c-*` implementation and test operation seam.
- `src/native/video.{hpp,cpp}`: menu software-reset/core-probe/ADV/timing/link state machine and phase diagnostics.
- `tests/support/fake_i2c.{hpp,cpp}`: scripted test-only I2C implementation.
- `tests/unit/video_recipe_test.cpp`: exact immutable data and provenance tests.
- `tests/unit/i2c_test.cpp`: bus scan, descriptor lifetime, transfer, validation, and deadline tests.
- `tests/unit/video_test.cpp`: full success order, exact wire writes, every failure phase, and link predicate/deadline tests.
- `src/native/hardware.{hpp,cpp}` and `tests/unit/native_hardware_test.cpp`: idle-only integration and mutation/error mapping.
- `src/linux/production_hardware.cpp`: production ownership/composition of `LinuxI2c` and `MenuVideoBringup`.
- `Makefile`, `scripts/check-active-tree.sh`, and `tests/active_tree_test.sh`: compilation, dependency, archive, raw-I/O, and forbidden-symbol gates.
- `README.md`, `ARCHITECTURE.md`, `DEVELOPMENT.md`, and `docs/support-matrix.md`: truthful software/physical status and the new narrow boundary.
- FogCast `build/native-runtime.inputs.lock.toml` plus its two focused fixtures: exact merged-runtime pin.
- FogCast Task 7 evidence/report and truth docs: native-first physical acceptance, legacy proof, and only then milestone status.

---

### Task 1: Commit the immutable menu video recipe

**Files:**
- Create: `src/native/video_recipe.hpp`
- Create: `src/native/video_recipe.cpp`
- Create: `tests/unit/video_recipe_test.cpp`
- Modify: `Makefile`

**Interfaces:**
- Consumes: no production runtime state; only fixed-width integer and container types.
- Produces:

```cpp
namespace mister { namespace native {

struct RegisterWrite {
	std::uint8_t address;
	std::uint8_t value;
};

struct VideoRecipe {
	const char* identity;
	const char* source_commit;
	const std::vector<std::uint16_t>& timing_words;
	const std::vector<RegisterWrite>& adv_initialization;
	const std::vector<RegisterWrite>& adv_mode;
};

const VideoRecipe& Menu720p60Recipe();

} }
```

- [ ] **Step 1: Add an exact failing recipe test and Make target**

Create `tests/unit/video_recipe_test.cpp`. Assert the identity and provenance, then compare all three returned vectors against independent local oracle literals. The timing oracle is exactly:

```cpp
const std::vector<std::uint16_t> expected_timing = {
	0x0020,
	0x0500, 0x006e, 0x0028, 0x00dc,
	0x02d0, 0x0005, 0x0005, 0x0014,
	0x4004, 0x0404, 0x0000,
	0x4003, 0x0000, 0x0001,
	0x4005, 0x0303, 0x0000,
	0x4009, 0x0002, 0x0000,
	0x4008, 0x0007, 0x0000,
	0x4007, 0xc28f, 0xe8f5,
};
```

The independent ADV initialization oracle is exactly:

```cpp
const std::vector<RegisterWrite> expected_initialization = {
	{0x98,0x03},{0xd6,0xc0},{0x41,0x10},{0x9a,0x70},
	{0x9c,0x30},{0x9d,0x61},{0xa2,0xa4},{0xa3,0xa4},
	{0xe0,0xd0},{0x35,0x40},{0x36,0xd9},{0x37,0x0a},
	{0x38,0x00},{0x39,0x2d},{0x3a,0x00},{0x16,0x38},
	{0x17,0x62},{0x3b,0x80},{0x3c,0x00},{0x48,0x08},
	{0x49,0xa8},{0x40,0x00},{0x4a,0x80},{0x4c,0x00},
	{0x55,0x10},{0x56,0x08},{0x57,0x08},{0x59,0x00},
	{0x73,0x01},{0x96,0xff},{0x94,0x00},{0xc9,0x00},
	{0x99,0x02},{0x9b,0x18},{0x9f,0x00},{0xa1,0x00},
	{0xa4,0x08},{0xa5,0x04},{0xa6,0x00},{0xa7,0x00},
	{0xa8,0x00},{0xa9,0x00},{0xaa,0x00},{0xab,0x40},
	{0xaf,0x06},{0xb9,0x00},{0xba,0x60},{0xbb,0x00},
	{0xde,0x9c},{0xe2,0x01},{0xe4,0x60},{0xfa,0x7d},
	{0x0a,0x00},{0x0b,0x0e},{0x0c,0x04},{0x0d,0x10},
	{0x14,0x02},{0x15,0x20},{0x01,0x00},{0x02,0x18},
	{0x03,0x00},{0x07,0x01},{0x08,0x22},{0x09,0x0a},
	{0x18,0xa8},{0x19,0x00},{0x1a,0x00},{0x1b,0x00},
	{0x1c,0x00},{0x1d,0x00},{0x1e,0x00},{0x1f,0x00},
	{0x20,0x00},{0x21,0x00},{0x22,0x08},{0x23,0x00},
	{0x24,0x00},{0x25,0x00},{0x26,0x00},{0x27,0x00},
	{0x28,0x00},{0x29,0x00},{0x2a,0x00},{0x2b,0x00},
	{0x2c,0x08},{0x2d,0x00},{0x2e,0x00},{0x2f,0x00},
	{0xc0,0x00},{0xc1,0x00},{0xc2,0x0f},{0xc3,0xff},
};

const std::vector<RegisterWrite> expected_mode = {
	{0x17, 0x62}, {0x3b, 0x40}, {0x3c, 0x04},
};
```

Also assert:

```cpp
assert(std::string(recipe.identity) == "menu_720p60");
assert(std::string(recipe.source_commit) ==
	"cc5eb4bfc4cb2887dd6ab8364bff7010d7c6978c");
assert(recipe.timing_words == expected_timing);
assert(Equal(recipe.adv_initialization, expected_initialization));
assert(Equal(recipe.adv_mode, expected_mode));
```

Add `video_recipe_test` to `TEST_BINS`, its direct compile rule, and its headers/sources to dependency tracking.

- [ ] **Step 2: Run the focused test and prove RED**

Run:

```sh
make build/tests/unit/video_recipe_test
```

Expected: compile failure because `native/video_recipe.hpp` and `Menu720p60Recipe()` do not exist.

- [ ] **Step 3: Implement the fixed recipe exactly**

Create the header with the interfaces above. In `video_recipe.cpp`, define function-local `static const std::vector` values containing exactly the test literals and return this stable object:

```cpp
static const VideoRecipe recipe = {
	"menu_720p60",
	"cc5eb4bfc4cb2887dd6ab8364bff7010d7c6978c",
	timing,
	initialization,
	mode,
};
return recipe;
```

Do not calculate PLL values, look up a mode, read configuration, or expose mutation methods.

- [ ] **Step 4: Run the focused test and prove GREEN**

Run:

```sh
make build/tests/unit/video_recipe_test
./build/tests/unit/video_recipe_test
```

Expected: `video_recipe_test` reports all exact-data assertions passed.

- [ ] **Step 5: Prove the test kills realistic recipe mutations**

Temporarily and separately change timing word `0x00dc` to `0x00db`, PLL word `0xe8f5` to `0xe8f4`, required ADV write `{0x9a,0x70}` to `{0x9a,0x71}`, and mode write `{0x3c,0x04}` to `{0x3c,0x03}`. Rebuild after each change and require the named recipe test to fail. Restore the exact recipe and rerun GREEN.

- [ ] **Step 6: Commit the recipe**

```sh
git add Makefile src/native/video_recipe.hpp src/native/video_recipe.cpp \
  tests/unit/video_recipe_test.cpp
git commit -m "feat: add fixed menu video recipe"
```

---

### Task 2: Add the bounded Linux I2C byte-register adapter

**Files:**
- Create: `src/native/linux/i2c.hpp`
- Create: `src/native/linux/i2c.cpp`
- Create: `tests/unit/i2c_test.cpp`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `native::Clock`, `mister::Error`, Linux i2c-dev when `__linux__` is defined.
- Produces:

```cpp
class I2c {
public:
	virtual ~I2c() {}
	virtual Error SelectFirst(std::uint8_t slave_address,
		std::uint8_t detection_register,
		std::uint64_t absolute_deadline_ms,
		std::string* selected_bus,
		std::uint8_t* detected_value) = 0;
	virtual Error ReadByte(std::uint8_t register_address,
		std::uint8_t* value,
		std::uint64_t absolute_deadline_ms) = 0;
	virtual Error WriteByte(std::uint8_t register_address,
		std::uint8_t value,
		std::uint64_t absolute_deadline_ms) = 0;
};

#if defined(MISTER_RUNTIME_TESTING)
class LinuxI2cTestOperations {
public:
	virtual ~LinuxI2cTestOperations() {}
	virtual int Open(const char* path, int flags) = 0;
	virtual int Close(int descriptor) = 0;
	virtual int SelectSlave(int descriptor, std::uint8_t address) = 0;
	virtual int ReadByteData(int descriptor, std::uint8_t address,
		std::uint8_t* value) = 0;
	virtual int WriteByteData(int descriptor, std::uint8_t address,
		std::uint8_t value) = 0;
};
#endif

class LinuxI2c final : public I2c {
public:
	LinuxI2c(Clock&);
#if defined(MISTER_RUNTIME_TESTING)
	LinuxI2c(Clock&, LinuxI2cTestOperations&);
#endif
	~LinuxI2c();
	LinuxI2c(const LinuxI2c&) = delete;
	LinuxI2c& operator=(const LinuxI2c&) = delete;
	Error SelectFirst(std::uint8_t, std::uint8_t, std::uint64_t,
		std::string*, std::uint8_t*) override;
	Error ReadByte(std::uint8_t, std::uint8_t*, std::uint64_t) override;
	Error WriteByte(std::uint8_t, std::uint8_t, std::uint64_t) override;
private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};
```

- [ ] **Step 1: Write the failing I2C contract tests**

Create a scripted `LinuxI2cTestOperations` in `tests/unit/i2c_test.cpp`. Add named tests proving:

```cpp
// first two candidates fail; bus 2 responds to detection register 0x41
assert(i2c.SelectFirst(0x39, 0x41, 1100, &bus, &power).ok());
assert(bus == "/dev/i2c-2");
assert(power == 0x40);
assert(operations.opened_paths == std::vector<std::string>({
	"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2"}));
assert(operations.selected_addresses ==
	std::vector<std::uint8_t>({0x39, 0x39}));
```

Also prove all of these independently:

- open uses `O_RDWR | O_CLOEXEC`;
- a failed open advances to the next bounded bus;
- a failed slave selection closes that candidate and advances;
- a failed detection read closes that candidate and advances;
- only the selected descriptor stays open until `LinuxI2c` destruction;
- no responding bus returns `io_failed` and leaves outputs unchanged;
- a null output, read/write before selection, or expired deadline returns `io_failed` without an operation call;
- selected `ReadByte` and `WriteByte` forward exact register/value bytes;
- read/write errors and destructor close errors do not produce a second descriptor owner;
- a second `SelectFirst` call returns `io_failed` instead of replacing a live selection;
- the scan never opens `/dev/i2c-3`.

Add the focused test binary to `TEST_BINS` and compile `i2c.cpp` with `-DMISTER_RUNTIME_TESTING`.

- [ ] **Step 2: Run the focused test and prove RED**

Run:

```sh
make build/tests/unit/i2c_test
```

Expected: compile failure because the I2C boundary is absent.

- [ ] **Step 3: Implement the adapter with one descriptor owner**

Use a private operations interface following `LinuxMmio`. On Linux, the production implementation must:

```cpp
int Open(const char* path, int flags) override { return open(path, flags); }
int SelectSlave(int fd, std::uint8_t address) override {
	return ioctl(fd, I2C_SLAVE, static_cast<unsigned long>(address));
}
```

Implement byte-data read/write with `I2C_SMBUS` and `I2C_SMBUS_BYTE_DATA`; reject a failed ioctl and copy a read value only after success. Put Linux-only headers and ioctl code under `#if defined(__linux__)`; the non-Linux production operations return failure while the test seam remains buildable.

`SelectFirst` must check `clock.NowMs() >= deadline` before each open/select/detect action, scan the literal array:

```cpp
const char* const buses[] = {
	"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2",
};
```

Close every rejected candidate immediately, store only the first descriptor whose select and detection read both succeed, and write outputs only after ownership is established. `ReadByte` and `WriteByte` check selection, outputs, and deadline before dispatch.

- [ ] **Step 4: Run the focused suite and prove GREEN**

Run:

```sh
make build/tests/unit/i2c_test
./build/tests/unit/i2c_test
```

Expected: all scan/lifetime/deadline/transfer tests pass.

- [ ] **Step 5: Run portability and mutation checks**

Run the ordinary host build on Linux, then on the available macOS checkout/runner. The macOS path must compile without Linux headers and a focused non-Linux construction check must return clear `io_failed` on selection. Separately mutate the scan bound to include bus 3, remove `O_CLOEXEC`, retain a failed candidate descriptor, skip the detection read, and dispatch after an expired deadline. Each mutation must fail a named I2C test; restore and rerun GREEN.

- [ ] **Step 6: Commit the I2C boundary**

```sh
git add Makefile src/native/linux/i2c.hpp src/native/linux/i2c.cpp \
  tests/unit/i2c_test.cpp
git commit -m "feat: add bounded ADV7513 I2C access"
```

---

### Task 3: Implement the menu video bring-up state machine

**Files:**
- Create: `src/native/video.hpp`
- Create: `src/native/video.cpp`
- Create: `tests/support/fake_i2c.hpp`
- Create: `tests/support/fake_i2c.cpp`
- Create: `tests/unit/video_test.cpp`
- Modify: `Makefile`

**Interfaces:**
- Consumes:

```cpp
CoreLoader::Probe(std::string*, std::uint64_t absolute_deadline_ms);
Spi::Exchange(std::uint8_t target,
	const std::vector<std::uint16_t>& request,
	std::vector<std::uint16_t>* response,
	std::uint64_t absolute_deadline_ms);
I2c::SelectFirst(std::uint8_t slave_address,
	std::uint8_t detection_register,
	std::uint64_t absolute_deadline_ms,
	std::string* selected_bus,
	std::uint8_t* detected_value);
I2c::ReadByte(std::uint8_t register_address,
	std::uint8_t* value,
	std::uint64_t absolute_deadline_ms);
I2c::WriteByte(std::uint8_t register_address,
	std::uint8_t value,
	std::uint64_t absolute_deadline_ms);
Clock::NowMs();
Menu720p60Recipe();
```

- Produces:

```cpp
struct VideoResult {
	Error error;
	std::string phase;
	std::string observed_core;
	std::string selected_bus;
	std::uint8_t power_before = 0;
	std::uint8_t power_after = 0;
	std::uint8_t link_status = 0;
};

class VideoBringup {
public:
	virtual ~VideoBringup() {}
	virtual VideoResult BringUp(const std::string& expected_core,
		std::uint64_t absolute_deadline_ms) = 0;
};

class MenuVideoBringup final : public VideoBringup {
public:
	MenuVideoBringup(CoreLoader&, Spi&, I2c&, Clock&, LogSink&,
		const VideoRecipe&);
	VideoResult BringUp(const std::string& expected_core,
		std::uint64_t absolute_deadline_ms) override;
};
```

- Test support produces a `FakeI2c` with ordered `Call` records, scripted select/read/write results, selected bus `/dev/i2c-1`, detection value, link-status queue, and `InitializationWrites()`/`ModeWrites()` accessors that partition the recorded writes at the timing event. It is never included in `LIB_SOURCES`.

- [ ] **Step 1: Write the failing success-order test**

Create `tests/unit/video_test.cpp` with one shared event vector used by a recording SPI and `FakeI2c`. Script `CoreLoader::Probe` to return `MENU`, detection register `0x41` to return `0x40`, the post-init `0x41` read to return `0x10`, and link register `0x42` to return `0x60`.

Require this exact high-level order:

```cpp
const std::vector<std::string> expected = {
	"spi:status_assert",
	"spi:probe",
	"i2c:select:/dev/i2c-1:0x39:0x41",
	"i2c:initialization",
	"i2c:read:0x41",
	"spi:timing",
	"i2c:mode",
	"spi:status_release",
	"i2c:read:0x42",
};
assert(events == expected);
```

Assert the literal reset/status and timing requests independently:

```cpp
const std::vector<std::uint16_t> asserted = {
	0x001e, 0x0001, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};
const std::vector<std::uint16_t> released = {
	0x001e, 0x0000, 0x0000, 0x0000, 0x0000,
	0x0000, 0x0000, 0x0000, 0x0000,
};
assert(spi.calls.front().request == asserted);
assert(spi.TimingCall().request == Menu720p60Recipe().timing_words);
assert(spi.calls[spi.calls.size() - 1].request == released);
assert(i2c.InitializationWrites() ==
	Menu720p60Recipe().adv_initialization);
assert(i2c.ModeWrites() == Menu720p60Recipe().adv_mode);
```

Assert the `VideoResult` contains `phase == "hdmi_verify"`, observed core `MENU`, bus `/dev/i2c-1`, power values `0x40` then `0x10`, status `0x60`, and no error. Assert every SPI and I2C call received the one caller-provided deadline.

- [ ] **Step 2: Add the complete failing error matrix**

Add named cases for each first-failure point. Every case must assert `ErrorCode::io_failed`, the exact phase, no later calls, and no status-release unless all prior mutation steps completed:

| Injected failure | Required phase | Last permitted action |
| --- | --- | --- |
| deadline before first exchange | `core_reset` | none |
| reset assertion exchange | `core_reset` | assertion attempt |
| probe transport/malformed identity | `core_probe` | probe attempt |
| identity `OTHER`, `Menu`, empty, or unterminated | `core_probe` | probe |
| no responding ADV bus | `hdmi_init` | bus 2 detection |
| any required initialization write | `hdmi_init` | that exact write |
| post-init `0x41` read | `hdmi_init` | that exact read |
| full timing SPI exchange | `video_timing` | timing attempt |
| any of three mode writes | `video_timing` | that exact mode write |
| software reset release | `core_release` | release attempt |
| link read transport error | `hdmi_verify` | link read |
| link bits `0x00`, `0x20`, or `0x40` through deadline | `hdmi_verify` | last bounded read |

Add link polling cases proving `0x00 -> 0x20 -> 0x40 -> 0x60` succeeds without any repeated reset, probe, initialization, timing, mode, or release write. Use a clock whose `NowMs()` advances predictably per check and prove expiry returns after a finite exact number of status reads.

Add log assertions for phases:

```text
core_reset
core_probe
hdmi_init
video_timing
core_release
hdmi_verify
```

Each failure log carries its direct `io_failed`; the success records contain observed `MENU`, and the final record is `hdmi_verify`.

- [ ] **Step 3: Run the focused test and prove RED**

Run:

```sh
make build/tests/unit/video_test
```

Expected: compile failure because `MenuVideoBringup` and `FakeI2c` are absent.

- [ ] **Step 4: Implement one linear state machine**

Implement `BringUp` without a retry/cleanup framework. Use small helpers that return a `VideoResult` immediately at the first failure. The production sequence is exactly:

```cpp
spi.Exchange(kUserIoTarget, asserted_status, nullptr, deadline);    // core_reset
core.Probe(&observed_core, deadline);                              // core_probe
if (observed_core != expected_core || expected_core != "MENU")
	return PhaseFailure("core_probe",
		{ErrorCode::io_failed, "unexpected menu core"}, result);
i2c.SelectFirst(0x39, 0x41, deadline, &selected_bus, &power_before);// hdmi_init
for (const RegisterWrite& write : recipe.adv_initialization)
	i2c.WriteByte(write.address, write.value, deadline);              // hdmi_init
i2c.ReadByte(0x41, &power_after, deadline);                         // hdmi_init
spi.Exchange(kUserIoTarget, recipe.timing_words, nullptr, deadline);// video_timing
for (const RegisterWrite& write : recipe.adv_mode)
	i2c.WriteByte(write.address, write.value, deadline);              // video_timing
spi.Exchange(kUserIoTarget, released_status, nullptr, deadline);    // core_release
do {
	if (clock.NowMs() >= deadline)
		return PhaseFailure("hdmi_verify",
			{ErrorCode::io_failed, "deadline exceeded"}, result);
	i2c.ReadByte(0x42, &link_status, deadline);                        // hdmi_verify
} while ((link_status & 0x60) != 0x60);
```

`PhaseFailure(const char* phase, const Error& cause, const VideoResult& partial)` is a private `video.cpp` helper: it copies the partial diagnostics, sets `phase`, preserves the nonempty direct `cause.message` while forcing `ErrorCode::io_failed`, writes the phase log, and returns. After every dependency line above, invoke it immediately when the returned `Error` is not OK. The polling loop performs only deadline checks and `0x42` reads. It never reopens the bus or repeats any mutation. Map all dependency failures and mismatches to `ErrorCode::io_failed` with a stable message naming the phase; do not add a public error code.

Write each phase record through the provided `LogSink` with operation `start`, empty system, and the observed core once known. The successful final record must identify `MENU`; detailed bus/register fields remain in `VideoResult` and may be emitted by the production stderr sink as part of the stable message text without entering the daemon protocol.

For the successful final `hdmi_verify` record, use `ErrorCode::none` and build the diagnostic in this exact field order so `StderrLogSink` preserves the approved evidence without treating it as failure:

```cpp
std::string("recipe=") + recipe.identity +
" bus=" + selected_bus +
" address=0x39 power_before=" + HexByte(power_before) +
" power_after=" + HexByte(power_after) +
" link_status=" + HexByte(link_status)
```

Define the private formatting helper exactly:

```cpp
std::string HexByte(std::uint8_t value)
{
	static const char digits[] = "0123456789abcdef";
	std::string output = "0x00";
	output[2] = digits[(value >> 4) & 0x0f];
	output[3] = digits[value & 0x0f];
	return output;
}
```

The success fixture asserts the exact message `recipe=menu_720p60 bus=/dev/i2c-1 address=0x39 power_before=0x40 power_after=0x10 link_status=0x60`. Failure records keep their direct phase error instead of this completion message.

- [ ] **Step 5: Run focused tests and prove GREEN**

Run:

```sh
make build/tests/unit/video_test
./build/tests/unit/video_test
```

Expected: the success order, exact wire data, full failure matrix, bounded polling, result diagnostics, and logging tests all pass.

- [ ] **Step 6: Perform the required mutation audit**

Make each mutation separately and require a named test failure: release before mode writes; accept core `OTHER`; change one timing word before dispatch; skip one required ADV initialization write; accept `(status & 0x20) != 0`; repeat initialization on a failed link predicate; and use a fresh deadline per phase. Restore the source and rerun the focused suite GREEN.

- [ ] **Step 7: Commit the video state machine**

```sh
git add Makefile src/native/video.hpp src/native/video.cpp \
  tests/support/fake_i2c.hpp tests/support/fake_i2c.cpp \
  tests/unit/video_test.cpp
git commit -m "feat: bring up fixed native idle video"
```

---

### Task 4: Make video success part of native idle admission

**Files:**
- Modify: `src/native/hardware.hpp`
- Modify: `src/native/hardware.cpp`
- Modify: `tests/unit/native_hardware_test.cpp`
- Modify: `src/linux/production_hardware.cpp`
- Modify: `Makefile`
- Modify: `scripts/check-active-tree.sh`
- Modify: `tests/active_tree_test.sh`
- Modify: `README.md`
- Modify: `ARCHITECTURE.md`
- Modify: `DEVELOPMENT.md`
- Modify: `docs/support-matrix.md`

**Interfaces:**
- Consumes: `VideoBringup::BringUp("MENU", absolute_deadline_ms)` and the existing runtime `HardwareResult`/`Runtime::Start()` mapping.
- Produces:

```cpp
struct NativeTimeouts {
	std::uint32_t program_ms = 30000;
	std::uint32_t core_io_ms = 10000;
	std::uint32_t video_ms = 10000;
};

NativeHardware(ArtifactOpener&, FpgaManager&, CoreLoader&,
	VideoBringup&, Clock&, LogSink&, std::string idle_rbf,
	NativeTimeouts);
```

- Production ownership becomes:

```text
LinuxMmio + SteadyClock -> LinuxFpgaManager + LinuxSpi
LinuxSpi -> CoreLoader
SteadyClock -> LinuxI2c
CoreLoader + LinuxSpi + LinuxI2c + SteadyClock + LogSink + recipe
  -> MenuVideoBringup
all of the above -> NativeHardware
```

- [ ] **Step 1: Write failing native idle integration tests**

Add a `RecordingVideo` implementing `VideoBringup` to `native_hardware_test.cpp`. Change the fixture to inject it. Replace the old two-event idle assertion with:

```cpp
assert(fixture.hardware.LoadIdle().error.ok());
assert(fixture.events == std::vector<std::string>({
	"open:" + fixture.idle,
	"program:" + fixture.idle,
	"video:MENU",
}));
assert(fixture.video.deadlines == std::vector<std::uint64_t>({10100}));
```

Add named cases proving:

- preflight failure calls neither FPGA nor video and returns `mutation_attempted=false`;
- FPGA failure never calls video and preserves the FPGA mutation flag;
- video failure returns `io_failed`, `mutation_attempted=true`, and its observed core;
- successful idle returns `mutation_attempted=true` and observed core `MENU`;
- `Launch` and `LoadDevelopmentRBF` never call video;
- the existing `Runtime::Start()` attempted-idle failure regression still proves `reboot_required` plus `idle_failed`; no public state or error is added;
- production construction still fails on the compile-time deliberately missing RBF before touching `/dev/mem` or I2C.

- [ ] **Step 2: Run the native hardware tests and prove RED**

Run:

```sh
make build/tests/unit/native_hardware_test build/tests/unit/runtime_test
./build/tests/unit/native_hardware_test
./build/tests/unit/runtime_test
```

Expected: compile or assertion failure because `NativeHardware` has no video dependency and idle stops after FPGA programming.

- [ ] **Step 3: Integrate video only into `LoadIdle()`**

Store `VideoBringup& video_`. After successful FPGA programming, call:

```cpp
const VideoResult video = video_.BringUp("MENU",
	Deadline(clock_, timeouts_.video_ms));
if (!video.error.ok())
	return {CoreIoError(video.error), true, video.observed_core};
return {{}, true, video.observed_core};
```

Do not call video after a failed program. Do not call it from `Launch` or `LoadDevelopmentRBF`. Do not perform cleanup or reprogramming on a video error; the outer runtime already maps an attempted startup failure to `idle_failed` and `reboot_required`.

- [ ] **Step 4: Wire the production component**

In `ProductionHardware`, construct members in declaration order:

```cpp
native::PosixArtifactOpener opener_;
native::LinuxMmio mmio_;
SteadyClock clock_;
native::LinuxFpgaManager fpga_;
native::LinuxSpi spi_;
native::CoreLoader core_;
native::LinuxI2c i2c_;
native::MenuVideoBringup video_;
native::NativeHardware hardware_;
```

Initialize `i2c_(clock_)`, `video_(core_, spi_, i2c_, clock_, log, native::Menu720p60Recipe())`, then inject `video_` into `NativeHardware`. Keep the unavailable-hardware path unchanged.

- [ ] **Step 5: Extend build and archive closure exactly**

Add these three production sources to `LIB_SOURCES`:

```make
src/native/video_recipe.cpp
src/native/video.cpp
src/native/linux/i2c.cpp
```

Add all three focused tests to `TEST_BINS`, update direct rules and dependency headers, and extend `archive-audit` so the only canonical archive contains exactly 12 production members. Add `video_recipe.o`, `video.o`, and `i2c.o` to required members and allowed source cases. Add `src/native/linux/i2c.o` to the exact raw-I/O owner list; no other new raw owner is permitted. Add dependency checks proving `hardware.d` includes `video.hpp`, `video.d` includes `video_recipe.hpp`, `core_loader.hpp`, `spi.hpp`, and `i2c.hpp`, and `production_hardware.d` includes the concrete I2C/video headers.

Extend the active-tree fixture so deleting any one new production member, adding a fake I2C source, restoring a Main video symbol, or linking `libi2c` fails for the intended diagnostic. Keep forbidden Main symbols including `user_io_` and fake symbols rejected.

- [ ] **Step 6: Update truthful repository documentation in the behavior commit**

Update `ARCHITECTURE.md` with the new production graph and the exact idle sequence. Update `README.md` and `DEVELOPMENT.md` to say the fixed menu video path is software-tested, while physical status is claimed only by later evidence from an exact pinned FogCast image. Keep `Hardware-supported systems: 0`, every game row `hardware: no`, and production profiles empty. Add an idle-baseline note to `docs/support-matrix.md` that does not turn it into a game-system row or hardware pass.

Do not claim visible HDMI, supported native idle hardware, game video, development video, or a supported system in this commit.

- [ ] **Step 7: Run focused integration tests and prove GREEN**

Run:

```sh
make build/tests/unit/native_hardware_test build/tests/unit/runtime_test
./build/tests/unit/native_hardware_test
./build/tests/unit/runtime_test
make archive-audit
make active-tree-test
```

Expected: idle ordering and failure mapping pass; launch/development remain unchanged; archive/active-tree gates recognize exactly the new production boundary.

- [ ] **Step 8: Commit production integration and truthful docs**

```sh
git add Makefile README.md ARCHITECTURE.md DEVELOPMENT.md \
  docs/support-matrix.md scripts/check-active-tree.sh tests/active_tree_test.sh \
  src/native/hardware.hpp src/native/hardware.cpp \
  src/linux/production_hardware.cpp tests/unit/native_hardware_test.cpp
git commit -m "feat: require video for native idle"
```

---

### Task 5: Close the runtime repository gates and independent review

**Files:**
- Verify: all Task 1-4 production, test, build, and documentation files
- Write outside Git: the task implementation report and immutable review brief under the FogCast milestone evidence directory
- Modify only if a gate or reviewer demonstrates a defect: the narrow owning file and its focused test

**Interfaces:**
- Consumes: the complete Task 1-4 branch and the repository's canonical checks.
- Produces: a clean, independently reviewed runtime branch whose host, sanitizer, deterministic archive, dependency invalidation, macOS, and pinned Arm results are recorded without a hardware claim.

- [ ] **Step 1: Run the complete clean host gate**

```sh
make clean
make -j4 all
make -j4 test
make sanitize
make tsan
make archive-audit
make active-tree-test
scripts/check-history.sh
git diff --check
```

Expected: all tests pass; the archive is deterministic and contains exactly the 12 production objects; no fake or Main mutation symbol is linked; dependency invalidation sees all new headers.

- [ ] **Step 2: Run the pinned Arm production gate**

```sh
runtime_target_bin=/home/deano/.cache/toolchains/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin
make clean
make target \
  TARGET_CXX="$runtime_target_bin/arm-none-linux-gnueabihf-g++" \
  TARGET_AR="$runtime_target_bin/arm-none-linux-gnueabihf-ar"
make BUILD_DIR=build/target archive-audit \
  CXX="$runtime_target_bin/arm-none-linux-gnueabihf-g++" \
  AR="$runtime_target_bin/arm-none-linux-gnueabihf-ar" \
  NM="$runtime_target_bin/arm-none-linux-gnueabihf-nm" \
  CXXFILT="$runtime_target_bin/arm-none-linux-gnueabihf-c++filt"
file build/target/mister-runtime build/target/libmister-runtime.a
"$runtime_target_bin/arm-none-linux-gnueabihf-readelf" -h \
  build/target/mister-runtime
"$runtime_target_bin/arm-none-linux-gnueabihf-readelf" -d \
  build/target/mister-runtime
```

Expected: ELF32 Arm EABI5 hard-float daemon, 12-member Arm archive, no external `libi2c`, and no fake/Main symbols.

- [ ] **Step 3: Restore and verify the host products**

```sh
make clean
make -j4 test
make archive-audit
git status --short
git diff --check
```

Expected: clean host GREEN and no uncommitted repository changes.

- [ ] **Step 4: Verify macOS build portability**

On the connected macOS checkout at the same commit, run:

```sh
make clean
make -j4 all
make -j4 test
```

Expected: Apple clang/BSD ar/Apple ld build and tests pass. The non-Linux production I2C path compiles and reports `io_failed` only if invoked; no Linux i2c header is required on macOS.

- [ ] **Step 5: Write the implementation evidence report**

Record branch base/head, every focused RED/GREEN, mutation result, all exact gate commands/results, host and Arm archive members, target ELF identity, macOS result, diff/status, and explicit `no physical hardware run` in:

```text
/home/deano/fes/FogCast-POC/.superpowers/sdd/2026-09-01-bootable-native-baseline/
  task-7-native-idle-video-implementation-report.md
```

- [ ] **Step 6: Request an independent frozen-range review**

Give the reviewer the approved design, this plan, the implementation report, exact base/head SHAs, and the frozen `git diff --stat` plus `git diff -U10`. Require findings sorted as Critical/Important/Minor and an explicit READY/NOT READY verdict. The reviewer must inspect:

- exact recipe bytes and provenance;
- Linux/non-Linux I2C ownership and descriptor lifetime;
- reset/probe/init/timing/mode/release/link ordering;
- one absolute deadline and link-poll-only repetition;
- idle-only integration and existing failure mapping;
- archive/raw-I/O/fake/Main-symbol guards;
- truthful zero-system documentation.

- [ ] **Step 7: Close all review findings with focused RED/GREEN rounds**

For every accepted finding, return it to the original owning task implementer, add a failing regression first, apply the smallest correction, rerun that focused suite plus the complete Step 1/2 gates, update the report, and amend only the owning commit. Repeat independent review until no Critical or Important issue remains and any deferred Minor item is explicitly accepted.

- [ ] **Step 8: Freeze the reviewed branch**

```sh
git status --short
git log --oneline --decorate main..HEAD
git diff --check main...HEAD
```

Expected: tracked-clean branch, coherent focused commits, and exact reviewed range ready for integration.

---

### Task 6: Merge runtime and pin the exact merge in FogCast

**Files:**
- Runtime PR: all reviewed commits from the feature branch
- Modify in `/home/deano/fes/FogCast-POC`:
  - `build/native-runtime.inputs.lock.toml`
  - `scripts/tests/native-runtime-inputs_test.sh`
  - `scripts/tests/native-runtime-smoke_test.sh`

**Interfaces:**
- Consumes: reviewed runtime feature HEAD and clean runtime/FogCast main branches.
- Produces: merged runtime main SHA, then a merged FogCast commit whose lock and two independent fixtures name that exact runtime merge.

- [ ] **Step 1: Open and review the runtime PR**

From the reviewed runtime feature worktree:

```sh
feature_branch=$(git branch --show-current)
test -n "$feature_branch"
git push -u origin "$feature_branch"
pr_body=$(printf '%s\n' \
  '## Summary' \
  '- add the immutable 720p60 menu recipe and provenance tests' \
  '- add bounded direct Linux I2C access and idle-only video admission' \
  '- keep production profiles empty and native game support at zero' \
  '' \
  '## Verification' \
  '- host, sanitizer, TSan, archive, active-tree, and history gates pass' \
  '- pinned Arm EABI5 hard-float build and archive audit pass' \
  '- macOS host build and tests pass' \
  '' \
  'Physical acceptance is pending the exact merged runtime pin and native image.')
gh pr create \
  --base main \
  --head "$feature_branch" \
  --title "feat: bring up native idle HDMI" \
  --body "$pr_body"
```

The PR body must summarize the fixed recipe, bounded I2C boundary, idle-only admission, tests, macOS result, Arm result, and `physical acceptance pending`. Request the independent PR review and fix accepted comments using the same focused RED/GREEN discipline.

- [ ] **Step 2: Merge runtime and freeze the merge identity**

```sh
feature_worktree=$(pwd -P)
feature_branch=$(git branch --show-current)
gh pr merge --merge --delete-branch
cd /home/deano/fes/libmister-runtime
git -C /home/deano/fes/libmister-runtime pull --ff-only origin main
runtime_merge=$(git -C /home/deano/fes/libmister-runtime rev-parse HEAD)
test "$runtime_merge" = \
  "$(git -C /home/deano/fes/libmister-runtime rev-parse origin/main)"
test -z "$(git -C /home/deano/fes/libmister-runtime status --porcelain)"
printf '%s\n' "$runtime_merge"
git worktree remove "$feature_worktree"
git branch -d "$feature_branch"
```

Expected: local main and origin/main are exact and clean. Record the full `runtime_merge`; do not pin the pre-merge feature SHA.

- [ ] **Step 3: Create the isolated FogCast pin branch and prove RED**

Create `build/pin-runtime-idle-video` from fresh FogCast main. Before changing the lock, update both focused fixtures to require `$runtime_merge`, then run:

```sh
fogcast_repo=/home/deano/fes/FogCast-POC
fogcast_pin=$fogcast_repo/.worktrees/pin-runtime-idle-video
git -C "$fogcast_repo" fetch origin main
git -C "$fogcast_repo" worktree add \
  -b build/pin-runtime-idle-video "$fogcast_pin" origin/main
cd "$fogcast_pin"
runtime_merge=$(git -C /home/deano/fes/libmister-runtime rev-parse origin/main)
task_tmp=$(cat "$fogcast_repo/.superpowers/sdd/2026-09-01-bootable-native-baseline/fogcast-tmpdir")
TMPDIR="$task_tmp" sh scripts/tests/native-runtime-inputs_test.sh
TMPDIR="$task_tmp" sh scripts/tests/native-runtime-smoke_test.sh
```

Expected: both fail against the old lock for the intended commit mismatch only.

- [ ] **Step 4: Change only the three runtime SHA lines and prove GREEN**

Replace the old runtime commit with `$runtime_merge` in exactly:

```text
build/native-runtime.inputs.lock.toml
scripts/tests/native-runtime-inputs_test.sh
scripts/tests/native-runtime-smoke_test.sh
```

Keep the idle repository, commit, path, SHA-256, size, and install path byte-identical. Run:

```sh
runtime_merge=$(git -C /home/deano/fes/libmister-runtime rev-parse origin/main)
TMPDIR="$task_tmp" sh scripts/tests/native-runtime-inputs_test.sh
TMPDIR="$task_tmp" sh scripts/tests/native-runtime-smoke_test.sh
LIBMISTER_RUNTIME_DIR=/home/deano/fes/libmister-runtime \
  scripts/verify-native-runtime-inputs.sh
git diff --check
git diff -- build/native-runtime.inputs.lock.toml \
  scripts/tests/native-runtime-inputs_test.sh \
  scripts/tests/native-runtime-smoke_test.sh
```

Expected: both fixtures and strict real-checkout verifier pass; the diff is exactly three one-line SHA substitutions.

- [ ] **Step 5: Run the full FogCast software gate and commit**

```sh
make test
make vet
sh -n scripts/verify-native-runtime-inputs.sh \
  scripts/tests/native-runtime-inputs_test.sh \
  scripts/native-runtime-smoke.sh \
  scripts/tests/native-runtime-smoke_test.sh
git diff --check
git add build/native-runtime.inputs.lock.toml \
  scripts/tests/native-runtime-inputs_test.sh \
  scripts/tests/native-runtime-smoke_test.sh
git commit -m "build: pin native idle video runtime"
```

- [ ] **Step 6: Review, merge, and clean the FogCast pin PR**

Push `build/pin-runtime-idle-video`, open a PR to FogCast main, and request an independent review of the exact three-line range:

```sh
pin_branch=$(git branch --show-current)
test "$pin_branch" = build/pin-runtime-idle-video
git push -u origin "$pin_branch"
pin_body=$(printf '%s\n' \
  '## Summary' \
  '- pin the exact merged native idle video runtime' \
  '- keep the locked menu RBF identity byte-identical' \
  '' \
  '## Verification' \
  '- focused lock and native smoke fixtures pass' \
  '- strict real runtime checkout verification passes' \
  '- full make test and make vet pass')
gh pr create \
  --base main \
  --head "$pin_branch" \
  --title "build: pin native idle video runtime" \
  --body "$pin_body"
```

After the independent reviewer returns READY:

```sh
gh pr merge --merge --delete-branch
git -C /home/deano/fes/FogCast-POC pull --ff-only origin main
fogcast_merge=$(git -C /home/deano/fes/FogCast-POC rev-parse HEAD)
runtime_merge=$(git -C /home/deano/fes/libmister-runtime rev-parse origin/main)
test -z "$(git -C /home/deano/fes/FogCast-POC status --porcelain)"
grep -F "commit = '$runtime_merge'" \
  /home/deano/fes/FogCast-POC/build/native-runtime.inputs.lock.toml
cd /home/deano/fes/FogCast-POC
git worktree remove \
  /home/deano/fes/FogCast-POC/.worktrees/pin-runtime-idle-video
git branch -d build/pin-runtime-idle-video
```

Record exact runtime and FogCast merge SHAs for Task 7.

---

### Task 7: Run native-first physical acceptance, then legacy proof

**Files:**
- Verify/build in: `/home/deano/fes/FogCast-POC`
- Verify source in: `/home/deano/fes/libmister-runtime`
- Create outside Git: a fresh run directory under the recorded task TMPDIR
- Update outside Git on failure or pass: `/home/deano/fes/FogCast-POC/.superpowers/sdd/2026-09-01-bootable-native-baseline/task-7a-report.md`
- Create only after every physical gate passes:
  - `docs/hardware/native-idle-baseline.md`
- Modify only after every physical gate passes:
  - `README.md`
  - `docs/ARCHITECTURE.md`
  - `docs/DEVELOPMENT.md`
  - `docs/superpowers/specs/2026-08-31-libmister-runtime-design.md`

**Interfaces:**
- Consumes: exact merged runtime/FogCast identities, locked idle RBF, reviewed image scripts, target `192.168.10.239`, host API `127.0.0.1:8787`, ShadowCast `/dev/video0`, and the known-good legacy path.
- Produces: either (a) preserved failure evidence plus fresh legacy/Menu restoration and no truth commit, or (b) native idle HDMI physical evidence, fresh legacy/Sonic proof, and reviewed truth-document commit.

- [ ] **Step 1: Freeze authorities and create a fresh evidence root**

```sh
cd /home/deano/fes/FogCast-POC
task_tmp=$(cat .superpowers/sdd/2026-09-01-bootable-native-baseline/fogcast-tmpdir)
run="$task_tmp/task7-video-acceptance-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run"
container_runtime="$task_tmp/task5-docker"
runtime_dir=/home/deano/fes/libmister-runtime
fogcast_commit=$(git rev-parse HEAD)
runtime_commit=$(git -C "$runtime_dir" rev-parse HEAD)
test "$fogcast_commit" = "$(git rev-parse origin/main)"
test "$runtime_commit" = "$(git -C "$runtime_dir" rev-parse origin/main)"
test -z "$(git status --porcelain)"
test -z "$(git -C "$runtime_dir" status --porcelain)"
grep -F "commit = '$runtime_commit'" build/native-runtime.inputs.lock.toml
```

Verify the wrapper is executable and contains only the approved `sudo -n /usr/bin/docker "$@"` execution. Record wrapper hash, Docker server version, lock bytes, idle cache SHA/size, git identities, current Pi boot/health/Main/FIFO/`CORENAME=MENU`, read-only ROM mount, and ShadowCast MJPG 1920x1080/30 capabilities before any mutation.

- [ ] **Step 2: Run the fresh software baseline**

```sh
TMPDIR="$task_tmp" make test >"$run/01-make-test.log" 2>&1
make vet >"$run/02-make-vet.log" 2>&1
LIBMISTER_RUNTIME_DIR="$runtime_dir" \
  scripts/verify-native-runtime-inputs.sh \
  >"$run/03-native-inputs.log" 2>&1
```

Expected: all pass from clean merged sources. Stop before image/device work on any failure.

- [ ] **Step 3: Build native twice before rebuilding unaffected legacy images**

```sh
TMPDIR="$task_tmp" \
CONTAINER_RUNTIME="$container_runtime" \
LIBMISTER_RUNTIME_DIR="$runtime_dir" \
  make target-image-native >"$run/04-native-build.log" 2>&1
sha256sum build/output/target-image/native-dev/linux.img \
  >"$run/05-native-image.sha256"
```

Expected: the canonical target performs two independent native-dev builds and accepts only identical images. Preserve `reproducibility.txt` and the promoted digest.

- [ ] **Step 4: Run all native pre-hardware verifiers**

```sh
CONTAINER_RUNTIME="$container_runtime" \
  make target-image-native-verify >"$run/06-native-verify.log" 2>&1
CONTAINER_RUNTIME="$container_runtime" \
  make target-image-native-qemu-smoke >"$run/07-native-qemu.log" 2>&1
LIBMISTER_RUNTIME_DIR="$runtime_dir" \
  scripts/verify-native-runtime-inputs.sh \
  >"$run/08-native-inputs-final.log" 2>&1
```

Copy/hash the manifest and library report into the run inventory. Require the runtime and agent to be Arm artifacts, every runtime `NEEDED` library to exist, exactly one locked idle RBF, exact build-inputs bytes, and no Main/FIFO startup wiring.

- [ ] **Step 5: Deploy native and run the unchanged lifecycle smoke**

```sh
make target-image-deploy \
  TARGET_IMAGE=build/output/target-image/native-dev/linux.img \
  >"$run/09-native-deploy.log" 2>&1
make target-native-smoke >"$run/10-native-smoke.log" 2>&1
```

Expected: exactly one runtime and agent, no Main, no FIFO, exact installed inputs, initial ready/idle, one Stop, one reboot, changed boot ID, and fresh ready/idle under the reviewed default bounds. Do not change timeouts or retry a failed mutation.

- [ ] **Step 6: Collect the direct native video diagnostics**

Use bounded SSH to preserve `/usr/share/mister-runtime/build-inputs`, executable paths, FPGA manager state, `/var/log/mister-runtime.log`, and `/var/log/mister-agent.log`. Native does not create or rely on Main's `/tmp/CORENAME`; use the runtime's named `core_probe` record as the authority. Require:

```text
runtime core_probe record has core=MENU
one selected bus from /dev/i2c-0..2 at address 0x39
ADV 0x41 before and after initialization recorded
recipe menu_720p60 completed
ADV 0x42 has both 0x20 and 0x40 set
runtime state idle
```

Any missing identity, phase, register predicate, process identity, or link result is a failure even when the API reports idle.

- [ ] **Step 7: Capture exactly five count-bounded HDMI frames**

```sh
capture_dir=$(mktemp -d \
  build/output/target-image/native-dev/evidence.XXXXXX)
{
  v4l2-ctl --list-devices
  v4l2-ctl --device /dev/video0 --all
  v4l2-ctl --device /dev/video0 --list-formats-ext
} >"$capture_dir/v4l2-video0-report.txt"
timeout 20s ffmpeg -hide_banner -loglevel error \
  -f v4l2 -input_format mjpeg -video_size 1920x1080 -framerate 30 \
  -i /dev/video0 -vf fps=1 -frames:v 5 -strftime 1 -y \
  "$capture_dir/idle-%Y%m%dT%H%M%S.png" \
  2>"$capture_dir/ffmpeg.stderr"
frame_count=$(find "$capture_dir" -maxdepth 1 -type f \
  -name 'idle-*.png' | wc -l)
test "$frame_count" -eq 5
set -- "$capture_dir"/idle-*.png
test "$#" -eq 5
sha256sum "$capture_dir/v4l2-video0-report.txt" \
  "$capture_dir"/ffmpeg.stderr "$capture_dir"/idle-*.png \
  >"$capture_dir/capture-sha256.txt"
```

The external timeout is only a hang bound; frame count is the completion condition. Open all five PNGs individually with the image viewer. Require a stable present HDMI link and consistent geometry in every frame. Black pixels are acceptable only if the capture device still reports a stable signal; any `No HDMI Signal`, corrupt geometry, missing frame, or unstable mode fails.

- [ ] **Step 8: Apply the mandatory failure branch when needed**

On any failure in Steps 1-7: stop immediately, collect only read-only diagnostics, hash an evidence inventory, deploy the newly built legacy dev image if it already exists or the last reviewed fresh legacy dev otherwise, require target ready/host idle/Main/FIFO/`CORENAME=MENU`, append the exact failure to `/home/deano/fes/FogCast-POC/.superpowers/sdd/2026-09-01-bootable-native-baseline/task-7a-report.md`, and make no runtime/FogCast truth commit. Diagnose the named phase before changing code.

- [ ] **Step 9: Only after native HDMI passes, rebuild and verify legacy prod/dev**

```sh
TMPDIR="$task_tmp" CONTAINER_RUNTIME="$container_runtime" \
  make target-images >"$run/11-legacy-build.log" 2>&1
CONTAINER_RUNTIME="$container_runtime" \
  make target-image-verify >"$run/12-legacy-verify.log" 2>&1
sha256sum build/output/target-image/prod/linux.img \
  build/output/target-image/dev/linux.img \
  >"$run/13-legacy-images.sha256"
```

Expected: two independent builds per variant, identical accepted pairs, and both structural verifiers pass from the same final FogCast commit.

- [ ] **Step 10: Restore fresh legacy dev and run Sonic 2**

```sh
make target-image-deploy \
  TARGET_IMAGE=build/output/target-image/dev/linux.img \
  >"$run/14-legacy-deploy.log" 2>&1
game_id=$(curl --fail --silent --show-error --get \
  --data-urlencode 'q=Sonic the Hedgehog 2' \
  http://127.0.0.1:8787/api/v1/games | \
  python3 -c 'import json,sys; games=json.load(sys.stdin)["games"]; assert games; print(games[0]["id"])')
make target-smoke GAME_ID="$game_id" EXPECTED_CORE=MegaDrive \
  >"$run/15-sonic-legacy-smoke.log" 2>&1
```

Expected: fresh legacy target becomes ready, Sonic 2 launches, core becomes `MegaDrive`, Stop succeeds, and core returns to `MENU`. Leave the Pi on fresh legacy dev/Menu.

- [ ] **Step 11: Write truth documents only after every prior gate passes**

Create an isolated documentation worktree from the exact FogCast merge used for the run:

```sh
fogcast_repo=/home/deano/fes/FogCast-POC
docs_worktree=$fogcast_repo/.worktrees/native-idle-hdmi-acceptance
git -C "$fogcast_repo" worktree add \
  -b docs/native-idle-hdmi-acceptance "$docs_worktree" main
cd "$docs_worktree"
```

Create `docs/hardware/native-idle-baseline.md` with date, full FogCast/runtime merges, native/prod/dev image hashes, idle source/hash/size, native boot IDs, installed binary hashes, process/FIFO result, core identity, selected I2C bus/address, ADV `0x41`/`0x42`, recipe identity, lifecycle Stop/reboot result, V4L2 report hash, all five frame names/hashes/inspection result, and Sonic game ID/core/Stop/Menu result.

Update the four listed FogCast truth documents to state exactly:

```text
legacy prod/dev = current game-capable path
native-dev = hardware-tested idle 720p60 HDMI baseline
native game systems = 0
native game and development-RBF video = unsupported/unaccepted
next milestone = Mega Drive vertical slice
```

Do not commit binary images, captures, logs, ROM paths, passwords, or temporary inventories.

- [ ] **Step 12: Verify, commit, independently review, and merge truth docs**

```sh
make test
make vet
git diff --check
git status --short
git add README.md docs/ARCHITECTURE.md docs/DEVELOPMENT.md \
  docs/hardware/native-idle-baseline.md \
  docs/superpowers/specs/2026-08-31-libmister-runtime-design.md
git commit -m "docs: record native idle HDMI acceptance"
```

Request an independent review that checks every support claim against the frozen evidence inventory and verifies that native game support remains zero. Fix accepted comments and rerun tests. After READY:

```sh
docs_branch=$(git branch --show-current)
git push -u origin "$docs_branch"
docs_body=$(printf '%s\n' \
  '## Summary' \
  '- record the physically accepted native 720p60 idle HDMI baseline' \
  '- retain zero native game systems and the legacy game path' \
  '' \
  '## Evidence' \
  '- native lifecycle, ADV link, and five-frame HDMI gates pass' \
  '- fresh legacy Sonic 2 launch, Stop, and MENU return pass')
gh pr create \
  --base main \
  --head "$docs_branch" \
  --title "docs: record native idle HDMI acceptance" \
  --body "$docs_body"
gh pr merge --merge --delete-branch
cd /home/deano/fes/FogCast-POC
git pull --ff-only origin main
git worktree remove \
  /home/deano/fes/FogCast-POC/.worktrees/native-idle-hdmi-acceptance
git branch -d docs/native-idle-hdmi-acceptance
git status --short
```

Finally recheck that the Pi remains fresh legacy dev/Menu.
