// Build and run with c++ -std=c++11 test_pentium_pairing.cpp -o /tmp/test-pentium-pairing
#include "pentium_pairing.hpp"

#include <algorithm>
#include <cassert>
#include <initializer_list>

using namespace pentium_pairing;

static Instruction insn(std::initializer_list<std::uint8_t> bytes, bool default32 = true) {
    Instruction result;
    assert(bytes.size() <= result.bytes.size());
    std::copy(bytes.begin(), bytes.end(), result.bytes.begin());
    result.length = static_cast<std::uint8_t>(bytes.size());
    result.default32 = default32;
    return result;
}

int main() {
    const Instruction mov_eax = insn({0xb8, 1, 0, 0, 0});
    const Instruction mov_ecx = insn({0xb9, 1, 0, 0, 0});
    const Instruction add_ebx_edx = insn({0x01, 0xd3});
    const Instruction load_al = insn({0x8a, 0x07});
    const Instruction mov_ah = insn({0xb4, 4});

    assert(can_pair(mov_eax, add_ebx_edx));
    assert(can_pair(mov_eax, mov_ecx));
    assert(!can_pair(mov_eax, insn({0x8b, 0xc8})));       // RAW EAX -> source
    assert(!can_pair(mov_eax, insn({0x89, 0xc8})));       // WAW EAX
    assert(!can_pair(insn({0x89, 0xc8}), mov_ecx));       // WAR ECX
    assert(!can_pair(load_al, mov_ah));                    // AH/AL share full GPR
    assert(can_pair(load_al, mov_ecx));
    assert(!can_pair(mov_ecx, load_al));                   // V may not load
    assert(!can_pair(add_ebx_edx, insn({0x84, 0xc0})));   // TEST writes flags
    assert(!can_pair(add_ebx_edx, insn({0x40})));         // INC writes flags
    assert(!can_pair(mov_ecx, insn({0x14, 1})));          // ADC reads carry
    assert(!can_pair(mov_ecx, insn({0x83, 0xd8, 1})));    // SBB in group 1
    assert(can_pair(mov_ecx, insn({0x83, 0xf8, 1})));     // CMP EAX,imm8
    assert(!can_pair(mov_eax, insn({0x83, 0xf8, 1})));
    assert(!can_pair(mov_eax, insn({0xa8, 0xff})));       // implicit AL
    assert(can_pair(mov_ecx, insn({0xf6, 0xc0, 0xff})));  // TEST AL,imm8
    assert(!classify(insn({0xf6, 0xd0})).u);              // NOT AL
    assert(!classify(insn({0x0f, 0xaf, 0xc3})).u);        // IMUL r,r
    assert(!classify(insn({0x03, 0x07})).u);              // memory ALU excluded
    assert(!classify(insn({0x83, 0x07, 1})).u);           // memory RMW excluded

    const Instruction store = insn({0x89, 0x44, 0x8d, 0x04}); // [EBP+ECX*4+4] <- EAX
    assert(!can_pair(store, mov_ecx));                        // index
    assert(!can_pair(store, insn({0x45})));                   // base EBP
    assert(!can_pair(store, mov_eax));                        // store data
    assert(can_pair(store, add_ebx_edx));
    assert(!classify(store).v);
    assert(!classify(insn({0x89, 0x44, 0x8d})).u);            // missing disp8
    assert(!classify(insn({0x89, 0x04})).u);                  // missing SIB
    assert(can_pair(insn({0xc7, 0x04, 0x25, 0, 1, 0, 0, 7, 0, 0, 0}),
                    insn({0x45})));                           // SIB no base/index
    assert(!classify(insn({0xc7, 0x0f, 1, 0, 0, 0})).u);     // invalid /1
    assert(!can_pair(insn({0xa3, 0, 0, 1, 0}), mov_eax));   // implicit store data
    assert(can_pair(insn({0xa1, 0, 0, 1, 0}), mov_ecx));

    const Instruction store16 = insn({0x89, 0x00}, false);    // [BX+SI] <- AX
    assert(can_pair(store16, insn({0x41}, false)));
    assert(!can_pair(store16, insn({0x43}, false)));
    assert(!can_pair(store16, insn({0x46}, false)));
    assert(can_pair(insn({0x89, 0x06, 0, 1}, false), insn({0x45}, false))); // disp16
    assert(!can_pair(insn({0x89, 0x46, 0}, false), insn({0x45}, false)));  // BP+disp8
    assert(classify(insn({0x81, 0xc3, 1, 0}, false)).v);
    assert(!classify(insn({0x81, 0xc3, 1, 0}, true)).u);     // imm32 incomplete
    assert(!classify(insn({0x88, 0xc8, 0}, false)).u);       // extra bytes

    for (auto excluded : {insn({0x66, 0x40}), insn({0x67, 0x8b, 0x07}),
                          insn({0xf0, 0x83, 0x00, 1}), insn({0xf3, 0xa4}),
                          insn({0x26, 0x8a, 0x07}), insn({0x52}), insn({0xd9, 0xf2}),
                          insn({0x75, 0}), insn({0xeb, 0}), insn({0xc3}),
                          insn({0xcd, 0x21}), insn({0xcf}), insn({0xfb})}) {
        assert(!can_pair(excluded, mov_ecx));
        assert(!can_pair(mov_ecx, excluded));
    }
    auto after_event = mov_ecx;
    after_event.boundary_before = true;
    assert(!can_pair(mov_eax, after_event));
    assert(can_pair(after_event, mov_eax));                  // boundary precedes U
    assert(!can_pair(mov_ecx, insn({0x40}, false)));          // mode change
    assert(!classify(Instruction{}).u);
    auto bad_length = mov_ecx;
    bad_length.length = 16;
    assert(!classify(bad_length).u);
}
