// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <array>
#include <cstdint>
#include <memory>
#include <string>
#include <utility>
#include <vector>

namespace mister {

enum class ErrorCode {
	none,
	invalid_request,
	unsupported_protocol,
	unknown_system,
	missing_media,
	busy,
	program_failed,
	core_mismatch,
	io_failed,
	idle_failed,
	save_failed,
	invalid_package,
	unsupported_target,
	unsupported_programming_profile,
	unsupported_abi,
	unsupported_interface,
	corrupt_data,
	incompatible_data,
	stale_revision,
};

enum class State {
	idle,
	starting,
	running_development,
	reboot_required,
};

enum class Execution { none, development };

struct Error {
	Error() = default;
	Error(ErrorCode code_value, std::string message_value,
		std::string phase_value = {}, std::string expected_value = {},
		std::string observed_value = {})
		: code(code_value), message(std::move(message_value)),
		  phase(std::move(phase_value)), expected(std::move(expected_value)),
		  observed(std::move(observed_value)) {}
	ErrorCode code = ErrorCode::none;
	std::string message;
	std::string phase;
	std::string expected;
	std::string observed;
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

struct CoreMetadata {
	std::string id;
	std::string name;
	std::string description;
	std::string version;
	std::string system;
};

struct CoreTarget {
	std::string platform;
	std::string device;
	std::string programming_profile;
};

struct CorePayload {
	std::string file;
	std::uint64_t size = 0;
	std::string sha256;
};

struct CoreRom {
	std::string id;
	std::string role;
	std::uint64_t source_size = 0;
	std::string file;
	std::uint64_t size = 0;
	std::string sha256;
};

struct CoreRomRequirement {
	std::string id;
	std::string role;
	std::uint64_t source_size = 0;
	std::uint64_t source_offset = 0;
};

struct CoreRomMap {
	std::string file;
	std::uint64_t size = 0;
	std::string sha256;
};

struct VersionedContract {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
};

struct CoreInterface {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
	bool required = false;
};

struct CoreBuild {
	std::string id;
	std::string repository;
	std::string revision;
	std::string recipe_sha256;
	std::string toolchain;
};

struct CoreDescriptor {
	std::uint16_t format = 0;
	CoreMetadata core;
	CoreTarget target;
	CorePayload payload;
	CoreRom rom;
	std::vector<CoreRomRequirement> roms;
	CoreRomMap rom_map;
	VersionedContract abi;
	std::vector<CoreInterface> interfaces;
	CoreBuild build;
};

struct SupportedInterface {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
};

struct SupportedABI {
	std::string id;
	std::uint16_t major = 0;
	std::uint16_t minor = 0;
	std::vector<SupportedInterface> interfaces;
};

struct MediaStreamCapability {
	SupportedInterface interface;
	std::uint32_t min_bytes = 0;
	std::uint32_t max_bytes = 0;
	std::uint32_t chunk_bytes = 0;
};

enum class MediaUnitState { empty, loading, ready };
struct MediaDataBinding {
    std::string game_id, base_media_id;
    std::uint8_t unit = 0;
};

// Explicit library disk delivered while a ROM-linked ST is still reset-held.
struct InitialComputerMedia {
    std::string path, data_root;
    std::uint32_t size = 0;
    MediaDataBinding binding;
};

// One removable-media unit of an active fes.computer generation. Limits and
// state come from the unit's live MediaInfo, not from its declaration.
struct MediaUnitCapability {
	std::uint8_t unit = 0;
	SupportedInterface interface;
	std::uint32_t min_bytes = 0;
	std::uint32_t max_bytes = 0;
	std::uint32_t chunk_bytes = 0;
	MediaUnitState state = MediaUnitState::empty;
	// Persistent bindings may exist without an in-session save operation.
	bool saveable = true;
    // Explicit library binding only; ordinary development insertion omits it.
    std::string persistence_mode = "volatile", game_id, base_media_id, revision = "absent";
};

struct Capabilities {
	std::uint32_t rom_linking = 0;
	std::vector<std::string> programming_profiles;
	std::vector<SupportedABI> abis;
	std::vector<SupportedInterface> active_interfaces;
	// Empty interface ID means absent. This is observed active-session data,
	// not the compiled driver declaration registry above.
	MediaStreamCapability media_stream;
	// Observed units of the active fes.computer generation, ascending by unit.
	std::vector<MediaUnitCapability> media_units;
};

// fes.keyboard.hid 1.0: rows 0..7 hold Keyboard/Keypad usages 16r..16r+15,
// row 8 the eight modifier usages 0xe0..0xe7 in bits 0..7.
using KeyboardHidRows = std::array<std::uint16_t, 9>;

struct ObservedIdentity {
	VersionedContract abi;
	std::string build_id;
};

struct CoreCompositionSlot {
	std::uint8_t slot = 0;
	std::string expansion_id;
};

struct CoreCompositionPart {
	std::string role, part_id;
};

// A single-socket composition names one expansion_id. A multi-slot
// composition instead lists one expansion per slot in ascending slot order.
struct CoreComposition {
	std::string id, package_id, expansion_id, shell_sha256, payload_sha256;
	std::uint64_t payload_size = 0;
	std::vector<CoreCompositionSlot> expansions;
	std::string layout;
	std::vector<CoreCompositionPart> parts;
};

struct CoreExpansionPath {
	std::uint8_t slot = 0;
	std::string path;
};

struct CorePartPath { std::string role, path; };

struct CoreCompositionRequest {
	std::string expansion_path, payload_path;
	std::vector<CoreExpansionPath> expansions;
	std::vector<CorePartPath> parts;
	CoreComposition composition;
};

struct CoreROMLink {
	std::string rom_id, map_sha256, source_sha256, programmed_sha256;
	std::uint64_t source_size = 0, programmed_size = 0;
};

struct CoreROMSource {
	std::string id, role, source_sha256;
	std::uint64_t source_size = 0;
};

struct CoreROMLinks {
	std::vector<CoreROMSource> sources;
	std::string map_sha256, programmed_sha256;
	std::uint64_t programmed_size = 0;
};

struct ActiveCorePackage {
	std::string package_id;
	CoreDescriptor descriptor;
	ObservedIdentity observed;
	CoreComposition composition;
	CoreROMLink rom_link;
	CoreROMLinks rom_links;
};

// fes.pc386.machine 1.0 settings, words 0..6 of its record.
struct Pc386MachineSettings {
	std::uint16_t preset = 0;
	std::uint16_t frequency = 0;
	std::uint16_t code_profile = 0;
	std::uint16_t turbo = 0;
	std::uint16_t sb_base = 0x220;
	std::uint16_t sb_irq = 7;
	std::uint16_t sb_dma = 1;
};

struct CoreData {
	std::string package_id;
	std::string core_id;
	VersionedContract layout;
	std::string mode = "volatile";
	std::string revision = "absent";
	std::uint16_t paddle_speed = 1;
	std::uint16_t best_rally = 0;
	// A per-game record (fes.computer) names its library game and keeps the
	// layout's words; a per-core Pong record leaves both empty.
	std::string game_id;
	std::vector<std::uint16_t> words;
};

// A fes.media.ata-disk launch: the staged immutable base image, the geometry
// its partition table was written with, and the media-data root that holds
// its game's chunk delta.
struct AtaDiskImage {
	std::string path;
	std::uint32_t size = 0;
	std::string base_media_id;
	std::string data_root;
	std::uint16_t cylinders = 0;
	std::uint16_t heads = 0;
	std::uint16_t sectors_per_track = 0;
};

// A next-launch settings change: Pong's paddle speed, or the pc386 settings.
struct CoreSettingsUpdate {
	std::uint16_t paddle_speed = 1;
	bool pc386 = false;
	Pc386MachineSettings machine;
};

struct CorePackageInspection {
	std::string package_id;
	CoreDescriptor descriptor;
	VersionedContract persistence_layout;
	bool compatible = false;
	Error compatibility_error;
};

struct MenuGeometry {
 std::uint32_t width=0,height=0,stride=0,frame_bytes=0,slot_bytes=0;
};
// A region of a menu frame, in pixels.
struct MenuRect {
 std::uint16_t x=0,y=0,w=0,h=0;
};
struct MenuDisplayInfo {
 MenuGeometry geometry;
 // Idle firmware scans XRGB (format 1). A session-display plane blends
 // premultiplied RGBA (format 2) and reports where the machine picture lands.
 std::uint16_t pixel_format=0;
 MenuRect picture;
 bool configured=false,enabled=false,pending=false,quiesced=false,faulted=false;
 std::uint32_t displayed_sequence=0,underflows=0;
 // Timing of the present that produced this info, for frame-time
 // diagnostics: writing back the client's pixels, the slot submit, and the
 // wait until scanout shows the new slot, and for a blitter frame the run
 // that drew it. Zero when not measured.
 std::uint32_t clean_us=0,blit_us=0,submit_us=0,scanout_wait_us=0;
};
struct MenuDisplayStatus {
 bool available=false;
 // A running-core plane binds both its independent display generation and
 // the gameplay generation. Idle MENU status omits these fields on the wire.
 bool session=false;
 std::uint64_t core_generation=0;
 std::string package_id;
 std::uint64_t generation=0;
 MenuGeometry geometry;
 // Session planes only: the machine picture inside the launcher frame, and
 // whether the launcher holds input. A shown plane may leave input with the
 // machine, as a surround does.
 MenuRect picture;
 bool launcher_focus=false;
 std::uint32_t displayed_sequence=0,underflows=0;
 // The grant also covers the blitter's command lists and texture heap, and
 // blitter_minor is the menu-blitter minor the firmware declares, which says
 // which ops it runs. Reported in the grant, not in status.
 bool blitter=false;
 // The peer must close its descriptor and unmap before committing each frame.
 // Allocation-backed adapters use this to permit recovery before the reply.
 bool frame_mapping=false;
 std::uint16_t blitter_minor=0;
 // A list frame has run on this blitter grant. The grant takes CPU frames
 // until then and only lists after. Not reported.
 bool blitted=false;
 Error error;
};
constexpr std::size_t kMenuWrittenMaxRects=16;
// Window bytes the client wrote, by window offset.
struct MenuSpan {
 std::uint32_t offset=0,bytes=0;
};
constexpr std::size_t kMenuUploadedMaxSpans=16;
// What a frame drew into its slot. A CPU frame names the regions it wrote
// (empty: the whole frame). A blitter frame names count commands from first
// in the list region, and the heap bytes it uploaded since its last frame.
struct MenuDraw {
 std::vector<MenuRect> written;
 bool blit=false;
 std::uint32_t first=0,count=0;
 std::vector<MenuSpan> uploaded;
};
class Runtime;
// One outstanding menu preparation: the client draws into slot of the
// granted window, then commits that slot and what it drew. Preparations on a
// generation count from 1, so a client whose last preparation was n knows
// another peer could have written the shared heap when this one is not n+1.
class MenuFrame final {
public:
 MenuFrame()=default;
 MenuFrame(const MenuFrame&)=delete;
 MenuFrame& operator=(const MenuFrame&)=delete;
 std::uint8_t slot() const {return slot_;}
 std::uint64_t preparation() const {return preparation_count_;}
 void SetDraw(MenuDraw draw) {draw_=std::move(draw);}
private:
 friend class Runtime;
 std::shared_ptr<void> preparation_;
 std::uint64_t generation_=0,preparation_count_=0;
 std::uint8_t slot_=0;
 MenuDraw draw_;
};

struct Status {
 MenuDisplayStatus menu_display;
	State state = State::starting;
	Execution execution = Execution::none;
	std::string system;
	std::string core;
	std::string package_id;
	std::string declared_core;
	Capabilities capabilities;
	ActiveCorePackage active_package;
	CoreData core_data;
	std::uint64_t generation = 0;
	Error error;
};

struct InputRecipe {
	std::uint8_t player_count = 0;
	std::uint16_t player_command = 0;
	std::uint16_t up = 0, down = 0, left = 0, right = 0;
	std::uint16_t a = 0, b = 0, c = 0, start = 0;
	std::uint16_t x = 0, y = 0, l = 0, r = 0, select = 0;
};

struct HardwareResult {
	Error error;
	bool mutation_attempted = false;
	std::string observed_core;
	// A failed execution release may have run the newly bound library disk.
	// Failure cleanup must save it or retain that owner without idle programming.
	bool bound_media_may_have_run = false;
};

struct HardwareFault {
	std::uint64_t generation = 0;
	Error error;
};

struct CorePackageInfo {
	std::string package_id;
	std::string declared_core;
	std::string system;
	CoreDescriptor descriptor;
};

class AdmittedCorePackage {
public:
	virtual ~AdmittedCorePackage() {}
	const CorePackageInfo& info() const { return info_; }
	virtual CoreComposition composition() const { return {}; }

protected:
	explicit AdmittedCorePackage(CorePackageInfo info)
		: info_(std::move(info)) {}

private:
	CorePackageInfo info_;
};

class HardwareFaultSink {
public:
	virtual ~HardwareFaultSink() = default;
	virtual void ReportHardwareFault(HardwareFault fault) = 0;
};

class Hardware {
public:
	virtual ~Hardware() {}
	virtual void SetFaultSink(HardwareFaultSink*) = 0;
	virtual HardwareResult LoadIdle() = 0;
 virtual HardwareResult ConfigureMenuPackage(const std::string&,const std::string&) {
  return {{ErrorCode::unsupported_interface,"menu display unavailable"},false,""};
 }
 virtual MenuDisplayStatus menu_display() const {return {};}
 virtual Error SetSessionDisplay(bool,bool) {
  return {ErrorCode::unsupported_interface,"session display unavailable","menu"};
 }
 // A new descriptor, owned by the caller, for the grant of both menu slots
 // and, with a blitter, its lists and heap.
 virtual Error ShareMenuWindow(int*) {
  return {ErrorCode::unsupported_interface,"menu display unavailable"};
 }
 // The slot scanout is not showing, which the next frame is drawn into.
 virtual Error MenuBackSlot(std::uint8_t*) const {
  return {ErrorCode::unsupported_interface,"menu display unavailable"};
 }
 // Writes back what the client wrote for the fabric, runs the blitter for a
 // blitter frame, then shows slot.
 virtual Error PresentMenuSlot(std::uint8_t,const MenuDraw&,MenuDisplayInfo*) {
  return {ErrorCode::unsupported_interface,"menu display unavailable"};
 }
	virtual Error FlushSave() { return {}; }
	// Fault retirement revokes asynchronous input before capturing bound media.
	virtual Error FlushFaultSave() { return FlushSave(); }
	// FlushSave may stop input before a persistence failure. RestoreInput
	// re-establishes the still-active generation after that proven pre-mutation
	// failure.
	virtual Error RestoreInput(std::uint64_t) { return {}; }
	virtual Error AdmitCoreComposition(const std::string&, const std::string&,
		const CoreCompositionRequest&, std::unique_ptr<AdmittedCorePackage>*)
	{
		return {ErrorCode::unsupported_protocol, "core composition loading is unavailable"};
	}
	virtual Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<AdmittedCorePackage>*)
	{
		return {ErrorCode::unsupported_protocol,
			"core package loading is unavailable"};
	}
	virtual Error InspectCorePackage(const std::string&, const std::string&,
		CorePackageInspection*)
	{
		return {ErrorCode::unsupported_protocol,
			"core package inspection is unavailable"};
	}
	// The game ID keys a per-game record; it is empty for per-core data.
	virtual Error PrepareCoreData(AdmittedCorePackage*, const std::string&, const std::string&, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Error RefreshCoreData(AdmittedCorePackage*, CoreData*)
	{
		return {};
	}
	// Binds the game's ATA disk: opens the image and its delta namespace and
	// reads the manifest, before programming.
	virtual Error PrepareAtaDisk(AdmittedCorePackage*, const std::string&, const AtaDiskImage&)
	{ return {ErrorCode::unsupported_interface, "ATA disk unavailable", "admission"}; }
	virtual Error PrepareInitialComputerMedia(AdmittedCorePackage*, const InitialComputerMedia&)
	{ return {ErrorCode::unsupported_interface, "initial computer media unavailable", "admission"}; }
	virtual Error RefreshInitialComputerMedia(AdmittedCorePackage*)
	{ return {ErrorCode::unsupported_interface, "initial computer media unavailable", "admission"}; }
	virtual Error AttachProgrammedBitstream(AdmittedCorePackage*, const std::string&,
		const std::string&)
	{
		return {ErrorCode::unsupported_protocol, "programmed bitstream is unavailable"};
	}
	virtual Error AttachROMBitstream(AdmittedCorePackage*, const std::string&, const CoreROMLink&)
	{ return {ErrorCode::unsupported_protocol, "ROM linking is unavailable"}; }
	virtual Error AttachROMsBitstream(AdmittedCorePackage*, const std::string&, const CoreROMLinks&)
	{ return {ErrorCode::unsupported_protocol, "two-source ROM linking is unavailable"}; }
	virtual Error RecheckProgrammedBitstream(AdmittedCorePackage*) { return {}; }
	virtual Error InspectCoreData(const std::string&, const std::string&, const std::string&,
		const std::string&, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Error UpdateCoreSettings(const std::string&, const std::string&, const std::string&,
		const std::string&, const std::string&, const CoreSettingsUpdate&, CoreData*)
	{
		return {ErrorCode::unsupported_interface, "core data unavailable"};
	}
	virtual Capabilities capabilities() const { return {}; }
	virtual HardwareResult LoadCore(std::unique_ptr<AdmittedCorePackage>,
		std::uint64_t)
	{
		return {{ErrorCode::unsupported_protocol,
			"core package loading is unavailable"}, false, ""};
	}
	virtual HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t)
	{
		return {{ErrorCode::unsupported_programming_profile,
			"contained development loading is unavailable", "compatibility"},
			false, ""};
	}
	virtual Error SetController(std::uint8_t, std::uint16_t, std::uint16_t)
	{
		return {ErrorCode::unsupported_interface, "controller ports are unavailable", "input"};
	}
	virtual Error SetComputerKeyboard(std::uint64_t)
	{
		return {ErrorCode::unsupported_interface,
			"computer keyboard is unavailable", "input"};
	}
	virtual Error LoadComputerMedia(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"computer media is unavailable", "request"};
	}
	virtual Error LoadComputerMediaLive(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"live computer media is unavailable", "request"};
	}
	virtual Error ClearComputerMedia()
	{
		return {ErrorCode::unsupported_interface,
			"clear computer media is unavailable", "request"};
	}
	virtual Error LoadComputerFirmware(const std::string&)
	{
		return {ErrorCode::unsupported_interface,
			"firmware slot is unavailable", "request"};
	}
	virtual Error LoadComputerMediaStream(const std::string&, std::uint32_t)
	{
		return {ErrorCode::unsupported_interface, "computer media stream is unavailable", "request"};
	}
	virtual Error SendMouseRelative(std::int16_t, std::int16_t, std::uint8_t)
	{
		return {ErrorCode::unsupported_interface, "relative mouse is unavailable", "input"};
	}
	virtual Error SetKeyboardHid(const KeyboardHidRows&)
	{
		return {ErrorCode::unsupported_interface, "keyboard HID is unavailable", "input"};
	}
	virtual Error InsertComputerMedia(std::uint8_t, const std::string&, std::uint32_t)
	{
		return {ErrorCode::unsupported_interface, "computer media units are unavailable", "request"};
	}
    virtual Error InsertLibraryComputerMedia(std::uint8_t, const std::string&, std::uint32_t,
        const std::string&, const MediaDataBinding&) {
        return {ErrorCode::unsupported_interface, "library media persistence is unavailable"};
    }
	virtual Error EjectComputerMedia(std::uint8_t)
	{
		return {ErrorCode::unsupported_interface, "computer media units are unavailable", "request"};
	}
	// Writes a completed BASIC SAVE image. An empty file means no SAVE is pending.
	virtual Error CaptureZx81Tape(const std::string&)
	{
		return {ErrorCode::unsupported_interface, "ZX81 tape capture is inactive", "request"};
	}
	virtual Error AcknowledgeZx81Tape()
	{
		return {ErrorCode::unsupported_interface, "ZX81 tape acknowledgement is inactive", "request"};
	}
};

