#include "capture_log.hpp"
#include "fake_i2c.hpp"
#include "native/hardware.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"
#include "native/adv7513.hpp"
#include <cassert>
#include <cstdio>
#include <string>
using namespace mister::native;
const std::uint64_t kDeadline = 100;
std::size_t scenarios = 0;
const std::size_t kExpectedScenarios = 243;
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
	// The verified link, then its PLL lock and detected mode, are the last
	// reads before sample packets are enabled.
	assert(calls[calls.size()-4].type == Type::read &&
		calls[calls.size()-4].address == 0x42);
	assert(calls[calls.size()-3].type == Type::read &&
		calls[calls.size()-3].address == 0x9e);
	assert(calls[calls.size()-2].type == Type::read &&
		calls[calls.size()-2].address == 0x3e);
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


bool Mentions(const std::string& message, const char* needle)
{ return message.find(needle) != std::string::npos; }

// Bring-up requires the pixel PLL behind a ready link, and deliberately does
// not require the detected VIC: the idle health check owns that verdict so a
// wrong mode gets its bounded reactivation instead of a refused bring-up.
void TestLinkRequiresPllLockAndRecordsVic()
{
	for (bool splash : {false, true}) {
		const auto bring_up = [splash](Fixture& fixture) {
			FixedVideoBringup fixed(fixture.i2c, fixture.clock,
				fixture.log, Menu720p60Recipe());
			SplashVideoBringup idle(fixture.i2c, fixture.clock,
				fixture.log, Menu720p60Recipe());
			return splash ? idle.BringUp(SplashIdle(), kDeadline)
				: fixed.BringUpCustom(kDeadline);
		};
		// The sample is read 0x42 then 0x9e then 0x3e, and the whole tail is
		// pinned. Tests below script a failure by index across both detail
		// registers, so they depend on that order; and without a whole-tail
		// assertion a reordered or duplicated field would pass everywhere.
		{
			using Type = mister_test::FakeI2c::CallType;
			Fixture healthy;
			const auto result = bring_up(healthy);
			assert(result.error.ok());
			const auto& calls = healthy.i2c.calls;
			assert(calls[calls.size()-3].type == Type::read &&
				calls[calls.size()-3].address == 0x42);
			assert(calls[calls.size()-2].type == Type::read &&
				calls[calls.size()-2].address == 0x9e);
			assert(calls.back().type == Type::read && calls.back().address == 0x3e);
			const std::string tail = " link_status=0x60 pll_lock=1 rx_sense=1 vic=4";
			assert(result.error.message.size() >= tail.size() &&
				result.error.message.compare(
					result.error.message.size() - tail.size(),
					tail.size(), tail) == 0);
			++scenarios;
		}
		// An unlocked PLL through the deadline fails hdmi_verify, and the
		// message names the register and carries the sample it refused on.
		{
			Fixture unlocked;
			unlocked.i2c.pll_status = 0x02;
			const auto result = bring_up(unlocked);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pixel PLL did not lock"));
			assert(Mentions(result.error.message, "pll_lock=0"));
			assert(Mentions(result.error.message, "link_status=0x60"));
			assert(Mentions(result.error.message, "vic=4"));
			++scenarios;
		}
		// The PLL locks over the frames after the pixel stream starts, so an
		// unlocked first reading is not a failure.
		{
			Fixture settles;
			settles.i2c.pll_statuses = {0x02, 0x02, 0x12};
			const auto result = bring_up(settles);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pll_lock=1"));
			assert(settles.i2c.pll_statuses.empty());
			++scenarios;
		}
		// A wrong detected mode is recorded and does not fail bring-up.
		{
			Fixture wrong_mode;
			wrong_mode.i2c.vic_status = 0x04;
			const auto result = bring_up(wrong_mode);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "vic=1"));
			assert(Mentions(result.error.message, "pll_lock=1"));
			++scenarios;
		}
		// An unreadable 0x9e leaves the PLL unenforced rather than refusing a
		// picture that may be good, matching the idle check's read policy.
		{
			Fixture unreadable;
			unreadable.i2c.pll_status = 0x02;
			unreadable.i2c.detail_read_error =
				{mister::ErrorCode::io_failed, "detail unreadable"};
			const auto result = bring_up(unreadable);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pll_lock=? rx_sense=1 vic=?"));
			++scenarios;
		}
		// A spent budget is not an unreadable register. The transport reports
		// the deadline from the detail reads too, and accepting that would
		// pass the never-locked PLL this poll exists to refuse. The clock
		// starts two ticks out so the loop is entered and the deadline falls
		// between the link read and the detail read.
		{
			Fixture spent;
			spent.clock.now = kDeadline - 2;
			spent.i2c.pll_status = 0x02;
			spent.i2c.detail_read_error =
				{mister::ErrorCode::io_failed, "deadline exceeded"};
			const auto result = bring_up(spent);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "deadline exceeded"));
			assert(Mentions(result.error.message, "link_status=0x60"));
			// The point of the arithmetic: the budget must run out AFTER the
			// link read and while reading the detail, not before the loop.
			// Without this the test could pass by never entering the poll.
			bool read_link = false, tried_pll = false;
			for (const auto& call : spent.i2c.calls) {
				if (call.address == 0x42) read_link = true;
				if (call.address == 0x9e) tried_pll = true;
			}
			assert(read_link && tried_pll);
			++scenarios;
		}
		// A PLL once read as unlocked stays refused. The second sample's
		// unreadable 0x9e must not retract a measured fact and let bring-up
		// through -- an ordinary bus blip would otherwise launder the
		// refusal into success.
		{
			Fixture blip;
			blip.i2c.pll_status = 0x02;
			blip.i2c.fail_detail_read_index = 2;
			const auto result = bring_up(blip);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pixel PLL did not lock"));
			++scenarios;
		}
		// Unlocked, then unreadable, then locked. The stickiness must refuse
		// only while nothing newer has measured a lock: this is the sequence
		// a tighter reading of it would turn into a refused activation, so it
		// is pinned rather than left to the next person's judgement.
		{
			Fixture recovers;
			recovers.i2c.pll_statuses = {0x02, 0x12};
			recovers.i2c.fail_detail_read_index = 2;
			const auto result = bring_up(recovers);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pll_lock=1"));
			assert(recovers.i2c.pll_statuses.empty());
			++scenarios;
		}
		// Reporting stays per sample even so. A link that goes down after an
		// unlocked reading must not print that reading's PLL and VIC beside
		// the new link status: the refusal stands, the stale fields do not.
		{
			Fixture went_down;
			went_down.i2c.pll_status = 0x02;
			went_down.i2c.link_statuses = {0x60, 0x40};
			const auto result = bring_up(went_down);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pixel PLL did not lock"));
			assert(Mentions(result.error.message, "link_status=0x40"));
			assert(Mentions(result.error.message, "pll_lock=? rx_sense=0 vic=?"));
			++scenarios;
		}
		// A failed link read names the register it failed on and claims no
		// sample: the transport leaves its output untouched on failure, so
		// the previous byte must not be printed as this one.
		{
			Fixture no_link;
			no_link.i2c.link_read_error =
				{mister::ErrorCode::io_failed, "bus wedged"};
			const auto result = bring_up(no_link);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "link status read failed"));
			assert(Mentions(result.error.message, "link_status=? pll_lock=? rx_sense=? vic=?"));
			++scenarios;
		}
		// The VIC is recorded, never required, so an unreadable 0x3e must not
		// let a definitively unlocked PLL through. It is read on its own
		// account and its failure does not touch the PLL verdict.
		{
			Fixture no_vic;
			no_vic.i2c.pll_status = 0x02;
			no_vic.i2c.vic_read_error =
				{mister::ErrorCode::io_failed, "vic unreadable"};
			const auto result = bring_up(no_vic);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pixel PLL did not lock"));
			assert(Mentions(result.error.message, "pll_lock=0"));
			assert(Mentions(result.error.message, "vic=?"));
			++scenarios;
		}
		// The mirror of it: a locked PLL finishes the poll whether or not the
		// VIC answered, and the line says which of the two it measured.
		{
			Fixture locked_no_vic;
			locked_no_vic.i2c.vic_read_error =
				{mister::ErrorCode::io_failed, "vic unreadable"};
			const auto result = bring_up(locked_no_vic);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pll_lock=1"));
			assert(Mentions(result.error.message, "rx_sense=1"));
			assert(Mentions(result.error.message, "vic=?"));
			++scenarios;
		}
		// An optional VIC failure is harmless until its read exhausts the
		// caller's budget. That deadline must remain fatal even after the PLL
		// has locked and would otherwise finish the poll successfully.
		{
			Fixture spent_vic;
			spent_vic.clock.now = kDeadline - 2;
			spent_vic.i2c.vic_read_error =
				{mister::ErrorCode::io_failed, "deadline exceeded"};
			const auto result = bring_up(spent_vic);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message,
				"VIC register 0x3e read failed: deadline exceeded"));
			assert(Mentions(result.error.message, "pll_lock=1"));
			assert(Mentions(result.error.message, "vic=?"));
			++scenarios;
		}
		// 0x9e unreadable while 0x3e answers: the requirement is unenforced
		// because nothing ever measured the PLL, and the line says exactly
		// which of the two it knows.
		{
			Fixture no_pll;
			no_pll.i2c.pll_read_error =
				{mister::ErrorCode::io_failed, "pll unreadable"};
			const auto result = bring_up(no_pll);
			assert(result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "pll_lock=? rx_sense=1 vic=4"));
			++scenarios;
		}
		// A link that never goes ready still reports the link, and reports no
		// PLL sample because none was ever taken behind a ready link.
		{
			Fixture never_ready;
			never_ready.i2c.link_statuses = {0x40};
			const auto result = bring_up(never_ready);
			assert(!result.error.ok() && result.phase == "hdmi_verify");
			assert(Mentions(result.error.message, "deadline exceeded"));
			assert(Mentions(result.error.message, "link_status=0x40"));
			assert(Mentions(result.error.message, "pll_lock=?"));
			++scenarios;
		}
	}
}

int main() {
 TestApplicationAudioPolicyAndFailureOrdering();
 TestLinkRequiresPllLockAndRecordsVic();
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
 // Pinned so a scenario that stops running is noticed rather than quietly
 // lowering a number nobody reads.
 assert(scenarios == kExpectedScenarios);
 std::printf("video_test: %zu scenarios passed\n", scenarios);
}
