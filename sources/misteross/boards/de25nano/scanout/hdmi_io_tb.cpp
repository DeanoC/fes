#include "Vfes_de25_hdmi_io_tb.h"
#include "Vfes_de25_hdmi_io_tb___024root.h"
#include "verilated.h"
#include "expected_lut.h"
#include <array>
#include <cstdio>
#include <cstdlib>
#include <vector>
#include <cstring>

static void check(bool value, const char* message) {
    if (!value) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}

// Digital safety subset of ADI ADV7513 Guide Rev.B, sections4.2.1/4.7/4.10,
// Table74 and main-map94/95/96/97. No analog/video/CEC simulation.
// PD/AD is assumed inactive and no CDC/CEC events occur; D6 is never written.
// ADI says SOME registers cannot be written whileHPDlow but gives no per-register
// rejection list here. We therefore do NOT invent one: low-HPD config writes are
// unqualified observations, never evidence of restoration. Only a matching write
// after the LAST rise certifies each HPD-resettable LUT address.
struct Adv7513 {
    bool hpd = true, monitor_sense = true, power_down = true;
    unsigned power_writes = 0, irq_clears = 0, high_epoch = 1;
    unsigned low_config_writes_unqualified = 0;
    unsigned mask94 = 0xc0, mask95 = 0, status96 = 0, status97 = 0;
    std::array<unsigned,256> last_high_epoch{}, value{};
    static bool hpd_resettable(unsigned reg) { return reg <= 0x93 || (reg >= 0xaf && reg <= 0xcc); }
    bool interrupt_pending() const { return (status96 & mask94) || (status97 & mask95); }
    bool hpd_irq() const { return status96 & 0x80; }
    void reset_interrupt_bank() {
        mask94 = 0xc0; mask95 = 0;
        status96 &= 0xc0; status97 = 0; // Table74 preserves both HPD/monitor latches.
    }
    void hotplug(bool high) {
        if (hpd == high) return;
        hpd = high;
        if (!high) {
            power_down = true;
            for (unsigned r = 0; r < 256; ++r) if (hpd_resettable(r)) last_high_epoch[r] = 0;
            reset_interrupt_bank();
        } else ++high_epoch;
        status96 |= 0x80;
    }
    void monitor(bool high) {
        if (monitor_sense != high) { monitor_sense = high; status96 |= 0x40; }
    }
    void write(const std::array<unsigned,3>& frame) {
        check(frame[0] == 0x72, "unexpected transmitter address");
        const unsigned reg = frame[1], data = frame[2];
        if (reg == 0x41) {
            ++power_writes;
            // The documented lowHPD power-up prohibition is exact; no inferred
            // per-register lowHPD acceptance beyond this control.
            if (hpd) {
                power_down = data & 0x40;
                if (power_down) reset_interrupt_bank();
            }
        }
        if (reg == 0x94) mask94 = data;
        if (reg == 0x95) mask95 = data;
        if (reg == 0x96) { status96 &= ~data; if (data & 0x80) ++irq_clears; }
        if (reg == 0x97) status97 &= ~data;
        if (hpd_resettable(reg)) {
            if (hpd) { value[reg] = data; last_high_epoch[reg] = high_epoch; }
            else ++low_config_writes_unqualified;
        }
    }
    unsigned missing_resettable() const {
        unsigned missing = 0;
        for (const unsigned entry : expected_lut) {
            const unsigned reg = entry >> 8;
            if (hpd_resettable(reg) &&
                (last_high_epoch[reg] != high_epoch || value[reg] != (entry & 0xff))) ++missing;
        }
        return missing;
    }
};