class Runtime {
public:
	Runtime(Hardware&, LogSink&);
	~Runtime();
	Runtime(const Runtime&) = delete;
	Runtime& operator=(const Runtime&) = delete;
	// A selected MENU is the initial idle image. Empty arguments use the
	// board's splash recipe; they never program a foreign splash before MENU.
	Error Start(const std::string& menu_directory = {}, const std::string& menu_package_id = {});
 Error ConfigureMenuPackage(const std::string&,const std::string&);
 Error SetSessionDisplay(const std::string& expected_package_id,
  std::uint64_t expected_generation,bool visible,bool launcher_focus);
 // blitter reports whether the grant covers the blitter's lists and heap.
 Error ShareMenuWindow(std::uint64_t,int*,bool* blitter,std::uint16_t* blitter_minor,
  bool permit_frame_mapping=false,bool* frame_mapping=nullptr);
 Error BeginMenuFrame(std::uint64_t,std::unique_ptr<MenuFrame>*);
 Error PresentMenuFrame(std::uint64_t,MenuFrame&,MenuDisplayInfo*);
	Status status() const;
	Error LoadCore(const std::string& directory,
		const std::string& expected_package_id);
	// The game ID keys a fes.computer package's per-game record.
	Error LoadLibraryCore(const std::string&, const std::string&, const std::string&,
		const std::string& game_id = "", const AtaDiskImage* disk = nullptr);
	Error LoadComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&);
	Error LoadLibraryPartsCore(const std::string&, const std::string&, const std::string&, const CoreCompositionRequest&);
	Error InspectPartsCore(const std::string&, const std::string&, const CoreCompositionRequest&, CorePackageInspection*);
	Error LoadInitializedCore(const std::string&, const std::string&, const std::string&, const std::string&);
	Error LoadInitializedLibraryCore(const std::string&, const std::string&, const std::string&,
		const std::string&, const std::string&);
	Error LoadInitializedComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&,
		const std::string&, const std::string&);
	Error LoadROMCore(const std::string&, const std::string&, const std::string&, const CoreROMLink&, const InitialComputerMedia* = nullptr);
	Error LoadROMLibraryCore(const std::string&, const std::string&, const std::string&, const std::string&, const CoreROMLink&, const InitialComputerMedia* = nullptr);
	Error LoadROMComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&, const std::string&, const CoreROMLink&, const InitialComputerMedia* = nullptr);
	Error LoadROMsCore(const std::string&, const std::string&, const std::string&, const CoreROMLinks&);
	Error LoadROMsLibraryCore(const std::string&, const std::string&, const std::string&, const std::string&, const CoreROMLinks&);
	Error LoadROMsComposedCore(const std::string&, const std::string&, const CoreCompositionRequest&, const std::string&, const CoreROMLinks&);
	Error InspectCoreData(const std::string&, const std::string&, const std::string&,
		const std::string& game_id, CoreData*);
	Error UpdateCoreSettings(const std::string&, const std::string&, const std::string&,
		const std::string& game_id, const std::string& revision, const CoreSettingsUpdate&, CoreData*);
	Error InspectCore(const std::string& directory,
		const std::string& expected_package_id, CorePackageInspection*);
	Error LoadContainedDevelopmentRBF(const std::string&);
	Error SetComputerKeyboard(std::uint64_t matrix);
	Error SetController(const std::string& package_id, std::uint64_t generation,
		std::uint8_t port, std::uint16_t buttons, std::uint16_t keypad);
	// Launch-time hold-reset primary bind (optional package/generation when unbound).
	Error LoadComputerMedia(const std::string& path);
	Error LoadComputerMedia(const std::string& path, const std::string& expected_package_id,
		std::uint64_t expected_generation);
	// Mid-session mailbox fill without hold-reset soft-reboot.
	Error ReplaceLiveComputerMedia(const std::string& path,
		const std::string& expected_package_id, std::uint64_t expected_generation);
	Error ClearComputerMedia(const std::string& expected_package_id,
		std::uint64_t expected_generation);
	Error LoadComputerFirmware(const std::string& path);
	Error LoadComputerMediaStream(const std::string& path,
		const std::string& expected_package_id, std::uint64_t expected_generation,
		std::uint32_t size);
	// fes.computer: complete HID snapshot; the driver writes only changed rows.
	// Relative motion is transient; an unconfirmed delivery must not be retried.
	Error SendMouseRelative(const std::string& package_id, std::uint64_t generation,
		std::int16_t dx, std::int16_t dy, std::uint8_t buttons);
	Error SetKeyboardHid(const std::string& package_id, std::uint64_t generation,
		const KeyboardHidRows& rows);
	// fes.computer: live transfer into one declared media unit without holding
	// execution reset. A failed transfer ejects that unit once.
	Error InsertMedia(const std::string& path, const std::string& expected_package_id,
		std::uint64_t expected_generation, std::uint8_t unit, std::uint32_t size);
    Error SaveMedia(const std::string& expected_package_id, std::uint64_t expected_generation, std::uint8_t unit);
	Error CaptureZx81Tape(const std::string& path, const std::string& expected_package_id,
		std::uint64_t expected_generation);
	Error AcknowledgeZx81Tape(const std::string& expected_package_id,
		std::uint64_t expected_generation);
    Error InsertLibraryMedia(const std::string& path, const std::string& expected_package_id,
        std::uint64_t expected_generation, std::uint8_t unit, std::uint32_t size,
        const std::string& data_root, const MediaDataBinding&);
	Error EjectMedia(const std::string& expected_package_id,
		std::uint64_t expected_generation, std::uint8_t unit);
	Error Stop();
	// Programs idle again after reboot_required. Stop does not.
	Error RecoverIdle();

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

const char* ErrorCodeName(ErrorCode);
const char* StateName(State);
const char* ExecutionName(Execution);
const char* MediaUnitStateName(MediaUnitState);

} // namespace mister
