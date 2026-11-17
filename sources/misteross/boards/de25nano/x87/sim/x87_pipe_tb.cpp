// SPDX-License-Identifier: MIT
// Independent masked-arithmetic oracle: execute the same operation on the
// host's x87, preserving its complete legacy floating-point environment.
#include "Vfes_x87_pipe.h"
#include "verilated.h"

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <deque>
#include <random>
#include <stdexcept>
#include <string>
#include <vector>

#if !defined(__x86_64__) && !defined(__i386__)
#error "This independent arithmetic reference requires an x86 host with x87"
#endif

namespace {
enum Op { ADD, SUB, MUL, DIV, SQRT, CMP, UCMP };
const char *names[] = {"add", "sub", "mul", "div", "sqrt", "compare", "unordered_compare"};

struct __attribute__((packed)) Ext {
    uint64_t significand;
    uint16_t sign_exponent;
    bool operator==(const Ext &o) const {
        return significand == o.significand && sign_exponent == o.sign_exponent;
    }
};
static_assert(sizeof(Ext) == 10);
struct Request { unsigned op, pc, rc, tag; Ext a, b; };
struct Result {
    Ext value{};
    unsigned flags = 0, cc = 0, incremented = 0, tag = 0;
    bool operator==(const Result &o) const {
        return value == o.value && flags == o.flags && cc == o.cc &&
               incremented == o.incremented && tag == o.tag;
    }
};

std::string hex80(Ext x) {
    char s[32];
    std::snprintf(s, sizeof s, "%04x%016llx", x.sign_exponent,
                  static_cast<unsigned long long>(x.significand));
    return s;
}
Ext value(uint16_t exponent, uint64_t significand, bool negative = false) {
    return {significand, uint16_t(exponent | (negative ? 0x8000 : 0))};
}

Result reference(const Request &r) {
    // FNSAVE initializes x87 and saves all register values, tags, status,
    // pointers and control. FRSTOR restores them after the masked operation.
    // The compiler uses integer/SSE code outside these asm blocks.
    alignas(16) unsigned char saved[108];
    Ext out{};
    uint16_t sw = 0;
    const unsigned pc = r.pc == 1 ? 3 : r.pc;
    const uint16_t cw = uint16_t(0x007f | (pc << 8) | (r.rc << 10));
#define HOST_BINARY(instruction) \
    asm volatile("fnsave %[saved]\n\tfldcw %[cw]\n\tfldt %[b]\n\tfldt %[a]\n\t" \
                 "fnclex\n\t" instruction " %%st(1), %%st(0)\n\tfnstsw %[sw]\n\t" \
                 "fstpt %[out]\n\tfstp %%st(0)\n\tfrstor %[saved]" \
                 : [saved] "+m"(saved), [sw] "=m"(sw), [out] "=m"(out) \
                 : [cw] "m"(cw), [a] "m"(r.a), [b] "m"(r.b) \
                 : "st", "st(1)", "memory")
#define HOST_COMPARE(instruction) \
    asm volatile("fnsave %[saved]\n\tfldcw %[cw]\n\tfldt %[b]\n\tfldt %[a]\n\t" \
                 "fnclex\n\t" instruction " %%st(1)\n\tfnstsw %[sw]\n\tfrstor %[saved]" \
                 : [saved] "+m"(saved), [sw] "=m"(sw) \
                 : [cw] "m"(cw), [a] "m"(r.a), [b] "m"(r.b) \
                 : "st", "st(1)", "memory")
    switch (r.op) {
    case ADD: HOST_BINARY("fadd"); break;
    case SUB: HOST_BINARY("fsub"); break;
    case MUL: HOST_BINARY("fmul"); break;
    case DIV: HOST_BINARY("fdiv"); break;
    case SQRT:
        asm volatile("fnsave %[saved]\n\tfldcw %[cw]\n\tfldt %[a]\n\tfnclex\n\t"
                     "fsqrt\n\tfnstsw %[sw]\n\tfstpt %[out]\n\tfrstor %[saved]"
                     : [saved] "+m"(saved), [sw] "=m"(sw), [out] "=m"(out)
                     : [cw] "m"(cw), [a] "m"(r.a) : "st", "memory");
        break;
    case CMP: HOST_COMPARE("fcom"); break;
    case UCMP: HOST_COMPARE("fucom"); break;
    default: throw std::runtime_error("bad reference operation");
    }
#undef HOST_BINARY
#undef HOST_COMPARE
    Result result;
    result.value = out;
    result.flags = sw & 0x3f;
    result.cc = ((sw >> 12) & 4) | ((sw >> 9) & 2) | ((sw >> 8) & 1);
    result.incremented = (sw >> 9) & 1;
    result.tag = r.tag;
    return result;
}

unsigned passed = 0, failed = 0;
std::array<uint64_t, 7> operation_checks{}, operation_failures{};
void check(bool ok, const std::string &what) {
    if (ok) ++passed;
    else {
        ++failed;
        if (failed <= 60) std::printf("FAIL %s\n", what.c_str());
    }
}

void put(VlWide<3> &port, Ext x) {
    port[0] = uint32_t(x.significand);
    port[1] = uint32_t(x.significand >> 32);
    port[2] = x.sign_exponent;
}
Ext get(const VlWide<3> &port) {
    return {uint64_t(port[0]) | uint64_t(port[1]) << 32, uint16_t(port[2])};
}

struct Expected { Request request; Result result; uint64_t cycle; };
struct Unit {
    Vfes_x87_pipe top;
    std::deque<Expected> pending;
    uint64_t cycle = 0, accepted = 0, retired = 0;
    uint64_t first_issue = 0, last_issue = 0, first_response = 0, last_response = 0;
    unsigned max_pending = 0;
    bool was_held = false;
    Result held;
    std::vector<uint64_t> latency;

