/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mac_c64_source.h"
#include <assert.h>
#include <string.h>

/* The host utility emits this read-only record from the actual fitted C64
 * package or a regular-file test fixture. No firmware or ROM payload enters C. */
#include "c64_scope_identity.h"

static const struct fes_nano_spectrum48_program matched = FES_NANO_C64_SCOPE_IDENTITY;

int main(void)
{
    struct fes_nano_program p = {
        .initialized=1,.bound=1,.mixed_roles=1,.shared_core_shell=1,
        .owner=0x21,.generation=9,.role=FES_NANO_PROGRAM_ROLE_SPECTRUM48,
        .control_abi=FES_NANO_PROGRAM_SPECTRUM48_ABI,.bytes=matched.bytes,.chunk=0xfffc0,
    };
    struct nano_sdm_input_binding b = {.owner=p.owner,.generation=p.generation,.epoch=5,
                                      .bytes=p.bytes,.chunk=p.chunk};
    memcpy(p.header+FES_NANO_PROGRAM_PACKAGE,matched.package,sizeof(matched.package));
    memcpy(p.header+FES_NANO_PROGRAM_BUILD,matched.build,sizeof(matched.build));
    memcpy(p.header+FES_NANO_PROGRAM_SHA256,matched.sha256,sizeof(matched.sha256));
    memcpy(p.header+FES_NANO_PROGRAM_SHA384,matched.sha384,sizeof(matched.sha384));
    memcpy(p.header+FES_NANO_PROGRAM_CHUNKS,matched.chunks,sizeof(matched.chunks));
    memcpy(b.digest384,matched.sha384,sizeof(b.digest384));
    assert(fes_nano_program_matches_c64(&p,&matched,&b));
    /* Same ABI, sizes, role, owner and secure MAC do not rescue any stale
     * field, including the protected SDM payload actually completed. */
    for (unsigned i=FES_NANO_PROGRAM_PACKAGE; i<FES_NANO_PROGRAM_ROLE_OFFSET; i++){
        p.header[i]^=0x42;
        assert(!fes_nano_program_matches_c64(&p,&matched,&b));
        p.header[i]^=0x42;
    }
    b.digest384[3]^=0x80; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); b.digest384[3]^=0x80;
    b.owner++; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); b.owner--;
    b.generation++; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); b.generation--;
    b.bytes--; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); b.bytes++;
    b.chunk--; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); b.chunk++;
    p.failed=1; assert(!fes_nano_program_matches_c64(&p,&matched,&b)); p.failed=0;
    p.role=FES_NANO_PROGRAM_ROLE_MENU; assert(!fes_nano_program_matches_c64(&p,&matched,&b));
    return 0;
}
