// SPDX-License-Identifier: MIT
// pc386 system bench: pc386_system (the machine, its audio and fes_i386_cpu
// with the firmware ROM) with the three DDR ports on one window model, and a
// host that does what the kit's host does through pc386_host_link: it waits
// for the link's status block, stages the ATA image and the configuration in
// the window, then reads the status block, the debug ring and VRAM. Nothing
// else reaches inside the system, and no oracle backs the CPU: a CPU stop
// ends the run. The host acts every 16 system clocks. The pixel clock runs
// throughout, so the presenter and plane load port 2 as on a kit.
//
// It prints:
//   ATTACH <cycle> <epoch>                  the link's status block appeared
//   CONFIG <cycle>                          the host wrote the configuration
//   POST <code> <cycle>                     each new POST code in the status block
//   DEBUG <text>                            each line from the debug ring
//   PROMPT <cycle>                          the prompt on the screen's last row
//   SCREEN <row> |<80 characters>|          the text screen at the end, from VRAM
//   STATUS <field>=<value> ...              the last status block
//   END <reason> cycles=<n> seconds=<simulated>
// reason: prompt, stopped (the CPU stopped for good), stalled (no
// instruction retired for 2 s: a hang, or HLT with interrupts off), or limit.
//
// usage: system_tb --disk F [--clock-hz N] [--limit-seconds S] [--prompt TEXT]
#include "Vpc386_system.h"
#include "pc386_ddr_model.h"

#include <algorithm>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

constexpr uint32_t CONFIG = 0x01000000u, STATUS = 0x01000040u, RING = 0x01001000u, RING_BYTES = 4096;
constexpr uint32_t IMAGE = 0x08000000u;
constexpr uint32_t CONFIG_MAGIC = 0x43363833u, STATUS_MAGIC = 0x53363833u;
constexpr unsigned HOST_PERIOD = 16;
constexpr double PIXEL_HZ = 74.25e6;

// Port 0 as fes_i386_cpu names it, for pc386::Port0.
struct Port0Signals {
    IData &ddr_address;
    CData &ddr_burstcount, &ddr_read, &ddr_write;
    VlWide<4> &ddr_writedata;
    SData &ddr_byteenable;
    CData &ddr_waitrequest;
    VlWide<4> &ddr_readdata;
    CData &ddr_readdatavalid;
};

pc386::PortSignals port1(Vpc386_system &top) {
    return {&top.p1_address, &top.p1_burstcount, &top.p1_read, &top.p1_write, &top.p1_writedata,
            &top.p1_byteenable, &top.p1_waitrequest, &top.p1_readdata, &top.p1_readdatavalid};
}
pc386::PortSignals port2(Vpc386_system &top) {
    return {&top.p2_address, &top.p2_burstcount, &top.p2_read, &top.p2_write, &top.p2_writedata,
            &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata, &top.p2_readdatavalid};
}

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read " + path);
    return {std::istreambuf_iterator<char>(input), std::istreambuf_iterator<char>()};
}

class Bench {
public:
    Bench(std::vector<uint8_t> image, uint32_t clock_hz, double limit_seconds, std::string prompt)
        : image(std::move(image)), clock_hz(clock_hz), prompt(std::move(prompt)) {
        if (this->image.size() % 512 || this->image.size() > (128u << 20)) throw std::runtime_error("bad disk image size");
        sys_ps = 1e12 / clock_hz;
        limit = uint64_t(limit_seconds * clock_hz);
        geometry();
    }

