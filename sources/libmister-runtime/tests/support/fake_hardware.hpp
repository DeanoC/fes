// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <condition_variable>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <functional>
#include <cstdint>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace mister_test {

class FakeHardware final : public mister::Hardware {
public:
	FakeHardware();
	void SetFaultSink(mister::HardwareFaultSink*) override;
	mister::HardwareResult LoadIdle() override;
 mister::HardwareResult ConfigureMenuPackage(const std::string&,const std::string& id) override {
  ++menu_configure_calls;if(!menu_configure_result.error.ok())return menu_configure_result;
  menu_status.session=false;menu_status.core_generation=0;
  menu_configured=true;menu_status.available=true;menu_status.package_id=id;
  menu_status.geometry={1280,720,5120,3686400,4194304};return {{},true,"fes.menu"};
 }
 mister::MenuDisplayStatus menu_display() const override {return menu_status;}
 // The grant is ordinary memory here: both 4 MiB slots, plus the lists and
 // heap with a blitter, mappable by clients.
 mister::Error ShareMenuWindow(int* fd) override {
  ++menu_share_calls;if(!menu_status.available)return {mister::ErrorCode::busy,"menu presentation unavailable","menu"};
  if(menu_window<0) {
   menu_window=memfd_create("fake-menu-window",MFD_CLOEXEC);
   if(menu_window<0||ftruncate(menu_window,menu_status.blitter?83886080:8388608)<0)return {mister::ErrorCode::io_failed,"fake window","menu"};
  }
  *fd=fcntl(menu_window,F_DUPFD_CLOEXEC,0);return *fd<0?mister::Error{mister::ErrorCode::io_failed,"fake window","menu"}:mister::Error{};
 }
 int menu_window=-1;
 mister::Error MenuBackSlot(std::uint8_t* slot) const override {
  if(!menu_status.available)return {mister::ErrorCode::busy,"menu presentation unavailable","menu"};
  *slot=std::uint8_t(menu_slot^1);return {};
 }
 mister::Error PresentMenuSlot(std::uint8_t slot,const mister::MenuDraw& draw,mister::MenuDisplayInfo* info) override {
  ++menu_present_calls;menu_present_slots.push_back(slot);menu_present_draws.push_back(draw);if(on_menu_present)on_menu_present();
  if(!menu_present_result.ok()) {
   if(menu_status.session){menu_status.available=false;menu_status.error=menu_present_result;}
   return menu_present_result;
  }
  menu_slot=slot;info->geometry=menu_status.geometry;info->displayed_sequence=++menu_status.displayed_sequence;return {};
 }
 std::uint8_t menu_slot=0;
 std::vector<std::uint8_t> menu_present_slots;
 std::vector<mister::MenuDraw> menu_present_draws;
 unsigned menu_share_calls=0;
 bool menu_configured=false;
 unsigned menu_configure_calls=0;
 mister::HardwareResult menu_configure_result;
 mister::MenuDisplayStatus menu_status;
 unsigned menu_present_calls=0;
 mister::Error menu_present_result;
 std::function<void()> on_menu_present;
 // A failure models a plane whose drain is not proved: launcher focus stays.
 mister::Error SetSessionDisplay(bool visible,bool launcher_focus) override {
  ++session_display_calls;if(on_session_display)on_session_display();
  if(!session_display_result.ok()){
   menu_status.available=false;menu_status.launcher_focus=true;menu_status.error=session_display_result;
   return session_display_result;
  }
  menu_status.available=visible;menu_status.launcher_focus=launcher_focus;if(visible)menu_status.error={};return {};
 }
 unsigned session_display_calls=0;
 mister::Error session_display_result;
 std::function<void()> on_session_display;
 mister::Error SetComputerKeyboard(std::uint64_t matrix) override {
  ++keyboard_calls;keyboard_matrix=matrix;return {};
 }
 unsigned keyboard_calls=0;
 std::uint64_t keyboard_matrix=0;
	mister::Error FlushSave() override { ++flush_calls; if (on_flush) on_flush(); return flush_result; }
	mister::Error RestoreInput(std::uint64_t generation) override
	{
		++restore_input_calls;
		restored_input_generations.push_back(generation);
		if (on_restore_input) on_restore_input();
		return restore_input_result;
	}
	mister::Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<mister::AdmittedCorePackage>*) override;
	mister::Error AdmitCoreComposition(const std::string&, const std::string&,
		const mister::CoreCompositionRequest&, std::unique_ptr<mister::AdmittedCorePackage>*) override;
	mister::Error InspectCorePackage(const std::string&, const std::string&,
		mister::CorePackageInspection*) override;
	mister::Capabilities capabilities() const override { return supported; }
	mister::Error SendMouseRelative(std::int16_t dx, std::int16_t dy, std::uint8_t buttons) override {
		++mouse_calls; mouse_snapshot = {dx,dy,buttons}; if(on_mouse) on_mouse(); return mouse_result;
	}
	int mouse_calls=0; std::vector<std::int32_t> mouse_snapshot;
	mister::Error mouse_result; std::function<void()> on_mouse;
	mister::Error SetController(std::uint8_t port, std::uint16_t buttons,
		std::uint16_t keypad) override
	{
		++controller_calls;
		controller_snapshot = {port, buttons, keypad};
		if (on_controller) on_controller();
		return controller_result;
	}
	int controller_calls = 0;
	std::vector<std::uint16_t> controller_snapshot;
	std::function<void()> on_controller;
	mister::Error controller_result;
	mister::Error SetKeyboardHid(const mister::KeyboardHidRows& rows) override
	{
		++keyboard_hid_calls;
		keyboard_hid_rows = rows;
		if (on_keyboard_hid) on_keyboard_hid();
		return keyboard_hid_result;
	}
	mister::Error InsertComputerMedia(std::uint8_t unit, const std::string& path,
		std::uint32_t size) override
	{
		++insert_media_calls;
		insert_media_unit = unit;
		insert_media_path = path;
		insert_media_size = size;
		if (on_insert_media) on_insert_media();
		return insert_media_result;
	}
	mister::Error EjectComputerMedia(std::uint8_t unit) override
	{
		++eject_media_calls;
		eject_media_unit = unit;
		if (on_eject_media) on_eject_media();
		return eject_media_result;
	}
    mister::Error InsertLibraryComputerMedia(std::uint8_t unit,const std::string& path,
        std::uint32_t size,const std::string& root,const mister::MediaDataBinding& binding) override {
        ++library_media_calls; library_media_root=root;library_media_binding=binding;
        return InsertComputerMedia(unit,path,size);
    }
    int library_media_calls=0;
    std::string library_media_root;
    mister::MediaDataBinding library_media_binding;
	int keyboard_hid_calls = 0;
	mister::KeyboardHidRows keyboard_hid_rows{};
	std::function<void()> on_keyboard_hid;
	mister::Error keyboard_hid_result;
	int insert_media_calls = 0;
	std::uint8_t insert_media_unit = 0xff;
	std::string insert_media_path;
	std::uint32_t insert_media_size = 0;
	std::function<void()> on_insert_media;
	mister::Error insert_media_result;
	int eject_media_calls = 0;
	std::uint8_t eject_media_unit = 0xff;
	std::function<void()> on_eject_media;
	mister::Error eject_media_result;
	mister::Error LoadComputerMediaStream(const std::string& path, std::uint32_t size) override
	{
		++media_stream_calls;
		media_stream_path = path;
		media_stream_size = size;
		if (on_media_stream) on_media_stream();
		return media_stream_result;
	}
	mister::Error LoadComputerMedia(const std::string& path) override
	{
		++media_calls;
		media_path = path;
		if (on_media) on_media();
		return media_result;
	}
	mister::Error LoadComputerMediaLive(const std::string& path) override
	{
		++live_media_calls;
		live_media_path = path;
		if (on_live_media) on_live_media();
		return live_media_result;
	}
	mister::Error ClearComputerMedia() override
	{
		++clear_media_calls;
		if (on_clear_media) on_clear_media();
		return clear_media_result;
	}
	int media_stream_calls = 0;
	std::string media_stream_path;
	std::uint32_t media_stream_size = 0;
	std::function<void()> on_media_stream;
	mister::Error media_stream_result;
	int media_calls = 0;
	std::string media_path;
	std::function<void()> on_media;
	mister::Error media_result;
	int live_media_calls = 0;
	std::string live_media_path;
	std::function<void()> on_live_media;
	mister::Error live_media_result;
	int clear_media_calls = 0;
	std::function<void()> on_clear_media;
	mister::Error clear_media_result;
	mister::HardwareResult LoadCore(
		std::unique_ptr<mister::AdmittedCorePackage>, std::uint64_t) override;
	int flush_calls = 0;
	std::function<void()> on_flush;
	mister::Error flush_result;
	int restore_input_calls = 0;
	std::function<void()> on_restore_input;
	mister::Error restore_input_result;
	std::vector<std::uint64_t> restored_input_generations;
	mister::HardwareResult LoadDevelopmentRBF(const std::string&,
		std::uint64_t generation);
	mister::HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t generation) override;
	void BlockLaunch();
	void WaitUntilLaunchEntered();
	void ReleaseLaunch();
	void BlockNextIdle();
	void WaitUntilIdleEntered();
	void ReleaseIdle();
	void ReportFault(std::uint64_t generation, mister::Error);
	bool WaitForIdleCalls(int count);

	mister::HardwareResult idle_result;
	mister::HardwareResult launch_result;
	mister::HardwareResult development_result;
	mister::Error admission_result;
	mister::HardwareResult core_result;
	mister::Error inspection_result;
	bool inspection_compatible = true;
	mister::Error compatibility_error;
	mister::Capabilities supported;
	mister::CorePackageInfo core_info = {
		std::string(64, 'a'), "custom-core", "", {}};
	int idle_calls;
	int launch_calls;
	int development_calls;
	int admission_calls = 0;
	int core_calls = 0;
	int inspection_calls = 0;
	int contained_development_calls = 0;
	int fault_sink_sets;
	bool idle_without_fault_sink;
	std::vector<std::uint64_t> launch_generations;
	std::vector<std::string> development_rbfs;
	std::vector<std::uint64_t> development_generations;
	std::vector<std::thread::id> idle_threads;
	std::vector<std::string> events;
	std::vector<std::uint64_t> core_generations;

private:
	std::mutex mutex_;
	std::condition_variable condition_;
	bool block_launch_;
	bool launch_entered_;
	bool release_launch_;
	bool block_idle_ = false;
	bool idle_entered_ = false;
	bool release_idle_ = false;
	mister::HardwareFaultSink* fault_sink_;
};

} // namespace mister_test
