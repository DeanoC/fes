// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_fpga_manager.hpp"
#include "native/artifacts.hpp"
#include "native/sha256.hpp"
#include <cassert>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <fcntl.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>
#include <iostream>
#include <map>
using namespace mister;
using namespace mister::native;
namespace {
void Put(const std::string& p, const std::string& s) { std::ofstream f(p, std::ios::binary); f << s; assert(f.good()); }
std::string Get(const std::string& p) { std::ifstream f(p,std::ios::binary); return {(std::istreambuf_iterator<char>(f)),{}}; }
bool Exists(const std::string& p) { struct stat s{}; return !lstat(p.c_str(),&s); }
struct Time final : Clock { std::uint64_t now = 0; std::uint64_t NowMs() const override { return now; } };
struct Ops final : De25FpgaTestOperations {
    De25FpgaTestPaths& paths; Time& time;
    int created=0, written=0, removed=0, closed=0;
    bool short_write=false, error_close=false, late_close=false, unapplied=false;
    bool remove_error=false, create_error=false, open_error=false;
    Ops(De25FpgaTestPaths& p, Time& t):paths(p),time(t) {}
    std::string Dir() const {return paths.config+"/device-tree/overlays/fes-runtime";}
    int Create(int parent) override {
        ++created;
        if(create_error) {errno=EACCES;return -1;}
        if(mkdirat(parent,"fes-runtime",0700)) return -1;
        if(!open_error) Put(Dir()+"/dtbo", "");
        Put(Dir()+"/status","unapplied\n");return 0;
    }
    ssize_t Write(int fd,const unsigned char* p,std::size_t n) override {
        ++written;
        assert(Get(Dir()+"/status")=="unapplied\n");
        // Model the actual pinned configfs binary attribute: nothing applies
        // at write; its release callback applies the complete buffered blob.
        return write(fd,p,short_write?n-1:n);
    }
    int Close(int fd) override {
        ++closed;const int r=close(fd);assert(!r);
        if(late_close)time.now=1000;
        Put(Dir()+"/status",unapplied?"unapplied\n":"applied\n");
        Put(paths.state,unapplied?"write error\n":"operating\n");
        if(error_close) {errno=EINTR;return -1;}
        return 0;
    }
    int Remove(int parent) override {
        ++removed;
        if(remove_error) {errno=EBUSY;return -1;}
        unlink((Dir()+"/dtbo").c_str());unlink((Dir()+"/status").c_str());
        return unlinkat(parent,"fes-runtime",AT_REMOVEDIR);
    }
};
struct Fixture {
    std::string root; Time clock; De25FpgaTestPaths paths; Ops ops;
    int lease=-1; Artifact artifact;
    Fixture():ops(paths,clock) {
        char name[]="/tmp/fes-de25-manager-XXXXXX";auto* made=mkdtemp(name);assert(made);root=made;
        paths={root+"/config",root+"/firmware",root+"/state",root+"/search",root+"/lease",root+"/record",root+"/notes"};
        for(const auto& p:{paths.config,paths.config+"/device-tree",paths.config+"/device-tree/overlays",paths.firmware})assert(!mkdir(p.c_str(),0700));
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
    std::unique_ptr<LinuxDe25FpgaManager> Manager(De25OverlayProfile profile = De25OverlayProfile::firmware_only) {return std::unique_ptr<LinuxDe25FpgaManager>(new LinuxDe25FpgaManager(clock,lease,paths,ops,profile));}
    NativeResult Program(LinuxDe25FpgaManager& m) {return m.Program(artifact,ProgrammingProfile::de25_hps_first_v1,100);}
    void Preserved() {assert(Exists(ops.Dir()) && Exists(paths.firmware+"/fes-runtime-core.rbf"));}
};
void Positive() {
    Fixture f;auto m=f.Manager();assert(f.Program(*m).error.ok());
    assert(f.ops.closed==1 && !m->Poisoned());
    assert(Get(f.paths.firmware+"/fes-runtime-core.rbf")=="immutable test image");
    assert(f.Program(*m).error.ok());assert(f.ops.closed==2 && f.ops.removed==1);
    assert(!m->BootHpsDdrLayout());assert(!m->ReleaseHpsDdrPorts(100).ok());
    m.reset();f.Preserved();auto fresh=f.Manager();auto r=f.Program(*fresh);
    assert(!r.error.ok() && !r.mutation_attempted && f.ops.written==2);
    std::cout<<"success applies on close, reprograms in context, and preserves state at destruction\n";
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
    {
        Fixture f;auto m = f.Manager(De25OverlayProfile::allocated_scanout);
        assert(f.Program(*m).error.ok());
        assert(Get(f.ops.Dir()+"/dtbo") == bytes(scanout));
        assert(f.Program(*m).error.ok() && f.ops.removed == 1);
        // An inherited marker is never adopted by a fresh caller.
        auto fresh = f.Manager(De25OverlayProfile::allocated_scanout);
        auto r = f.Program(*fresh);assert(!r.error.ok() && !r.mutation_attempted);
        // A successful context must retain its exact selected marker too.
        Put(f.ops.Dir()+"/dtbo", bytes(basic));
        r = f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && f.ops.removed == 1);
    }
    {
        Fixture f;auto m = f.Manager(static_cast<De25OverlayProfile>(42));
        auto r = f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted);
        assert(!f.ops.created && !Exists(f.paths.firmware+"/fes-runtime-core.rbf"));
    }
    {
        Fixture f;auto m = f.Manager(De25OverlayProfile::allocated_scanout);
        f.ops.error_close = true;auto r = f.Program(*m);
        assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());
        f.Preserved();assert(!f.ops.removed && f.ops.written == 1);
        r = f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted && f.ops.written == 1);
    }
    std::cout<<"explicit scanout marker, invalid selection, exact replacement and uncertainty retention pass\n";
}
void Uncertain() {
    for(int mode=0;mode<4;++mode) {
        Fixture f;auto m=f.Manager();f.ops.short_write=mode==0;f.ops.error_close=mode==1;f.ops.late_close=mode==2;f.ops.unapplied=mode==3;
        auto r=f.Program(*m);assert(!r.error.ok() && r.mutation_attempted && m->Poisoned());
        assert(f.ops.written==1 && f.ops.closed==1 && f.ops.removed==0);f.Preserved();
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
        if(mode==10) {assert(!mkdir((f.paths.firmware+"/updates").c_str(),0700));Put(f.paths.firmware+"/updates/fes-runtime-core.rbf","wrong image");}
        auto r=f.Program(*m);assert(!r.error.ok() && !r.mutation_attempted);
        assert(f.ops.written==0 && f.ops.closed==0);
        if(mode==5 || mode==6)assert(!Exists(f.paths.firmware+"/fes-runtime-core.rbf") && !Exists(f.ops.Dir()));
        if(mode==4)assert(Get(f.root+"/input.rbf")=="immutable test image");
    }
    std::cout<<"preflight/staging failures make no programming call and clean only new resources\n";
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
}
int main(int argc,char** argv) {
    if(argc==2) {const auto v=De25ProgrammingOverlay();std::ofstream f(argv[1],std::ios::binary);f.write(reinterpret_cast<const char*>(v.data()),v.size());assert(f.good());return 0;}
    Positive();AllocatedScanoutProfile();Uncertain();BeforeMutation();ExistingFailure();ProfilesAndLease();
    std::cout<<"de25_fpga_manager_test: passed\n";
}