struct Slave {
    Vfes_de25_hdmi_io_tb& t;
    Adv7513* chip = nullptr;
    bool scl = true, sda = true, active = false;
    unsigned bits = 0, byte = 0, nack_byte;
    unsigned ack_delay = 0;
    unsigned long long cycles = 0, last_rise = 0;
    unsigned long long observations = 0, last_fall_ns = 0, minimum_master_hold_ns = ~0ULL;
    unsigned master_hold_samples = 0;
    std::array<unsigned,3> frame{};
    std::vector<std::array<unsigned,3>> frames;
    explicit Slave(Vfes_de25_hdmi_io_tb& top, unsigned nack) : t(top), nack_byte(nack) {}
    void observe() {
        // Each observation is one half of the 50 MHz reference period.
        const unsigned long long now_ns = 10 * ++observations;
        if (ack_delay && !--ack_delay) {
            check(!t.scl, "delayed ACK missed the low clock phase");
            t.slave_low = 1; t.eval();
        }
        bool now_scl = t.scl, now_sda = t.sda;
        if (active && scl && !now_scl) last_fall_ns = now_ns;
        // Ordinary master data bits only; the explicit slave model's ACK
        // assertion/release and START/STOP transitions are separate boundaries.
        if (active && !now_scl && sda != now_sda && bits < 27 &&
            bits % 9 >= 1 && bits % 9 <= 7 && !t.slave_low) {
            const auto hold = now_ns - last_fall_ns;
            if (hold < minimum_master_hold_ns) minimum_master_hold_ns = hold;
            ++master_hold_samples;
        }
        if (scl && now_scl && sda && !now_sda) {
            if (active) std::fprintf(stderr,"repeated START after bits=%u writes=%zu\n",bits,frames.size());
            check(!active, "unexpected repeated start");
            active = true; bits = byte = 0; last_rise = 0; frame = {};
        }
        if (scl && now_scl && !sda && now_sda && active) {
            if (!(bits >= 27 && bits <= 28))
                std::fprintf(stderr,"STOP after bits=%u writes=%zu frame=%02x %02x %02x\n",bits,frames.size(),frame[0],frame[1],frame[2]);
            check(bits >= 27 && bits <= 28, "incomplete write before stop");
            frames.push_back(frame);
            if (chip) chip->write(frame);
            active = false;
        }
        if (active && !scl && now_scl) {
            if (bits && bits < 27) check(cycles - last_rise == 5002, "vendor bus period changed");
            last_rise = cycles;
            if (bits < 27) {
                if (bits % 9 < 8) {
                    byte = (byte << 1) | unsigned(now_sda);
                    if (bits % 9 == 7) frame[bits / 9] = byte;
                } else {
                    const bool nack = frames.empty() && bits / 9 == nack_byte;
                    check(now_sda == nack, "slave ACK/NACK bus level");
                    byte = 0;
                }
            }
            ++bits;
        }
        if (active && scl && !now_scl) {
            if (bits < 27 && bits % 9 == 8) {
                // Assert ACK after 3.51 us in the low phase, rather than
                // providing a zero-latency slave response at the clock edge.
                ack_delay = (frames.empty() && bits / 9 == nack_byte) ? 0 : 351;
                t.slave_low = 0;
            } else if (bits % 9 == 0) { ack_delay = 0; t.slave_low = 0; }
            t.eval();
        }
        scl = t.scl; sda = t.sda;
    }
    void tick() {
        if (chip) t.irq_n = !chip->interrupt_pending();
        t.clk = 0; t.eval(); observe();
        t.clk = 1; t.eval(); observe();
        ++cycles;
    }
    void until_ready(unsigned count) {
        for (unsigned cycle = 0; cycle < 8000000 && !t.ready; ++cycle) tick();
        check(t.ready, "configuration did not complete within fixed bound");
        check(frames.size() == count, "configuration transaction count");
    }
};

