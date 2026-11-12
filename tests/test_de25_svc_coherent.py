import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]

@unittest.skipUnless(shutil.which("cc"), "C compiler required")
class ServiceCoherentDMATests(unittest.TestCase):
    def test_actual_helper_faults_and_dma_window(self):
        patch = (ROOT / "image/kernel/de25-platform/permanent-suppliers.patch").read_text()
        marker = "+static int svc_alloc_coherent_memory("
        self.assertEqual(patch.count(marker), 1)
        lines = patch[patch.index(marker):].splitlines()
        body = []
        for line in lines:
            self.assertTrue(line.startswith("+"), "helper must be wholly source-bound in patch")
            body.append(line[1:])
            if line == "+}":
                break
        else:
            self.fail("unterminated coherent allocation helper")
        helper = "\n".join(body) + "\n"
        with tempfile.TemporaryDirectory() as tmp:
            source = pathlib.Path(tmp) / "check.c"
            binary = pathlib.Path(tmp) / "check"
            source.write_text(PRELUDE + helper + CASES)
            subprocess.run(["cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror",
                            str(source), "-o", str(binary)], check=True, capture_output=True, timeout=30)
            result = subprocess.run([str(binary)], check=True, capture_output=True, text=True, timeout=10)
            self.assertIn("PASS ten actual-helper", result.stdout)

PRELUDE = r'''#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdio.h>
#include <errno.h>
#include <assert.h>
#include <string.h>
typedef uint64_t dma_addr_t;
#define DMA_BIT_MASK(n) ((UINT64_C(1)<<(n))-1)
#define PAGE_ALIGN(n) (((n)+4095)&~(size_t)4095)
#define GFP_KERNEL 0
struct stratix10_svc_controller {void *dev;};
struct stratix10_svc_data_mem {void *vaddr;uint64_t paddr;dma_addr_t dma_addr;size_t size;};
static unsigned allocs,frees;static size_t allocated,freed;static dma_addr_t address,freed_addr;static bool fail;static char storage;
static void *dma_alloc_coherent(void *dev,size_t size,dma_addr_t *addr,int flags){assert(dev==(void*)123&&flags==0);allocs++;allocated=size;*addr=address;return fail?NULL:&storage;}
static void dma_free_coherent(void *dev,size_t size,void *va,dma_addr_t addr){assert(dev==(void*)123&&va==&storage);frees++;freed=size;freed_addr=addr;}
'''

CASES = r'''
static void check(size_t size,dma_addr_t addr,bool failure,int expected,unsigned allocations,unsigned releases){
 struct stratix10_svc_controller ctrl={(void*)123};struct stratix10_svc_data_mem value,before;
 memset(&value,0x5a,sizeof(value));before=value;allocs=frees=0;address=addr;fail=failure;
 int result=svc_alloc_coherent_memory(&ctrl,&value,size);assert(result==expected&&allocs==allocations&&frees==releases);
 if(expected){assert(!memcmp(&value,&before,sizeof(value)));if(releases)assert(freed==PAGE_ALIGN(size)&&freed_addr==addr);}
 else{assert(value.vaddr==&storage&&value.paddr==addr&&value.dma_addr==addr&&value.size==PAGE_ALIGN(size)&&allocated==value.size);}
}
int main(void){
 check(0,0,false,-EINVAL,0,0);
 check(((size_t)1<<29)+1,0,false,-EINVAL,0,0);
 check((size_t)-1,0,false,-EINVAL,0,0);
 check(512*1024,0,false,0,1,0);
 check(1,0x1000,false,0,1,0);
 check(512*1024,0x1ff80000,false,0,1,0);
 check(8192,0x1ffff000,false,-ERANGE,1,1);
 check(4096,0x20000000,false,-ERANGE,1,1);
 check(4096,UINT64_MAX-4095,false,-ERANGE,1,1);
 check(512*1024,0,true,-ENOMEM,1,0);
 puts("PASS ten actual-helper allocation/fault/boundary cases; no physical DMA");return 0;
}
'''
