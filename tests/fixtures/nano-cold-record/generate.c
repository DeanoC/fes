/* Produce a real completed SPL cold record over an injected register model. */
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "nano_cold.h"

struct model { uint32_t reg[FES_COLD_REGISTER_COUNT]; };
static int read_reg(void *context, unsigned reg, uint32_t *value)
{
    struct model *m = context;
    assert(reg < FES_COLD_REGISTER_COUNT);
    *value = m->reg[reg];
    return 0;
}
static int write_reg(void *context, unsigned reg, uint32_t value)
{
    struct model *m = context;
    assert(reg == FES_COLD_PER0 || reg == FES_COLD_SD_INT ||
           reg == FES_COLD_SD_SIGNAL || reg == FES_COLD_SD_CLOCK);
    m->reg[reg] = value;
    return 0;
}
static void barrier(void *context) { assert(context); }
static void wait_reg(void *context) { assert(context); }

int main(int argc, char **argv)
{
    assert(argc == 3 && strlen(argv[1]) == 64);
    uint8_t chain[32];
    for (unsigned i = 0; i < 32; i++) {
        char byte[3] = {argv[1][2*i], argv[1][2*i+1], 0};
        char *end;
        chain[i] = (uint8_t)strtoul(byte, &end, 16);
        assert(end == byte+2);
    }
    char seed_text[17];
    memcpy(seed_text, argv[1], 16); seed_text[16] = 0;
    const uint64_t epoch = 0x101;
    const uint64_t owner = ((strtoull(seed_text, 0, 16) | 1ULL) ^ epoch) | 1ULL;
    const unsigned core = (unsigned)strtoul(argv[2], 0, 0);
    assert(core == 0 || core == 2);
    struct model m = {0};
    m.reg[FES_COLD_CAUSE] = 4;
    m.reg[FES_COLD_PER0] = FES_NANO_MASTER_RESET & ~FES_NANO_COLD_SD_RESET;
    m.reg[FES_COLD_BRG] = 15;
    m.reg[FES_COLD_CPUS] = 15;
    m.reg[FES_COLD_BOOT_KIND] = 4;
    m.reg[FES_COLD_CPU0_POWER+core] = 0x90000021;
    m.reg[FES_COLD_PCH_REQUEST] = m.reg[FES_COLD_PCH_STATUS] = 0x20 | (1U << (core+1));
    m.reg[FES_COLD_BOOT_CONFIG] = 0x70021e | (core << 10);
    m.reg[FES_COLD_SD_CLOCK] = 7;
    struct fes_nano_cold state = {0};
    struct fes_nano_cold_record record;
    struct fes_nano_cold_io io = {&m, read_reg, write_reg, barrier, wait_reg};
    assert(!fes_nano_cold_begin(&state, io, chain, owner, epoch, core));
    assert(!fes_nano_cold_finish(&state, 1, &record));
    assert(state.valid && !state.failed && record.flags == FES_NANO_COLD_FLAGS);
    assert(record.boot.fabric == FES_NANO_FABRIC_ABSENT);
    assert(record.checksum == fes_nano_cold_checksum(&record));
    assert(fwrite(&record, sizeof(record), 1, stdout) == 1);
    return 0;
}
