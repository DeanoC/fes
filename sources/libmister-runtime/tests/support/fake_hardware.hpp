// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <condition_variable>
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
  menu_status.session=false;menu_status.core_generation=0;
  menu_configured=true;menu_status.available=true;menu_status.package_id=id;
  menu_status.geometry={1280,720,5120,3686400,4194304};return {{},true,"fes.menu"};
 }
 mister::MenuDisplayStatus menu_display() const override {return menu_status;}
 mister::Error PresentMenuFrame(const mister::MenuFrame&,mister::MenuDisplayInfo* info) override {
  ++menu_present_calls;if(on_menu_present)on_menu_present();
  if(!menu_present_result.ok()) {
   if(menu_status.session){menu_status.available=false;menu_status.error=menu_present_result;}
   return menu_present_result;
  }
  info->geometry=menu_status.geometry;info->displayed_sequence=++menu_status.displayed_sequence;return {};
 }
 bool menu_configured=false;
 mister::MenuDisplayStatus menu_status;
 unsigned menu_present_calls=0;
 mister::Error menu_present_result;
 std::function<void()> on_menu_present;
 mister::Error SetSessionDisplay(bool visible) override {
  ++session_display_calls;if(on_session_display)on_session_display();
  if(!session_display_result.ok()){menu_status.available=false;menu_status.error=session_display_result;return session_display_result;}
  menu_status.available=visible;if(visible)menu_status.error={};return {};
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
