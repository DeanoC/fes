// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"
#include "native/core_driver.hpp"

#include <cstdint>
#include <ctime>
#include <mutex>
#include <vector>

namespace mister {
namespace native {

class Clock;
class Mmio;
class ComputerMediaSnapshot;

struct MediaStreamInfo {
	std::uint32_t minimum = 0;
	std::uint32_t maximum = 0;
	std::uint16_t chunk_bytes = 0;
};

class FesGp final {
public:
	FesGp(Mmio&, Clock&);
	void BeginSession();
	bool Poisoned();
	// Sample a stable ACK, align the host toggle to it, and drop poison.
	// Does not issue a command. Failure leaves the mailbox poisoned.
	Error Realign(std::uint64_t absolute_deadline_ms);
	std::uint64_t NowMs() const;
	// Poll the runtime clock until `absolute_ms`. Equal samples are a coarse
	// millisecond clock: yield one wall millisecond and sample again. A clock
	// that stays flat for the whole wait, a clock that steps backwards, or a
	// deadline that arrives first returns an I/O error and issues no command.
	Error WaitUntilMs(std::uint64_t absolute_ms, std::uint64_t absolute_deadline_ms);
	Error Exchange(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t absolute_deadline_ms,
		std::uint16_t* response);
	Error Identify(const CoreDescriptor&, std::uint64_t absolute_deadline_ms,
		bool* safe_to_quiesce = nullptr, std::uint32_t* observed_capabilities = nullptr);

private:
	// Caller holds mutex_. Read only; never reissues the outstanding request.
	Error ReadSettledResponse(bool acknowledged, std::uint64_t deadline,
		std::uint32_t* response);
	Mmio& mmio_;
	Clock& clock_;
	std::mutex mutex_;
	bool request_toggle_ = false;
	bool poisoned_ = false;
};

class FesGpCoreDriver final : public CoreDriver {
public:
	explicit FesGpCoreDriver(FesGp&);
	void BeginSession() override;
	Error CaptureData(
		const CoreDriverContext&, std::uint64_t, std::vector<std::uint16_t>*) override;
	Error RestoreData(
		const CoreDriverContext&, const std::vector<std::uint16_t>&, std::uint64_t) override;
	Error ResumeData(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Quiesce(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult Identify(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult NeutralizeButtons(const CoreDriverContext&, std::uint64_t) override;
	CoreDriverResult SetButtons(const CoreDriverContext&, std::uint16_t,
		std::uint64_t) override;
	CoreDriverResult Start(const CoreDriverContext&, std::uint64_t) override;
	Error SetKeyboardMatrix(std::uint64_t matrix, std::uint64_t deadline);
	Error SetController(std::uint8_t port, std::uint16_t buttons,
		std::uint16_t keypad, std::uint64_t deadline);
	// Launch-time primary bind: Quiesce (hold reset) → begin/data/commit → release.
	Error LoadMedia(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	// Mid-session replace: begin/data/commit while execution stays released.
	Error LoadMediaLive(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	// Mid-session eject: media begin with eject index and argument 0.
	// A core that rejects that index (error 2) is retried with control-index
	// begin and argument 0. Error 3 on that command is invalid argument:
	// sealed golden cores reject argument 0 and have no media_busy sample.
	// Clear waits until the runtime clock advances across the longest $0347
	// copy before a minimum-length begin, which is what drops readiness.
	// A repeated millisecond is not a stall. If the clock stays flat or the
	// wait does not fit in the deadline, the mailbox is left unchanged and
	// the result is busy. Execution stays released: reset would restart BASIC.
	Error ClearMedia(std::uint64_t deadline);
	Error LoadFirmware(const std::vector<std::uint8_t>& bytes, std::uint64_t deadline);
	Error StreamInfo(MediaStreamInfo*) const;
	Error LoadMediaStream(const ComputerMediaSnapshot&, Clock&, std::uint64_t deadline);
	Error AbortMediaStream(std::uint64_t deadline);
	// Word 7, and word 16 in bits 16..31 for a fes.computer 1.1 identity.
	std::uint32_t observed_capabilities() const { return observed_capabilities_; }

	// fes.computer. Rows that equal the last acknowledged state are not
	// written; after Start every row is written once, in ascending order.
	Error SetKeyboardHid(const KeyboardHidRows& rows, std::uint64_t deadline);
	Error SendMouseRelative(std::int16_t dx, std::int16_t dy, std::uint8_t buttons,
		std::uint64_t deadline);
	Error CaptureMedia(std::uint8_t unit, Clock&, std::uint64_t deadline, std::vector<unsigned char>*);
	Error CaptureZx81Tape(Clock&, std::uint64_t deadline, std::vector<unsigned char>*);
	Error AcknowledgeZx81Tape(std::uint64_t deadline);
	Error EnsureZx81TapeSaved(std::uint64_t deadline);
	Error ResumeMedia(std::uint8_t unit, std::uint64_t deadline);
	Error RefreshMediaState(std::uint8_t unit, std::uint64_t deadline);
	Error MarkMediaSaved(std::uint8_t unit, std::uint64_t deadline);
	bool MediaWriteCapable(std::uint8_t unit) const;
	bool TapeWriteCapable() const;
    bool MediaGeometryCapable(std::uint8_t unit) const;
	bool home_computer() const { return home_computer_; }
	// fes.media.ata-disk 1.0 on unit 0. The image lives in the HPS DDR window;
	// the mailbox carries control, geometry, the CRC and the dirty map.
	struct AtaDiskInfo {
		std::uint32_t minimum = 0, maximum = 0;
		std::uint16_t chunk_sectors = 0, state = 0, flags = 0, epoch = 0;
	};
	bool AtaDiskCapable() const { return ata_disk_; }
	Error ReadAtaDiskInfo(std::uint64_t deadline, AtaDiskInfo*);
	Error AtaDiskControl(std::uint16_t argument, std::uint64_t deadline);
	// DiskCommit words 0..7 and Commit, then DiskInfo until the core's CRC
	// pass ends: ready, or a CRC mismatch that leaves the unit loading.
	Error CommitAtaDisk(std::uint32_t sectors, std::uint16_t cylinders, std::uint16_t heads,
		std::uint16_t sectors_per_track, std::uint32_t crc, std::uint64_t deadline);
	// After Freeze: the ascending chunk indices of the dirty map.
	Error ReadAtaDirty(std::uint64_t deadline, std::vector<std::uint32_t>* chunks);
	// fes.rtc.wall-clock 1.0: the four words of a local time, while held.
	bool WallClockCapable() const { return wall_clock_; }
	Error SetWallClock(const std::tm& local, std::uint64_t deadline);
	// Units of declared media interfaces with their last live MediaInfo state.
	const std::vector<MediaUnitCapability>& media_units() const { return media_units_; }
	// Live transfer while execution stays released: MediaInfo, Begin, 512-byte
	// Chunk/Data, Commit, then MediaInfo must report ready. Any failure after the
	// first mailbox exchange ejects the unit once within `cleanup_ms`; an
	// ambiguous mutation is never repeated.
	Error InsertMedia(std::uint8_t unit, const ComputerMediaSnapshot&, Clock&,
		std::uint64_t deadline, std::uint32_t cleanup_ms);
	Error EjectMedia(std::uint8_t unit, std::uint64_t deadline);

private:
	CoreDriverResult Gameplay(std::uint16_t, std::uint64_t);
	CoreDriverResult NeutralizeKeyboard(std::uint64_t deadline);
	Error NeutralizeControllers(std::uint64_t deadline);
	Error DataControl(std::uint16_t, std::uint64_t);
	bool ValidData(const std::vector<std::uint16_t>&) const;
	// The persistence opcodes and layout of the identified registry.
	struct DataLayout {
		std::uint8_t control, read, write, info;
		std::uint16_t word_count, tag;
		const char* layout;
	};
	DataLayout data_{};
	Error StreamCommand(std::uint8_t opcode, std::uint8_t index,
		std::uint16_t argument, std::uint64_t deadline);
	Error TransferMediaBlob(const std::vector<std::uint8_t>& bytes,
		std::uint64_t deadline, bool hold_reset);
	Error MediaBusyOrIo(const Error& error) const;
	Error RecoverPoisonedMediaLink(std::uint64_t deadline);
	struct LiveMediaInfo {
		std::uint32_t minimum = 0;
		std::uint32_t maximum = 0;
		std::uint16_t chunk_bytes = 0;
		std::uint16_t state = 0;
	};
	MediaUnitCapability* FindMediaUnit(std::uint8_t unit);
	Error ComputerCommand(std::uint8_t opcode, std::uint8_t index, std::uint16_t argument,
		std::uint64_t deadline, bool* mutation_started = nullptr,
		const char* phase = "input");
	Error ReadMediaInfo(std::uint8_t unit, std::uint64_t deadline, LiveMediaInfo*);
	Error ReadMediaState(std::uint8_t unit, std::uint64_t deadline, std::uint16_t* state);
	Error RecoverComputerMailbox(std::uint64_t deadline);
	Error TransferMediaUnit(MediaUnitCapability&, const ComputerMediaSnapshot&, Clock&,
		std::uint64_t deadline, bool* mutation_started);
	Error AbandonMediaUnit(MediaUnitCapability&, std::uint64_t deadline);
	FesGp& gp_;
	CoreDescriptor identified_{};
	bool have_identity_ = false;
	bool persistence_verified_ = false;
	bool reset_held_ = true;
	bool freeze_attempted_ = false;
	bool computer_ = false;
	bool media_ = false;
	bool firmware_ = false;
	bool application_ = false;
	bool gamepad_ = false;
	bool controller_ports_ = false;
	bool keypad_ports_ = false;
	std::uint32_t observed_capabilities_ = 0;
	MediaStreamInfo stream_info_;
	bool stream_verified_ = false;
	bool stream_pending_ = false;
	bool home_computer_ = false;
	bool keyboard_hid_ = false;
	bool mouse_relative_ = false;
	bool floppy_write_ = false;
	bool tape_write_ = false;
	bool wall_clock_ = false;
	bool ata_disk_ = false;
	KeyboardHidRows hid_rows_{};
	bool hid_rows_known_ = false;
	std::vector<MediaUnitCapability> media_units_;
};

} // namespace native
} // namespace mister
