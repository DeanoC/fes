// SPDX-License-Identifier: GPL-3.0-or-later
// Boot stock EmuTOS through physical SDRAM commands while the native line
// cache scans that same RAM with an independent 74.25 MHz pixel clock.
#include "Vst_boot_memory_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <iterator>
#include <set>
#include <string>
#include <vector>

static void require(bool value, const char *what, uint64_t cycle = 0) {
    if (!value) {
        std::cerr << "FAIL: " << what << " at system cycle " << cycle << '\n';
        std::exit(1);
    }
}

// Same digital SDRAM command checks as memory_tb.cpp: packed addresses, CAS2,
// single-word bursts, auto-precharge, byte masks, initialization and refresh.
// ISSI IS42S16320D conservative whole-cycle limits at 52.224 MHz; this model
// does not establish analog pad setup/hold, electrical behavior or routing.
// https://www.issi.com/WW/pdf/42-45R-S_86400D-16320D-32160D.pdf
class Sdram {
public:
    std::vector<uint16_t> words = std::vector<uint16_t>(0xa6800, 0);
    uint64_t cycle = 0, reads = 0, writes = 0, refreshes = 0, mode_sets = 0;
    uint64_t last_refresh = 0, read_due = 0;
    uint16_t pending_read = 0;
    std::array<unsigned, 4> row{};
    std::array<bool, 4> open{};
    std::array<uint64_t, 4> activated{}, available{};
    bool precharged = false, initialized = false;

    uint16_t tick(const Vst_boot_memory_sim_top &dut) {
        ++cycle;
        const uint16_t sample = read_due == cycle ? pending_read : 0xf13d;
        if (!dut.sdram_cke || dut.sdram_ncs) return sample;
        const unsigned command = (dut.sdram_nras << 2) | (dut.sdram_ncas << 1) | dut.sdram_nwe;
        const unsigned bank = dut.sdram_ba;
        const unsigned address = ((dut.sdram_a & 0x3fc) << 15) |
            (row[bank] << 4) | (bank << 2) | (dut.sdram_a & 3);
        if (command != 7)
            require(cycle - last_refresh >= 5 || refreshes == 0, "SDRAM tRFC violated", cycle);
        switch (command) {
        case 3:
            require(initialized, "ACTIVATE before mode initialization", cycle);
            require(!open[bank] && cycle >= available[bank], "ACTIVATE before bank recovery", cycle);
            open[bank] = true;
            row[bank] = dut.sdram_a;
            activated[bank] = cycle;
            break;
        case 4:
        case 5:
            require(open[bank], "column command without open row", cycle);
            require(cycle - activated[bank] >= 2, "SDRAM tRCD violated", cycle);
            require(address < words.size(), "physical memory address escaped RAM/media bounds", cycle);
            require(dut.sdram_a & 0x400, "column command lacks auto-precharge", cycle);
            open[bank] = false;
            available[bank] = cycle + (command == 4 ? 2 : 3);
            if (command == 4) {
                require(dut.dq_oe, "WRITE without driven data", cycle);
                if (!(dut.sdram_a & 0x800)) words[address] = (words[address] & 0xff00) | (dut.dq_out & 0xff);
                if (!(dut.sdram_a & 0x1000)) words[address] = (words[address] & 0xff) | (dut.dq_out & 0xff00);
                ++writes;
            } else {
                require(!dut.dq_oe, "READ has output data contention", cycle);
                require(!(dut.sdram_a & 0x1800), "READ unexpectedly masked", cycle);
                read_due = cycle + 2;
                pending_read = words[address];
                ++reads;
            }
            break;
        case 2:
            require(dut.sdram_a & 0x400, "PRECHARGE is not all-bank", cycle);
            require(cycle >= 5223, "SDRAM initialization power delay too short", cycle);
            for (unsigned b = 0; b < 4; ++b) {
                require(!open[b] || cycle - activated[b] >= 2, "SDRAM tRAS violated", cycle);
                require(cycle >= available[b], "PRECHARGE before write/read recovery", cycle);
                open[b] = false;
                available[b] = cycle + 1;
            }
            precharged = true;
            break;
        case 1:
            require(precharged, "refresh before initial precharge", cycle);
            for (unsigned b = 0; b < 4; ++b)
                require(!open[b] && cycle >= available[b], "refresh with active/recovering bank", cycle);
            last_refresh = cycle;
            ++refreshes;
            break;
        case 0:
            require(refreshes >= 2, "mode set before two initialization refreshes", cycle);
            require(dut.sdram_a == 0x20, "SDRAM requires burst one, CAS two", cycle);
            initialized = true;
            ++mode_sets;
            break;
        case 7: break;
        default: require(false, "unexpected SDRAM command", cycle);
        }
        return sample;
    }
};

