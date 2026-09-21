// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_catch_game.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
static void check(bool ok, const char* message) { if (!ok) { std::cerr << message << '\n'; std::exit(1); } }
int main() {
    Vfes_catch_game d;
    auto tick = [&]() { d.clk=0; d.eval(); d.clk=1; d.eval(); };
    d.reset=1; d.frame_tick=0; d.buttons=0; tick();
    check(d.paddle==140 && d.lives==3 && d.score==0, "reset state");
    d.reset=0;
    for (int i=0;i<100;++i) tick();
    check(d.drop_y==24, "state must only advance on video frame");
    d.frame_tick=1;
    for (int i=0;i<95;++i) tick();
    check(d.score==1 && d.lives==3 && d.sound_toggle==1, "first centered drop caught with sound event");
    d.buttons=4;
    for (int i=0;i<80;++i) tick();
    check(d.paddle==0, "left clamp");
    d.buttons=12; tick(); check(d.paddle==0, "opposing directions cancel");
    d.buttons=8;
    for (int i=0;i<80;++i) tick();
    check(d.paddle==280, "right clamp");
    // Restart yields deterministic play regardless of previous state.
    d.buttons=16; tick();
    check(d.paddle==140 && d.lives==3 && d.score==0, "A restarts");
    tick(); check(d.drop_y==26, "held A must not repeatedly reset");
    d.buttons=4; for (int i=0;i<40;++i) tick(); d.buttons=0;
    for (int i=0;i<400;++i) tick();
    check(d.lives==0, "three missed targets end game");
    auto y=d.drop_y; tick(); check(d.drop_y==y, "game over freezes play");
    d.x=100; d.y=100; d.eval(); check(d.rgb==0x600818, "game over visible");
    d.buttons=16; tick(); d.x=150; d.y=224; d.eval();
    check(d.rgb==0x40c0ff, "paddle pixels");
    d.reset=1; tick(); check(d.rgb==0 && d.sound_toggle==0, "hold clears picture and sound event");
    std::cout << "Catch frame pacing, catches, score, lives, edges, restart, pixels and hold passed\n";
}
