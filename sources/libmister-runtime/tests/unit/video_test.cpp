#include "capture_log.hpp"
#include "fake_i2c.hpp"
#include "native/hardware.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"
#include "native/adv7513.hpp"
#include <cassert>
#include <cstdio>
using namespace mister::native;
const std::uint64_t kDeadline = 100;
std::size_t scenarios = 0;
class ClockFake final : public Clock {
public: mutable std::uint64_t now = 0;
 std::uint64_t NowMs() const override { return now++; }
};
struct Fixture { mister_test::FakeI2c i2c; ClockFake clock; mister_test::CaptureLog log; };
void TestApplicationAudioPolicyAndFailureOrdering()
{
	using namespace mister::native;
	using Type = mister_test::FakeI2c::CallType;
	Fixture fixture;
	FixedVideoBringup video(fixture.i2c, fixture.clock,
		fixture.log, Menu720p60Recipe());
	assert(video.BringUpCustom(kDeadline, true).error.ok());
	const auto& calls = fixture.i2c.calls;
	assert(calls[1].address == 0x44 && calls[1].value == 0x11);
	assert(calls[calls.size()-2].type == Type::read &&
		calls[calls.size()-2].address == 0x42);
	assert(calls.back().address == 0x44 && calls.back().value == 0x79);
	const std::size_t enabled_calls = calls.size();
	assert(video.Quiesce(kDeadline).error.ok());
	assert(calls.back().address == 0x41 && (calls.back().value & 0x40));
	assert(video.BringUpCustom(kDeadline).error.ok());
	for (std::size_t i = enabled_calls; i < calls.size(); ++i)
		if (calls[i].type == Type::write && calls[i].address == 0x44)
			assert(calls[i].value == 0x11);
	assert(video.BringUpCustom(kDeadline, true).error.ok());
	assert(calls.back().address == 0x44 && calls.back().value == 0x79);
	// Every setup write failure is bounded; none enables sample packets.
	const auto init_count = Menu720p60Recipe().adv_initialization.size();
	for (std::size_t i = 0; i < adv7513::ApplicationAudio48k().size(); ++i) {
		Fixture failed;
		failed.i2c.fail_write_index = init_count + i;
		FixedVideoBringup attempt(failed.i2c, failed.clock,
			failed.log, Menu720p60Recipe());
		const auto result = attempt.BringUpCustom(kDeadline, true);
		assert(!result.error.ok() && result.phase == "audio_setup");
		for (const auto& call : failed.i2c.calls)
			if (call.type == Type::write && call.address == 0x44)
				assert(call.value == 0x11);
	}
	Fixture failed_link;
	failed_link.i2c.link_read_error = {mister::ErrorCode::io_failed, "link failed"};
	FixedVideoBringup attempt(failed_link.i2c, failed_link.clock,
		failed_link.log, Menu720p60Recipe());
	assert(attempt.BringUpCustom(kDeadline, true).phase == "hdmi_verify");
	for (const auto& call : failed_link.i2c.calls)
		if (call.type == Type::write && call.address == 0x44) assert(call.value == 0x11);
	Fixture failed_enable;
	failed_enable.i2c.fail_write_index = init_count + adv7513::ApplicationAudio48k().size() +
		Menu720p60Recipe().adv_mode.size() + adv7513::HdmiWake().size();
	FixedVideoBringup enable(failed_enable.i2c, failed_enable.clock,
		failed_enable.log, Menu720p60Recipe());
	const auto enable_result = enable.BringUpCustom(kDeadline, true);
	assert(!enable_result.error.ok() && enable_result.phase == "audio_enable");
	assert(failed_enable.i2c.calls.back().address == 0x44);
	// An ambiguous enable failure still permits the ordinary bounded power-down.
	assert(enable.Quiesce(kDeadline).error.ok());
	assert(failed_enable.i2c.calls.back().address == 0x41);
	++scenarios;
}

int main() {
 TestApplicationAudioPolicyAndFailureOrdering();
 // Every retained video write is fallible. Stop exactly at the failing write.
 for (bool splash : {false, true}) {
  Fixture good;
  FixedVideoBringup fixed(good.i2c, good.clock, good.log, Menu720p60Recipe());
  SplashVideoBringup idle(good.i2c, good.clock, good.log, Menu720p60Recipe());
  assert((splash ? idle.BringUp(SplashIdle(), kDeadline) : fixed.BringUpCustom(kDeadline)).error.ok());
  std::size_t writes = 0;
  for (const auto& call : good.i2c.calls) {
   assert(call.deadline == kDeadline);
   if (call.type == mister_test::FakeI2c::CallType::write) ++writes;
  }
  for (std::size_t i = 0; i < writes; ++i) {
   Fixture bad; bad.i2c.fail_write_index = i;
   FixedVideoBringup f(bad.i2c, bad.clock, bad.log, Menu720p60Recipe());
   SplashVideoBringup v(bad.i2c, bad.clock, bad.log, Menu720p60Recipe());
   assert(!(splash ? v.BringUp(SplashIdle(), kDeadline) : f.BringUpCustom(kDeadline)).error.ok());
   assert(bad.i2c.calls.back().type == mister_test::FakeI2c::CallType::write);
   std::size_t attempted = 0;
   for (const auto& call : bad.i2c.calls) if (call.type == mister_test::FakeI2c::CallType::write) ++attempted;
   assert(attempted == i+1); ++scenarios;
  }
  for (int failure = 0; failure < 5; ++failure) {
   Fixture bad;
   const mister::Error error{mister::ErrorCode::io_failed, "test failure"};
   if (failure == 0) bad.i2c.select_error = error;
   if (failure == 1) bad.i2c.power_read_error = error;
   if (failure == 2) bad.i2c.link_read_error = error;
   if (failure == 3) bad.i2c.link_statuses = {0x40};
   if (failure == 4) bad.clock.now = kDeadline;
   FixedVideoBringup f(bad.i2c, bad.clock, bad.log, Menu720p60Recipe());
   SplashVideoBringup v(bad.i2c, bad.clock, bad.log, Menu720p60Recipe());
   assert(!(splash ? v.BringUp(SplashIdle(), kDeadline) : f.BringUpCustom(kDeadline)).error.ok());
   if (failure == 4) assert(bad.i2c.calls.empty());
   ++scenarios;
  }
 }
 std::printf("video_test: %zu scenarios passed\n", scenarios);
}