struct Pending {
    bool active = false, complete = false;
    unsigned wait = 0;
    uint32_t address = 0;
    uint16_t data = 0;
    uint8_t lanes = 0;
    bool write = false;
};

class Boot {
    static constexpr uint64_t SystemHz = 52224000;
    static constexpr unsigned Width = 1650, Height = 750, Frame = Width * Height;
    static constexpr uint32_t DE = 1u << 24, HS = 1u << 25, VS = 1u << 26;
    static constexpr uint32_t CE = 1u << 27, SOF = 1u << 28, EOL = 1u << 29, HOLD = 1u << 30;
    Vst_boot_memory_sim_top dut;
    Sdram sdram;
    std::vector<uint8_t> rom;
    std::vector<uint8_t> picture = std::vector<uint8_t>(1280 * 720 * 3);
    std::vector<uint8_t> complete_picture;
    Pending rom_transfer, cpu_transfer, video_transfer;
    uint64_t next_sys = 0, next_pixel = 9871, system_cycles = 0, pixel_cycles = 0, boot_start = 0;
    uint64_t faults = 0, mfp = 0, vbl = 0, cpu_reads = 0, cpu_writes = 0, video_reads = 0;
    uint64_t concurrent_cycles = 0, complete_frames = 0;
    unsigned max_cpu_latency = 0, max_video_latency = 0;
    bool last_fault = false, last_ack = false, picture_started = false;
    uint32_t previous_request = 0;
    unsigned position = 0, previous_position = 0;
    bool demo_mode = false;
    unsigned trace_start = 6, trace_end = 7;
    uint64_t native_epoch = 0, palette_writes = 0, frame_palette_writes = 0;
    unsigned observed_frames = 0, frame_cpu_max = 0, frame_video_max = 0;
    uint64_t frame_video_reads = 0;
    std::vector<uint8_t> native_picture = std::vector<uint8_t>(320*200*3);
    std::ofstream raster_trace, logo_trace;
    std::set<uint64_t> saved_logo_hashes;
    std::string trace_prefix;
    bool tracing() const { return system_cycles >= boot_start + trace_start*SystemHz &&
                                 system_cycles < boot_start + trace_end*SystemHz; }

    uint16_t word(unsigned address) const {
        require(!(address & 1) && address < 524288, "RAM observation outside 512 KiB", system_cycles);
        return sdram.words[address / 2];
    }
    uint32_t longword(unsigned address) const {
        return (uint32_t(word(address)) << 16) | word(address + 2);
    }

    void rom_before_edge() {
        if (dut.reset_sys || !dut.rom_req) {
            require(dut.reset_sys || !rom_transfer.active || rom_transfer.complete,
                    "ROM request abandoned before ready", system_cycles);
            rom_transfer = {};
            dut.rom_ready = 0;
            return;
        }
        const unsigned address = dut.rom_addr * 2;
        require(address + 1 < rom.size(), "ROM request escaped exact 192 KiB", system_cycles);
        if (!rom_transfer.active) {
            rom_transfer.active = true;
            rom_transfer.address = address;
            rom_transfer.wait = 2 + (address % 3);
        }
        require(rom_transfer.address == address, "ROM address changed before ready", system_cycles);
        dut.rom_rdata = (uint16_t(rom[address]) << 8) | rom[address + 1];
        if (rom_transfer.wait) {
            --rom_transfer.wait;
            dut.rom_ready = 0;
        } else {
            dut.rom_ready = 1;
            rom_transfer.complete = true;
        }
    }

