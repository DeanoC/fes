// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_zx81_video.h"
#include "Vfes_de25_zx81_video___024root.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#define R(name) t.rootp->fes_de25_zx81_video__DOT__##name
static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL DE25 video: %s\n", message); std::exit(1); }
}
static bool ink(unsigned seed, unsigned x, unsigned y) {
    return ((x * 13 + y * 7 + seed * 19) % 31) < 15;
}
struct Bench {
    Vfes_de25_zx81_video t;
    uint64_t now = 0, sn = 5, pn = 3, sys_edges = 0, pixel_edges = 0, samples = 0;
    unsigned sh, ph, publications = 0, swaps = 0, source_seed = 0;
    unsigned bank_seed[2]{}, bank_rows[2]{};
    bool sys_running = true, pix_running = true;
    Bench(unsigned a, unsigned b): sh(a), ph(b) {
        t.reset_n = 0; t.clk_sys = 0; t.pixel_clk = 0;
        t.held = 0; t.ce_6m5 = 1; t.zx_pixel = 0; t.hblank = 1; t.vblank = 1;
        t.eval(); cycles(8); t.reset_n = 1; t.eval(); cycles(8);
    }
    void step() {
        now = std::min(sn, pn);
        bool sr = false, pr = false;
        if (sn == now) { if (sys_running) { t.clk_sys = !t.clk_sys; sr = t.clk_sys; } sn += sh; }
        if (pn == now) { if (pix_running) { t.pixel_clk = !t.pixel_clk; pr = t.pixel_clk; } pn += ph; }
        unsigned h = R(horizontal), v = R(vertical), front = R(front);
        bool valid = R(display_valid);
        unsigned display_front = R(front);
        bool req = R(request), ack = R(acknowledge);
        if (sr && R(sample)) {
            check(R(write_address) < 41u*270u*2u, "write address outside storage");
            check(!valid || R(writer) != display_front, "writer touched the displayed bank");
            check(!R(pending), "writer touched a published bank");
        }
        t.eval();
        if (sr) {
            ++sys_edges;
            if (t.reset_n && req != R(request)) {
                ++publications;
                bank_seed[R(writer)] = source_seed;
                bank_rows[R(writer)] = R(published_rows);
            }
        }
        if (pr) {
            ++pixel_edges;
            check(bool(t.de) == (h < 1280 && v < 720), "DE/RAM latency mismatch");
            check(bool(t.hsync) == (h >= 1390 && h < 1430), "HS geometry mismatch");
            check(bool(t.vsync) == (v >= 725 && v < 730), "VS geometry mismatch");
            bool image = h >= 312 && h < 968 && v >= 90 && v < 630;
            unsigned x = image ? (h-312)/2 : 0, y = image ? (v-90)/2 : 0;
            bool bright = image && valid && (y >= bank_rows[display_front] || !ink(bank_seed[display_front], x, y));
            check(t.red == (bright ? 255 : 0) && t.green == t.red && t.blue == t.red,
                  "display pixel differs from the complete published source frame");
            if (image && valid) ++samples;
            if (R(front) != front || R(acknowledge) != ack) {
                check(h == 1649 && v == 749, "bank changed within an output frame");
                check(R(front_rows) == bank_rows[R(front)], "frame metadata crossed incoherently");
                ++swaps;
            }
        }
    }
    void cycles(uint64_t n) { auto end=sys_edges+n; while(sys_edges<end) step(); }
    void pixels(uint64_t n) { auto end=pixel_edges+n; while(pixel_edges<end) step(); }
    void frame(unsigned seed, unsigned height=270, unsigned width=328, bool finish=true) {
        source_seed=seed;
        t.vblank=1; t.hblank=1; cycles(4);
        t.vblank=0; cycles(2);
        for(unsigned y=0;y<height;++y) {
            for(unsigned x=0;x<width;++x) { t.hblank=0; t.zx_pixel=ink(seed,x,y); cycles(1); }
            t.hblank=1; cycles(2);
        }
        if(finish) { t.vblank=1; cycles(4); }
    }
    void reset_stopped() {
        // A terminal epoch reset invalidates owners while stopped, but the
        // selected scanout bank remains visible until the next frame boundary.
        sys_running=false; pix_running=false;
        const bool had_picture = R(display_valid);
        const bool held_de = t.de, held_hs = t.hsync, held_vs = t.vsync;
        const uint8_t held_red = t.red;
        t.reset_n=0; t.eval();
        check(!R(front_valid) && !R(request) && !R(acknowledge) &&
              R(display_valid) == had_picture && t.de == held_de &&
              t.hsync == held_hs && t.vsync == held_vs && t.red == held_red,
              "stopped-clock reset changed presentation before a raster boundary");
        // The integrated computer sim checks the real full-raster wait. Here
        // place the raster on its boundary to exercise the invalidation edge
        // without adding another 1.2M-pixel wait to each clock-ratio case.
        R(horizontal)=1649; R(vertical)=749;
        R(de_q)=0; R(hs_q)=0; R(vs_q)=0;
        R(image_q)=0; R(valid_q)=0; R(stored_q)=0; R(bit_q)=0;
        t.eval();
        pix_running=true;
        bool cleared_at_boundary = false;
        const uint64_t deadline = pixel_edges + 3;
        while (pixel_edges < deadline && !cleared_at_boundary) {
            const bool boundary = R(boundary);
            const uint64_t before = pixel_edges;
            step();
            if (boundary && pixel_edges != before) cleared_at_boundary = true;
        }
        check(cleared_at_boundary && !R(display_valid),
              "reset did not hide the last presented frame at a raster boundary");
        pixels(2);
        check(!R(front_valid) && !t.red, "reader replayed an old frame after boundary reset");
        t.reset_n=1; t.eval();
        sys_running=true; cycles(8);
    }
    void run() {
        pixels(1650*750+37); check(!R(front_valid), "startup displayed unwritten RAM");
        frame(1); pixels(1650*750*2); check(swaps==1 && samples, "first complete frame absent");
        unsigned before=publications;
        frame(2,270,327); pixels(1650*750); check(publications==before, "short line was published");
        frame(3,270,329); pixels(1650*750); check(publications==before, "long line was published");
        frame(4,7); pixels(1650*750*2); check(publications==before+1, "short source frame absent");
        frame(15,300); pixels(1650*750*2);
        check(R(front_rows)==270, "overscan crop changed");
        frame(16,7,328,false);
        for(unsigned x=0;x<10;++x) { t.hblank=0; t.zx_pixel=ink(16,x,7); cycles(1); }
        t.vblank=1; cycles(4); pixels(1650*750*2);
        check(R(front_rows)==7, "partial final source line became visible");
        // A stopped reader cannot release the pending writer bank. Several
        // complete newer source frames must be dropped, with no overwrite.
        pix_running=false;
        frame(5); before=publications;
        frame(6); frame(7); check(publications==before && R(pending), "stopped reader freed pending bank");
        pix_running=true; pixels(1650*750*2);
        check(R(front_valid) && !R(pending) && R(front_rows)==270 &&
              bank_seed[R(front)]==5,
              "queued completed frame did not become the displayed bank after reader resumed");
        frame(8); pixels(1650*750*2);
        // The reader repeats the last complete image with the source stopped.
        sys_running=false; pixels(1650*750*2); sys_running=true;
        // HOLD aborts a partial source capture and does not reset HDMI timing.
        frame(9,10,328,false); t.held=1; cycles(4); before=publications;
        t.held=0; t.vblank=1; cycles(4);
        check(publications==before, "HOLD published an incomplete source frame");
        frame(10); pixels(1650*750*2);
        // Abort a pending publication as well as a partial capture.
        pix_running=false; frame(11); reset_stopped();
        pixels(1650*750); check(!R(front_valid), "old publication survived reset");
        frame(12); pixels(1650*750*2);
        frame(13,17,328,false); reset_stopped();
        frame(14); pixels(1650*750*2);
        std::printf("PASS DE25 video: sys-half=%u pix-half=%u publications=%u swaps=%u checked-pixels=%llu; "
                    "startup, geometry, scaled edges, short/long lines, clock loss, HOLD, and reset-boundary presentation\n",
                    sh,ph,publications,swaps,(unsigned long long)samples);
    }
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    for(auto pair : {std::pair<unsigned,unsigned>{5,7}, {11,3}, {3,13}}) { Bench b(pair.first,pair.second); b.run(); }
}
