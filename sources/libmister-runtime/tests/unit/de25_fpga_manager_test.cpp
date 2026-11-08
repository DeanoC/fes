// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_fpga_manager.hpp"
#include "native/linux/mmio.hpp"
#include "native/artifacts.hpp"
#include "native/sha256.hpp"
#include "native/core_package.hpp"
#include "de25_menu_operations.hpp"
#include "de25_pr_endpoint.hpp"
#include <cassert>
#include <algorithm>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <fcntl.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>
#include <iostream>
#include <map>
#include <chrono>
#include <future>
#include <thread>
#include <limits>
using namespace mister;
using namespace mister::native;
namespace {
void Put(const std::string& p, const std::string& s) { std::ofstream f(p, std::ios::binary); f << s; assert(f.good()); }
std::string Get(const std::string& p) { std::ifstream f(p,std::ios::binary); return {(std::istreambuf_iterator<char>(f)),{}}; }
bool Exists(const std::string& p) { struct stat s{}; return !lstat(p.c_str(),&s); }
struct Time final : Clock { std::uint64_t now = 0; std::uint64_t NowMs() const override { return now; } };
struct Ops : De25FpgaTestOperations {
    De25FpgaTestPaths& paths; Time& time;
    int created=0, written=0, removed=0, closed=0;
    int search_writes=0;
    std::string expected_payload="immutable test image", expected_persona_payload="immutable test persona";
    std::string fault_name;
    std::vector<std::string> events;
    std::function<void()> base_applied, persona_applied;
    std::function<void(const std::string&)> before_write;
    std::function<void(const std::string&)> before_remove;
    bool late_remove=false;
    bool Matches(const char* name) const { return fault_name.empty() || fault_name==name; }
    bool short_write=false, error_close=false, late_close=false, unapplied=false;
    bool remove_error=false, create_error=false, open_error=false;
    bool short_search=false, restore_error=false, changed_search=false;
    bool search_newline=false;
    LinuxMmioTestOperations* startup_registers=nullptr;
    LinuxMmioTestOperations* StartupRegisters() override { return startup_registers; }
    Ops(De25FpgaTestPaths& p, Time& t):paths(p),time(t) {}
    std::string Dir(const char* name="fes-runtime") const {return paths.config+"/device-tree/overlays/"+name;}
    std::string Name(int fd) const {
        struct stat opened{}, candidate{};assert(!fstat(fd,&opened));
        for(const char* name:{"fes-runtime","fes-runtime-persona"}) {
            if(!stat((Dir(name)+"/dtbo").c_str(),&candidate) && opened.st_dev==candidate.st_dev && opened.st_ino==candidate.st_ino) return name;
        }
        assert(false);return {};
    }
    int Create(int parent,const char* name) override {
        ++created;events.push_back(std::string("create:")+name);
        if(create_error && Matches(name)) {errno=EACCES;return -1;}
        if(mkdirat(parent,name,0700)) return -1;
        if(!open_error || !Matches(name)) Put(Dir(name)+"/dtbo", "");
        Put(Dir(name)+"/status","unapplied\n");return 0;
    }
    ssize_t Write(int fd,const unsigned char* p,std::size_t n) override {
        const auto name=Name(fd);const auto dir=Dir(name.c_str());
        ++written;events.push_back("write:"+name);
        assert(Get(dir+"/status")=="unapplied\n");
        assert(Get(paths.search)==paths.firmware+"\n");
        const char* file=name=="fes-runtime"?"fes-runtime-core.rbf":"fes-runtime-persona.rbf";
        assert(Get(paths.firmware+"/"+file)==(name=="fes-runtime"?expected_payload:expected_persona_payload));
        struct stat staged{};assert(!stat((paths.firmware+"/"+file).c_str(),&staged));
        assert((staged.st_mode & 0777)==0444);
        if(before_write)before_write(name);
        // Match configfs buffering: the release callback applies the blob.
        return write(fd,p,short_write && Matches(name.c_str())?n-1:n);
    }
    int Close(int fd) override {
        const auto name=Name(fd);const auto dir=Dir(name.c_str());const bool fault=Matches(name.c_str());
        ++closed;events.push_back("close:"+name);const int r=close(fd);assert(!r);
        if(late_close && fault)time.now=1000;
        Put(dir+"/status",unapplied && fault?"unapplied\n":"applied\n");
        Put(paths.state,unapplied && fault?"write error\n":"operating\n");
        if(changed_search && fault)Put(paths.search,"/another/loader\n");
        if(search_newline && fault)Put(paths.search,paths.firmware+"\n\n");
        if(!(unapplied && fault)) {
            if(name=="fes-runtime" && base_applied)base_applied();
            if(name=="fes-runtime-persona" && persona_applied)persona_applied();
        }
        if(error_close && fault) {errno=EINTR;return -1;}
        return 0;
    }
    int Remove(int parent,const char* name) override {
        ++removed;events.push_back(std::string("remove:")+name);
        if(before_remove)before_remove(name);
        if(remove_error && Matches(name)) {errno=EBUSY;return -1;}
        unlink((Dir(name)+"/dtbo").c_str());unlink((Dir(name)+"/status").c_str());
        const int result=unlinkat(parent,name,AT_REMOVEDIR);
        if(late_remove && Matches(name))time.now=1000;
        return result;
    }
    ssize_t WriteSearch(int fd,const char* p,std::size_t n) override {
        ++search_writes;
        if(restore_error && n==1) {errno=EIO;return -1;}
        // Model the string parameter's setter and its newline-terminated show.
        if(n==1) {assert(p[0]=='\0');return write(fd,"\n",1);}
        assert(p[n-1]!='\n');
        const auto count=write(fd,p,short_search?n-1:n);assert(write(fd,"\n",1)==1);return count;
    }
};
struct Fixture {
    std::string root; Time clock; De25FpgaTestPaths paths; Ops ops;
    int lease=-1; Artifact artifact;
    Fixture():ops(paths,clock) {
        char name[]="/tmp/fes-de25-manager-XXXXXX";auto* made=mkdtemp(name);assert(made);root=made;
        paths={root+"/config",root+"/firmware",root+"/state",root+"/search",root+"/lease",root+"/record",root+"/notes",root+"/lib-firmware",{}};
        for(const auto& p:{paths.config,paths.config+"/device-tree",paths.config+"/device-tree/overlays",paths.firmware,paths.default_firmware})assert(!mkdir(p.c_str(),0700));
        Put(paths.state,"unknown\n");Put(paths.search,"\n");
        Put(paths.notes,"test kernel notes");Sha256 hash;const auto notes=Get(paths.notes);hash.Update(notes.data(),notes.size());
        Put(paths.record,"{\"classification\":\"de25-owner-candidate\",\"kernel\":\"6.12.11-fes-de25-owner\",\"programming_contract\":\"de25-hps-first-retained-v1\",\"kernel_notes_sha256\":\""+Sha256Hex(hash.Final())+"\"}");
        assert(!chmod(paths.record.c_str(),0644));
        lease=open(paths.lease.c_str(),O_RDWR|O_CREAT|O_CLOEXEC,0600);assert(lease>=0);assert(!flock(lease,LOCK_EX|LOCK_NB));
        Put(root+"/input.rbf","immutable test image");PosixArtifactOpener opener;assert(OpenRBFArtifact(root+"/input.rbf",opener,&artifact).ok());
    }
    ~Fixture() {
        // Test-only tree: every name is our private mkdtemp directory.
        std::string cmd="rm -rf -- '"+root+"'";assert(!system(cmd.c_str()));if(lease>=0)close(lease);
    }
    std::unique_ptr<LinuxDe25FpgaManager> Manager() {return std::unique_ptr<LinuxDe25FpgaManager>(new LinuxDe25FpgaManager(clock,lease,paths,ops));}
    NativeResult Program(LinuxDe25FpgaManager& m, ProgrammingProfile p = ProgrammingProfile::de25_hps_first_v1) {return m.Program(artifact,p,100);}
    void Preserved() {assert(Exists(ops.Dir()) && Exists(paths.firmware+"/fes-runtime-core.rbf"));}
};
std::string RecoveryPackage(Fixture& f,bool menu,const std::string& suffix="") {
    const auto packages=f.root+"/packages",tmp=packages+"/incoming";
    assert(!mkdir(packages.c_str(),0700)&&!mkdir(tmp.c_str(),0700));
    const auto payload=Get(f.root+"/input.rbf");Sha256 hash;hash.Update(payload.data(),payload.size());
    auto manifest=Get("tests/fixtures/core-bundle-v2/manifests/valid-basic.toml");
    auto replace=[&](const std::string& a,const std::string& b){const auto p=manifest.find(a);assert(p!=std::string::npos);manifest.replace(p,a.size(),b);};
    replace("fes.pong",menu?"fes.menu":"fes.zx81");replace("de10_nano","de25_nano");replace("5CSEBA6U23I7","A5EB013BB23BE4SCS");
    replace("fes-gp-v1",menu?"de25-linux-menu-v1":"de25-linux-computer-v1");replace("size = 12","size = "+std::to_string(payload.size()));
    replace("e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1",Sha256Hex(hash.Final()));
    replace("fes.simple-game",menu?"fes.application":"fes.computer");replace("0123456789abcdef0123456789abcdef","0123456789abcdeffedcba9876543210");
    const auto first=manifest.find("[[interfaces]]"),last=manifest.find("[build]");assert(first<last);
    std::vector<std::string> ids=menu?std::vector<std::string>{"fes.video.fixed-720p60","fes.video.menu-display"}:
        std::vector<std::string>{"fes.video.fixed-720p60","fes.keyboard.hid","fes.gamepad.ports","fes.audio.pcm-s16-stereo-48k",
            "fes.media.zx81-tape","fes.media.zx81-tape-write","fes.video.session-display","fes.fabric.video.display-plane"};
    ids.push_back("fes.fabric.de25-scanout");std::string interfaces;
    for(const auto& id:ids)interfaces+="[[interfaces]]\nid = \""+id+"\"\nmajor = 1\nminor = "+
        (id=="fes.fabric.de25-scanout"?"1":"0")+"\nrequired = true\n\n";
    manifest.replace(first,last-first,interfaces);Put(tmp+"/manifest.toml",manifest);Put(tmp+"/core.rbf",payload);
    OpenedCorePackage opened;const auto e=OpenCorePackage(tmp,"",&opened);if(!e.ok())std::cerr<<e.message<<std::endl;assert(e.ok());
    const auto path=packages+"/"+opened.package_id+suffix;assert(!rename(tmp.c_str(),path.c_str()));f.paths.package_roots={packages};return path;
}
void RecoverVerifiedAllocatedConfigurationOnlyAfterKernelRetirement() {
    for(bool menu:{false,true}) for(const std::string& suffix:{std::string{},std::string{"-0123456789abcdef0123456789abcdef"}}) {
        Fixture f;RecoveryPackage(f,menu,suffix);auto old=f.Manager();
        assert(f.Program(*old,menu?ProgrammingProfile::de25_linux_menu_v1:ProgrammingProfile::de25_linux_computer_v1).error.ok());old.reset();
        mister_test::de25_menu::Operations owner;owner.retained=owner.allocated=true;owner.state.state=1;LinuxDe25Menu memory(f.clock,owner);
        unsigned guarded=0;
        LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory,
            [&](const CoreDescriptor& d,std::uint64_t){++guarded;assert(d.core.id=="fes.zx81"&&f.ops.removed==0&&owner.reaps==0);return Error{};});
        Put(f.root+"/next.rbf","different admitted incoming image");PosixArtifactOpener opener;
        assert(OpenRBFArtifact(f.root+"/next.rbf",opener,&f.artifact).ok());f.ops.expected_payload="different admitted incoming image";
        const auto r=fresh.Program(f.artifact,ProgrammingProfile::de25_linux_menu_v1,100);if(!r.error.ok())std::cerr<<r.error.message<<std::endl;
        assert(r.error.ok()&&r.mutation_attempted&&!fresh.Poisoned());
        assert(owner.owner_infos==1&&owner.reaps==1&&owner.allocations==0&&owner.published.empty());
        assert(guarded==(menu?0u:1u));assert(f.ops.removed==1&&f.ops.written==2&&f.ops.created==2);f.Preserved();assert(fresh.ReleaseConfiguration(100).error.ok());
    }
    for(int malformed=0;malformed<7;++malformed) {
        Fixture f;const auto package=RecoveryPackage(f,true);auto old=f.Manager();
        assert(f.Program(*old,ProgrammingProfile::de25_linux_menu_v1).error.ok());old.reset();
        const std::string token="0123456789abcdef0123456789abcdef";
        std::string publication=package+"-"+token;
        if(malformed==0)publication.pop_back();
        if(malformed==1)publication+='0';
        if(malformed==2)publication.back()='F';
        if(malformed==3)publication[publication.size()-33]='_';
        if(malformed==4)publication=f.paths.package_roots[0]+"/"+std::string(64,'0')+"-"+token;
        if(malformed==5)publication[publication.size()-34]='A';
        if(malformed==6) {
            const auto hidden=f.paths.package_roots[0]+"/unpublished";
            assert(!rename(package.c_str(),hidden.c_str())&&!symlink(hidden.c_str(),publication.c_str()));
        } else assert(!rename(package.c_str(),publication.c_str()));
        mister_test::de25_menu::Operations owner;owner.retained=owner.allocated=true;owner.state.state=1;
        LinuxDe25Menu memory(f.clock,owner);LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory);
        const auto r=f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1);
        assert(!r.error.ok()&&owner.owner_infos==0&&owner.reaps==0&&f.ops.removed==0&&f.ops.written==1);f.Preserved();
    }
    for(int fault=0;fault<10;++fault) {
        Fixture f;const auto package=RecoveryPackage(f,true);auto old=f.Manager();
        assert(f.Program(*old,ProgrammingProfile::de25_linux_menu_v1).error.ok());old.reset();
        mister_test::de25_menu::Operations owner;owner.retained=owner.allocated=true;owner.state.state=1;
        if(fault==0)f.paths.package_roots.clear();
        if(fault==1)Put(package+"/core.rbf","changed package payload");
        if(fault==2)Put(f.ops.Dir()+"/dtbo","foreign overlay");
        if(fault==3)Put(f.ops.Dir()+"/status","unapplied\n");
        if(fault==4)assert(!chmod((f.paths.firmware+"/fes-runtime-core.rbf").c_str(),0600));
        if(fault==5){const auto p=f.paths.firmware+"/fes-runtime-core.rbf";assert(!unlink(p.c_str())&&!symlink((f.root+"/input.rbf").c_str(),p.c_str()));}
        if(fault==6)owner.wrong_build=true;
        if(fault==7)owner.state.state=4;
        if(fault==8)f.ops.remove_error=true;
        if(fault==9)Put(f.paths.search,"/another/loader\n");
        LinuxDe25Menu memory(f.clock,owner);LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory);
        const auto r=f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1);
        assert(!r.error.ok()&&f.ops.written==1&&f.ops.created==1);f.Preserved();
        assert(owner.reaps==(fault==8?1:0)&&f.ops.removed==(fault==8?1:0));
        const auto reaps=owner.reaps,removed=f.ops.removed;
        assert(!f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1).error.ok());
        assert(owner.reaps==reaps&&f.ops.removed==removed&&f.ops.written==1);
    }
    {Fixture f;RecoveryPackage(f,true);auto old=f.Manager();assert(f.Program(*old).error.ok());old.reset();
     mister_test::de25_menu::Operations owner;LinuxDe25Menu memory(f.clock,owner);
     LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory);
     assert(!f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1).error.ok()&&owner.reaps==0);f.Preserved();}
    for(int fault=0;fault<4;++fault) {
        Fixture f;const auto package=RecoveryPackage(f,true);auto old=f.Manager();
        assert(f.Program(*old,ProgrammingProfile::de25_linux_menu_v1).error.ok());old.reset();
        mister_test::de25_menu::Operations owner;owner.retained=owner.allocated=true;owner.state.state=1;
        owner.on_reap=[&]{
            if(fault==0){const auto path=f.paths.firmware+"/fes-runtime-core.rbf";assert(!chmod(path.c_str(),0600));Put(path,"changed retained firmware");assert(!chmod(path.c_str(),0444));}
            if(fault==1)Put(package+"/core.rbf","changed outgoing package");
            if(fault==2)assert(!mkdir((f.paths.config+"/device-tree/overlays/foreign").c_str(),0700));
            if(fault==3)f.clock.now=1000;
        };
        LinuxDe25Menu memory(f.clock,owner);LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory);
        const auto r=f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1);
        assert(!r.error.ok()&&r.mutation_attempted&&fresh.Poisoned());
        assert(owner.reaps==1&&f.ops.removed==0&&f.ops.written==1);f.Preserved();
        assert(!f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1).error.ok()&&owner.reaps==1&&f.ops.removed==0);
    }
    for(bool missing:{false,true}) {
        Fixture f;RecoveryPackage(f,false);auto old=f.Manager();
        assert(f.Program(*old,ProgrammingProfile::de25_linux_computer_v1).error.ok());old.reset();
        mister_test::de25_menu::Operations owner;LinuxDe25Menu memory(f.clock,owner);
        unsigned calls=0;De25RecoveryGuard guard;
        if(!missing)guard=[&](const CoreDescriptor&,std::uint64_t){++calls;return Error{ErrorCode::save_failed,"pending SAVE remains owned","save"};};
        LinuxDe25FpgaManager fresh(f.clock,f.lease,f.paths,f.ops,&memory,std::move(guard));
        const auto r=f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1);
        assert(!r.error.ok()&&owner.reaps==0&&f.ops.removed==0&&f.ops.written==1&&calls==(missing?0u:1u));f.Preserved();
        assert(!f.Program(fresh,ProgrammingProfile::de25_linux_menu_v1).error.ok()&&calls==(missing?0u:1u));
    }
    std::cout<<"restart abandons only admitted allocated images after independent owner drain; uncertainty is retained\n";
}