    void track_before(Pending& pending, bool req, unsigned address, uint16_t data,
                      uint8_t lanes, bool writing, const char *message) {
        if (dut.reset_sys || !req) {
            require(dut.reset_sys || !pending.active || pending.complete,
                    "memory client abandoned request before ready", system_cycles);
            pending = {};
        } else if (!pending.active) {
            pending.active = true;
            pending.address = address;
            pending.data = data;
            pending.lanes = lanes;
            pending.write = writing;
        } else {
            require(pending.address == address && pending.data == data &&
                    pending.lanes == lanes && pending.write == writing, message, system_cycles);
        }
        if (req) ++pending.wait;
    }

    void system_edge() {
        rom_before_edge();
        track_before(cpu_transfer, dut.cpu_req, dut.cpu_addr, dut.cpu_wdata,
                     dut.cpu_byte_enable, dut.cpu_write, "CPU SDRAM request changed before completion");
        track_before(video_transfer, dut.video_req, dut.video_addr, 0, 3, false,
                     "video SDRAM request changed before completion");
        dut.dq_sample = sdram.tick(dut);
        dut.eval();
        const unsigned line = dut.debug_native_line, phase = dut.debug_horizontal_phase;
        const bool native_vblank = !dut.reset_sys && dut.vblank;
        const bool palette_write = !dut.reset_sys && dut.debug_palette_write;
        if (demo_mode && palette_write) {
            ++palette_writes; ++frame_palette_writes;
            if (tracing()) raster_trace << "{\"kind\":\"palette_write\",\"cycle\":" << system_cycles
                << ",\"frame\":" << native_epoch << ",\"line\":" << line << ",\"horizontal_phase\":" << phase
                << ",\"address\":" << dut.debug_palette_address << ",\"data\":" << dut.debug_palette_data
                << ",\"lanes\":" << unsigned(dut.debug_palette_lanes) << "}\n";
        }
        if (demo_mode && dut.capture_pixel) {
            require(dut.capture_x < 320 && dut.capture_y < 200, "native capture coordinate bounds", system_cycles);
            const unsigned offset = (dut.capture_y*320 + dut.capture_x)*3, rgb = dut.capture_rgb;
            for (unsigned c=0;c<3;++c) {
                const unsigned v=(rgb >> (6-c*3)) & 7;
                native_picture[offset+c]=(v<<5)|(v<<2)|(v>>1);
            }
        }
        dut.clk_sys = 1;
        dut.eval();
        ++system_cycles;
        if (dut.cpu_ready) {
            require(cpu_transfer.active && !cpu_transfer.complete,
                    "CPU SDRAM completion duplicated or unrequested", system_cycles);
            cpu_transfer.complete = true;
            if (cpu_transfer.write) ++cpu_writes; else ++cpu_reads;
            if (cpu_transfer.wait > frame_cpu_max) frame_cpu_max = cpu_transfer.wait;
            if (cpu_transfer.wait > max_cpu_latency) max_cpu_latency = cpu_transfer.wait;
        }
        if (dut.video_ready) {
            require(video_transfer.active && !video_transfer.complete,
                    "video SDRAM completion duplicated or unrequested", system_cycles);
            video_transfer.complete = true;
            ++video_reads; ++frame_video_reads;
            if (video_transfer.wait > frame_video_max) frame_video_max = video_transfer.wait;
            if (video_transfer.wait > max_video_latency) max_video_latency = video_transfer.wait;
        }
        if (demo_mode && dut.native_frames != observed_frames) {
            observed_frames = dut.native_frames;
            if (tracing()) {
                uint64_t hash=UINT64_C(14695981039346656037);
                std::array<uint64_t,64> row_hashes;
                for (unsigned y=0;y<64;++y) {
                    row_hashes[y]=UINT64_C(14695981039346656037);
                    for (unsigned x=65;x<245;++x) for (unsigned c=0;c<3;++c) {
                        const auto value=native_picture[(y*320+x)*3+c];
                        hash^=value; hash*=UINT64_C(1099511628211);
                        row_hashes[y]^=value; row_hashes[y]*=UINT64_C(1099511628211);
                    }
                }
                logo_trace << "{\"cycle\":" << system_cycles << ",\"frame\":" << native_epoch
                    << ",\"capture\":" << observed_frames << ",\"logo_fnv1a64\":\"" << std::hex << hash << std::dec
                    << "\",\"palette_writes_so_far\":" << frame_palette_writes << ",\"fetches\":" << frame_video_reads
                    << ",\"max_fetch_wait\":" << frame_video_max << ",\"max_cpu_ram_wait\":" << frame_cpu_max
                    << ",\"underruns\":" << dut.native_underruns << ",\"row_fnv1a64\":[";
                for (unsigned y=0;y<64;++y) { if(y) logo_trace << ','; logo_trace << '"' << std::hex << row_hashes[y] << std::dec << '"'; }
                logo_trace << "]}\n";
                if (saved_logo_hashes.size()<8 && saved_logo_hashes.insert(hash).second) {
                    std::ofstream image(trace_prefix+"-trace-logo-"+std::to_string(native_epoch)+"-native.ppm", std::ios::binary);
                    require(image.good(),"trace logo image unavailable");
                    image << "P6\n320 200\n255\n";
                    image.write(reinterpret_cast<const char*>(native_picture.data()),native_picture.size());
                }
            }
        }
        if (native_vblank) { ++native_epoch; frame_palette_writes=0; frame_cpu_max=frame_video_max=0; frame_video_reads=0; }
        concurrent_cycles += dut.cpu_req && dut.video_req;
        if (!dut.reset_sys) {
            if (dut.debug_bus_error && !last_fault) ++faults;
            if (dut.irq_ack && !last_ack) {
                if (demo_mode && tracing()) raster_trace << "{\"cycle\":" << system_cycles
                    << ",\"frame\":" << native_epoch << ",\"line\":" << line << ",\"horizontal_phase\":" << phase
                    << ",\"iack\":" << unsigned(dut.irq_level) << ",\"mfp_vector\":"
                    << (dut.irq_level==6 ? unsigned(dut.debug_irq_vector) : 0) << "}\n";
                if (dut.irq_level == 6) ++mfp;
                if (dut.irq_level == 4) ++vbl;
            }
            require(system_cycles - boot_start < 1000 || !dut.debug_halted,
                    "CPU double-fault HALT during assembled SDRAM boot", system_cycles);
        }
        last_fault = dut.debug_bus_error;
        last_ack = dut.irq_ack;
        dut.clk_sys = 0;
        dut.eval();
    }