static void scenario(unsigned nack) {
    Vfes_de25_hdmi_io_tb t;
    t.clk = 0; t.reset = 1; t.irq_n = 1; t.slave_low = 0; t.eval();
    t.rootp->fes_de25_hdmi_io_tb__DOT__board__DOT__video_clock__DOT__locked = 1;
    t.eval();
    // Give the two-state simulator a real asynchronous assertion edge;
    // power-up X-to-0 reset behavior is not modeled by Verilator.
    auto reset_tick = [&]() { t.clk = 0; t.eval(); t.clk = 1; t.eval(); };
    t.reset = 0;
    for (unsigned i = 0; i < 4; ++i) reset_tick();
    t.reset = 1;
    for (unsigned i = 0; i < 8; ++i) reset_tick();
    if (!(t.scl && t.sda && !t.ready && t.irq_filtered))
        std::fprintf(stderr,"reset SCL=%u SDA=%u ready=%u irq=%u\n",t.scl,t.sda,t.ready,t.irq_filtered);
    check(t.scl && t.sda && !t.ready && t.irq_filtered, "safe reset boundary");
    Slave bus(t, nack);
    t.reset = 0;
    const unsigned retried = nack < 3;
    bus.until_ready(33 + retried);
    for (unsigned i = 0; i < bus.frames.size(); ++i) {
        const unsigned index = i >= retried ? i - retried : 0;
        const auto& frame = bus.frames[i];
        if (!(frame[0] == 0x72 && frame[1] == (expected_lut[index] >> 8) &&
              frame[2] == (expected_lut[index] & 0xff))) {
            std::fprintf(stderr,"frame %u: %02x %02x %02x, expected LUT[%u]=%04x\n",i,
                frame[0],frame[1],frame[2],index,expected_lut[index]);
            check(false, "register sequence changed");
        }
    }
    if (!retried) {
        t.irq_n = 0; t.eval();
        check(t.irq_filtered, "raw interrupt bypassed synchronizer");
        bus.tick(); check(t.irq_filtered, "first IRQ stage reached configuration input");
        bus.tick(); check(!t.irq_filtered, "second IRQ stage did not propagate");
        for (unsigned i = 0; i < 15000 && bus.frames.size() == 33; ++i) bus.tick();
        t.irq_n = 1;
        // Wait for a complete reconfiguration, including READY dropping.
        for (unsigned i = 0; i < 15000 && t.ready; ++i) bus.tick();
        check(!t.ready, "interrupt did not invalidate configuration");
        bus.until_ready(66);
        for (unsigned i = 33; i < 66; ++i)
            check(bus.frames[i][0] == 0x72 && bus.frames[i][1] == (expected_lut[i-33] >> 8) &&
                  bus.frames[i][2] == (expected_lut[i-33] & 0xff), "interrupt reconfiguration register sequence");
    }
    check(bus.master_hold_samples != 0, "master SDA timing monitor was not exercised");
    if (bus.minimum_master_hold_ns < 100)
        std::fprintf(stderr, "master SDA minimum hold=%llu ns, observed transitions=%u\n",
                     bus.minimum_master_hold_ns, bus.master_hold_samples);
    check(bus.minimum_master_hold_ns >= 100, "ADV7513 master SDA data hold below 100 ns");
    std::printf("{\"case\":\"hdmi-inputs-nack-%u\",\"status\":\"pass\",\"writes\":%zu,\"clock_cycles\":%llu,\"minimum_master_data_hold_ns\":%llu,\"data_hold_samples\":%u}\n",nack,bus.frames.size(),bus.cycles, bus.minimum_master_hold_ns, bus.master_hold_samples);
}

static void reconnect(unsigned phase, bool with_monitor = false) {
    Vfes_de25_hdmi_io_tb t;
    t.clk=0; t.reset=0; t.irq_n=1; t.slave_low=0; t.eval();
    t.rootp->fes_de25_hdmi_io_tb__DOT__board__DOT__video_clock__DOT__locked = 1;
    t.eval();
    Adv7513 chip; Slave bus(t,3); bus.chip=&chip;
    for(unsigned n=0;n<4;++n)bus.tick();
    t.reset=1; for(unsigned n=0;n<8;++n)bus.tick();
    t.reset=0; bus.until_ready(33);
    check(!chip.power_down,"initial connected configuration did not power up");
    chip.hotplug(false);
    if (with_monitor) chip.monitor(false);
    const unsigned old_power=chip.power_writes;
    const unsigned old_clear=chip.irq_clears;
    const auto before=bus.frames.size();
    if(phase==0) {
        for(unsigned n=0;bus.frames.size()==before;++n){check(n<8000000,"first reconfiguration write bound");bus.tick();}
    } else if(phase==2) {
        for(unsigned n=0;chip.irq_clears==old_clear;++n){check(n<8000000,"interrupt clear bound");bus.tick();}
    } else if(phase==3) {
        for(unsigned n=0;t.ready;++n){check(n<8000000,"configuration start bound");bus.tick();}
        for(unsigned n=0;!t.ready;++n){check(n<8000000,"configuration end bound");bus.tick();}
    } else {
        for(unsigned n=0;chip.power_writes==old_power;++n){check(n<8000000,"disconnect reconfiguration did not reach power write");bus.tick();}
    }
    check(chip.power_down,"HPD-low power write incorrectly powered up transmitter");
    // Reconnect after0x41 but before the stock0x96 W1C. Both edges share one
    // interrupt latch; clearing it later must not lose the need to power up.
    chip.hotplug(true);
    if (with_monitor) chip.monitor(true);
    if(phase==4){chip.hotplug(false);chip.hotplug(true);}
    for(unsigned n=0;n<12000000;++n)bus.tick();
    std::printf("{\"case\":\"hpd-reconnect-phase-%u\",\"hpd\":%s,\"power_down\":%s,\"ready\":%s,\"irq_pending\":%s,\"power_writes\":%u,\"irq_clears\":%u}\n",
        phase,chip.hpd?"true":"false",chip.power_down?"true":"false",t.ready?"true":"false",chip.hpd_irq()?"true":"false",chip.power_writes,chip.irq_clears);
    std::fflush(stdout);
    std::printf("{\"case\":\"register-replay-%u-monitor-%u\",\"missing_resettable_rows\":%u,\"low_HPD_writes_unqualified\":%u,\"mask94\":%u,\"mask95\":%u,\"status96\":%u}\n",phase,with_monitor,chip.missing_resettable(),chip.low_config_writes_unqualified,chip.mask94,chip.mask95,chip.status96);
    std::fflush(stdout);
    check(t.ready&&!chip.interrupt_pending()&&!chip.power_down,"reconnect lost: READY asserted but no post-HPD-high power-up write");
    check(chip.missing_resettable()==0,"HPD-resettable configuration was not fully replayed after last rise");
    check(chip.mask94==0xc0 && chip.mask95==0,"unexpected default IRQ masks");
}

