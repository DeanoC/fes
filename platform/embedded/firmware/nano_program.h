/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_PROGRAM_H
#define FES_NANO_PROGRAM_H
#include <stdint.h>

/* Private cold-platform descriptor. Its location and DMA envelope belong to
 * the platform, never to this non-secure request. All fields are little-endian
 * except the identity/digest byte strings. Unused digest slots and padding are
 * zero. The descriptor supplies integrity, not package/producer authentication.
 */
#define FES_NANO_PROGRAM_MAGIC 0x46505231U
#define FES_NANO_PROGRAM_VERSION 1U
#define FES_NANO_PROGRAM_HEADER_BYTES 640U
#define FES_NANO_PROGRAM_METADATA_BYTES 4096U
#define FES_NANO_PROGRAM_MAX_BYTES ((8U<<20)-FES_NANO_PROGRAM_METADATA_BYTES)
#define FES_NANO_PROGRAM_MAX_CHUNKS 9U
/* The bounded byte count uses less than 24 bits. This acknowledgement bit
 * distinguishes captured per-load bindings from the former fixed-source wire
 * response; mismatched native/firmware versions reject before START. */
#define FES_NANO_PROGRAM_BOUND_REPLY 0x80000000U
#define FES_NANO_PROGRAM_CONTEXT 40U
#define FES_NANO_PROGRAM_PACKAGE 72U
#define FES_NANO_PROGRAM_BUILD 104U
#define FES_NANO_PROGRAM_SHA256 120U
#define FES_NANO_PROGRAM_SHA384 152U
#define FES_NANO_PROGRAM_CHUNKS 200U
#define FES_NANO_PROGRAM_ROM_VERSION 2U
#define FES_NANO_PROGRAM_ROM_HEADER_BYTES 128U
#define FES_NANO_PROGRAM_ROM_MAGIC 0x31425246U /* FRB1 */
#define FES_NANO_PROGRAM_ROM_SHA256 24U
#define FES_NANO_PROGRAM_ROM_SHA384 56U
/* The final eight bytes follow all nine digest slots. Version 3 selects one
 * of the platform's two compiled personalities; it supplies no address, mask
 * or master inventory. Both roles carry the fixed ROM-extension slot, zero
 * for menu and mandatory for the current held-ROM computer driver. */
#define FES_NANO_PROGRAM_ROLE_VERSION 3U
#define FES_NANO_PROGRAM_ROLE_OFFSET 632U
#define FES_NANO_PROGRAM_ROLE_MENU 1U
#define FES_NANO_PROGRAM_ROLE_COMPUTER 2U
#define FES_NANO_PROGRAM_MENU_ABI 0x10203043U
#define FES_NANO_PROGRAM_COMPUTER_ABI 0x10203041U

struct fes_nano_program_ops {
    void *context;
    /* Zero means the exact owner/generation, closed bridges, stopped devices
     * and qualified CPU mapping remain valid. Calls must have bounded return. */
    int (*guard)(void *, uint64_t owner, uint64_t generation);
    /* Read only the platform's fixed descriptor, never a supplied address. */
    int (*read)(void *, uint8_t result[FES_NANO_PROGRAM_HEADER_BYTES]);
};
struct fes_nano_program {
    struct fes_nano_program_ops io;
    uint8_t context[32], header[FES_NANO_PROGRAM_HEADER_BYTES];
    uint64_t owner, generation;
    uint32_t maximum, chunk, bytes, chunks;
    unsigned initialized, bound, failed;
    /* Trusted opt-in before capture. Version 2 binds a ROM extension at the
     * fixed metadata offset HEADER_BYTES, not a non-secure supplied address. */
    int (*read_rom)(void *, uint8_t result[FES_NANO_PROGRAM_ROM_HEADER_BYTES]);
    uint8_t rom_header[FES_NANO_PROGRAM_ROM_HEADER_BYTES];
    uint32_t rom_offset, rom_bytes, rom_crc;
    unsigned mixed_roles;
    uint32_t role, control_abi;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_program_init(struct fes_nano_program *, struct fes_nano_program_ops,
                         const uint8_t context[32], uint32_t maximum, uint32_t chunk,
                         unsigned qualified);
/* The configuration engine must authorize a fresh cold attempt or a successful
 * prior transfer/return before capture. Failure is terminal, including a lost
 * read; changing the request cannot repair or replay that attempt. */
int fes_nano_program_capture(struct fes_nano_program *, uint64_t owner, uint64_t generation);
const uint8_t *fes_nano_program_expected(const struct fes_nano_program *,
                                       uint32_t offset, uint32_t bytes, unsigned whole);
int fes_nano_program_build(const struct fes_nano_program *, uint32_t words[4]);
int fes_nano_program_enable_rom(struct fes_nano_program *,
    int (*read_rom)(void *, uint8_t result[FES_NANO_PROGRAM_ROM_HEADER_BYTES]));
/* Trusted platform opt-in, before any capture and after installing read_rom.
 * It replaces fixed v1/v2 capture with v3; requests cannot enable or downgrade
 * it. Captured role is available only after successful capture; it is not a
 * configuration-completion or live activation proof. */
int fes_nano_program_enable_roles(struct fes_nano_program *);
int fes_nano_program_role(const struct fes_nano_program *, uint32_t *role, uint32_t *control_abi);
#ifdef __cplusplus
}
#endif
#endif
