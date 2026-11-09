// SPDX-License-Identifier: MIT
// machine_tb --census: the oracle's instruction census, step by step.
//
// Census::begin takes the oracle's state before a step and names the
// instruction at CS:EIP (its stem, read through the page tables when paging
// is on); Census::end takes the state after it and the oracle's StepResult and
// counts what the step did. Census::write prints one line per key, sorted:
//   <key> <count> <first cs:eip or ->
// The keys are listed in docs/pc386-game-drives.md, "Instruction census".
// Nothing here changes the guest: the page walk reads the tables without
// setting A or D.
#pragma once
#include "i386_oracle.h"

#include <cstdint>
#include <cstdio>
#include <functional>
#include <map>
#include <ostream>
#include <string>
#include <utility>
#include <vector>

namespace pc386_census {

// The stem of the instruction at bytes, in the lockstep's naming
// (test_fes_i386_lockstep.py's SCOPE): the opcode, 0F and the second byte for
// a two-byte one, and the ModRM reg field after a dot for a group, after "67"
// and "66" for those prefixes. With sysreg, MOV to and from CRn, DRn and TRn
// (0F20-0F26) also take the register number from the reg field: 0F22.3 is
// MOV CR3, r32.
inline std::string stem(const uint8_t *bytes, unsigned length, bool sysreg = false) {
    bool operand = false, address = false;
    unsigned at = 0;
    for (; at < length; ++at) {
        uint8_t b = bytes[at];
        if (b == 0x66) operand = true;
        else if (b == 0x67) address = true;
        else if (!(b == 0xf0 || b == 0xf2 || b == 0xf3 || b == 0x26 || b == 0x2e || b == 0x36 || b == 0x3e ||
                   b == 0x64 || b == 0x65)) break;
    }
    char text[32];
    std::string out = std::string(address ? "67" : "") + (operand ? "66" : "");
    if (at >= length) return out + "??";
    unsigned opcode = bytes[at];
    bool group;
    if (opcode == 0x0f && at + 1 < length) {
        opcode = 0x0f00 | bytes[++at];
        group = opcode == 0x0f00 || opcode == 0x0f01 || opcode == 0x0fba || opcode == 0x0fc7 ||
                (sysreg && opcode >= 0x0f20 && opcode <= 0x0f26 && opcode != 0x0f25);
        std::snprintf(text, sizeof text, "%04X", opcode);
    } else {
        group = (opcode >= 0x80 && opcode <= 0x83) || opcode == 0xc0 || opcode == 0xc1 ||
                (opcode >= 0xd0 && opcode <= 0xd3) || (opcode >= 0xd8 && opcode <= 0xdf) || opcode == 0xf6 ||
                opcode == 0xf7 || opcode == 0xfe || opcode == 0xff;
        std::snprintf(text, sizeof text, "%02X", opcode);
    }
    out += text;
    if (group && at + 1 < length) out += "." + std::to_string((bytes[at + 1] >> 3) & 7);
    return out;
}

// The opcode part of a census stem: no mode suffix and no 66 or 67 prefix.
inline std::string opcode_of(const std::string &stem) {
    std::string op = stem.substr(0, stem.find(':'));
    while (op.size() > 2 && (op.compare(0, 2, "66") == 0 || op.compare(0, 2, "67") == 0)) op.erase(0, 2);
    return op;
}

// A fully associative TLB of 4 KiB pages with LRU replacement. A touch that
// misses is a walk.
struct Tlb {
    unsigned entries;
    std::vector<uint32_t> pages;  // most recent first
    uint64_t accesses = 0, walks = 0;
    explicit Tlb(unsigned n) : entries(n) {}
    void flush() { pages.clear(); }
    void touch(uint32_t page) {
        ++accesses;
        for (size_t i = 0; i < pages.size(); ++i)
            if (pages[i] == page) {
                pages.erase(pages.begin() + i);
                pages.insert(pages.begin(), page);
                return;
            }
        ++walks;
        pages.insert(pages.begin(), page);
        if (pages.size() > entries) pages.pop_back();
    }
};

class Census {
public:
    // A byte at a physical address, as the bus sees it (A20 applied).
    using PhysicalByte = std::function<uint8_t(uint32_t)>;
    explicit Census(PhysicalByte read) : read_(std::move(read)) {}

    std::map<std::string, uint64_t> counts;
    std::map<std::string, std::string> first;
    // The plan's TLBs (i386-cpu-design.md, "Paging"): a 16-entry DTLB and an
    // 8-entry ITLB, flushed on every CR3 write, a task switch's included.
    Tlb dtlb{16}, itlb{8};
    uint64_t paged_steps = 0, page_cross = 0;