    void pixel_edge() {
        const uint32_t current = dut.video_request;
        if (!dut.reset_pixel) {
            const unsigned x = position % Width, y = position / Width;
            uint32_t timing = CE;
            if (x < 1280 && y < 720) timing |= DE;
            if (x >= 1390 && x < 1430) timing |= HS;
            if (y >= 725 && y < 730) timing |= VS;
            if (!position) timing |= SOF;
            if (x == 1649) timing |= EOL;
            require((current & ~(0xffffff | HOLD)) == timing,
                    "assembled scanout lost fixed raster timing", system_cycles);
        }
        dut.clk_pixel = 1;
        dut.eval();
        if (dut.reset_pixel) {
            position = previous_position = 0;
            previous_request = 0;
        } else {
            const uint32_t timing = previous_request & (CE | DE | HS | VS);
            const uint32_t rgb = (previous_request & DE) && !(previous_request & HOLD)
                ? previous_request & 0xffffff : 0;
            require(dut.video_response == (timing | rgb),
                    "assembled direct video part/boundaries misaligned", system_cycles);
            if (previous_request & SOF) {
                if (picture_started) {
                    complete_picture = picture;
                    ++complete_frames;
                }
                picture_started = true;
            }
            if (previous_request & DE) {
                const unsigned x = previous_position % Width, y = previous_position / Width;
                const unsigned offset = (y * 1280 + x) * 3;
                picture[offset] = rgb >> 16;
                picture[offset + 1] = rgb >> 8;
                picture[offset + 2] = rgb;
            }
            previous_request = current;
            previous_position = position;
            position = (position + 1) % Frame;
        }
        dut.clk_pixel = 0;
        dut.eval();
        ++pixel_cycles;
    }