    std::string run() {
        top.pixel_locked = 0;
        top.p0_reset = top.p1_reset = top.p2_reset = 1;
        for (int n = 0; n < 32; ++n) clock();
        top.pixel_locked = 1;
        top.p0_reset = top.p1_reset = top.p2_reset = 0;
        while (reason.empty()) {
            clock();
            if (cycles % HOST_PERIOD == 0) host();
            if (cycles > limit) reason = "limit";
        }
        for (int row = 0; row < 25; ++row) std::printf("SCREEN %d |%s|\n", row, screen_row(row).c_str());
        print_status();
        std::printf("END %s cycles=%llu seconds=%.3f\n", reason.c_str(), (unsigned long long)cycles,
                    double(cycles) / clock_hz);
        return reason;
    }

private:
    Vpc386_system top;
    pc386::Window window;
    Port0Signals p0{top.p0_address, top.p0_burstcount, top.p0_read, top.p0_write, top.p0_writedata,
                    top.p0_byteenable, top.p0_waitrequest, top.p0_readdata, top.p0_readdatavalid};
    pc386::Port0<pc386::Window> ddr0{window, 0x5eed0020, "port 0"};
    pc386::Port ddr1{window, 0x5eed0021, "port 1"};
    pc386::Port ddr2{window, 0x5eed0022, "port 2"};
    std::vector<uint8_t> image;
    uint32_t clock_hz;
    std::string prompt, reason, debug_line;
    uint16_t cylinders = 0;
    uint8_t heads = 0, spt = 0;
    double sys_ps = 0, now_ps = 0, pixel_ps = 0;
    uint64_t cycles = 0, limit = 0, progress_at = 0, retired = 0, last_screen = 0;
    uint32_t epoch = 0, debug_read = 0;
    int last_post = -1;
    bool attached = false, prompt_seen = false;

    // The geometry machine_tb infers: the first partition's end head and
    // sector, else 16 heads of 63 sectors.
    void geometry() {
        heads = 16; spt = 63;
        if (image.size() >= 512 && image[510] == 0x55 && image[511] == 0xaa && image[0x1be + 4]) {
            uint8_t end_head = image[0x1be + 5], end_sector = image[0x1be + 6] & 0x3f;
            if (end_sector) { heads = uint8_t(end_head + 1); spt = end_sector; }
        }
        uint64_t per_cylinder = uint64_t(heads) * spt;
        cylinders = uint16_t(std::min<uint64_t>(image.size() / 512 / per_cylinder, 16383));
    }

    void clock() {
        ddr0.drive(p0, cycles);
        ddr1.drive(port1(top), cycles);
        ddr2.drive(port2(top), cycles);
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycles;
        now_ps += sys_ps;
        while (pixel_ps <= now_ps) {
            top.pixel_clk = !top.pixel_clk;
            top.eval();
            pixel_ps += 1e12 / PIXEL_HZ / 2;
        }
    }

    uint64_t word(uint32_t offset) { uint64_t v; std::memcpy(&v, window.at(offset), 8); return v; }
    void store(uint32_t offset, uint64_t value) { std::memcpy(window.at(offset), &value, 8); }
    uint64_t status(unsigned n) { return word(STATUS + 8 * n); }

    // Text mode: character n at planar offset 2n in plane 0.
    std::string screen_row(int row) {
        std::string text;
        for (int column = 0; column < 80; ++column) {
            uint8_t c = pc386::vram_byte(window, 2 * (row * 80 + column), 0);
            text.push_back(c >= 0x20 && c < 0x7f ? char(c) : ' ');
        }
        return text;
    }
    std::string last_row() {
        for (int row = 24; row >= 0; --row) {
            std::string text = screen_row(row);
            text.erase(text.find_last_not_of(' ') + 1);
            if (!text.empty()) return text;
        }
        return "";
    }