static void controls() {
    Adv7513 chip;
    check(chip.mask94==0xc0 && chip.mask95==0,"documented IRQ default masks");
    chip.monitor(false); check(chip.interrupt_pending() && chip.status96==0x40,"monitor edge shared IRQ");
    chip.write({0x72,0x94,0}); check(!chip.interrupt_pending() && chip.status96==0x40,"mask must not clear sticky status");
    chip.write({0x72,0x94,0xc0}); check(chip.interrupt_pending(),"unmask must expose retained status");
    chip.hotplug(false); check(chip.status96==0xc0 && chip.interrupt_pending(),"HPD reset must preserve both sticky latches");
    chip.write({0x72,0x96,0x80}); check(chip.status96==0x40 && chip.interrupt_pending(),"HPD W1C cannot clear monitor latch");
    chip.write({0x72,0x96,0x40}); check(!chip.interrupt_pending(),"shared IRQ stays active until all enabled status clears");
    chip.write({0x72,0x41,0x10}); check(chip.power_down,"lowHPD must prohibit power-up");
    chip.hotplug(true); chip.write({0x72,0x41,0x10}); check(!chip.power_down,"highHPD explicit power-up");
    chip.write({0x72,0x01,0x55}); check(chip.value[1]==0x55 && chip.last_high_epoch[1]==chip.high_epoch,"highHPD config observation");
    chip.write({0x72,0x94,0}); chip.hotplug(false);
    check(chip.last_high_epoch[1]==0 && chip.mask94==0xc0 && chip.hpd_irq(),"HPD resets configuration and masks but retains HPD IRQ");
    chip.write({0x72,0x01,0x55}); check(chip.last_high_epoch[1]==0 && chip.low_config_writes_unqualified>0,"lowHPD config write cannot qualify restoration");
    chip.hotplug(true); check(chip.last_high_epoch[1]!=chip.high_epoch,"last rise requires new configuration replay");
    chip.write({0x72,0x01,0x55}); check(chip.last_high_epoch[1]==chip.high_epoch,"post-rise write certifies that register only");
    std::puts("{\"case\":\"model-controls\",\"status\":\"pass\",\"PD_AD\":\"assumed inactive\",\"CEC_CDC_events\":\"assumed absent\",\"low_HPD_config_acceptance\":\"unspecified, not claimed\"}");
}
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    if(argc==2&&!std::strcmp(argv[1],"--controls")){controls();return 0;}
    if(argc>=3&&!std::strcmp(argv[1],"--phase")) {reconnect(std::strtoul(argv[2],nullptr,10),argc==4&&!std::strcmp(argv[3],"--monitor"));return 0;}
    for (unsigned nack : {3u,0u,1u,2u}) scenario(nack);
    return 0;
}