    // Linear to physical through s's page tables: false if the PDE or the PTE
    // is not present. Without paging the address is its own.
    bool translate(const fes_i386::State &s, uint32_t linear, uint32_t &physical) const {
        physical = linear;
        if (!(s.cr0 & 0x80000000u)) return true;
        const uint32_t pde = read32((s.cr3 & ~0xfffu) + ((linear >> 22) << 2));
        if (!(pde & 1)) return false;
        const uint32_t pte = read32((pde & ~0xfffu) + (((linear >> 12) & 0x3ff) << 2));
        if (!(pte & 1)) return false;
        physical = (pte & ~0xfffu) | (linear & 0xfff);
        return true;
    }
    // FFh where the page is not present.
    uint8_t linear_byte(const fes_i386::State &s, uint32_t linear) const {
        uint32_t physical;
        return translate(s, linear, physical) ? read_(physical) : uint8_t(0xff);
    }

    // Name the instruction at before's CS:EIP; site is its CS:EIP for the
    // first-seen column.
    const std::string &begin(const fes_i386::State &before, const std::string &site) {
        uint8_t bytes[15];
        const uint32_t linear = before.seg[fes_i386::SEG_CS].base + before.eip;
        for (unsigned n = 0; n < 15; ++n) bytes[n] = linear_byte(before, linear + n);
        stem_ = stem(bytes, 15, true);
        if (before.eflags & 0x20000) stem_ += ":v86";
        else if (before.cr0 & 1) stem_ += ":pm" + std::to_string(before.cpl);
        site_ = site;
        first.emplace(stem_, site);
        return stem_;
    }

    void end(const fes_i386::State &before, const fes_i386::State &after, const fes_i386::StepResult &result) {
        using fes_i386::EventKind;
        if (result.retired) ++counts[stem_];
        // A step whose first event is an INTR or NMI ran no instruction: it
        // only delivered that event.
        const bool delivery = !result.events.empty() && (result.events[0].kind == EventKind::ExternalInterrupt ||
                                                        result.events[0].kind == EventKind::Nmi);
        const std::string op = opcode_of(stem_);
        const bool vm = before.eflags & 0x20000;
        const unsigned iopl = (before.eflags >> 12) & 3;
        const bool tr_changed = after.tr.selector != before.tr.selector || after.tr.base != before.tr.base;
        const bool task_switch = tr_changed && !(op == "0F00.3" && !delivery);  // not LTR
        if (before.cr0 & 1) {
            // Each event with its gate (read through the page tables) and
            // the CPLs.
            std::string gate_source;
            for (const fes_i386::Event &event : result.events) {
                std::string key = std::string("event:") + kind_name(event.kind) + ':' + std::to_string(event.vector);
                if (before.cr0 & 0x80000000u) key += ":paged";
                if (vm) key += ":v86";
                uint8_t gate[8];
                for (unsigned n = 0; n < 8; ++n) gate[n] = linear_byte(before, before.idtr.base + event.vector * 8u + n);
                const unsigned type = gate[5] & 0x1f;
                if ((type & 0xf) == 5 && gate_source.empty())
                    gate_source = std::string(kind_name(event.kind)) + ':' + std::to_string(event.vector);
                key += ":gate=" + hex(type, 2) + ":dpl=" + std::to_string((gate[5] >> 5) & 3) +
                       ":sel=" + hex(gate[2] | gate[3] << 8, 4);
                if (event.kind == EventKind::Exception) key += ":err=" + hex(event.error_code, 4);
                key += ":cpl=" + std::to_string(before.cpl) + '>' + std::to_string(after.cpl);
                count(key);
            }
            if (after.cpl != before.cpl && result.events.empty())
                count("cpl:" + std::to_string(before.cpl) + '>' + std::to_string(after.cpl) + ':' + stem_);
            if ((after.cr0 ^ before.cr0) & 0x80000000u) count("cr0.pg:" + stem_);
            if (after.ldtr.selector != before.ldtr.selector) count("ldtr:" + hex(after.ldtr.selector, 4) + ':' + stem_);
            if (after.tr.selector != before.tr.selector) count("tr:" + hex(after.tr.selector, 4) + ':' + stem_);
            if ((after.eflags ^ before.eflags) & 0x3000)
                count("iopl:" + std::to_string((after.eflags >> 12) & 3) + ':' + stem_);
            if (after.dr7 != before.dr7) count("dr7:" + hex(after.dr7, 8) + ':' + stem_);

            // Task switches by source: a task gate's event, or the
            // instruction (JMP or CALL to a TSS or task gate, IRET with NT).
            if (task_switch) {
                std::string source = gate_source;
                if (source.empty()) {
                    if (op == "EA" || op == "FF.5") source = "jmp:" + stem_;
                    else if (op == "9A" || op == "FF.3") source = "call:" + stem_;
                    else if (op == "CF" && (before.eflags & 0x4000)) source = "iret:" + stem_;
                    else source = "other:" + stem_;
                }
                count("ts:" + source + ":tss=" + hex(after.tr.ar & 0x1f, 2) +
                      ":nt=" + std::to_string((after.eflags >> 14) & 1) + ":cr3chg=" +
                      std::to_string(after.cr3 != before.cr3) +
                      ":ldtchg=" + std::to_string(after.ldtr.selector != before.ldtr.selector) +
                      ":vm=" + std::to_string((after.eflags >> 17) & 1) + ":cpl=" + std::to_string(before.cpl) + '>' +
                      std::to_string(after.cpl));
            }
            if (tr_changed && after.tr.limit >= 0x67) {
                const uint32_t iomap = linear_byte(after, after.tr.base + 0x66) |
                                       uint32_t(linear_byte(after, after.tr.base + 0x67)) << 8;
                count("tss-iomap:base=" + hex(iomap, 4) + ":limit=" + hex(after.tr.limit, 5));
            }

            // V86: steps by IOPL, entries, the IOPL-sensitive instructions
            // and their outcome, and I/O that reads the TSS bitmap.
            if (vm) count("v86:steps:iopl=" + std::to_string(iopl));
            if (!vm && (after.eflags & 0x20000))
                count(std::string("v86:enter:") + (task_switch ? "task:" : "iret:") + stem_ +
                      ":iopl=" + std::to_string((after.eflags >> 12) & 3));
            if (!delivery) {
                if (vm && (op == "FA" || op == "FB" || op == "9C" || op == "9D" || op == "CD" || op == "CF"))
                    count("v86:sens:" + stem_.substr(0, stem_.find(':')) + ":iopl=" + std::to_string(iopl) + ':' +
                          outcome(result));
                const bool io = op == "E4" || op == "E5" || op == "E6" || op == "E7" || op == "EC" || op == "ED" ||
                                op == "EE" || op == "EF" || op == "6C" || op == "6D" || op == "6E" || op == "6F";
                if (io && (vm || before.cpl > iopl))
                    count(std::string("io-bitmap:") + (vm ? "v86" : "pm" + std::to_string(before.cpl)) + ':' +
                          outcome(result));
            }
        }

        // The TLB models, on every paged step: the instruction's page in the
        // ITLB, each data access's pages (both of a page-crossing one) in
        // the DTLB.
        const bool paged = before.cr0 & 0x80000000u;
        if (paged) {
            if (!was_paged_ || before.cr3 != last_cr3_) flush();
            ++paged_steps;
            if (!delivery) itlb.touch((before.seg[fes_i386::SEG_CS].base + before.eip) >> 12);
            auto data = [&](const fes_i386::Access &a) {
                const uint32_t from = a.linear >> 12, to = (a.linear + a.length - 1) >> 12;
                if (from != to) ++page_cross;
                for (uint32_t page = from;; ++page) {
                    dtlb.touch(page);
                    if (page == to) break;
                }
            };
            for (const auto &a : result.reads) data(a);
            for (const auto &a : result.writes) data(a);
        }
        // Flushes: a CR3 write, a PG change, and a task switch into a 32-bit
        // TSS with paging on, which loads CR3 whatever its value.
        if ((result.retired && op == "0F22.3") || ((after.cr0 ^ before.cr0) & 0x80000000u) ||
            (task_switch && (after.cr0 & 0x80000000u) && (after.tr.ar & 8)))
            flush();
        was_paged_ = after.cr0 & 0x80000000u;
        last_cr3_ = after.cr3;
    }