    void host() {
        if (!attached) {
            uint64_t announced = status(0);
            if (uint32_t(announced) != STATUS_MAGIC) return;
            epoch = uint32_t(announced >> 32);
            attached = true;
            std::printf("ATTACH %llu %08x\n", (unsigned long long)cycles, epoch);
            // The image, then the configuration, word 0 last.
            for (size_t at = 0; at < image.size(); at += 1u << 20) {
                size_t length = std::min<size_t>(1u << 20, image.size() - at);
                std::memcpy(window.at(IMAGE + uint32_t(at)), image.data() + at, length);
            }
            store(CONFIG + 8, 640ull | uint64_t(15360) << 16);
            store(CONFIG + 16, 0x22ull | 7ull << 8 | 1ull << 16 | 1ull << 32 | 1ull << 34);
            store(CONFIG + 24, uint64_t(image.size() / 512) | uint64_t(cylinders) << 32 | uint64_t(heads) << 48 |
                                   uint64_t(spt) << 56);
            // 2026-10-26 12:00:00, a Monday (weekday 2), as machine_tb seeds it.
            store(CONFIG + 32, 2026ull | 10ull << 16 | 26ull << 24 | 12ull << 32 | 2ull << 56);
            store(CONFIG, uint64_t(epoch) << 32 | CONFIG_MAGIC);
            std::printf("CONFIG %llu\n", (unsigned long long)cycles);
            return;
        }
        uint64_t s1 = status(1), s2 = status(2);
        if (!(s1 & 1)) return;  // the machine is not running yet
        int post = int((s2 >> 8) & 0xff);
        if (post != last_post) {
            std::printf("POST %02x %llu\n", post, (unsigned long long)cycles);
            last_post = post;
        }
        // The debug ring, a line at a time.
        uint32_t written = uint32_t(s1 >> 32);
        if (written - debug_read > RING_BYTES) throw std::runtime_error("the debug ring overran the host");
        for (; debug_read != written; ++debug_read) {
            char c = char(window.byte(RING + debug_read % RING_BYTES));
            if (c == '\n') { std::printf("DEBUG %s\n", debug_line.c_str()); debug_line.clear(); }
            else if (c != '\r') debug_line.push_back(c);
        }
        if (s2 & 2) { reason = "stopped"; return; }
        if ((s2 >> 32) != retired) { retired = s2 >> 32; progress_at = cycles; }
        else if (cycles - progress_at > uint64_t(clock_hz) * 2) { reason = "stalled"; return; }
        // The screen, every 50 ms: the prompt alone on the last row, twice.
        if (cycles - last_screen >= clock_hz / 20) {
            last_screen = cycles;
            bool shown = last_row() == prompt;
            if (shown && prompt_seen) reason = "prompt";
            else if (shown) std::printf("PROMPT %llu\n", (unsigned long long)cycles);
            prompt_seen = shown;
        }
    }

    void print_status() {
        uint64_t s1 = status(1), s2 = status(2), s3 = status(3);
        std::printf("STATUS running=%llu debug_bytes=%llu debug_drops=%llu halted=%llu stopped=%llu a20m=%llu "
                    "intr=%llu post=%02llx cpu_resets=%llu retired=%llu cs=%04llx vector=%02llx eip=%08llx "
                    "frames_shown=%llu scanout_underflows=%llu capture_drops=%llu present_underflows=%llu "
                    "plane_underflows=%llu samples=%llu dma_cycles=%llu opl2_samples=%llu\n",
                    (unsigned long long)(s1 & 1), (unsigned long long)(s1 >> 32),
                    (unsigned long long)((s1 >> 16) & 0xffff), (unsigned long long)(s2 & 1),
                    (unsigned long long)((s2 >> 1) & 1), (unsigned long long)((s2 >> 2) & 1),
                    (unsigned long long)((s2 >> 3) & 1), (unsigned long long)((s2 >> 8) & 0xff),
                    (unsigned long long)((s2 >> 16) & 0xff), (unsigned long long)(s2 >> 32),
                    (unsigned long long)(s3 & 0xffff), (unsigned long long)((s3 >> 24) & 0xff),
                    (unsigned long long)(s3 >> 32), (unsigned long long)uint32_t(status(4)),
                    (unsigned long long)(status(4) >> 32), (unsigned long long)uint32_t(status(5)),
                    (unsigned long long)(status(5) >> 32), (unsigned long long)uint32_t(status(6)),
                    (unsigned long long)(status(6) >> 32), (unsigned long long)uint32_t(status(7)),
                    (unsigned long long)(status(7) >> 32));
    }
};

}  // namespace

int main(int argc, char **argv) {
    std::setvbuf(stdout, nullptr, _IOLBF, 0);  // the log fills as the boot runs
    std::string disk, prompt = "C:\\>";
    double limit_seconds = 12;
    uint32_t clock_hz = 61440000;
    for (int n = 1; n + 1 < argc; n += 2) {
        std::string flag = argv[n], value = argv[n + 1];
        if (flag == "--disk") disk = value;
        else if (flag == "--clock-hz") clock_hz = uint32_t(std::stoul(value));
        else if (flag == "--limit-seconds") limit_seconds = std::stod(value);
        else if (flag == "--prompt") prompt = value;
        else { std::fprintf(stderr, "system_tb: unknown flag %s\n", flag.c_str()); return 2; }
    }
    if (disk.empty()) {
        std::fprintf(stderr, "usage: system_tb --disk F [--clock-hz N] [--limit-seconds S] [--prompt TEXT]\n");
        return 2;
    }
    try {
        Bench bench(read_file(disk), clock_hz, limit_seconds, prompt);
        return bench.run() == "prompt" ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "system_tb: %s\n", error.what());
        return 1;
    }
}
