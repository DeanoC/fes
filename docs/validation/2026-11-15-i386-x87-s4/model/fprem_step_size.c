// How far does ONE partial step actually reduce? Print the exponent
// difference before and after each step, for the same cases whose
// instruction counts did not fit "64 bits a step".
#include <stdio.h>
#include <string.h>
#include <stdint.h>
typedef struct { uint64_t sig; uint16_t se; } f80;
static uint16_t step(long double *a, long double b){
    uint16_t sw; long double r=*a;
    __asm__ volatile("fldt %2\n\tfldt %1\n\tfprem\n\tfnstsw %%ax\n\tfstpt %1\n\tfstp %%st(0)"
        :"=a"(sw),"+m"(r):"m"(b):"st");
    *a=r; return sw;
}
static int ex(long double v){ f80 r; memcpy(&r,&v,10); return (r.se&0x7fff)-16383; }
int main(){
    for(int n=60;n<=360;n+=60){
        long double b=3.0L, a=__builtin_ldexpl(0x1.5555555555555554p0L,n);
        int d0=ex(a)-ex(b), first_drop=0;
        printf("D=%3d:", d0);
        for(int s=0;s<14;s++){
            int before=ex(a)-ex(b);
            uint16_t sw=step(&a,b);
            int after=ex(a)-ex(b);
            printf(" %d->%d(-%d)", before, after, before-after);
            if(s==0) first_drop = before-after;
            if(!((sw>>10)&1)) { printf(" done"); break; }
        }
        printf("\n");
        // The candidate rules against the FIRST STEP'S OWN DROP, which is what
        // has to be explained. Printing d0 here instead labelled the whole
        // difference as the first drop -- D=119 read as a drop of 119 where
        // the line above says 119->64 -- so the summary could not validate
        // anything. Codex found it on PR #1889.
        printf("      first drop=%d ; (D&31)|32=%d ; cancellation=%d\n",
               first_drop, (d0 & 31) | 32, first_drop - ((d0 & 31) | 32));
    }
    return 0;
}