    void write(std::ostream &out) const {
        std::map<std::string, uint64_t> all = counts;
        all["paging:steps"] = paged_steps;
        all["paging:page-cross"] = page_cross;
        for (const Tlb *t : {&dtlb, &itlb}) {
            const std::string name = std::string("tlb:") + (t == &dtlb ? "dtlb" : "itlb") + std::to_string(t->entries);
            all[name + ":accesses"] = t->accesses;
            all[name + ":walks"] = t->walks;
        }
        for (const auto &[key, n] : all) {
            const auto at = first.find(key);
            out << key << ' ' << n << ' ' << (at == first.end() ? std::string("-") : at->second) << '\n';
        }
    }

private:
    PhysicalByte read_;
    std::string stem_, site_;
    bool was_paged_ = false;
    uint32_t last_cr3_ = 0;

    uint32_t read32(uint32_t physical) const {
        uint32_t value = 0;
        for (unsigned n = 0; n < 4; ++n) value |= uint32_t(read_(physical + n)) << (8 * n);
        return value;
    }
    void flush() {
        dtlb.flush();
        itlb.flush();
    }
    void count(const std::string &key) {
        first.emplace(key, site_);
        ++counts[key];
    }
    static const char *kind_name(fes_i386::EventKind kind) {
        static const char *names[] = {"exception", "intr", "nmi", "int"};
        return names[unsigned(kind)];
    }
    static std::string hex(unsigned value, int digits) {
        char text[16];
        std::snprintf(text, sizeof text, "%0*x", digits, value);
        return text;
    }
    // An instruction's outcome: ok, int (it delivered its INT n), gp=<error
    // code> or exc<vector>=<error code>.
    static std::string outcome(const fes_i386::StepResult &result) {
        if (result.events.empty()) return "ok";
        const fes_i386::Event &e = result.events[0];
        if (e.kind == fes_i386::EventKind::SoftwareInterrupt) return "int";
        if (e.kind == fes_i386::EventKind::Exception)
            return (e.vector == 13 ? std::string("gp") : "exc" + std::to_string(e.vector)) + '=' + hex(e.error_code, 4);
        return kind_name(e.kind);
    }
};

}  // namespace pc386_census
