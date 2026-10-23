// SPDX-License-Identifier: GPL-2.0-or-later
// Bit-exact check of the default SID state-variable filter sequencer against
// the same fixed-point equations, plus cutoff/damping monotonicity and a DC
// response sanity check. This qualifies the sequencer and shared multiplier,
// not analogue MOS6581 fidelity.
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include "Vsid_svf_sim_top.h"
#include "verilated.h"
namespace{
const double PHI_HZ=985248.0;
const int POINTS[][2]={{0,220},{128,230},{256,250},{384,300},{512,420},{640,780},{768,1600},{832,2300},{896,3200},{960,4300},{992,5000},{1008,5400},{1016,5700},{1023,6000},{1024,4600},{1152,4800},{1280,5300},{1408,6300},{1536,8600},{1664,10800},{1792,11700},{1920,12000},{2047,12000}};
double f0(int fc){for(unsigned i=0;i+1<sizeof(POINTS)/sizeof(POINTS[0]);++i){int a=POINTS[i][0],fa=POINTS[i][1],b=POINTS[i+1][0],fb=POINTS[i+1][1];if(a<=fc&&fc<=b)return a==b?fa:fa+double(fb-fa)*(fc-a)/(b-a);}return 12000;}
long cutoff(int fc){return std::lround(2*std::sin(M_PI*f0((fc>>4)*16+8)/PHI_HZ)*1048576.0);}
long damping(int res){return std::lround(1048576.0/(0.707+res/15.0));}
int32_t wrap28(int64_t v){v&=(int64_t(1)<<28)-1;if(v&(int64_t(1)<<27))v-=int64_t(1)<<28;return int32_t(v);}
int32_t sext28(uint32_t v){return wrap28(int64_t(v&0x0fffffff));}
struct Model{
 int32_t lp=0,bp=0,hp=0;
 int32_t sample(unsigned w0,unsigned w1,unsigned w2,unsigned e0,unsigned e1,unsigned e2,unsigned fc,unsigned rf,unsigned mv){
  auto voice=[](unsigned w,unsigned e){return int32_t((int(w)-2048)*int(e));};
  int32_t v[3]={voice(w0,e0),voice(w1,e1),voice(w2,e2)};
  int64_t filtered=0,direct=0;
  for(unsigned i=0;i<3;++i){bool f=(rf>>i)&1;if(f)filtered+=v[i];else if(!(i==2&&(mv&0x80)))direct+=v[i];}
  filtered=wrap28(filtered);direct=wrap28(direct);
  long c=cutoff(fc),d=damping(rf>>4);
  hp=wrap28(filtered-lp-wrap28((int64_t(bp)*d)>>20));
  bp=wrap28(bp+wrap28((int64_t(hp)*c)>>20));
  lp=wrap28(lp+wrap28((int64_t(bp)*c)>>20));
  int64_t mixed=wrap28(direct+((mv&0x10)?lp:0)+((mv&0x20)?bp:0)+((mv&0x40)?hp:0));
  int64_t scaled=(mixed*int64_t(mv&15))>>10;
  if(scaled>32767)scaled=32767;if(scaled<-32768)scaled=-32768;return int32_t(scaled);
 }
};
}
int main(int argc,char **argv){
 Verilated::commandArgs(argc,argv);Vsid_svf_sim_top rtl;Model model;
 auto edge=[&](){rtl.clk=0;rtl.eval();rtl.clk=1;rtl.eval();};
 rtl.reset=1;rtl.start=0;edge();rtl.reset=0;edge();
 uint32_t rng=0x6581c64;auto random=[&](){rng^=rng<<13;rng^=rng>>17;rng^=rng<<5;return rng;};
 unsigned samples=0,worst_latency=0;
 auto run=[&](unsigned w0,unsigned w1,unsigned w2,unsigned e0,unsigned e1,unsigned e2,unsigned fc,unsigned rf,unsigned mv){
  rtl.wave0=w0;rtl.wave1=w1;rtl.wave2=w2;rtl.env0=e0;rtl.env1=e1;rtl.env2=e2;rtl.fc=fc;rtl.res_filt=rf;rtl.mode_vol=mv;
  rtl.start=1;edge();rtl.start=0;unsigned latency=1;
  while(!rtl.done){edge();if(++latency>48){std::fprintf(stderr,"SVF sample %u did not finish within 48 edges\n",samples);std::exit(1);}}
  if(rtl.busy){std::fprintf(stderr,"SVF busy after done\n");std::exit(1);}
  if(latency>worst_latency)worst_latency=latency;
  int32_t expected=model.sample(w0,w1,w2,e0,e1,e2,fc,rf,mv);
  if(int32_t(rtl.sample)!=expected||sext28(rtl.lp)!=model.lp||sext28(rtl.bp)!=model.bp||sext28(rtl.hp)!=model.hp){
   std::fprintf(stderr,"SVF mismatch sample %u: got %d expected %d (lp %d/%d bp %d/%d hp %d/%d)\n",samples,int32_t(rtl.sample),expected,sext28(rtl.lp),model.lp,sext28(rtl.bp),model.bp,sext28(rtl.hp),model.hp);std::exit(1);
  }
  ++samples;
 };
 // Random registers, including every routing/mode/volume combination.
 for(unsigned i=0;i<200000;++i){unsigned r=random(),s=random();
  run(r&0xfff,(r>>12)&0xfff,(s>>20)&0xfff,(r>>24)&0xff,s&0xff,(s>>8)&0xff,(random()>>5)&0x7ff,random()&0xff,(i%4096)<256?i%256:random()&0xff);}
 // Steady DC into the filter: LP settles to the input, HP to zero, for every cutoff.
 for(unsigned fc=0;fc<2048;fc+=64){
  rtl.reset=1;edge();rtl.reset=0;edge();model=Model();
  for(unsigned n=0;n<40000;++n)run(0xfff,0x800,0x800,0xff,0,0,fc,0x01,0x1f);
  int32_t expected=(0xfff-2048)*0xff;
  if(std::abs(model.lp-expected)>expected/32||std::abs(model.hp)>expected/32){std::fprintf(stderr,"SVF DC response fc=%u lp=%d hp=%d expected %d\n",fc,model.lp,model.hp,expected);std::exit(1);}
 }
 for(int fc=0;fc<2047;++fc)if(fc!=1023&&cutoff(fc+1)<cutoff(fc)){std::fprintf(stderr,"cutoff table not monotonic at %d\n",fc);std::exit(1);}
 for(int res=0;res<15;++res)if(damping(res+1)>=damping(res)){std::fprintf(stderr,"damping table not monotonic at %d\n",res);std::exit(1);}
 std::printf("SID SVF PASS: %u samples bit-exact, worst latency %u edges, DC and table checks\n",samples,worst_latency);
 return 0;
}
