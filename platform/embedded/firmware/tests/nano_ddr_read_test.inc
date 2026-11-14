/* SPDX-License-Identifier: BSD-3-Clause */
/* Included with the actual Avalon reader and inherited-probe RTL fixture. */
struct DDRReadFixture : Fixture {
    struct fes_nano_ddr_read reader{};
    fes_nano_ddr_read_observation output;
    unsigned fail_write=0,lose_phase=0,scope_calls=0,lose_scope=0;
    explicit DDRReadFixture(std::string fault=""):Fixture(std::move(fault),false) {
        golden=true;std::memset(&output,0xa5,sizeof(output));
        reader.io={this,owned_read,read_word,write_word,wait_word,trace};
        reader.qualified=1;reader.abi=0x10203041;reader.bus_base=0x95000000;reader.window_bytes=8192;
        std::copy(std::begin(build),std::end(build),reader.build);
    }
    static int owned_read(void *context) {
        auto& f=*static_cast<DDRReadFixture*>(context);
        if(++f.scope_calls==f.lose_scope)f.owned=false;
        return Fixture::owns(&f);
    }
    static int read_word(void *context,uint32_t offset,uint32_t *value) {
        auto& f=*static_cast<DDRReadFixture*>(context);
        int error=Fixture::read(&f,offset,value);
        if(f.fault=="returned-bytes" && offset==0x94 && f.reader.phase==FES_DDR_READ_VERIFY)*value=16;
        if(f.fault=="checksum" && offset==0x98 && f.reader.phase==FES_DDR_READ_VERIFY)*value^=1;
        if(f.fault=="last-data" && offset==0xa0 && f.reader.phase==FES_DDR_READ_VERIFY)*value^=1;
        return error;
    }
    static int write_word(void *context,uint32_t offset,uint32_t value) {
        auto& f=*static_cast<DDRReadFixture*>(context);
        if(++f.writes==f.fail_write)return -9;
        if(f.fault=="write-before")return -9;
        if(f.fault.empty() && offset==FES_EMBEDDED_REG_COMMAND && value==FES_EMBEDDED_COMMAND_DISABLE) {
            require(f.addresses==1 && f.beats==0 && !f.pending.empty(),"disable did not cover a real outstanding read");
            require(!f.d.ddr_rready && f.access(false,FES_EMBEDDED_REG_ACK_GENERATION_LO)==0,"read drained before disable");
        }
        f.csr_write(offset,value);
        if(f.fault=="owner-after-write")f.owned=false;
        return f.fault=="write-after"?-9:f.fault=="write-positive"?9:0;
    }
    static void wait_word(void *context) { Fixture::wait(static_cast<DDRReadFixture*>(context)); }
    static void trace(void *context,unsigned phase) {
        auto& f=*static_cast<DDRReadFixture*>(context);
        if(phase==f.lose_phase)f.owned=false;
    }
    void retired() {
        require(reader.failed && !reader.done && reader.first_error<0,"DDR read failure retained success");
        fes_nano_ddr_read_observation sentinel;std::memset(&sentinel,0xa5,sizeof(sentinel));
        require(!std::memcmp(&output,&sentinel,sizeof(output)),"failed DDR read changed output");
        const auto counts=std::array<unsigned,4>{reads,writes,waits,scope_calls};int error=reader.first_error;
        require(fes_nano_ddr_read(&reader,&output)==error,"DDR read lost first error");
        require(counts==std::array<unsigned,4>{reads,writes,waits,scope_calls},"DDR read replayed retired work");
        require(writes<=10 && addresses<=1,"DDR read commands replayed");
    }
};
static std::array<unsigned,2> ddr_read_cases() {
    unsigned cases=0,points=0,write_points=0,scope_points=0;
    for(unsigned i=0;i<8;i++)require(fes_nano_ddr_read_pattern(i)==Fixture::golden_data[i],"pinned memory pattern differs");
    for(bool immediate:{false,true}) {
        DDRReadFixture f;
        if(immediate) { f.stall=false;f.responses=true; }
        int result=fes_nano_ddr_read(&f.reader,&f.output);
        require(!result && f.reader.done,"bounded DDR read failed phase="+std::to_string(f.reader.phase)+
                " error="+std::to_string(result)+" polls="+std::to_string(f.reader.polls));
        require(f.addresses==1 && f.beats==1 && f.pending.empty() && !f.d.ddr_arvalid,"DDR receipt did not consume the real burst");
        require(f.output.returned_bytes==32 && f.output.generation==1 && f.output.cookie==7,"DDR receipt differs");
        auto counts=std::array<unsigned,4>{f.reads,f.writes,f.waits,f.scope_calls};
        require(fes_nano_ddr_read(&f.reader,&f.output)==-1 && f.reader.done,"completed DDR read replayed");
        require(counts==std::array<unsigned,4>{f.reads,f.writes,f.waits,f.scope_calls},"repeated DDR read performed I/O");
        if(!immediate) { points=f.reads;write_points=f.writes;scope_points=f.scope_calls; }
        unsigned writes=f.writes;
        require(!fes_nano_fabric_stop(&f.stop) && f.stop.done && !f.stop.disable_issued && !f.stop.probe.cancel_issued &&
                writes==f.writes,"complete master callback lost the existing DDR drain receipt");
        cases++;
    }
    for(const std::string fault:{"scope","scope-two","identity","version","abi","build","base","span","maintenance",
        "stale-ack","request","read-positive","owner-after-read","write-before","write-after","write-positive",
        "owner-after-write","held","no-response","response","response-id","extra-response","missing-last","wrong-data",
        "returned-bytes","checksum","last-data"}) {
        DDRReadFixture f(fault);
        require(fes_nano_ddr_read(&f.reader,&f.output)<0,"invalid DDR read accepted: "+fault);f.retired();cases++;
    }
    for(unsigned point=1;point<=points;point++) {
        DDRReadFixture f;f.fail_read=point;
        require(fes_nano_ddr_read(&f.reader,&f.output)==-7 && f.reads==point,"DDR read lost access failure");f.retired();cases++;
    }
    for(unsigned point=1;point<=write_points;point++) {
        DDRReadFixture f;f.fail_write=point;
        require(fes_nano_ddr_read(&f.reader,&f.output)==-9 && f.writes==point,"DDR write failure replayed");f.retired();cases++;
    }
    for(unsigned phase=FES_DDR_READ_INITIAL;phase<=FES_DDR_READ_COMPLETE;phase++) {
        DDRReadFixture f;f.lose_phase=phase;
        require(fes_nano_ddr_read(&f.reader,&f.output)==-1,"DDR phase lost ownership");f.retired();cases++;
    }
    for(unsigned point=1;point<=scope_points;point++) {
        DDRReadFixture f;f.lose_scope=point;
        require(fes_nano_ddr_read(&f.reader,&f.output)==-1,"DDR access lost ownership");f.retired();cases++;
    }
    for(unsigned missing=0;missing<9;missing++) {
        DDRReadFixture f;
        switch(missing) {
        case 0:f.reader.qualified=0;break;case 1:f.reader.qualified=2;break;case 2:f.reader.abi^=1;break;
        case 3:f.reader.bus_base+=16;break;case 4:f.reader.window_bytes=4096;break;
        case 5:f.reader.io.owns=nullptr;break;case 6:f.reader.io.read32=nullptr;break;
        case 7:f.reader.io.write32=nullptr;break;case 8:f.reader.io.wait=nullptr;break;
        }
        require(fes_nano_ddr_read(&f.reader,&f.output)==-22 && !f.reads && !f.writes,"unqualified DDR read performed I/O");
        f.retired();cases++;
    }
    return {cases,points};
}
