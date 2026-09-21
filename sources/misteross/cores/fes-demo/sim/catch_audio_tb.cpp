// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_catch_audio.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
static void check(bool ok,const char* m){if(!ok){std::cerr<<m<<'\n';std::exit(1);}}
int main(){
    Vfes_catch_audio d; d.locked=0; d.exec_reset=1; d.sound_toggle=0; d.clk=0;d.eval();
    auto cycles=[&](unsigned n){unsigned ones=0;while(n--){d.clk=0;d.eval();d.clk=1;d.eval();ones+=d.sdata;}return ones;};
    d.locked=1; check(!cycles(2000),"held startup silent");
    d.exec_reset=0;check(!cycles(2000),"no event silent");
    d.sound_toggle=1;check(cycles(20000)>0,"catch event chime");
    cycles(4800*256);check(!cycles(2000),"chime bounded to 100 ms");
    d.sound_toggle=0;check(cycles(20000)>0,"second toggle chime");
    d.exec_reset=1;cycles(1000);check(!cycles(2000),"hold mutes chime");
    d.exec_reset=0;check(!cycles(2000),"release must not replay event");
    d.sound_toggle=1;cycles(1000);d.locked=0;d.eval();check(!d.sdata,"loss of clock lock immediately mutes output");
    std::cout<<"Catch event CDC, bounded chime, hold and clock loss passed\n";
}