    void event() {
        // Exact frequency ratio, with unrelated phase. Units are arbitrary
        // integer event ticks, not analog timing or an HDL delay model.
        if (next_sys < next_pixel) {
            system_edge();
            next_sys += 74250;
        } else {
            pixel_edge();
            next_pixel += 52224;
        }
    }

    void status() const {
        std::cout << "assembled seconds=" << std::dec << (system_cycles - boot_start) / SystemHz
                  << " pc=" << std::hex << dut.debug_pc << " bus=" << dut.debug_addr
                  << " phystop=" << longword(0x42e) << " screen=" << longword(0x44e)
                  << " memvalid=" << longword(0x420) << " hz200=" << std::dec << longword(0x4ba)
                  << " frclock=" << longword(0x466) << " CPU R/W=" << cpu_reads << '/' << cpu_writes
                  << " video R=" << video_reads << " refreshes=" << sdram.refreshes
                  << " faults=" << faults << " MFP/VBL=" << mfp << '/' << vbl
                  << " native frames/skipped/underruns=" << dut.native_frames << '/'
                  << dut.native_skipped << '/' << dut.native_underruns
                  << " indexed underruns=" << dut.debug_underruns << '\n' << std::flush;
    }

public:
    explicit Boot(const char *path, const char *disk_path = nullptr) {
        std::ifstream input(path, std::ios::binary);
        require(input.good(), "stock ROM unavailable");
        rom.assign(std::istreambuf_iterator<char>(input), {});
        require(rom.size() == 196608, "stock ROM must be exactly 192 KiB");
        dut.media_ready=0; dut.media_size=737280;
        if (disk_path) {
            std::ifstream disk_input(disk_path, std::ios::binary);
            require(disk_input.good(), "demo disk unavailable");
            std::vector<uint8_t> disk{std::istreambuf_iterator<char>(disk_input), {}};
            require(disk.size() >= 368640 && disk.size() <= 839680 && !(disk.size()&1), "demo disk bounds");
            for (unsigned i=0;i<disk.size();i+=2) sdram.words[0x40000+i/2]=(uint16_t(disk[i])<<8)|disk[i+1];
            dut.media_size=disk.size(); dut.media_ready=1; demo_mode=true;
        }
        dut.clk_sys = dut.clk_pixel = 0;
        dut.reset_sys = dut.reset_pixel = dut.cold_reset = 1;
        dut.rom_ready = 0;
        dut.rom_rdata = 0xffff;
        dut.dq_sample = 0;
        dut.eval();
        while (system_cycles < 64) event();
        dut.cold_reset = 0;
        dut.eval();
        while (!dut.initialized) {
            event();
            require(system_cycles < 20000, "physical SDRAM initialization timed out", system_cycles);
        }
        require(sdram.initialized && sdram.mode_sets == 1,
                "controller initialized without physical mode-register sequence", system_cycles);
        dut.reset_sys = dut.reset_pixel = 0;
        boot_start = system_cycles;
        dut.eval();
        std::cout << "physical SDRAM initialized after " << system_cycles << " clocks\n" << std::flush;
    }

