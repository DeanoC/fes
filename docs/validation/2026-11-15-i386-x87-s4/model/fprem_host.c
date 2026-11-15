// What the host's own x87 does for FPREM (D9 F8) and FPREM1 (D9 F5) over
// large exponent differences: how far each partial step reduces, what C2
// does, and what the final C0/C3/C1 quotient bits are.
#include <stdio.h>
#include <string.h>
#include <stdint.h>

typedef struct { uint64_t sig; uint16_t se; } f80;

static void show(const char*tag, long double v){
    f80 r; memcpy(&r,&v,10);
    printf("%s sig=%016llx se=%04x", tag,(unsigned long long)r.sig,r.se);
}

// run one FPREM/FPREM1 step: st0 = fprem(st0, st1), return the status word
static uint16_t step(long double *a, long double b, int one){
    uint16_t sw; long double r = *a;
    if(one) __asm__ volatile("fldt %2\n\tfldt %1\n\tfprem1\n\tfnstsw %%ax\n\tfstpt %1\n\tfstp %%st(0)"
        : "=a"(sw), "+m"(r) : "m"(b) : "st");
    else    __asm__ volatile("fldt %2\n\tfldt %1\n\tfprem\n\tfnstsw %%ax\n\tfstpt %1\n\tfstp %%st(0)"
        : "=a"(sw), "+m"(r) : "m"(b) : "st");
    *a = r; return sw;
}

static int expof(long double v){ f80 r; memcpy(&r,&v,10); return (r.se&0x7fff)-16383; }

int main(){
    struct { const char*name; long double a, b; } cs[] = {
        {"diff 0",    1.0L*3,                 2.0L},
        {"diff 63",   0x1.3579bdfp0L * (long double)(1ULL<<63), 1.0L},
        {"diff 64",   0x1.3579bdfp0L * 2.0L*(long double)(1ULL<<63), 1.0L},
        {"diff 65",   0x1.3579bdfp0L * 4.0L*(long double)(1ULL<<63), 1.0L},
        {"diff 200",  __builtin_ldexpl(0x1.23456789abcdefp0L, 200), 1.0L},
        {"diff 1000", __builtin_ldexpl(0x1.fedcba987654321p0L, 1000), 3.0L},
    };
    for(unsigned i=0;i<sizeof cs/sizeof*cs;i++)
      for(int one=0;one<2;one++){
        long double a=cs[i].a, b=cs[i].b;
        printf("== %-9s %s  expdiff=%d\n", cs[i].name, one?"FPREM1":"FPREM ",
               expof(a)-expof(b));
        for(int s=0;s<12;s++){
            int e0=expof(a);
            uint16_t sw=step(&a,b,one);
            int c0=(sw>>8)&1, c1=(sw>>9)&1, c2=(sw>>10)&1, c3=(sw>>14)&1;
            printf("   step%-2d exp %5d -> %5d (reduced %3d)  C2=%d", s, e0, expof(a),
                   e0-expof(a), c2);
            if(!c2) printf("  C0=%d C3=%d C1=%d  q&7=%d", c0,c3,c1, c3*4+c1*2+c0);
            printf("\n");
            if(!c2) break;
        }
      }
    return 0;
}
