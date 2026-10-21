// SPDX-License-Identifier: MIT
// Optional golden-observation generator; not a CI/runtime dependency.
// Build with perfect6502 commit09fc542877a84318291aa42dab143a3e2c3db974:
// cc -O2 -I/path/to/perfect6502 so_phase_oracle.c \
//    /path/to/perfect6502/netlist_sim.c -o so_phase_oracle
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "perfect6502.c"

int main(int argc, char **argv) {
    if (argc < 4 || argc > 5) return 2;
    int adjacent = !strcmp(argv[1], "clv");
    if (!adjacent && strcmp(argv[1], "settled")) return 2;
    unsigned offset = (unsigned)strtoul(argv[2], NULL, 10);
    unsigned width = (unsigned)strtoul(argv[3], NULL, 10);
    unsigned falling = argc == 5 ? (unsigned)strtoul(argv[4], NULL, 10) : 1;
    if (offset >= (adjacent ? 48u : 16u) || width > 32 || falling > 1) return 2;
    const unsigned char settled[] = {
        0xb8,0xea,0xea,0x50,0xfe,0xa9,0xa5,0x8d,0,2,0x4c,0x0a,0xc0
    };
    const unsigned char clv_loop[] = {
        0xb8,0x50,0xfe,0xa9,0xa5,0x8d,0,2,0x4c,0x08,0xc0
    };
    memset(memory, 0, sizeof(memory));
    memcpy(memory + 0xc000, adjacent ? clv_loop : settled,
           adjacent ? sizeof(clv_loop) : sizeof(settled));
    memory[0xfffc] = 0; memory[0xfffd] = 0xc0;
    void *chip = initAndResetChip();
    setNode(chip, so, falling ? 1 : 0); recalcNodeList(chip);
    unsigned armed=0, injected=0, anchor=0, first_v=0, marker=0, fetches=0;
    int previous_sync=0;
    for (unsigned tick=0; tick<8000; ++tick) {
        if (tick % 8 == 0) {
            step(chip);
            // Node539 is the SYNC pad in the pinned original NMOS netlist.
            int sync = isNodeHigh(chip, 539);
            if (sync && !previous_sync &&
                readAddressBus(chip) == (adjacent ? 0xc001 : 0xc003)) {
                if (++fetches == (adjacent ? 1u : 2u)) {
                    armed=1; anchor=tick;
                }
            }
            previous_sync=sync;
        }
        if (armed && !injected && tick == anchor + (adjacent ? 0u : 16u) + offset) {
            setNode(chip, so, falling ? 0 : 1); recalcNodeList(chip);
            injected=tick;
        }
        if (injected && width && tick == injected + width) {
            setNode(chip, so, falling ? 1 : 0); recalcNodeList(chip);
        }
        if (injected && !first_v && (readP(chip) & 0x40)) first_v=tick;
        if (memory[0x200] == 0xa5) { marker=tick; break; }
    }
    printf("mode=%s offset=%u width=%u falling=%u anchor=%u injection=%u "
           "overflow=%u marker=%u captured=%u\n", argv[1], offset, width, falling,
           anchor, injected, first_v, marker, marker != 0);
    destroyChip(chip);
    return armed && injected ? 0 : 1;
}