    Unit() {
        top.clk = top.req_valid = top.resp_ready = top.flush = 0;
        top.req_op = top.req_pc = top.req_rc = top.req_tag = 0;
        put(top.req_a, {}); put(top.req_b, {});
        top.reset = 1;
        for (unsigned i = 0; i < 4; ++i) edge();
        top.reset = 0;
        top.eval();
    }
    void edge() {
        top.clk = 0; top.eval();
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
        ++cycle;
    }
    Result response() {
        Result r;
        r.value = get(top.resp_value);
        r.flags = top.resp_flags; r.cc = top.resp_cc;
        r.incremented = top.resp_incremented; r.tag = top.resp_tag;
        return r;
    }
    // Observe handshakes before the edge that transfers them. The expected
    // queue orders completions independently of each operation's latency.
    bool step(const Request *request, bool ready = true, bool flush = false) {
        top.req_valid = request != nullptr;
        top.resp_ready = ready;
        top.flush = flush;
        if (request) {
            top.req_op = request->op; top.req_pc = request->pc;
            top.req_rc = request->rc; top.req_tag = request->tag;
            put(top.req_a, request->a); put(top.req_b, request->b);
        }
        top.eval();
        if (flush) {
            pending.clear(); was_held = false;
            edge(); top.flush = 0; top.eval();
            return false;
        }
        if (was_held)
            check(top.resp_valid && response() == held, "held response remains stable under backpressure");
        was_held = top.resp_valid && !ready;
        if (was_held) held = response();
        if (top.resp_valid && ready) {
            if (pending.empty()) check(false, "response has a matching accepted request");
            else {
                const auto e = pending.front(); pending.pop_front();
                const auto actual = response();
                const bool compare = e.request.op == CMP || e.request.op == UCMP;
                const bool ok = actual.value == e.result.value && actual.flags == e.result.flags &&
                    actual.tag == e.result.tag && (compare ? actual.cc == e.result.cc :
                    actual.incremented == e.result.incremented);
                ++operation_checks[e.request.op];
                if (!ok) ++operation_failures[e.request.op];
                char detail[220];
                std::snprintf(detail, sizeof detail,
                    "%s pc=%u rc=%u tag=%u a=%s b=%s got=%s flags=%02x cc=%x c1=%u expected=%s flags=%02x cc=%x c1=%u",
                    names[e.request.op], e.request.pc, e.request.rc, e.request.tag,
                    hex80(e.request.a).c_str(), hex80(e.request.b).c_str(), hex80(actual.value).c_str(),
                    actual.flags, actual.cc, actual.incremented, hex80(e.result.value).c_str(),
                    e.result.flags, e.result.cc, e.result.incremented);
                check(ok, detail);
                latency.push_back(cycle - e.cycle);
            }
            if (!retired++) first_response = cycle;
            last_response = cycle;
        }
        bool fire = request && top.req_ready;
        if (fire) {
            pending.push_back({*request, reference(*request), cycle});
            if (!accepted++) first_issue = cycle;
            last_issue = cycle;
            max_pending = std::max(max_pending, unsigned(pending.size()));
        }
        edge();
        return fire;
    }
    void drain() {
        unsigned watchdog = 0;
        while (!pending.empty() && ++watchdog < 100000) step(nullptr);
        check(pending.empty(), "all accepted requests eventually complete");
        for (unsigned i = 0; i < 32; ++i) step(nullptr);
    }
    void stream(const std::vector<Request> &requests, std::mt19937_64 *random = nullptr) {
        size_t index = 0;
        uint64_t deadline = cycle + requests.size() * 600 + 100000;
        bool offer = false;
        while ((index < requests.size() || !pending.empty()) && cycle < deadline) {
            if (index < requests.size() && (!random || ((*random)() & 3))) offer = true;
            const bool ready = !random || ((*random)() % 5 != 0);
            if (step(offer && index < requests.size() ? &requests[index] : nullptr, ready)) {
                ++index; offer = false;
            }
        }
        check(index == requests.size() && pending.empty(), "stream finishes within watchdog");
    }
};

std::vector<Ext> classes() {
    return {
        value(0, 0), value(0, 0, true),
        value(0x3fff, 0x8000000000000000ULL), value(0x3fff, 0x8000000000000000ULL, true),
        value(0x4000, 0x8000000000000000ULL), value(0x3ffe, 0x8000000000000000ULL),
        value(0x3fff, 0xffffffffffffffffULL), value(0x3fff, 0x8000008000000000ULL),
        value(0x3fff, 0x8000000000000400ULL), value(0x3fc0, 0x8000000000000000ULL),
        value(1, 0x8000000000000000ULL), value(1, 0x8000000000000001ULL),
        value(0, 1), value(0, 0x7fffffffffffffffULL), value(0, 0x8000000000000000ULL),
        value(0x7ffe, 0xffffffffffffffffULL), value(0x7ffe, 0xffffffffffffffffULL, true),
        value(0x7fff, 0x8000000000000000ULL), value(0x7fff, 0x8000000000000000ULL, true),
        value(0x7fff, 0xc000000000000001ULL), value(0x7fff, 0xc000000000000002ULL, true),
        value(0x7fff, 0x8000000000000001ULL), value(0x3fff, 0x4000000000000000ULL),
        value(0x7fff, 0), value(0x3fff, 0)
    };
}

Ext random_value(std::mt19937_64 &random, const std::vector<Ext> &special) {
    const auto kind = random() % 8;
    if (kind == 0) return special[random() % special.size()];
    uint16_t exponent;
    if (kind <= 3) exponent = uint16_t(0x3fff + int(random() % 161) - 80);
    else if (kind == 4) exponent = uint16_t(1 + random() % 8);
    else if (kind == 5) exponent = uint16_t(0x7ffe - random() % 8);
    else if (kind == 6) return value(0, random() & 0x7fffffffffffffffULL, random() & 1);
    else exponent = uint16_t(1 + random() % 0x7ffe);
    return value(exponent, random() | 0x8000000000000000ULL, random() & 1);
}

void arithmetic(unsigned samples) {
    Unit unit;
    std::mt19937_64 random(0x8738625);
    const auto values = classes();
    std::vector<Request> requests;
    unsigned tag = 0;
    for (unsigned pc = 0; pc < 4; ++pc)
        for (unsigned rc = 0; rc < 4; ++rc) {
            // Tininess is measured after precision rounding with an
            // unbounded exponent, before packing extended denormals. Probe
            // both sides of each PC's transition, including values whose
            // final packed result rounds back into the minimum normal.
            const Ext minimum = value(1, 0x8000000000000000ULL);
            for (unsigned precision : {24U, 53U, 64U}) {
                const unsigned spacing = 64 - precision;
                const uint64_t below = uint64_t(0) - (uint64_t(1) << spacing);
                const uint64_t above = 0x8000000000000000ULL +
                    (uint64_t(1) << (precision < 64 ? 63 - precision : 0));
                for (int delta = -2; delta <= 2; ++delta) {
                    if (delta <= 0 || below <= UINT64_MAX - unsigned(delta)) {
                        const Ext near_below = value(0x3ffe, below + delta);
                        requests.push_back({MUL, pc, rc, tag++ & 255, minimum, near_below});
                        requests.push_back({DIV, pc, rc, tag++ & 255, minimum, near_below});
                    }
                    if (delta >= 0 || above >= 0x8000000000000000ULL + unsigned(-delta)) {
                        const Ext near_above = value(0x3fff, above + delta);
                        requests.push_back({MUL, pc, rc, tag++ & 255, minimum, near_above});
                        requests.push_back({DIV, pc, rc, tag++ & 255, minimum, near_above});
                    }
                }
            }
            // Cross every operand class, including NaN priority and every
            // signed-zero pairing, under every control-word combination.
            for (unsigned op = 0; op <= UCMP; ++op) {
                for (auto a : values)
                    for (auto b : values) {
                        if (op == SQRT && !(b == values.front())) continue;
                        requests.push_back({op, pc, rc, tag++ & 255, a, b});
                    }
                for (unsigned i = 0; i < samples; ++i)
                    requests.push_back({op, pc, rc, tag++ & 255,
                        random_value(random, values), random_value(random, values)});
            }
        }
    unit.stream(requests, &random);
    unit.drain();
    std::printf("METRIC arithmetic_requests %llu\n", static_cast<unsigned long long>(unit.accepted));
    std::printf("METRIC max_outstanding %u\n", unit.max_pending);
}

void reference_preservation() {
    alignas(16) unsigned char original[108]{}, before[108]{}, after[108]{};
    const uint16_t cw = 0x0b7f; // PC64, round upward, all exceptions masked.
    asm volatile("fnsave %[original]\n\tfldcw %[cw]\n\tfld1\n\tfldpi\n\t"
                 "fnsave %[before]\n\tfrstor %[before]"
                 : [original] "+m"(original), [before] "+m"(before)
                 : [cw] "m"(cw) : "st", "st(1)", "memory");
    const Request request{DIV, 0, 1, 0, value(0x3fff, 0x8000000000000000ULL),
                         value(0x4000, 0xc000000000000000ULL)};
    const auto result = reference(request);
    asm volatile("fnsave %[after]\n\tfrstor %[original]"
                 : [after] "+m"(after) : [original] "m"(original)
                 : "st", "st(1)", "memory");
    check(std::memcmp(before, after, sizeof before) == 0,
          "host instruction reference preserves complete x87 environment and stack");
    check(result.flags == 0x20, "independent reference records its own inexact exception");
}

void protocol() {
    Unit unit;
    const Ext one = value(0x3fff, 0x8000000000000000ULL);
    Request request{DIV, 3, 0, 0, one, value(0x4000, 0xc000000000000000ULL)};
    // The slow oldest operation and fast younger operations exercise order
    // and fill the bounded queue while the output is held.
    unsigned accepted = 0;
    for (unsigned i = 0; i < 500; ++i) {
        request.op = accepted ? ADD : DIV;
        request.tag = accepted;
        if (unit.step(&request, false)) ++accepted;
    }
    check(accepted == 16, "backpressure fills exactly the 16-entry queue");
    check(unit.top.resp_valid, "held oldest response is available");
    unit.step(nullptr, false, true);
    for (unsigned i = 0; i < 300; ++i) unit.step(nullptr);
    check(unit.pending.empty(), "flush cancels full queue including held output");
    request.op = SQRT; request.a = value(0x4000, 0x8000000000000000ULL);
    while (!unit.step(&request)) {}
    unit.step(nullptr, true, true);
    for (unsigned i = 0; i < 300; ++i) unit.step(nullptr);
    request.op = MUL; request.tag = 199; request.a = one; request.b = one;
    while (!unit.step(&request)) {}
    unit.drain();
    std::printf("METRIC protocol_checks %u\n", 1U);
}

void throughput() {
    Unit unit;
    const Ext one = value(0x3fff, 0x8000000000000000ULL);
    std::vector<Request> requests;
    const unsigned ops[] = {ADD, SUB, MUL, CMP, UCMP};
    for (unsigned i = 0; i < 4096; ++i)
        requests.push_back({ops[i % 5], i & 3, (i >> 2) & 3, i & 255, one,
                           value(0x3ffe, 0x9000000000000000ULL)});
    unit.stream(requests);
    const uint64_t issue_span = unit.last_issue - unit.first_issue + 1;
    const uint64_t response_span = unit.last_response - unit.first_response + 1;
    check(issue_span == requests.size(), "fast mixed stream accepts one request each clock");
    check(response_span == requests.size(), "fast mixed stream retires one response each clock");
    std::printf("METRIC fast_requests %zu\nMETRIC fast_issue_cycles %llu\nMETRIC fast_response_cycles %llu\n",
                requests.size(), static_cast<unsigned long long>(issue_span),
                static_cast<unsigned long long>(response_span));
    if (!unit.latency.empty())
        std::printf("METRIC fast_latency_cycles %llu\n", static_cast<unsigned long long>(unit.latency.front()));
    // A true recurrence waits for the preceding result, measuring dependent
    // arithmetic independently of the independent-stream throughput claim.
    for (unsigned op : {unsigned(ADD), unsigned(MUL), unsigned(DIV), unsigned(SQRT)}) {
        Unit dependent;
        Request request{op, 3, 0, 0, value(0x3fff, 0xa000000000000000ULL), one};
        const uint64_t begin = dependent.cycle;
        for (unsigned i = 0; i < 24; ++i) {
            request.tag = i;
            while (!dependent.step(&request)) {}
            unsigned watchdog = 0;
            Ext result{};
            while (!dependent.pending.empty() && ++watchdog < 1000) {
                dependent.top.eval();
                if (dependent.top.resp_valid) result = get(dependent.top.resp_value);
                dependent.step(nullptr);
            }
            check(dependent.pending.empty(), std::string(names[op]) + " dependency completes");
            request.a = result;
        }
        std::printf("METRIC dependent_%s_cycles %llu\n", names[op],
                    static_cast<unsigned long long>(dependent.cycle - begin));
        std::printf("METRIC dependent_%s_operations 24\n", names[op]);
    }
}
} // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    unsigned samples = 256;
    if (argc == 3 && std::string(argv[1]) == "--samples") samples = unsigned(std::strtoul(argv[2], nullptr, 10));
    try {
        reference_preservation();
        arithmetic(samples);
        protocol();
        throughput();
    } catch (const std::exception &e) {
        check(false, std::string("bench exception: ") + e.what());
    }
    for (unsigned op = 0; op <= UCMP; ++op) {
        std::printf("METRIC %s_checks %llu\nMETRIC %s_failures %llu\n", names[op],
                    static_cast<unsigned long long>(operation_checks[op]), names[op],
                    static_cast<unsigned long long>(operation_failures[op]));
    }
    std::printf("SUMMARY %u %u\n", passed, failed);
    return failed ? 1 : 0;
}
