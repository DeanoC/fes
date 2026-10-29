// SPDX-License-Identifier: MIT
// pc386 M4 diagnostic machine bench: pc386_diag (the machine, its audio and
// pc386_bus_master) with the three DDR ports on one window model, and a host
// that runs a diagnostic program through the command ring exactly as the
// kit's pc386-diag tool does (scripts/pc386_diag.py describes both files).
// The host acts every 16 system clocks. The pixel clock runs throughout, so
// the presenter and plane load port 2 as on a kit.
//
// usage: diag_tb --program F --results F [--limit-seconds S]
#include "Vpc386_diag.h"
#include "pc386_ddr_model.h"

#include <cstdio>
#include <cstring>
#include <fstream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

constexpr uint32_t DOORBELL = 0x01000000u, STATUS = 0x01000010u;
constexpr uint32_t COMMANDS = 0x01010000u, RESULTS = 0x01020000u;
constexpr uint32_t MAGIC = 0x33384d34u, RING = 4096;
constexpr uint32_t CAPTURE_OFFSET = 0x00a00000u, CAPTURE_BYTES = 0x00200000u, CAPTURE_STRIDE = 3200u;
constexpr unsigned HOST_PERIOD = 16;
constexpr double PIXEL_HZ = 74.25e6;

uint32_t get32(const std::vector<uint8_t> &data, size_t at) {
    if (at + 4 > data.size()) throw std::runtime_error("program file ends early");
    uint32_t value;
    std::memcpy(&value, &data[at], 4);
    return value;
}

// Port 0 is 128 bits wide; the bus master issues single beats.
class Port0 {
public:
    Port0(pc386::Window &window, uint64_t seed) : window(window), random(seed) {}
    void drive(Vpc386_diag &top, uint64_t cycle) {
        uint32_t draw_wait = random.next(), draw_latency = random.next();
        top.p0_readdatavalid = 0;
        if (!reads.empty() && reads.front().first <= cycle) {
            uint8_t *source = window.at(offset(reads.front().second));
            for (int word = 0; word < 4; ++word) std::memcpy(&top.p0_readdata[word], source + 4 * word, 4);
            top.p0_readdatavalid = 1;
            reads.erase(reads.begin());
        }
        bool refuse = (draw_wait & 15) == 0 || reads.size() >= 14;
        top.p0_waitrequest = refuse;
        if (refuse) return;
        if (top.p0_read || top.p0_write) {
            if (top.p0_burstcount != 1) throw std::runtime_error("port 0: the bus master issues single beats");
            if (top.p0_read) {
                unsigned roll = draw_latency % 1000;
                unsigned latency = roll < 1 ? 54 + (draw_latency >> 10) % 26 :
                                   roll < 15 ? 32 + (draw_latency >> 10) % 20 : 17 + (draw_latency >> 10) % 2;
                reads.emplace_back(cycle + latency, top.p0_address);
            } else {
                uint8_t *target = window.at(offset(top.p0_address));
                for (int lane = 0; lane < 16; ++lane)
                    if (top.p0_byteenable & (1u << lane))
                        target[lane] = uint8_t(top.p0_writedata[lane / 4] >> (8 * (lane % 4)));
            }
        }
    }

private:
    uint32_t offset(uint32_t word) {
        uint64_t byte = uint64_t(word) * 16;
        if (byte < pc386::WINDOW_BASE || byte + 16 > uint64_t(pc386::WINDOW_BASE) + pc386::WINDOW_BYTES)
            throw std::runtime_error("port 0: command outside the DDR window");
        return uint32_t(byte - pc386::WINDOW_BASE);
    }
    pc386::Window &window;
    pc386::Random random;
    std::vector<std::pair<uint64_t, uint32_t>> reads;
};

pc386::PortSignals port1(Vpc386_diag &top) {
    return {&top.p1_address, &top.p1_burstcount, &top.p1_read, &top.p1_write, &top.p1_writedata,
            &top.p1_byteenable, &top.p1_waitrequest, &top.p1_readdata, &top.p1_readdatavalid};
}
pc386::PortSignals port2(Vpc386_diag &top) {
    return {&top.p2_address, &top.p2_burstcount, &top.p2_read, &top.p2_write, &top.p2_writedata,
            &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata, &top.p2_readdatavalid};
}

class Bench {
public:
    Bench(const std::vector<uint8_t> &program, double limit_seconds) : program(program) {
        if (program.size() < 12 || std::memcmp(program.data(), "P386PRG1", 8) != 0)
            throw std::runtime_error("not a pc386 diagnostic program");
        clock_hz = get32(program, 8);
        at = 12;
        sys_ps = 1e12 / clock_hz;
        limit = uint64_t(limit_seconds * clock_hz);
        out.insert(out.end(), {'P', '3', '8', '6', 'R', 'E', 'S', '1'});
    }

    void run() {
        top.pixel_locked = 0;
        top.p0_reset = top.p1_reset = top.p2_reset = 1;
        for (int n = 0; n < 32; ++n) clock();
        top.pixel_locked = 1;
        top.p0_reset = top.p1_reset = top.p2_reset = 0;
        while (!finished) {
            clock();
            if (cycles % HOST_PERIOD == 0) host();
            if (cycles > limit) throw std::runtime_error("simulated time limit reached at record offset " +
                                                         std::to_string(at));
        }
        put('E');
        put64(nanoseconds());
    }

