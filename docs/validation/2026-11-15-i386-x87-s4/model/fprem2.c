// Pin two things: how many QUOTIENT bits a partial step consumes (not the
// exponent drop, which cancellation moves), and the C0/C3/C1 encoding.
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
typedef struct { uint64_t sig; uint16_t se; } f80;
static uint16_t step(long double *a, long double b, int one){
    uint16_t sw; long double r=*a;
    if(one) __asm__ volatile("fldt %2\n\tfldt %1\n\tfprem1\n\tfnstsw %%ax\n\tfstpt %1\n\tfstp %%st(0)"
        :"=a"(sw),"+m"(r):"m"(b):"st");
    else __asm__ volatile("fldt %2\n\tfldt %1\n\tfprem\n\tfnstsw %%ax\n\tfstpt %1\n\tfstp %%st(0)"
        :"=a"(sw),"+m"(r):"m"(b):"st");
    *a=r; return sw;
}
int main(){
    // b = 1.0 exactly, a = odd integer * 2^k : then the true quotient is the
    // integer a, and each step's consumed quotient bits are visible because
    // a mod 1 == 0 only at the end. Use b = 3.0 so the quotient is not a
    // power of two.
    printf("-- quotient bits consumed per step (b=3, a=2^n*1.0101..) --\n");
    for(int n=60; n<=400; n+=60){
        long double b=3.0L, a=__builtin_ldexpl(0x1.5555555555555554p0L,n);
        printf("n=%3d expdiff=%3d:", n, n-1);
        for(int s=0;s<12;s++){
            uint16_t sw=step(&a,b,0);
            if(!((sw>>10)&1)){ printf(" done@%d", s+1); break; }
            printf(" .");
        }
        printf("\n");
    }
    printf("\n-- the C0/C3/C1 encoding: q = C0*4 + C3*2 + C1 --\n");
    printf("  a mod b   FPREM q  C0 C3 C1  |  FPREM1 q  C0 C3 C1\n");
    for(int q=0;q<=9;q++){
        long double b=1.0L;
        for(int one=0;one<2;one++){
            long double a=(long double)q + 0.25L;
            uint16_t sw=step(&a,b,one);
            int c0=(sw>>8)&1,c1=(sw>>9)&1,c2=(sw>>10)&1,c3=(sw>>14)&1;
            if(!one) printf("  %d.25      ", q);
            printf("%s q=%d  %d  %d  %d %s", one?" | ":"", c0*4+c3*2+c1, c0,c3,c1,
                   c2?"(C2!)":"");
        }
        printf("\n");
    }
    printf("\n-- the invalid cases --\n");
    struct{const char*n; long double a,b;} iv[]={
        {"inf mod 2", __builtin_infl(), 2.0L},
        {"2 mod 0",   2.0L, 0.0L},
        {"0 mod 2",   0.0L, 2.0L},
        {"2 mod inf", 2.0L, __builtin_infl()},
    };
    for(unsigned i=0;i<4;i++){
        long double a=iv[i].a;
        __asm__ volatile("fnclex");
        uint16_t sw=step(&a,iv[i].b,0);
        f80 r; memcpy(&r,&a,10);
        printf("  %-10s sw=%04x IE=%d C2=%d  result sig=%016llx se=%04x\n",
               iv[i].n, sw, sw&1, (sw>>10)&1,(unsigned long long)r.sig, r.se);
    }
    return 0;
}