    void run(unsigned seconds, const char *prefix, unsigned start=6, unsigned end=7) {
        trace_start=start; trace_end=end;
        if (demo_mode) {
            trace_prefix=prefix;
            raster_trace.open(std::string(prefix)+"-raster.jsonl");
            logo_trace.open(std::string(prefix)+"-logo.jsonl");
            require(raster_trace.good() && logo_trace.good(), "demo trace output unavailable");
        }
        uint64_t next_status = boot_start + SystemHz;
        const uint64_t limit = boot_start + SystemHz * seconds;
        while (system_cycles < limit) {
            event();
            if (system_cycles >= next_status) {
                status();
                next_status += SystemHz;
            }
        }
        status();
        require(dut.native_frames > 100 && dut.native_underruns == 0,
                "native low-resolution capture missed frames/rows under SDRAM contention", system_cycles);
        if (!demo_mode) {
        require(longword(0x420) == 0x752019f3, "EmuTOS did not validate physical RAM", system_cycles);
        require(longword(0x42e) == 0x80000, "EmuTOS did not size physical RAM at 512 KiB", system_cycles);
        require(longword(0x44e) == 0x78000 && dut.screen_base == 0x78000,
                "EmuTOS framebuffer base is incorrect", system_cycles);
        require(longword(0x4ba) > 1000 && mfp > 1000, "200 Hz timer did not continue during contention", system_cycles);
        require(longword(0x466) > 100 && vbl > 100, "VBL processing stopped during contention", system_cycles);
        }
        require(cpu_writes > 100000 && video_reads > 100000 && concurrent_cycles > 100000,
                "CPU and video did not exercise simultaneous shared SDRAM traffic", system_cycles);
        require(complete_frames > 100 && complete_picture.size() == 1280 * 720 * 3,
                "no complete fixed-raster output frame captured", system_cycles);
        std::set<uint32_t> colors;
        for (unsigned i = 0; i < complete_picture.size(); i += 3)
            colors.insert((uint32_t(complete_picture[i]) << 16) |
                          (uint32_t(complete_picture[i + 1]) << 8) | complete_picture[i + 2]);
        require(colors.size() >= 3, "rendered EmuTOS screen lacks meaningful color content", system_cycles);
        std::ofstream ppm(std::string(prefix) + ".ppm", std::ios::binary);
        require(ppm.good(), "cannot write rendered PPM", system_cycles);
        ppm << "P6\n1280 720\n255\n";
        ppm.write(reinterpret_cast<const char *>(complete_picture.data()), complete_picture.size());
        std::ofstream raw(std::string(prefix) + "-ram.bin", std::ios::binary);
        require(raw.good(), "cannot write physical RAM snapshot", system_cycles);
        for (unsigned i = 0; i < 0x40000; ++i) {
            raw.put(sdram.words[i] >> 8);
            raw.put(sdram.words[i]);
        }
        if (demo_mode) {
            std::ofstream metrics(std::string(prefix)+"-metrics.json");
            metrics << "{\"cycle\":" << system_cycles-boot_start << ",\"bus_faults\":" << faults
                << ",\"halted\":false,\"native_rgb_frames\":" << dut.native_frames
                << ",\"native_rgb_underruns\":" << dut.native_underruns << ",\"indexed_underruns\":" << dut.debug_underruns
                << ",\"committed_palette_writes\":" << palette_writes
                << ",\"cpu_reads\":" << cpu_reads << ",\"cpu_writes\":" << cpu_writes
                << ",\"video_reads\":" << video_reads << ",\"max_cpu_latency\":" << max_cpu_latency
                << ",\"max_video_latency\":" << max_video_latency << ",\"sdram_refreshes\":" << sdram.refreshes << "}\n";
            require(metrics.good(), "demo metrics output unavailable");
        }
        std::cout << (demo_mode ? "Demo diagnostic" : "Stock EmuTOS") << " assembled SDRAM/video boot PASS: " << system_cycles - boot_start
                  << " system clocks, " << pixel_cycles << " independent pixel clocks, "
                  << complete_frames << " complete frames, " << colors.size() << " RGB colors; max CPU/video latency "
                  << max_cpu_latency << '/' << max_video_latency << " clocks; output " << prefix << ".ppm\n";
        dut.final();
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require((argc >= 2 && argc <= 4) || argc==7, "usage: boot_memory_tb STOCK_192K_ROM [SECONDS=8] [OUTPUT_PREFIX=emutos-sdram]");
    const unsigned seconds = argc >= 3 ? unsigned(std::strtoul(argv[2], nullptr, 10)) : 8;
    require(seconds >= 6 && seconds <= 30, "boot duration must be 6..30 emulated seconds");
    Boot boot(argv[1], argc==7 ? argv[4] : nullptr);
    boot.run(seconds, argc >= 4 ? argv[3] : "emutos-sdram", argc==7 ? unsigned(std::strtoul(argv[5],nullptr,10)) : 6,
             argc==7 ? unsigned(std::strtoul(argv[6],nullptr,10)) : 7);
    return 0;
}