void Positive() {
    Fixture f;auto m=f.Manager();assert(f.Program(*m).error.ok());
    assert(f.ops.closed==1 && !m->Poisoned());
    assert(Get(f.paths.search)=="\n" && f.ops.search_writes==2);
    assert(Get(f.paths.firmware+"/fes-runtime-core.rbf")=="immutable test image");
    assert(f.Program(*m).error.ok());assert(f.ops.closed==2 && f.ops.removed==1);
    assert(!m->BootHpsDdrLayout());assert(!m->ReleaseHpsDdrPorts(100).ok());
    m.reset();f.Preserved();auto fresh=f.Manager();auto r=f.Program(*fresh);
    assert(!r.error.ok() && !r.mutation_attempted && f.ops.written==2);
    std::cout<<"success applies on close, reprograms in context, and preserves state at destruction\n";
}
void CleanRelease() {
    for (auto profile : {ProgrammingProfile::de25_hps_first_v1, ProgrammingProfile::de25_linux_menu_v1}) {
        Fixture f;auto m=f.Manager();
        const auto empty=m->ReleaseConfiguration(100);
        assert(empty.error.ok() && !empty.mutation_attempted && !f.ops.removed);
        assert(f.Program(*m,profile).error.ok());
        const auto released=m->ReleaseConfiguration(100);
        assert(released.error.ok() && released.mutation_attempted && !m->Poisoned());
        assert(!Exists(f.ops.Dir()) && !Exists(f.paths.firmware+"/fes-runtime-core.rbf"));
        assert(f.ops.removed==1 && Get(f.paths.search)=="\n");
        m.reset();auto fresh=f.Manager();assert(f.Program(*fresh,profile).error.ok());
        assert(f.ops.created==2 && f.ops.written==2);
    }
    std::cout<<"explicit verified release leaves no inherited configuration for a fresh manager\n";
}
void ReleaseRefusals() {
    for (int fault=0;fault<8;++fault) {
        Fixture f;auto m=f.Manager();assert(f.Program(*m).error.ok());
        if(fault==0) {m.reset();m=f.Manager();}
        if(fault==1) {
            const auto path=f.paths.firmware+"/fes-runtime-core.rbf";
            assert(!chmod(path.c_str(),0600));Put(path,"different test image");assert(!chmod(path.c_str(),0444));
        }
        if(fault==2) Put(f.ops.Dir()+"/dtbo","foreign overlay");
        if(fault==3) Put(f.ops.Dir()+"/status","unapplied\n");
        if(fault==4) f.clock.now=101;
        if(fault==5) Put(f.paths.search,"/another/loader\n");
        if(fault==6) {
            const auto path=f.paths.firmware+"/fes-runtime-core.rbf";
            assert(!unlink(path.c_str()));assert(!symlink((f.root+"/input.rbf").c_str(),path.c_str()));
        }
        if(fault==7) assert(!chmod(f.paths.firmware.c_str(),0755));
        const auto r=m->ReleaseConfiguration(100);
        assert(!r.error.ok() && !r.mutation_attempted && !f.ops.removed);f.Preserved();
    }
    {
        Fixture f;auto m=f.Manager();assert(f.Program(*m).error.ok());f.ops.remove_error=true;
        const auto r=m->ReleaseConfiguration(100);
        assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());f.Preserved();
        assert(!m->ReleaseConfiguration(100).error.ok() && f.ops.removed==1);
    }
    {
        Fixture f;auto m=f.Manager();f.ops.unapplied=true;
        assert(!f.Program(*m).error.ok() && m->Poisoned());
        const auto r=m->ReleaseConfiguration(100);
        assert(!r.error.ok() && !r.mutation_attempted && !f.ops.removed);f.Preserved();
    }
    std::cout<<"release refuses inheritance, changed bytes/overlay/search, expiry and uncertain removal\n";
}
std::map<std::string, std::string> OverlayProperties(const std::vector<unsigned char>& blob) {
    auto word = [&](std::size_t offset) {
        assert(offset + 4 <= blob.size());
        return (std::uint32_t(blob[offset]) << 24) | (std::uint32_t(blob[offset+1]) << 16) |
               (std::uint32_t(blob[offset+2]) << 8) | blob[offset+3];
    };
    assert(word(0) == 0xd00dfeed && word(4) == blob.size());
    const auto strings = word(12), end = word(8) + word(36);
    assert(strings + word(32) == blob.size() && end == strings);
    auto text = [&](std::size_t offset, std::size_t limit) {
        std::string result;
        while (offset < limit && blob[offset]) result.push_back(char(blob[offset++]));
        assert(offset < limit && !blob[offset]);return result;
    };
    std::vector<std::string> stack;
    std::map<std::string, std::string> properties;
    for (std::size_t pos = word(8); pos < end;) {
        const auto token = word(pos);pos += 4;
        if (token == 1) {
            const auto name = text(pos, end);stack.push_back(name);
            pos = (pos + name.size() + 1 + 3) & ~std::size_t(3);
        } else if (token == 2) {
            assert(!stack.empty());stack.pop_back();
        } else if (token == 3) {
            const auto length = word(pos), name = word(pos+4);pos += 8;
            assert(name < word(32) && pos + length <= end);
            std::string path;
            for (const auto& node : stack) if (!node.empty()) path += "/" + node;
            const auto key = path + ":" + text(strings + name, blob.size());
            assert(properties.emplace(key, std::string(blob.begin()+pos, blob.begin()+pos+length)).second);
            pos = (pos + length + 3) & ~std::size_t(3);
        } else {
            assert(token == 9 && stack.empty() && pos == end);return properties;
        }
    }
    assert(false);return {};
}
struct StaticFixture : Fixture {
    mister_test::de25_pr::Endpoint endpoint;
    Artifact persona;
    De25StaticSelection selection;
    unsigned reads_at_entry = 0;
    std::function<void()> after_base, after_persona;
    StaticFixture() {
        using namespace mister_test::de25_pr;
        auto record=Get(paths.record);assert(!record.empty() && record.back()=='}');record.pop_back();
        Put(paths.record,record+",\"static_expansion_contract\":\"de25-cold-persona-retained-v1\"}");
        Put(root+"/persona.rbf",ops.expected_persona_payload);
        PosixArtifactOpener opener;assert(OpenRBFArtifact(root+"/persona.rbf",opener,&persona).ok());
        assert(HashOpenedArtifact(artifact,&selection.base_sha256).ok());
        assert(HashOpenedArtifact(persona,&selection.persona_sha256).ok());
        selection.control_build_id=kBuild;
        selection.persona_id=mister::native::generated::FesDe25PrControlPersonaA;
        ops.base_applied=[&] {endpoint=Endpoint{};if(after_base)after_base();};
        ops.persona_applied=[&] {endpoint.loaded_persona=selection.persona_id;if(after_persona)after_persona();};
        ops.before_write=[&](const std::string& name) {
            assert(Get(paths.firmware+"/fes-runtime-persona.rbf")==ops.expected_persona_payload);
            if(name=="fes-runtime") assert(endpoint.reads==reads_at_entry);
            else {
                assert(endpoint.registers[mister::native::generated::FesDe25PrControlStatusOffset]==kReadyFrozen);
                assert(endpoint.Commands(mister::native::generated::FesDe25PrControlCommandFreeze)==1);
            }
        };
    }
    NativeResult Load(LinuxDe25FpgaManager& manager) {
        reads_at_entry=endpoint.reads;
        return manager.ProgramStatic(artifact,persona,selection,endpoint,500);
    }
    void ImagesRetained() {
        Preserved();assert(Get(paths.firmware+"/fes-runtime-persona.rbf")==ops.expected_persona_payload);
    }
};
struct StartupOperations final : LinuxMmioTestOperations {
    StaticFixture& f;
    int fault=0, opened=0, mapped=0, unmapped=0, closed=0;
    explicit StartupOperations(StaticFixture& fixture):f(fixture) { f.ops.startup_registers=this; }
    std::size_t PageSize() const override { return 4096; }
    int Open() override {
        ++opened;f.ops.events.push_back("mmio:open");
        assert(Get(f.ops.Dir()+"/status")=="applied\n" && Get(f.paths.state)=="operating\n");
        assert(Get(f.paths.search)=="\n");
        return fault==1?-1:7;
    }
    int Close(int fd) override {
        assert(fd==7);++closed;f.ops.events.push_back("mmio:close");
        if(fault==12)f.clock.now=1000;
        return fault==11?-1:0;
    }
    int Map(int fd,std::uint64_t page,std::size_t span,void** address) override {
        assert(fd==7 && page==0x20010000u && span==4096);++mapped;
        *address=this;return fault==2?-1:0;
    }
    int Unmap(void* address,std::size_t span) override {
        assert(address==this && span==4096);++unmapped;f.ops.events.push_back("mmio:unmap");
        return fault==10?-1:0;
    }
    int Read32(void* address,std::size_t offset,std::uint32_t* value) override {
        assert(address==this && offset>=0x700 && offset<=0x73c);
        return f.endpoint.Read32(offset-0x700,value).ok()?0:-1;
    }
    int Write32(void* address,std::size_t offset,std::uint32_t value) override {
        assert(address==this && (offset==0x70c || offset==0x710));
        return f.endpoint.Write32(offset-0x700,value).ok()?0:-1;
    }
    NativeResult Load(LinuxDe25FpgaManager& manager) {
        f.reads_at_entry=f.endpoint.reads;
        return manager.ProgramStatic(f.artifact,f.persona,f.selection,500);
    }
};
void OwnedStaticMappingHasOneFullBaseLifetime() {
    using namespace mister::native::generated;
    StaticFixture f;StartupOperations registers(f);auto manager=f.Manager();
    for(auto persona:{FesDe25PrControlPersonaA,FesDe25PrControlPersonaB,FesDe25PrControlPersonaA}) {
        f.selection.persona_id=persona;f.ops.events.clear();
        assert(registers.Load(*manager).error.ok() && !manager->Poisoned());
        const auto& events=f.ops.events;
        auto base=std::find(events.begin(),events.end(),"close:fes-runtime");
        auto opened=std::find(events.begin(),events.end(),"mmio:open");
        auto child=std::find(events.begin(),events.end(),"close:fes-runtime-persona");
        auto unmapped=std::find(events.begin(),events.end(),"mmio:unmap");
        auto closed=std::find(events.begin(),events.end(),"mmio:close");
        assert(base<opened && opened<child && child<unmapped && unmapped<closed && closed!=events.end());
        assert(f.endpoint.registers[FesDe25PrControlPersonaIdOffset]==persona);
        assert(registers.opened==registers.closed && registers.mapped==registers.unmapped);
    }
    assert(registers.opened==3 && registers.closed==3);
    manager.reset();assert(registers.closed==3); // No destructor unload or cleanup retry.
    std::cout<<"owned static CSR mappings open after each fresh base and close before success, A/B/A\n";
}
void OwnedStaticMappingFaultsRetainAndRefuseReplay() {
    using namespace mister::native::generated;
    for(int fault=1;fault<=12;++fault) {
        StaticFixture f;StartupOperations registers(f);registers.fault=fault;auto manager=f.Manager();
        f.after_base=[&] {
            if(fault==3)f.endpoint.fail_read=FesDe25PrControlIdOffset;
            if(fault==4)f.endpoint.fail_write=FesDe25PrControlArgumentOffset;
            f.endpoint.on_read=[&](std::uint32_t) {
                if(f.endpoint.reads!=1)return;
                if(fault==5)Put(f.paths.state,"write error\n");
                if(fault==6) {assert(!unlink(f.paths.lease.c_str()));int fd=open(f.paths.lease.c_str(),O_CREAT|O_RDWR,0600);assert(fd>=0);close(fd);}
                if(fault==7)Put(f.ops.Dir()+"/dtbo","modified base");
                if(fault==8)assert(!mkdir((f.paths.config+"/device-tree/overlays/foreign").c_str(),0700));
                if(fault==9)f.clock.now=500;
            };
        };
        const auto result=registers.Load(*manager);
        assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        assert(registers.opened==1 && registers.closed==(fault==1?0:1));
        assert(registers.unmapped==(fault<=2?0:1));
        if(fault>=5 && fault<=9)assert(f.endpoint.reads==1 && f.endpoint.writes.empty());
        f.ImagesRetained();const auto events=f.ops.events;const auto opened=registers.opened;
        assert(!registers.Load(*manager).error.ok());
        assert(!manager->ReleaseConfiguration(500).error.ok());
        assert(registers.opened==opened && f.ops.events==events);
        manager.reset();assert(registers.closed==(fault==1?0:1));
    }
    // Missing fixture transport must never open a host's real /dev/mem.
    StaticFixture f;auto manager=f.Manager();
    const auto result=manager->ProgramStatic(f.artifact,f.persona,f.selection,500);
    assert(!result.error.ok() && manager->Poisoned() && !f.endpoint.reads);
    std::cout<<"owned static map/open/read/write/lease/epoch/deadline/unmap/close faults retain images and refuse replay\n";
}
void OwnedStaticMappingRefusesBeforeOpenAndAfterPersonaChange() {
    using namespace mister::native::generated;
    for(int fault=0;fault<4;++fault) {
        StaticFixture f;StartupOperations registers(f);auto manager=f.Manager();
        if(fault==0)f.selection.base_sha256=std::string(64,'0');
        if(fault==1)Put(f.paths.notes,"different kernel");
        if(fault==2)f.ops.error_close=true;
        if(fault==3)f.ops.unapplied=true;
        assert(!registers.Load(*manager).error.ok());
        assert(!registers.opened && !registers.mapped && !registers.closed && !f.endpoint.reads);
    }
    for(bool child:{false,true}) {
        StaticFixture f;StartupOperations registers(f);auto manager=f.Manager();
        f.after_persona=[&] {Put(f.ops.Dir(child?"fes-runtime-persona":"fes-runtime")+"/dtbo","changed overlay");};
        assert(!registers.Load(*manager).error.ok() && manager->Poisoned());
        assert(f.endpoint.Commands(FesDe25PrControlCommandFreeze)==1);
        assert(!f.endpoint.Commands(FesDe25PrControlCommandResume));
        assert(registers.opened==1 && registers.unmapped==1 && registers.closed==1);
        f.ImagesRetained();
    }
    std::cout<<"owned static transport stays closed before verified base and refuses Resume after base/persona overlay change\n";
}
void StaticProductionDispatchGate() {
    for(bool matched:{false,true}) {
        StaticFixture f;StartupOperations registers(f);
        if(!matched) {
            auto record=Get(f.paths.record);auto pos=record.find(",\"static_expansion_contract\"");
            assert(pos!=std::string::npos);Put(f.paths.record,record.substr(0,pos)+"}");
        }
        auto manager=f.Manager();FpgaManager& production=*manager;
        assert(!production.SupportsStaticComposition());
        assert(manager->CheckPlatform(500).ok());
        assert(production.SupportsStaticComposition()==(De25StaticEnabled()&&matched));
        CoreStaticComposition selected;
        selected.base_sha256=f.selection.base_sha256;selected.persona_sha256=f.selection.persona_sha256;
        selected.control_build_id=f.selection.control_build_id;selected.hardware_id=f.selection.persona_id;
        auto result=production.ProgramStatic(f.artifact,f.persona,selected,500);
        assert(result.error.ok()==(De25StaticEnabled()&&matched));
        if(!De25StaticEnabled()||!matched)
            assert(!result.mutation_attempted&&!registers.opened&&!f.ops.created&&!f.endpoint.reads);
        else assert(registers.opened==1&&registers.unmapped==1&&registers.closed==1);
    }
    std::cout<<"production static dispatch requires build flag and matched cold-start kernel before effects\n";
}
void StaticStartupAndFullReplacement() {
    using namespace mister::native::generated;
    using namespace mister_test::de25_pr;
    StaticFixture f;auto manager=f.Manager();
    for(auto persona:{FesDe25PrControlPersonaA,FesDe25PrControlPersonaB,FesDe25PrControlPersonaA}) {
        f.selection.persona_id=persona;f.ops.events.clear();
        auto result=f.Load(*manager);assert(result.error.ok() && result.mutation_attempted && !manager->Poisoned());
        assert(f.endpoint.registers[FesDe25PrControlStatusOffset]==kReadyRunning);
        assert(f.endpoint.registers[FesDe25PrControlPersonaIdOffset]==persona);
        assert(f.endpoint.Commands(FesDe25PrControlCommandFreeze)==1 && f.endpoint.Commands(FesDe25PrControlCommandResume)==1);
        assert(f.endpoint.registers[FesDe25PrControlCompletedSeqOffset]==2);
        f.ImagesRetained();assert(Exists(f.ops.Dir("fes-runtime-persona")) && Get(f.paths.search)=="\n");
        const auto& events=f.ops.events;
        const auto base=std::find(events.begin(),events.end(),"write:fes-runtime");
        const auto child=std::find(events.begin(),events.end(),"write:fes-runtime-persona");
        assert(base!=events.end() && child!=events.end() && base<child);
        if(persona!=FesDe25PrControlPersonaA || events.front().find("remove:")==0) {
            assert(events[0]=="remove:fes-runtime-persona" && events[1]=="remove:fes-runtime");
        }
    }
    f.ops.before_write={};f.ops.events.clear();
    assert(f.Program(*manager,ProgrammingProfile::de25_linux_menu_v1).error.ok());
    assert(f.ops.events[0]=="remove:fes-runtime-persona" && f.ops.events[1]=="remove:fes-runtime");
    assert(!Exists(f.ops.Dir("fes-runtime-persona")) && !Exists(f.paths.firmware+"/fes-runtime-persona.rbf"));
    assert(manager->ReleaseConfiguration(500).error.ok());
    std::cout<<"static A/B/A launches load a fresh full base each time; MENU replacement removes child before base\n";
}
void StaticRefusalsBeforeHardware() {
    for(int fault=0;fault<15;++fault) {
        StaticFixture f;auto manager=f.Manager();
        if(fault==0)f.selection.base_sha256=std::string(64,'0');
        if(fault==1)f.selection.persona_sha256=std::string(64,'0');
        if(fault==2)f.selection.control_build_id="malformed";
        if(fault==3)f.selection.persona_id=0;
        if(fault==4)Put(f.paths.firmware+"/fes-runtime-persona.rbf","foreign");
        if(fault==5)assert(!mkdir(f.ops.Dir("fes-runtime-persona").c_str(),0700));
        if(fault==6)Put(f.paths.default_firmware+"/fes-runtime-persona.rbf","fallback");
        if(fault==7)f.clock.now=500;
        if(fault==8)Put(f.paths.notes,"different kernel");
        if(fault==9) {unlink(f.paths.lease.c_str());int fd=open(f.paths.lease.c_str(),O_CREAT|O_RDWR,0600);assert(fd>=0);close(fd);}
        if(fault==10)f.selection.control_build_id=std::string(32,'0');
        if(fault>=11) {
            auto record=Get(f.paths.record);const auto pos=record.find(",\"static_expansion_contract\"");assert(pos!=std::string::npos);
            record.resize(pos);
            if(fault==12)record+=",\"static_expansion_contract\":\"unknown\"";
            if(fault==13)record+=",\"static_expansion_contract\":true";
            if(fault==14)record+=",\"static_expansion_contract\":\"de25-cold-persona-retained-v1\",\"static_expansion_contract\":\"de25-cold-persona-retained-v1\"";
            Put(f.paths.record,record+"}");
        }
        const auto result=f.Load(*manager);
        assert(!result.error.ok() && !result.mutation_attempted && !f.ops.created && !f.ops.removed && !f.ops.written && f.endpoint.reads==0);
    }
    std::cout<<"static payload/selection/fallback/ownership/kernel/deadline checks refuse before programming\n";
}
void StaticColdIdentityFailures() {
    using namespace mister::native::generated;
    using namespace mister_test::de25_pr;
    for(int fault=0;fault<5;++fault) {
        StaticFixture f;auto manager=f.Manager();
        f.after_base=[&] {
            if(fault==0)f.endpoint.registers[FesDe25PrControlBuildId0Offset]^=1;
            if(fault==1)f.endpoint.registers[FesDe25PrControlStatusOffset]=kReadyRunning;
            if(fault==2)f.endpoint.registers[FesDe25PrControlCompletedSeqOffset]=2;
            if(fault==3)f.endpoint.registers[FesDe25PrControlPersonaIdOffset]=FesDe25PrControlPersonaA;
            if(fault==4)f.endpoint.fail_read=FesDe25PrControlIdOffset;
        };
        auto result=f.Load(*manager);assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        assert(f.ops.written==1 && !f.endpoint.Commands(FesDe25PrControlCommandResume));f.ImagesRetained();
        const auto writes=f.ops.written;assert(!f.Load(*manager).error.ok() && f.ops.written==writes);
        assert(!manager->ReleaseConfiguration(500).error.ok() && f.ops.removed==0);
    }
    std::cout<<"non-cold or wrong static control identity poisons context before partial submission\n";
}
void StaticPersonaFailuresRetainBothImages() {
    using namespace mister::native::generated;
    for(int fault=0;fault<8;++fault) {
        StaticFixture f;auto manager=f.Manager();f.ops.fault_name="fes-runtime-persona";
        if(fault==0)f.ops.short_write=true;
        if(fault==1)f.ops.error_close=true;
        if(fault==2)f.ops.late_close=true;
        if(fault==3)f.ops.unapplied=true;
        if(fault==4)f.ops.create_error=true;
        if(fault==5)f.ops.open_error=true;
        if(fault==6)f.after_persona=[&]{f.ops.restore_error=true;};
        if(fault==7)f.after_persona=[&]{f.endpoint.loaded_persona^=1;};
        auto result=f.Load(*manager);assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        f.ImagesRetained();assert(f.ops.removed==0);
        assert(f.endpoint.Commands(FesDe25PrControlCommandFreeze)==1);
        assert(f.endpoint.Commands(FesDe25PrControlCommandResume)==(fault==7?1U:0U));
        const auto writes=f.ops.written;const auto commands=f.endpoint.writes.size();
        assert(!f.Load(*manager).error.ok() && f.ops.written==writes && f.endpoint.writes.size()==commands);
        assert(!f.Program(*manager).error.ok() && f.ops.written==writes);
    }
    std::cout<<"partial create/write/close/status/deadline/search/identity failures retain both images, never retry or resume after ambiguity\n";
}
void StaticCopyAndLookupRacesNeverSubmitWrongBytes() {
    using namespace mister::native::generated;
    for(bool persona:{false,true}) {
        StaticFixture f;auto manager=f.Manager();f.ops.before_write={};
        assert(f.Program(*manager,ProgrammingProfile::de25_linux_menu_v1).error.ok());
        const auto written=f.ops.written;
        f.ops.before_remove=[&](const std::string&) {
            const auto path=f.root+(persona?"/persona.rbf":"/input.rbf");
            auto data=Get(path);data[0]^=1;Put(path,data);
        };
        auto result=f.Load(*manager);
        assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        assert(f.ops.written==written && f.endpoint.writes.empty());
        assert(Exists(f.paths.firmware+"/fes-runtime-persona.rbf"));
    }
    for(bool persona:{false,true}) {
        StaticFixture f;auto manager=f.Manager();
        f.after_base=[&] {
            const auto path=f.paths.firmware+(persona?"/fes-runtime-persona.rbf":"/fes-runtime-core.rbf");
            assert(!chmod(path.c_str(),0600));auto data=Get(path);data[0]^=1;Put(path,data);assert(!chmod(path.c_str(),0444));
        };
        auto result=f.Load(*manager);assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        assert(f.ops.written==1 && f.endpoint.Commands(FesDe25PrControlCommandResume)==0);
        assert(Exists(f.ops.Dir()) && Exists(f.paths.firmware+"/fes-runtime-core.rbf") && Exists(f.paths.firmware+"/fes-runtime-persona.rbf"));
    }
    std::cout<<"retained-input copy races and staged base/persona changes are rejected before the next image submission\n";
}
void StaticReleaseChecksBothIdentitiesBeforeEffects() {
    for(int fault=0;fault<7;++fault) {
        StaticFixture f;auto manager=f.Manager();assert(f.Load(*manager).error.ok());
        if(fault<2) {
            const auto file=f.paths.firmware+(fault==0?"/fes-runtime-core.rbf":"/fes-runtime-persona.rbf");
            assert(!chmod(file.c_str(),0600));auto content=Get(file);content[0]^=1;Put(file,content);assert(!chmod(file.c_str(),0444));
        }
        if(fault==2)Put(f.ops.Dir("fes-runtime-persona")+"/dtbo","changed child");
        if(fault==3)Put(f.ops.Dir()+"/status","unapplied\n");
        if(fault==4)assert(!mkdir((f.paths.config+"/device-tree/overlays/foreign").c_str(),0700));
        if(fault==5)f.clock.now=500;
        if(fault==6)assert(!chmod((f.paths.firmware+"/fes-runtime-persona.rbf").c_str(),0600));
        const auto removed=f.ops.removed;auto result=manager->ReleaseConfiguration(500);
        assert(!result.error.ok() && !result.mutation_attempted && f.ops.removed==removed);
    }
    for(const char* blocked:{"fes-runtime-persona","fes-runtime"}) {
        StaticFixture f;auto manager=f.Manager();assert(f.Load(*manager).error.ok());
        f.ops.fault_name=blocked;f.ops.remove_error=true;
        auto result=manager->ReleaseConfiguration(500);assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        f.ImagesRetained();assert(Exists(f.ops.Dir()));
        assert(Exists(f.ops.Dir("fes-runtime-persona"))==(std::string(blocked)=="fes-runtime-persona"));
        const auto removed=f.ops.removed;assert(!manager->ReleaseConfiguration(500).error.ok() && f.ops.removed==removed);
    }
    {
        StaticFixture f;auto manager=f.Manager();assert(f.Load(*manager).error.ok());
        f.ops.events.clear();f.ops.fault_name="fes-runtime-persona";f.ops.late_remove=true;
        auto result=manager->ReleaseConfiguration(500);
        assert(!result.error.ok() && result.mutation_attempted && manager->Poisoned());
        assert(f.ops.events==std::vector<std::string>{"remove:fes-runtime-persona"});
        assert(!Exists(f.ops.Dir("fes-runtime-persona")));f.ImagesRetained();
    }
    {
        StaticFixture f;auto manager=f.Manager();assert(f.Load(*manager).error.ok());manager.reset();
        auto next=f.Manager();const auto removed=f.ops.removed;const auto written=f.ops.written;
        assert(!f.Load(*next).error.ok() && f.ops.removed==removed && f.ops.written==written);
        f.ImagesRetained();assert(Exists(f.ops.Dir("fes-runtime-persona")));
    }
    std::cout<<"static release verifies both payloads/overlays before effects; removal failure and inheritance stay closed\n";
}
void StaticStartupProfiles() {
    auto expected = OverlayProperties(De25ProgrammingOverlay());
    const std::string root = "/fragment@0/__overlay__";
    const auto terminated = [](const char* text) { return std::string(text) + std::string(1, '\0'); };
    expected[root + "/scanout:compatible"] = terminated("fes,de25-linux-static-expansion");
    expected[root + "/fes-expansion:compatible"] = terminated("fpga-region");
    // The base introduces a region without programming it or admitting the
    // ordinary scanout profile before its initial persona is complete.
    assert(OverlayProperties(De25ProgrammingOverlay(De25OverlayProfile::static_expansion_base)) == expected);
    expected = OverlayProperties(De25ProgrammingOverlay());
    expected["/fragment@0:target-path"] = terminated("/fpga-region/fes-expansion");
    expected[root + ":firmware-name"] = terminated("fes-runtime-persona.rbf");
    expected[root + ":partial-fpga-config"] = "";
    assert(OverlayProperties(De25ProgrammingOverlay(De25OverlayProfile::static_expansion_persona)) == expected);
    std::cout << "static startup base and initial partial persona have distinct exact closed FDT shapes\n";
}
void AllocatedScanoutProfile() {
    const auto basic = De25ProgrammingOverlay();
    const auto scanout = De25ProgrammingOverlay(De25OverlayProfile::allocated_scanout);
    const std::string marker = "fes,de25-linux-dma-scanout";
    auto bytes = [](const std::vector<unsigned char>& v) { return std::string(v.begin(), v.end()); };
    assert(bytes(basic).find(marker) == std::string::npos);
    assert(bytes(scanout).find(marker) != std::string::npos);
    auto ordinary = OverlayProperties(basic), allocated = OverlayProperties(scanout);
    assert(ordinary.size() == 3);
    assert(ordinary.at("/fragment@0:target-path") == std::string("/fpga-region\0", 13));
    assert(ordinary.at("/fragment@0/__overlay__:firmware-name") == std::string("fes-runtime-core.rbf\0", 21));
    const std::string profile_key = "/fragment@0/__overlay__/scanout:compatible";
    assert(allocated.at(profile_key) == marker + std::string(1, '\0'));
    allocated.erase(profile_key);assert(allocated == ordinary);
    assert(De25ProgrammingOverlay(static_cast<De25OverlayProfile>(42)).empty());
    for(auto profile:{ProgrammingProfile::de25_linux_menu_v1,ProgrammingProfile::de25_linux_computer_v1}) {
        Fixture f;auto m=f.Manager();assert(f.Program(*m,profile).error.ok());
        assert(Get(f.ops.Dir()+"/dtbo")==bytes(scanout));
    }
    {
        Fixture f;auto m = f.Manager();
        assert(f.Program(*m,ProgrammingProfile::de25_linux_menu_v1).error.ok());
        assert(Get(f.ops.Dir()+"/dtbo") == bytes(scanout));
        assert(f.Program(*m,ProgrammingProfile::de25_linux_menu_v1).error.ok() && f.ops.removed == 1);
        // An inherited marker is never adopted by a fresh caller.
        auto fresh = f.Manager();
        auto r = f.Program(*fresh,ProgrammingProfile::de25_linux_menu_v1);assert(!r.error.ok() && !r.mutation_attempted);
        // The outgoing bytes, not the incoming profile, authorize replacement.
        assert(f.Program(*m).error.ok());
        assert(Get(f.ops.Dir()+"/dtbo") == bytes(basic));
        assert(f.Program(*m,ProgrammingProfile::de25_linux_menu_v1).error.ok());
        assert(Get(f.ops.Dir()+"/dtbo") == bytes(scanout) && f.ops.removed == 3);
        // A successful context must retain its exact selected marker too.
        Put(f.ops.Dir()+"/dtbo", bytes(basic));
        r = f.Program(*m,ProgrammingProfile::de25_linux_menu_v1);assert(!r.error.ok() && !r.mutation_attempted && f.ops.removed == 3);
    }
    {
        Fixture f;auto m = f.Manager();
        auto r = f.Program(*m,static_cast<ProgrammingProfile>(42));assert(!r.error.ok() && !r.mutation_attempted);
        assert(!f.ops.created && !Exists(f.paths.firmware+"/fes-runtime-core.rbf"));
    }
    {
        Fixture f;auto m = f.Manager();
        f.ops.error_close = true;auto r = f.Program(*m,ProgrammingProfile::de25_linux_menu_v1);
        assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());
        f.Preserved();assert(!f.ops.removed && f.ops.written == 1);
        r = f.Program(*m,ProgrammingProfile::de25_linux_menu_v1);assert(!r.error.ok() && !r.mutation_attempted && f.ops.written == 1);
    }
    std::cout<<"profile-selected marker, MENU-computer-MENU switching, exact outgoing identity and uncertainty retention pass\n";
}
void Uncertain() {
    for(int mode=0;mode<4;++mode) {
        Fixture f;auto m=f.Manager();f.ops.short_write=mode==0;f.ops.error_close=mode==1;f.ops.late_close=mode==2;f.ops.unapplied=mode==3;
        auto r=f.Program(*m);assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());
        assert(f.ops.written==1 && f.ops.closed==1 && f.ops.removed==0);f.Preserved();
        assert(Get(f.paths.search)==f.paths.firmware+"\n");
        f.clock.now=0;r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && f.ops.written==1);
        m.reset();auto fresh=f.Manager();r=f.Program(*fresh);assert(!r.error.ok() && !r.mutation_attempted && f.ops.removed==0);
    }
    std::cout<<"partial/failed/late close and unapplied status retain state and refuse replay\n";
}
void BeforeMutation() {
    for(int mode=0;mode<11;++mode) {
        Fixture f;auto m=f.Manager();
        if(mode==0)f.clock.now=100;
        if(mode==1)Put(f.paths.search,"/wrong/search\n");
        if(mode==2)assert(!mkdir((f.paths.config+"/device-tree/overlays/other").c_str(),0700));
        if(mode==3)Put(f.paths.state,"write error\n");
        if(mode==4)assert(!symlink((f.root+"/input.rbf").c_str(),(f.paths.firmware+"/fes-runtime-core.rbf").c_str()));
        if(mode==5)f.ops.create_error=true;
        if(mode==6)f.ops.open_error=true;
        if(mode==7)Put(f.paths.record,"{}");
        if(mode==8)Put(f.paths.notes,"different running kernel");
        if(mode==9)assert(!chmod(f.paths.record.c_str(),0666));
        if(mode==10)assert(!chmod(f.paths.firmware.c_str(),0755));
        auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted);
        assert(f.ops.written==0 && f.ops.closed==0);
        if(mode==5 || mode==6) {
            assert(!Exists(f.paths.firmware+"/fes-runtime-core.rbf") && !Exists(f.ops.Dir()));
            assert(Get(f.paths.search)=="\n");
        }
        if(mode==4)assert(Get(f.root+"/input.rbf")=="immutable test image");
    }
    std::cout<<"preflight/staging failures make no programming call and clean only new resources\n";
}
void FirmwareSearchLifecycle() {
    {
        Fixture f;auto m=f.Manager();f.ops.short_search=true;
        auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && !m->Poisoned());
        assert(!f.ops.created && !f.ops.written && Get(f.paths.search)=="\n");
        assert(!Exists(f.paths.firmware+"/fes-runtime-core.rbf"));
    }
    for(int mode=0;mode<4;++mode) {
        Fixture f;auto m=f.Manager();
        f.ops.restore_error=mode<2;f.ops.create_error=mode==0;f.ops.changed_search=mode==2;f.ops.search_newline=mode==3;
        auto r=f.Program(*m);assert(!r.error.ok() && r.mutation_attempted==(mode!=0) && m->Poisoned());
        assert(Exists(f.paths.firmware+"/fes-runtime-core.rbf"));
        if(mode==2)assert(Get(f.paths.search)=="/another/loader\n");
        if(mode==3)assert(Get(f.paths.search)==f.paths.firmware+"\n\n");
        const int writes=f.ops.written; m.reset();
        auto fresh=f.Manager();r=f.Program(*fresh);assert(!r.error.ok() && !r.mutation_attempted && f.ops.written==writes);
    }
    {
        Fixture f;auto m=f.Manager();
        assert(!rmdir(f.paths.firmware.c_str()));assert(!symlink(f.root.c_str(),f.paths.firmware.c_str()));
        auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && !f.ops.search_writes);
    }
    std::cout<<"private writable staging, exclusive search ownership, restoration and retained uncertain lookup pass\n";
}
void FirmwareFallbacks() {
    for(const auto& prefix : {"/", "/updates/", "/6.12.11-fes-de25-owner/", "/updates/6.12.11-fes-de25-owner/"}) {
        Fixture f;auto m=f.Manager();const std::string directory=f.paths.default_firmware+prefix;
        const std::string command="mkdir -p '"+directory+"'";assert(!system(command.c_str()));
        const std::string stale=directory+"fes-runtime-core.rbf";Put(stale,"old image");
        auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted);
        assert(!f.ops.created && !f.ops.written && !f.ops.search_writes && Get(stale)=="old image");
    }
    {
        Fixture f;auto m=f.Manager();assert(!chmod(f.paths.default_firmware.c_str(),0555));
        assert(f.Program(*m).error.ok());
        assert(!Exists(f.paths.default_firmware+"/fes-runtime-core.rbf"));
    }
    std::cout<<"default search fallbacks refuse stale images; immutable firmware directory remains untouched\n";
}
void ExistingFailure() {
    Fixture f;auto m=f.Manager();assert(f.Program(*m).error.ok());
    f.ops.remove_error=true;auto r=f.Program(*m);
    assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());f.Preserved();
    assert(f.ops.written==1 && f.ops.closed==1);
    std::cout<<"failed removal poisons without another image submission\n";
}
void ProfilesAndLease() {
    Fixture f;auto m=f.Manager();
    for(auto p:{ProgrammingProfile::fes_gp_v1,ProgrammingProfile::development_contained_v1}) {
        auto r=m->Program(f.artifact,p,100);assert(!r.error.ok() && !r.mutation_attempted);
    }
    assert(!f.ops.created);
    unlink(f.paths.lease.c_str());int replacement=open(f.paths.lease.c_str(),O_CREAT|O_RDWR,0600);assert(replacement>=0);close(replacement);
    auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && !f.ops.created);
    std::cout<<"foreign profiles and replaced lease inode refuse before filesystem mutation\n";
}
struct WallClock final : Clock {
    std::uint64_t NowMs() const override {
        return std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now().time_since_epoch()).count();
    }
};
struct ThreadedOps : Ops {
    using Ops::Ops;
    bool ThreadedSubmission() const override { return true; }
};
struct BlockedOps final : ThreadedOps {
    bool block_write;
    std::promise<void> entered, release, close_returned;
    std::shared_future<void> gate;
    BlockedOps(De25FpgaTestPaths& paths,Time& time,bool writing)
        : ThreadedOps(paths,time),block_write(writing),gate(release.get_future().share()) {}
    ssize_t Write(int fd,const unsigned char* bytes,std::size_t size) override {
        if (block_write) { entered.set_value(); gate.wait(); }
        return Ops::Write(fd,bytes,size);
    }
    int Close(int fd) override {
        if (!block_write) { entered.set_value(); gate.wait(); }
        const int result=Ops::Close(fd);close_returned.set_value();return result;
    }
};
void ThreadedSubmissionCompletion() {
    for (unsigned fault=0;fault<4;++fault) {
        Fixture f;WallClock clock;ThreadedOps ops(f.paths,f.clock);
        ops.short_write=fault==1;ops.error_close=fault==2;
        LinuxDe25FpgaManager manager(clock,f.lease,f.paths,ops);
        const auto deadline=fault==3?std::numeric_limits<std::uint64_t>::max():clock.NowMs()+2000;
        const auto result=manager.Program(f.artifact,ProgrammingProfile::de25_hps_first_v1,deadline);
        assert(result.mutation_attempted && ops.written==1 && ops.closed==1);
        const bool failing=fault==1 || fault==2;
        assert(result.error.ok()==!failing);
        assert(manager.Poisoned()==failing);
        if (failing) {
            f.Preserved();
            const auto again=manager.Program(f.artifact,ProgrammingProfile::de25_hps_first_v1,
                                              clock.NowMs()+2000);
            assert(!again.error.ok() && !again.mutation_attempted);
            assert(ops.written==1 && ops.closed==1);
        } else {
            assert(Get(f.paths.search)=="\n");
            assert(Get(f.paths.state)=="operating\n");
        }
    }
    std::cout<<"owned submission worker completes before status checks and preserves short-write/close uncertainty\n";
}
void BlockedSubmissionRetainsLeaseBeyondManagerLifetime() {
    for (bool writing:{false,true}) {
        Fixture f;WallClock clock;BlockedOps ops(f.paths,f.clock,writing);
        auto manager=std::unique_ptr<LinuxDe25FpgaManager>(
            new LinuxDe25FpgaManager(clock,f.lease,f.paths,ops));
        auto entered=ops.entered.get_future();auto closed=ops.close_returned.get_future();
        std::promise<NativeResult> completion;auto result=completion.get_future();
        const auto deadline=clock.NowMs()+200;
        std::thread caller([&] {
            completion.set_value(manager->Program(f.artifact,
                ProgrammingProfile::de25_hps_first_v1,deadline));
        });
        assert(entered.wait_for(std::chrono::seconds(2))==std::future_status::ready);
        const bool bounded=result.wait_for(std::chrono::milliseconds(400))==std::future_status::ready;
        if (!bounded) ops.release.set_value(); // Unblock a regressed caller before failing.
        const auto failed=result.get();caller.join();assert(bounded);
        assert(!failed.error.ok() && failed.mutation_attempted && manager->Poisoned());
        assert(failed.error.message.find("not retired")!=std::string::npos);
        assert(closed.wait_for(std::chrono::milliseconds(0))!=std::future_status::ready);
        f.Preserved();assert(Get(f.paths.search)==f.paths.firmware+"\n");
        const auto again=manager->Program(f.artifact,ProgrammingProfile::de25_hps_first_v1,
                                          clock.NowMs()+1000);
        assert(!again.error.ok() && !again.mutation_attempted);
        assert(ops.written==(writing?0:1) && ops.closed==0);
        // Drop every original lease reference while the callback remains blocked.
        manager.reset();assert(!close(f.lease));f.lease=-1;
        const int contender=open(f.paths.lease.c_str(),O_RDWR|O_CLOEXEC);
        assert(contender>=0 && flock(contender,LOCK_EX|LOCK_NB)<0);
        assert(errno==EWOULDBLOCK || errno==EAGAIN);
        ops.release.set_value();
        assert(closed.wait_for(std::chrono::seconds(2))==std::future_status::ready);
        // The work item's lease is its last member destroyed, after callback
        // retirement. A successful contender independently proves its release.
        const auto retired_by=std::chrono::steady_clock::now()+std::chrono::seconds(2);
        while (flock(contender,LOCK_EX|LOCK_NB)) {
            assert(std::chrono::steady_clock::now()<retired_by);
            std::this_thread::sleep_for(std::chrono::milliseconds(1));
        }
        assert(ops.written==1 && ops.closed==1);
        f.Preserved();assert(Get(f.paths.search)==f.paths.firmware+"\n");
        assert(!close(contender));
    }
    std::cout<<"blocked write/close returns by deadline, refuses replay, and retains the physical lease after manager destruction until actual retirement\n";
}
}
int main(int argc,char** argv) {
    if (argc==1) {
        ThreadedSubmissionCompletion();
        BlockedSubmissionRetainsLeaseBeyondManagerLifetime();
    }
    if(argc==2 || argc==3) {
        auto profile=De25OverlayProfile::firmware_only;
        if(argc==3) {
            const std::string name=argv[1];
            assert(name=="allocated" || name=="static-base" || name=="static-persona");
            profile=name=="allocated"?De25OverlayProfile::allocated_scanout:
                name=="static-base"?De25OverlayProfile::static_expansion_base:De25OverlayProfile::static_expansion_persona;
        }
        const auto v=De25ProgrammingOverlay(profile);std::ofstream f(argv[argc-1],std::ios::binary);
        f.write(reinterpret_cast<const char*>(v.data()),v.size());assert(f.good());return 0;
    }
    RecoverVerifiedAllocatedConfigurationOnlyAfterKernelRetirement();Positive();CleanRelease();ReleaseRefusals();AllocatedScanoutProfile();StaticStartupProfiles();StaticProductionDispatchGate();StaticStartupAndFullReplacement();OwnedStaticMappingHasOneFullBaseLifetime();OwnedStaticMappingFaultsRetainAndRefuseReplay();OwnedStaticMappingRefusesBeforeOpenAndAfterPersonaChange();StaticRefusalsBeforeHardware();StaticColdIdentityFailures();StaticPersonaFailuresRetainBothImages();StaticCopyAndLookupRacesNeverSubmitWrongBytes();StaticReleaseChecksBothIdentitiesBeforeEffects();Uncertain();BeforeMutation();FirmwareSearchLifecycle();FirmwareFallbacks();ExistingFailure();ProfilesAndLease();
    std::cout<<"de25_fpga_manager_test: passed\n";
}
