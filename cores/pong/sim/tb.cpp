// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vpong_game.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>

static void require(bool yes, const char* message) {
    if (!yes) { std::cerr << message << '\n'; std::exit(1); }
}
struct Game {
    Vpong_game g;
    void tick() { g.clk=0; g.eval(); g.clk=1; g.eval(); }
    void frame() { g.frame_tick=1; tick(); g.frame_tick=0; tick(); }
    void reset() { g.reset=1; tick(); g.reset=0; tick(); }
    void start() { g.start=1; frame(); g.start=0; }
    bool white(unsigned x,unsigned y) {
        g.pixel_x=x; g.pixel_y=y; g.eval();
        return g.red==255 && g.green==255 && g.blue==255;
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    Game t; auto& g=t.g;
    t.reset();
    require(!g.playing && g.ball_x==158 && g.ball_y==118 && g.player_y==104 && g.ai_y==104,
            "reset positions/idle");
    require(g.player_score==0 && g.ai_score==0 && !g.tone,"reset scores/audio");
    require(t.white(12,104) && t.white(304,104) && t.white(158,118),"paddles and ball visible");
    require(t.white(140,8) && !t.white(143,12),"zero score glyph");
    require(!t.white(0,0) && !t.white(320,240),"background and blanking");
    g.up=1; for(int i=0;i<100;i++) t.frame();
    require(g.player_y==0,"top clamp");
    g.down=1; t.frame(); require(g.player_y==0,"opposing inputs neutral");
    g.up=0; for(int i=0;i<100;i++) t.frame();
    require(g.player_y==208,"bottom clamp"); g.down=0;
    t.reset(); t.start(); require(g.playing,"start begins game");
    auto x=g.ball_x; for(int i=0;i<20;i++) t.tick();
    require(g.ball_x==x,"movement only on frame tick");
    t.frame(); require(g.ball_x==155 && g.ball_y==120,"deterministic initial velocity");
    bool wall=false,paddle=false,point=false,tone=false;
    int prev_dy=2,prev_dx=-3;
    for(int i=0;i<10000;i++) {
        // Return from the upper paddle edge: a fast shot can beat the AI.
        int aim=int(g.ball_y) + 12;
        g.up=(int(g.player_y)+16 > aim);
        g.down=(int(g.player_y)+16 < aim);
        int bx=g.ball_x,by=g.ball_y,ay=g.ai_y;
        t.frame();
        require(g.player_y<=208 && g.ai_y<=208,"paddle bounds throughout rally");
        require(std::abs(int(g.ai_y)-ay)<=1,"bounded AI speed");
        if(g.playing) {
            int dx=int(g.ball_x)-bx,dy=int(g.ball_y)-by;
            if(dy*prev_dy<0 && (by<=3 || by>=233)) wall=true;
            if(dx*prev_dx<0) paddle=true;
            if(dx) prev_dx=dx; if(dy) prev_dy=dy;
            require(g.ball_y<=236,"ball vertical bounds");
        }
        for(int j=0;j<8;j++) {t.tick(); tone |= bool(g.tone);}
        if(g.player_score) {point=true; break;}
        if(!g.playing) t.start();
    }
    require(wall && paddle && point && tone,"wall/paddle bounce, player score and tone");
    require(!g.playing && g.ball_x==158 && g.ball_y==118,"point returns ball to idle serve");
    require(!t.white(140,8) && t.white(145,8),"score changes visible glyph");
    for(int i=0;i<1000;i++) t.tick(); require(!g.tone,"tone expires");
    t.reset();
    bool right_paddle=false;
    for(int i=0;i<20000 && !right_paddle;i++) {
        int aim=int(g.ball_y) + 2;
        g.up=(int(g.player_y)+16 > aim);
        g.down=(int(g.player_y)+16 < aim);
        if(!g.playing) { t.start(); continue; }
        int bx=g.ball_x;
        t.frame();
        if(bx==300 && g.ball_x==297) right_paddle=true;
    }
    require(right_paddle,"right paddle returns a ball");
    // Deliberately miss every left return to exercise the other scoring side.
    t.reset(); g.up=1; g.down=0;
    for(int i=0;i<60;i++) t.frame(); g.up=0;
    g.start=1; t.frame();
    for(int i=0;i<200 && g.playing;i++) t.frame();
    require(g.ai_score==1 && !g.playing,"held start permits first point");
    for(int i=0;i<20;i++) t.frame();
    require(!g.playing && g.ai_score==1,"held start does not auto-serve");
    g.start=0; t.frame();
    t.start(); require(g.playing,"fresh start serves next point");
    for(int i=0;i<20000 && g.ai_score<2;i++) {
        if(!g.playing) t.start(); else t.frame();
    }
    require(g.ai_score==2,"AI scores on a second player miss");
    t.reset(); require(!g.playing && !g.player_score && !g.ai_score && !g.tone,"reset after play");
    std::cout << "Pong simulation: reset, bounds, start, motion, collisions, scores, RGB and tone passed\n";
}
