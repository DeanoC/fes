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
        int d0=ex(a)-ex(b);
        printf("D=%3d:", d0);
        for(int s=0;s<14;s++){
            int before=ex(a)-ex(b);
            uint16_t sw=step(&a,b);
            int after=ex(a)-ex(b);
            printf(" %d->%d(-%d)", before, after, before-after);
            if(!((sw>>10)&1)) { printf(" done"); break; }
        }
        printf("\n");
        // the candidate rules, evaluated on the first step
        printf("      first drop=%d ; 32+(D%%32)=%d ; D%%64=%d ; 64=%d\n",
               d0-0, 32+(d0%32), d0%64, 64);
    }
    return 0;
}
