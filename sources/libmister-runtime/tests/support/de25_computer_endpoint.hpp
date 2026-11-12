// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "fes_computer_endpoint.hpp"
#include "native/generated/fes_de25_computer.hpp"
#include "native/generated/fes_gp.hpp"
#include "native/generated/fes_application.hpp"
#include "native/linux/de25_computer_mmio.hpp"
#include <cassert>
namespace mister_test {
using namespace mister::native::generated;
// A register bridge around the independent computer model. Every write must
// change the request toggle: the DE10 preload is deliberately rejected here.
class AtomicComputerBridge final : public mister::native::Mmio {
public:
	ComputerEndpoint endpoint;
 explicit AtomicComputerBridge(std::uint32_t capabilities=FesComputerCapabilityVideoFixed720p60 |
  FesComputerCapabilityKeyboardHid | FesComputerCapabilityMediaZx81Tape | FesComputerCapabilityMediaZx81TapeWrite,
  const std::string& build="00112233445566778899aabbccddeeff"):
  endpoint(capabilities,{{0,{1,FesComputerZx81TapeMaxBytes}}},build) {}
	std::uint32_t id = FesDe25ComputerIdentity, version = FesDe25ComputerVersion;
	std::uint32_t status = FesDe25ComputerStatusReady, fault = 0, held = 0, video=FesDe25ComputerVideoReady;
	bool changed_word = false, write_error = false, post_fault = false, post_busy = false;
	unsigned writes = 0;
 std::function<std::pair<unsigned,std::uint16_t>(unsigned,unsigned,unsigned)> display;
 bool display_response=false;
 std::uint32_t display_word=0;
	mister::Error Read32(std::uint32_t address, std::uint32_t* value) override
	{
		using mister::native::De25ComputerCpuBase;
		assert(address >= De25ComputerCpuBase);
		switch (address - De25ComputerCpuBase) {
		case FesDe25ComputerIdOffset: *value = id; break;
		case FesDe25ComputerVersionOffset: *value = version; break;
  case FesDe25ComputerVideoStatusOffset: *value = video; break;
		case FesDe25ComputerStatusOffset:
			*value = status | (post_busy && writes ? FesDe25ComputerStatusBusy : 0); break;
		case FesDe25ComputerFaultOffset:
			*value = fault | (post_fault && writes ? FesDe25ComputerFaultReset : 0); break;
		case FesDe25ComputerGpoOffset: *value = held ^ (changed_word && writes ? 1u : 0u); break;
		case FesDe25ComputerGpiOffset:
   if(display_response){*value=display_word;break;}
   return endpoint.Read32(kSpiGpiAddress, value);
		default: assert(false && "unexpected physical DE25 register");
		}
		return {};
	}
	mister::Error Write32(std::uint32_t address, std::uint32_t value) override
	{
		assert(address == mister::native::De25ComputerCpuBase + FesDe25ComputerGpoOffset);
		std::uint32_t previous = 0;
		assert(endpoint.Read32(kSpiGpiAddress, &previous).ok());
		assert(((value & FesGpRequestMask) != 0) != ((previous & FesGpAckMask) != 0));
		held = value;
		++writes;
		const auto error = endpoint.Write32(kFpgaGpoAddress, value);
  const auto opcode=(value>>24)&127;
  display_response=display&&opcode>=FesApplicationOpcodeMenuInfo&&opcode<=FesApplicationOpcodeMenuSubmit;
  if(display_response) {
   const auto reply=display(opcode,(value>>16)&255,value&65535);
   display_word=FesGpSignature|((value&FesGpRequestMask)?FesGpAckMask:0)|
    (reply.first?FesGpErrorMask:0)|reply.second;
  }
		return write_error ? mister::Error{mister::ErrorCode::io_failed, "uncertain write"} : error;
	}
};

}
