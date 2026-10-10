// SPDX-License-Identifier: GPL-3.0-or-later
// Original AUTO PRG under stock EmuTOS, using the real CPU, DMA, sector
// writer and physical SDRAM command model with concurrent dual-clock video.
// The immutable disk is a preloaded test fixture, not a host upload proof.
#include "Vst_boot_disk_sim_top.h"
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
    std::vector<uint16_t> words = std::vector<uint16_t>(0xa0000, 0);
    uint64_t cycle = 0, reads = 0, writes = 0, refreshes = 0, mode_sets = 0;
    uint64_t last_refresh = 0, read_due = 0;
    uint16_t pending_read = 0;
    std::array<unsigned, 4> row{};
    std::array<bool, 4> open{};
    std::array<uint64_t, 4> activated{}, available{};
    bool precharged = false, initialized = false;

    uint16_t tick(const Vst_boot_disk_sim_top &dut) {
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
    Vst_boot_disk_sim_top dut;
    Sdram sdram;
    std::vector<uint8_t> rom;
    std::vector<uint8_t> initial_disk, program;
    std::ofstream trace;
    std::string capture_prefix;
    std::vector<uint8_t> picture = std::vector<uint8_t>(1280 * 720 * 3);
    std::vector<uint8_t> complete_picture;
    Pending rom_transfer, cpu_transfer, video_transfer, dma_transfer, disk_read_transfer, disk_write_transfer;
    uint64_t next_sys = 0, next_pixel = 9871, system_cycles = 0, pixel_cycles = 0, boot_start = 0;
    uint64_t faults = 0, mfp = 0, vbl = 0, cpu_reads = 0, cpu_writes = 0, video_reads = 0;
    uint64_t concurrent_cycles = 0, complete_frames = 0;
    uint64_t disk_reads = 0, disk_writes = 0, dma_reads = 0, dma_writes = 0;
    uint64_t program_fetches = 0, trap_fetches = 0, pass_first_cycle = 0;
    unsigned program_base = 0;
    unsigned max_cpu_latency = 0, max_video_latency = 0;
    bool last_fault = false, last_ack = false, picture_started = false;
    uint32_t previous_request = 0;
    unsigned position = 0, previous_position = 0;

    uint16_t word(unsigned address) const {
        require(!(address & 1) && address < 524288, "RAM observation outside 512 KiB", system_cycles);
        return sdram.words[address / 2];
    }
    uint32_t longword(unsigned address) const {
        return (uint32_t(word(address)) << 16) | word(address + 2);
    }

    uint8_t disk_byte(unsigned offset) const {
        require(offset < 737280, "disk observation outside exact .st", system_cycles);
        const uint16_t value = sdram.words[0x40000 + offset / 2];
        return offset & 1 ? value & 0xff : value >> 8;
    }
    unsigned disk_le(unsigned offset, unsigned size) const {
        unsigned value = 0;
        for (unsigned i = 0; i < size; ++i) value |= unsigned(disk_byte(offset + i)) << (8 * i);
        return value;
    }
    bool disk_pass() const {
        constexpr char expected[] = "FES ST GEMDOS disk diagnostic v1\r\nPASS create/write/close/reopen/read/rename/delete 1537 bytes\r\n";
        unsigned pass = 0;
        for (unsigned i = 0; i < 112; ++i) {
            const unsigned entry = 7 * 512 + i * 32;
            if (!disk_byte(entry)) break;
            if (disk_byte(entry) == 0xe5) continue;
            std::string name;
            for (unsigned j = 0; j < 11; ++j) name += char(disk_byte(entry + j));
            if (name == "FAIL    TXT" || name == "FESDATA TMP" || name == "FESDATA NEW") return false;
            if (name == "PASS    TXT") pass = entry;
        }
        if (!pass || disk_le(pass + 28, 4) != sizeof(expected) - 1) return false;
        const unsigned cluster = disk_le(pass + 26, 2);
        if (cluster < 2 || cluster >= 715) return false;
        const unsigned offset = 14 * 512 + (cluster - 2) * 1024;
        for (unsigned i = 0; i < sizeof(expected) - 1; ++i)
            if (disk_byte(offset + i) != uint8_t(expected[i])) return false;
        return true;
    }

    void program_fetch() {
        if (cpu_transfer.write || dut.cpu_fc != 2) return;
        const unsigned address = cpu_transfer.address * 2;
        if (!program_base && address + program.size() - 28 <= 524288 &&
            word(address) == 0x7cff && word(address + 2) == 0x286f && word(address + 4) == 4) {
            for (unsigned i = 0; i < program.size() - 28; ++i) {
                const uint16_t value = sdram.words[(address + i) / 2];
                require(uint8_t((address + i) & 1 ? value : value >> 8) == program[28 + i],
                        "guest loaded PRG differs from original generated text", system_cycles);
            }
            program_base = address;
            std::cout << "guest original PRG entry=" << std::hex << address << std::dec << '\n' << std::flush;
        }
        if (program_base && address >= program_base && address < program_base + program.size() - 28) {
            ++program_fetches;
            const auto opcode = word(address);
            trap_fetches += opcode == 0x4e41;
            if (program_fetches <= 20000)
                trace << "{\"cycle\":" << system_cycles << ",\"address\":" << address
                      << ",\"pc\":" << dut.debug_pc << ",\"word\":" << opcode << "}\n";
        }
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
        track_before(dma_transfer, dut.dma_req, dut.dma_addr, dut.dma_wdata,
                     dut.dma_byte_enable, dut.dma_write, "DMA request changed before completion");
        track_before(disk_read_transfer, dut.media_req, dut.media_addr, 0, 3, false,
                     "media read changed before completion");
        track_before(disk_write_transfer, dut.media_write_req, dut.media_write_addr,
                     dut.media_write_data, 3, true, "sector write changed before completion");
        dut.dq_sample = sdram.tick(dut);
        dut.clk_sys = 1;
        dut.eval();
        ++system_cycles;
        if (dut.cpu_ready) {
            require(cpu_transfer.active && !cpu_transfer.complete,
                    "CPU SDRAM completion duplicated or unrequested", system_cycles);
            cpu_transfer.complete = true;
            if (cpu_transfer.write) ++cpu_writes; else ++cpu_reads;
            if (cpu_transfer.wait > max_cpu_latency) max_cpu_latency = cpu_transfer.wait;
            program_fetch();
        }
        if (dut.dma_ready) {
            require(dma_transfer.active && !dma_transfer.complete, "duplicate/unrequested DMA completion", system_cycles);
            dma_transfer.complete = true;
            if (dma_transfer.write) ++dma_writes; else ++dma_reads;
        }
        if (dut.media_valid) {
            require(disk_read_transfer.active && !disk_read_transfer.complete, "duplicate/unrequested media read", system_cycles);
            disk_read_transfer.complete = true; ++disk_reads;
        }
        if (dut.media_write_ready) {
            require(disk_write_transfer.active && !disk_write_transfer.complete, "duplicate/unrequested sector write", system_cycles);
            disk_write_transfer.complete = true; ++disk_writes;
        }
        if (dut.video_ready) {
            require(video_transfer.active && !video_transfer.complete,
                    "video SDRAM completion duplicated or unrequested", system_cycles);
            video_transfer.complete = true;
            ++video_reads;
            if (video_transfer.wait > max_video_latency) max_video_latency = video_transfer.wait;
        }
        concurrent_cycles += dut.cpu_req && dut.video_req;
        if (!dut.reset_sys) {
            if (dut.debug_bus_error && !last_fault) ++faults;
            if (dut.irq_ack && !last_ack) {
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

    void status() {
        std::cout << "assembled seconds=" << std::dec << (system_cycles - boot_start) / SystemHz
                  << " pc=" << std::hex << dut.debug_pc << " bus=" << dut.debug_addr
                  << " phystop=" << longword(0x42e) << " screen=" << longword(0x44e)
                  << " memvalid=" << longword(0x420) << " hz200=" << std::dec << longword(0x4ba)
                  << " frclock=" << longword(0x466) << " CPU R/W=" << cpu_reads << '/' << cpu_writes
                  << " video R=" << video_reads << " refreshes=" << sdram.refreshes
                  << " faults=" << faults << " MFP/VBL=" << mfp << '/' << vbl
                  << " underruns=" << dut.debug_underruns << " disk R/W=" << disk_reads << '/' << disk_writes
                  << " DMA R/W=" << dma_reads << '/' << dma_writes
                  << " original PRG fetch/trap=" << program_fetches << '/' << trap_fetches
                  << " PASS=" << disk_pass() << '\n' << std::flush;
        trace.flush();
        // Keep the last coherent observation even if a later guest assertion
        // fails. This is observation only, never a CPU/media response callback.
        std::ofstream progress(capture_prefix + "-progress-disk.st", std::ios::binary);
        for (unsigned i = 0; i < initial_disk.size(); ++i) progress.put(char(disk_byte(i)));
    }

public:
    Boot(const char *path, const char *disk_path, const char *program_path, const char *prefix) {
        capture_prefix = prefix;
        std::ifstream input(path, std::ios::binary);
        require(input.good(), "stock ROM unavailable");
        rom.assign(std::istreambuf_iterator<char>(input), {});
        require(rom.size() == 196608, "stock ROM must be exactly 192 KiB");
        std::ifstream disk_input(disk_path, std::ios::binary), program_input(program_path, std::ios::binary);
        require(disk_input.good() && program_input.good(), "original diagnostic fixture unavailable");
        initial_disk.assign(std::istreambuf_iterator<char>(disk_input), {});
        program.assign(std::istreambuf_iterator<char>(program_input), {});
        require(initial_disk.size() == 737280 && program.size() > 28 &&
                program[0] == 0x60 && program[1] == 0x1a, "diagnostic disk/PRG size or magic is wrong");
        for (unsigned i = 0; i < initial_disk.size(); i += 2)
            sdram.words[0x40000 + i / 2] = (uint16_t(initial_disk[i]) << 8) | initial_disk[i + 1];
        trace.open(std::string(prefix) + "-guest-fetch.jsonl");
        require(trace.good(), "cannot retain original guest instruction trace");
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

    void run(unsigned seconds, const char *prefix) {
        uint64_t next_status = boot_start + SystemHz;
        const uint64_t limit = boot_start + SystemHz * seconds;
        while (system_cycles < limit) {
            event();
            if (system_cycles >= next_status) {
                status();
                if (!pass_first_cycle && disk_pass()) pass_first_cycle = system_cycles;
                next_status += SystemHz;
            }
            if (pass_first_cycle && system_cycles >= boot_start + 8 * SystemHz && !dut.media_write_busy &&
                (dut.debug_pc < program_base || dut.debug_pc >= program_base + program.size() - 28)) break;
        }
        status();
        require(longword(0x420) == 0x752019f3, "EmuTOS did not validate physical RAM", system_cycles);
        require(longword(0x42e) == 0x80000, "EmuTOS did not size physical RAM at 512 KiB", system_cycles);
        require(longword(0x44e) == 0x78000 && dut.screen_base == 0x78000,
                "EmuTOS framebuffer base is incorrect", system_cycles);
        require(longword(0x4ba) > 1000 && mfp > 1000, "200 Hz timer did not continue during contention", system_cycles);
        require(longword(0x466) > 100 && vbl > 100, "VBL processing stopped during contention", system_cycles);
        require(cpu_writes > 100000 && video_reads > 100000 && concurrent_cycles > 100000,
                "CPU and video did not exercise simultaneous shared SDRAM traffic", system_cycles);
        require(complete_frames > 100 && complete_picture.size() == 1280 * 720 * 3,
                "no complete fixed-raster output frame captured", system_cycles);
        std::set<uint32_t> colors;
        for (unsigned i = 0; i < complete_picture.size(); i += 3)
            colors.insert((uint32_t(complete_picture[i]) << 16) |
                          (uint32_t(complete_picture[i + 1]) << 8) | complete_picture[i + 2]);
        require(colors.size() >= 3, "rendered EmuTOS screen lacks meaningful color content", system_cycles);
        require(program_base != 0 && program_fetches > 1000 && trap_fetches >= 15,
                "original PRG did not execute through actual user-mode CPU fetches", system_cycles);
        require(disk_reads > 1024 && disk_writes > 1024 && dma_reads > 1024 && dma_writes > 1024,
                "guest file operations did not use real DMA and media SDRAM", system_cycles);
        require(disk_pass(), "guest did not publish exact PASS marker and delete transient files", system_cycles);
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
        std::ofstream captured(std::string(prefix) + "-disk.st", std::ios::binary);
        require(captured.good(), "cannot retain guest disk capture", system_cycles);
        uint64_t changed_bytes = 0;
        for (unsigned i = 0; i < initial_disk.size(); ++i) {
            const auto byte = disk_byte(i); captured.put(char(byte));
            changed_bytes += byte != initial_disk[i];
        }
        require(changed_bytes > 1537, "guest disk capture lacks patterned file and FAT changes", system_cycles);
        std::ofstream metrics(std::string(prefix) + "-guest.json");
        metrics << "{\"schema\":1,\"system_cycles\":" << system_cycles - boot_start
                << ",\"program_base\":" << program_base << ",\"program_fetches\":" << program_fetches
                << ",\"trap_fetches\":" << trap_fetches << ",\"disk_reads\":" << disk_reads
                << ",\"disk_writes\":" << disk_writes << ",\"dma_reads\":" << dma_reads
                << ",\"dma_writes\":" << dma_writes << ",\"disk_changed_bytes\":" << changed_bytes
                << ",\"pass_first_cycle\":" << pass_first_cycle - boot_start
                << ",\"mfp\":" << mfp << ",\"vbl\":" << vbl
                << ",\"cpu_reads\":" << cpu_reads << ",\"cpu_writes\":" << cpu_writes
                << ",\"video_reads\":" << video_reads << ",\"concurrent_cycles\":" << concurrent_cycles
                << ",\"refreshes\":" << sdram.refreshes << ",\"faults\":" << faults
                << ",\"underruns\":" << dut.debug_underruns << ",\"complete_frames\":" << complete_frames << "}\n";
        std::cout << "Stock EmuTOS original GEMDOS disk SDRAM/video PASS: " << system_cycles - boot_start
                  << " system clocks, " << pixel_cycles << " independent pixel clocks, "
                  << complete_frames << " complete frames, " << colors.size() << " RGB colors; max CPU/video latency "
                  << max_cpu_latency << '/' << max_video_latency << " clocks; output " << prefix << ".ppm\n";
        dut.final();
    }
};

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 6, "usage: boot_disk_tb STOCK_192K_ROM AUTO_DISK ORIGINAL_PRG SECONDS OUTPUT_PREFIX");
    const unsigned seconds = unsigned(std::strtoul(argv[4], nullptr, 10));
    require(seconds >= 8 && seconds <= 30, "guest duration must be 8..30 emulated seconds");
    Boot boot(argv[1],argv[2],argv[3],argv[5]);
    boot.run(seconds,argv[5]);
    return 0;
}
