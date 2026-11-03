/* SPDX-License-Identifier: BSD-3-Clause */
#define FES_WINDOW_STOP_TEST 1
#include "nano_fabric_stop_test.cpp"
#include <memory>

static std::unique_ptr<Fixture> model;
static bool probe_cancelled;
extern "C" int fes_strong_window_main(int,char **);
extern "C" uint32_t fes_test_reader_read(uint32_t offset)
{
    uint32_t value=model->access(false,offset);
    if(model->fault=="wrong-cookie" && offset==72 && value)value++;
    return value;
}
extern "C" void fes_test_reader_write(uint32_t offset,uint32_t value) { model->csr_write(offset,value); }
extern "C" uint32_t fes_test_window_probe_read(uint32_t offset) { return model->p.read(offset); }
extern "C" void fes_test_window_probe_write(uint32_t offset,uint32_t value)
{
    model->p.write(offset,value);probe_cancelled=true;
}
extern "C" void fes_test_window_wait()
{
    model->tick();
    if(probe_cancelled) {model->p.stall=false;model->p.responses=true;}
    model->p.tick();
}
extern "C" void fes_test_window_fault(unsigned which)
{
    if(which==6)model->fault="wrong-cookie";
}
extern "C" void fes_test_window_frame(unsigned which)
{
    auto &f=*model;
    f.stall=false;f.responses=which!=5;
    f.csr_write(36,1);f.csr_write(40,0);f.csr_write(52,1);
    f.csr_write(0x84,1);f.csr_write(0x88,0);f.csr_write(0x8c,1);f.csr_write(0x80,1);
    if(which==0 || which==7) {
        unsigned ticks=0;
        while(!(f.access(false,0x90)&4) && ticks++<300000)f.tick();
        require(ticks<300000 && f.access(false,0xa0)==3686400 &&
                f.access(false,0xa4)==3686400 && f.beats*32==3686400,"full window was not delivered");
        printf("FES_TEST_WINDOW_FRAME bytes=%u crc32=%08x\n",3686400,f.access(false,0xa8));
    } else {
        for(unsigned i=0;i<20;i++)f.tick();
        require(f.d.ddr_arvalid || !f.pending.empty(),"failure test has no active memory request");
    }
    // Also activate the independent real probe master; firmware must stop it.
    f.p.start(Descriptor{});f.p.stall=false;f.p.responses=false;
    for(unsigned i=0;i<8;i++)f.p.tick();
    f.p.stall=true;
    require(!f.p.pending.empty(),"independent probe has no accepted memory request");
}
int main(int argc,char **argv)
{
    model=std::make_unique<Fixture>("",false);
    int result=fes_strong_window_main(argc,argv);
    model.reset();
    return result;
}