    const std::vector<uint8_t> &results() const { return out; }

private:
    Vpc386_diag top;
    pc386::Window window;
    Port0 ddr0{window, 0x5eed0010};
    pc386::Port ddr1{window, 0x5eed0011, "port 1"};
    pc386::Port ddr2{window, 0x5eed0012, "port 2"};
    const std::vector<uint8_t> &program;
    std::vector<uint8_t> out;
    std::vector<std::array<uint32_t, 4>> done;
    uint32_t clock_hz = 0, epoch = 0, submitted = 0, completed = 0;
    size_t at = 0;
    bool attached = false, finished = false;
    double sys_ps = 0, now_ps = 0, pixel_ps = 0;
    uint64_t cycles = 0, limit = 0;

    void clock() {
        ddr0.drive(top, cycles);
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

    uint64_t nanoseconds() const { return uint64_t(now_ps / 1000); }
    uint32_t word(uint32_t offset) { uint32_t v; std::memcpy(&v, window.at(offset), 4); return v; }
    void store(uint32_t offset, uint32_t value) { std::memcpy(window.at(offset), &value, 4); }
    void put(uint8_t byte) { out.push_back(byte); }
    void put32(uint32_t value) { for (int n = 0; n < 4; ++n) out.push_back(uint8_t(value >> (8 * n))); }
    void put64(uint64_t value) { for (int n = 0; n < 8; ++n) out.push_back(uint8_t(value >> (8 * n))); }

    // The host's share of a clock: attach, collect results, then advance the program.
    void host() {
        if (!attached) {
            if (word(STATUS + 4) != MAGIC) return;
            epoch = word(STATUS + 8);
            submitted = completed = word(STATUS);
            store(DOORBELL, submitted);
            store(DOORBELL + 4, epoch);
            attached = true;
            return;
        }
        uint32_t head = word(STATUS);
        while (completed != head) {
            std::array<uint32_t, 4> result;
            for (int n = 0; n < 4; ++n) result[n] = word(RESULTS + 16 * (completed % RING) + 4 * n);
            if (result[3] != epoch + completed) throw std::runtime_error("result sequence mismatch");
            put('R');
            for (uint32_t value : result) put32(value);
            done.push_back(result);
            ++completed;
        }
        bool rang = false;
        for (int budget = 64; budget > 0 && !finished; --budget) {
            uint8_t kind = program.at(at);
            if (kind == 'C') {
                if (submitted - completed >= RING - 16) break;
                uint32_t slot = COMMANDS + 16 * (submitted % RING);
                store(slot, get32(program, at + 1));
                store(slot + 4, get32(program, at + 5));
                store(slot + 8, get32(program, at + 9));
                store(slot + 12, epoch + submitted);
                ++submitted;
                rang = true;
                at += 21;
                continue;
            }
            if (rang) store(DOORBELL, submitted);
            rang = false;
            if (completed != submitted) break;          // every other record drains first
            if (kind == 'S') {
                uint32_t offset = get32(program, at + 1), length = get32(program, at + 5);
                if (at + 9 + length > program.size()) throw std::runtime_error("stage runs past the program");
                for (uint32_t n = 0; n < length; ++n) *window.at(offset + n) = program[at + 9 + n];
                put('S'); put32(offset); put32(length); put64(0);
                at += 9 + length;
            } else if (kind == 'M') {
                uint8_t length = program.at(at + 1);
                std::string name(program.begin() + at + 2, program.begin() + at + 2 + length);
                std::printf("MARK %s %llu\n", name.c_str(), (unsigned long long)cycles);
                put('M'); put(length);
                out.insert(out.end(), name.begin(), name.end());
                put64(nanoseconds()); put32(completed);
                at += 2 + length + 4;
            } else if (kind == 'D') {
                uint32_t offset = get32(program, at + 1), length = get32(program, at + 5);
                put('D'); put32(offset); put32(length);
                for (uint32_t n = 0; n < length; ++n) put(window.byte(offset + n));
                at += 9;
            } else if (kind == 'F') {
                if (done.size() < 2) throw std::runtime_error("a frame record needs READ 6 and READ 7 before it");
                uint32_t size = done[done.size() - 2][0], frame = done.back()[0];
                uint32_t width = size & 0x3ff, height = (size >> 16) & 0x3ff, buffer = (frame >> 16) & 3;
                put('F'); put32(width); put32(height); put32(buffer); put32(frame & 0xffff);
                uint32_t base = CAPTURE_OFFSET + buffer * CAPTURE_BYTES;
                for (uint32_t y = 0; y < height; ++y)
                    for (uint32_t x = 0; x < 4 * width; ++x) put(window.byte(base + y * CAPTURE_STRIDE + x));
                at += 1;
            } else if (kind == 'E') {
                finished = true;
            } else {
                throw std::runtime_error("bad program record at " + std::to_string(at));
            }
        }
        if (rang) store(DOORBELL, submitted);
    }
};

}  // namespace

int main(int argc, char **argv) {
    std::string program_path, results_path;
    double limit_seconds = 30;
    for (int n = 1; n + 1 < argc; n += 2) {
        std::string flag = argv[n], value = argv[n + 1];
        if (flag == "--program") program_path = value;
        else if (flag == "--results") results_path = value;
        else if (flag == "--limit-seconds") limit_seconds = std::stod(value);
        else { std::fprintf(stderr, "diag_tb: unknown flag %s\n", flag.c_str()); return 2; }
    }
    if (program_path.empty() || results_path.empty()) {
        std::fprintf(stderr, "usage: diag_tb --program F --results F [--limit-seconds S]\n");
        return 2;
    }
    try {
        std::ifstream input(program_path, std::ios::binary);
        std::vector<uint8_t> program((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
        Bench bench(program, limit_seconds);
        bench.run();
        std::ofstream output(results_path, std::ios::binary);
        output.write(reinterpret_cast<const char *>(bench.results().data()), std::streamsize(bench.results().size()));
    } catch (const std::exception &error) {
        std::fprintf(stderr, "diag_tb: %s\n", error.what());
        return 1;
    }
    return 0;
}
