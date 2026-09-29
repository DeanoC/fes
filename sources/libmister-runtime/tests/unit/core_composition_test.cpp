// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/core_composition.hpp"
#include "native/sha256.hpp"
#include <cassert>
#include <fstream>
#include <sys/stat.h>
#include <unistd.h>
#include <cstdlib>

using namespace mister;
using namespace mister::native;
namespace {
std::string Hash(const std::string& bytes) {Sha256 h;h.Update(bytes.data(),bytes.size());return Sha256Hex(h.Final());}
void Write(const std::string& path,const std::string& bytes) {std::ofstream f(path,std::ios::binary|std::ios::trunc);f<<bytes;assert(f.good());}
struct Fixture {
	std::string root,manifest,cart=std::string(40408,'c'),linked=std::string(40408,'l');
	OpenedCorePackage base;
	CoreCompositionRequest request;
	Fixture(bool coleco=false,int version=1) {
		char path[]="/tmp/fes-composition.XXXXXX";root=mkdtemp(path);
		assert(mkdir((root+"/expansion").c_str(),0700)==0);
		assert(mkdir((root+"/composition").c_str(),0700)==0);
		base.package_id=std::string(64,'a');base.descriptor.abi={coleco ? "fes.application" : "fes.simple-computer",1,0};
		const std::string slot=coleco ? "fes.expansion.coleco-bus" : "fes.expansion.zx81-bus";
		const std::string map=coleco ? (version==2 ? "fes.coleco-bus.socket/2" : "fes.coleco-bus.socket/1") : "fes.zx81-bus.socket/1";
		base.descriptor.interfaces={{slot,static_cast<std::uint16_t>(version),0,false}};
		base.descriptor.target.device="5CSEBA6U23I7";
		base.descriptor.build.id=std::string(32,'b');base.descriptor.payload.sha256=Hash("base");
		manifest="{\"cart_sha256\":\""+Hash(cart)+"\",\"cart_size\":40408,\"device\":\"5CSEBA6U23I7\",\"format\":1,"
			"\"map\":\""+map+"\",\"recipe_sha256\":\""+std::string(64,'c')+"\",\"revision\":\""+std::string(40,'d')+
			"\",\"shell_build_id\":\""+base.descriptor.build.id+"\",\"shell_package_id\":\""+base.package_id+
			"\",\"shell_sha256\":\""+base.descriptor.payload.sha256+"\",\"slot\":\""+slot+"\",\"slot_major\":"+std::to_string(version)+",\"slot_minor\":0}";
		request.expansion_path=root+"/expansion";request.payload_path=root+"/composition/linked.rbf";
		request.composition.package_id=base.package_id;request.composition.shell_sha256=base.descriptor.payload.sha256;
		request.composition.payload_sha256=Hash(linked);request.composition.payload_size=linked.size();
		Seal();Write(request.expansion_path+"/cart.rbf",cart);Write(request.payload_path,linked);
	}
	void Seal() {
		request.composition.expansion_id=Hash(std::string("fes-expansion-v1\0",17)+manifest);
		request.composition.id=Hash(std::string("fes-composition-v1\0",19)+base.package_id+std::string(1,'\0')+
			request.composition.expansion_id+std::string(1,'\0')+request.composition.payload_sha256);
		Write(request.expansion_path+"/manifest.json",manifest);
	}
	Error Open(OpenedCoreComposition* out) {return OpenCoreComposition({root},base,request,out);}
	~Fixture() {
		for(const auto& name:{"expansion/manifest.json","expansion/cart.rbf","expansion/extra","composition/linked.rbf","composition/old.rbf","alias"}) unlink((root+"/"+name).c_str());
		rmdir((root+"/expansion").c_str());rmdir((root+"/composition").c_str());assert(rmdir(root.c_str())==0);
	}
};
void ValidAndRetained() {
	Fixture f;OpenedCoreComposition out;assert(f.Open(&out).ok());
	assert(out.info.package_id==f.base.package_id && out.info.id==f.request.composition.id);
	assert(RecheckCoreComposition(out).ok());
	// Renaming/replacing the pathname cannot redirect the retained descriptor.
	assert(rename(f.request.payload_path.c_str(),(f.root+"/composition/old.rbf").c_str())==0);
	Write(f.request.payload_path,std::string(40408,'x'));assert(RecheckCoreComposition(out).ok());
	// In-place mutation of the retained inode is detected before programming.
	Write(f.root+"/composition/old.rbf",std::string(40408,'y'));assert(!RecheckCoreComposition(out).ok());
}
void ColecoBusAdmission() {
	Fixture f(true);OpenedCoreComposition out;assert(f.Open(&out).ok());
	f.base.descriptor.interfaces={{"fes.expansion.zx81-bus",1,0,false}};
	assert(!f.Open(&out).ok());
	f.base.descriptor.interfaces={{"fes.expansion.coleco-bus",1,0,false},{"fes.expansion.zx81-bus",1,0,false}};
	assert(!f.Open(&out).ok());
	f.base.descriptor.interfaces={{"fes.expansion.coleco-bus",1,0,false}};
	f.base.descriptor.abi.id="fes.simple-computer";
	assert(!f.Open(&out).ok());
}
void ColecoV2BusAdmission() {
	Fixture f(true,2);OpenedCoreComposition out;assert(f.Open(&out).ok());
	assert(RecheckCoreComposition(out).ok());
	const std::string patch="\"boundary_patch\":{\"bits\":[{\"value\":0,\"x\":3332,\"y\":803},"
		"{\"value\":1,\"x\":3333,\"y\":802}],"
		"\"contract\":\"fes.coleco.response-boundary/4\"},";
	f.manifest.insert(1,patch);f.Seal();assert(!f.Open(&out).ok());
	f.manifest.erase(1,patch.size());f.Seal();
	f.base.descriptor.interfaces[0].major=1;assert(!f.Open(&out).ok());
	f.base.descriptor.interfaces[0].major=2;
	const auto at=f.manifest.find("socket/2");assert(at!=std::string::npos);
	f.manifest.replace(at,8,"socket/1");f.Seal();assert(!f.Open(&out).ok());
}
void ColecoBoundaryPatchAdmission() {
	const std::string patch="\"boundary_patch\":{\"bits\":[{\"value\":0,\"x\":3332,\"y\":803},"
		"{\"value\":1,\"x\":3333,\"y\":802}],"
		"\"contract\":\"fes.coleco.response-boundary/4\"},";
	Fixture f(true);OpenedCoreComposition out;
	f.manifest.insert(1,patch);f.Seal();
	assert(f.Open(&out).ok());
	assert(RecheckCoreComposition(out).ok());
}
void RejectColecoBoundaryPatchVariants() {
	const std::string valid="\"boundary_patch\":{\"bits\":[{\"value\":0,\"x\":3332,\"y\":803},"
		"{\"value\":1,\"x\":3333,\"y\":802}],"
		"\"contract\":\"fes.coleco.response-boundary/4\"},";
	for (const auto& mutation : {
		std::pair<std::string,std::string>{"\"value\":1", "\"value\":2"},
		{"\"x\":3333", "\"x\":3334"},
		{"\"y\":803", "\"y\":804"},
		{"response-boundary/4", "response-boundary/3"},
		{"],\"contract\"", ",{\"value\":0,\"x\":3328,\"y\":906}],\"contract\""},
	}) {
		Fixture f(true);OpenedCoreComposition out;
		std::string patch=valid;
		const auto at=patch.find(mutation.first);assert(at!=std::string::npos);
		patch.replace(at,mutation.first.size(),mutation.second);
		f.manifest.insert(1,patch);f.Seal();
		assert(!f.Open(&out).ok());
	}
	Fixture zx81;OpenedCoreComposition out;
	zx81.manifest.insert(1,valid);zx81.Seal();
	assert(!zx81.Open(&out).ok());
}
void RejectBindings() {
	for(unsigned test=0;test<10;++test) {
		Fixture f;OpenedCoreComposition out;
		switch(test) {
		case 0:f.base.descriptor.interfaces.clear();break;
		case 1:f.base.descriptor.interfaces[0].required=true;break;
		case 2:f.base.descriptor.abi.id="fes.application";break;
		case 3:f.request.composition.id=std::string(64,'0');break;
		case 4:f.request.composition.package_id=std::string(64,'0');break;
		case 5:f.request.composition.shell_sha256=std::string(64,'0');break;
		case 6:f.request.composition.payload_size++;break;
		case 7:f.base.descriptor.build.id=std::string(32,'0');break;
		case 8:f.base.descriptor.target.device="other";break;
		case 9:f.base.descriptor.interfaces[0].minor=1;break;
		}
		assert(!f.Open(&out).ok());
	}
}
void RejectBytesAndPaths() {
	for(unsigned test=0;test<9;++test) {
		Fixture f;OpenedCoreComposition out;
		switch(test) {
		case 0:f.manifest+="\n";f.Seal();break;
		case 1:f.manifest.insert(1,"\"extra\":0,");f.Seal();break;
		case 2:Write(f.request.expansion_path+"/cart.rbf",std::string(40408,'x'));break;
		case 3:Write(f.request.payload_path,std::string(40408,'x'));break;
		case 4:Write(f.request.expansion_path+"/extra","x");break;
		case 5:assert(symlink(f.request.expansion_path.c_str(),(f.root+"/alias").c_str())==0);f.request.expansion_path=f.root+"/alias";break;
		case 6:f.request.expansion_path=f.root+"/expansion/../expansion";break;
		case 7:unlink(f.request.payload_path.c_str());assert(symlink((f.root+"/expansion/cart.rbf").c_str(),f.request.payload_path.c_str())==0);break;
		case 8:f.request.payload_path="/tmp/outside/linked.rbf";break;
		}
		assert(!f.Open(&out).ok());
	}
	Fixture f;OpenedCoreComposition out;assert(f.Open(&out).ok());
	Write(f.root+"/expansion/cart.rbf",std::string(40408,'z'));assert(!RecheckCoreComposition(out).ok());
}
}
void SharedGoIdentityVector() {
 // Produced independently by misteross/expansion TestAssetRoundTripAndComposition.
 const std::string manifest = R"VECTOR({"cart_sha256":"ebf60623524409a830b87c6aa99f50b61e648bd268d74b63a5163eff417f9def","cart_size":1816338,"device":"5CSEBA6U23I7","format":1,"map":"fes.zx81-bus.socket/1","recipe_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","revision":"dddddddddddddddddddddddddddddddddddddddd","shell_build_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","shell_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","shell_sha256":"f38894e270e9e2771e157ebdf8767e92ad58d63de0764e7b03afdef0842ec5a0","slot":"fes.expansion.zx81-bus","slot_major":1,"slot_minor":0})VECTOR";
 assert(Hash(std::string("fes-expansion-v1\0",17)+manifest)=="f0d17b28a77c63ee338391caf258c854007e6e5854997cd8aeeb1ffc7a59b808");
 const std::string identity=std::string("fes-composition-v1\0",19)+std::string(64,'a')+
  std::string(1,'\0')+"f0d17b28a77c63ee338391caf258c854007e6e5854997cd8aeeb1ffc7a59b808"+std::string(1,'\0')+"8be0d02e30165a365e563e52c8d6adea541f68fd1941c8c98f88f480dedba5fd";
 assert(Hash(identity)=="2c13493d7e935b366908cd17a97c6e741083dddbdeeab6bb985366a52b460b4b");
}
namespace {
// Multi-slot identity: domain NUL package NUL, "<slot>:<expansion>" NUL per
// ascending slot, then the linked payload digest.
std::string SlotCompositionId(const std::string& package,
	const std::vector<CoreCompositionSlot>& slots, const std::string& payload) {
	std::string canonical=std::string("fes-composition-v2\0",19)+package+std::string(1,'\0');
	for (const auto& slot : slots) canonical+=std::to_string(slot.slot)+":"+slot.expansion_id+std::string(1,'\0');
	return Hash(canonical+payload);
}
void SlotIdentityVector() {
	// Independent Python vectors (hashlib) for the documented byte layout.
	assert(SlotCompositionId(std::string(64,'a'),{{4,std::string(64,'b')},{6,std::string(64,'c')}},std::string(64,'d'))==
		"a2faef847dc24df1194f52cea4ae4fe3f09c9f4409cdd6f56e8fccd4dcb29541");
	assert(SlotCompositionId(std::string(64,'a'),{{7,std::string(64,'b')}},std::string(64,'d'))==
		"d9e6d7b76eb78d2c9db1252344e1fef75bd294f92d7c02f7196239672ca2f8dc");
}
struct SlotFixture {
	std::string root,linked=std::string(40408,'l');
	OpenedCorePackage base;
	CoreCompositionRequest request;
	std::vector<unsigned> slots;
	std::vector<std::string> manifests,carts;
	bool c64=false;
	explicit SlotFixture(std::vector<unsigned> selected={4,6}, bool commodore=false)
		: slots(std::move(selected)), c64(commodore) {
		char path[]="/tmp/fes-slot-composition.XXXXXX";root=mkdtemp(path);
		assert(mkdir((root+"/composition").c_str(),0700)==0);
		base.package_id=std::string(64,'a');base.descriptor.abi={"fes.computer",1,0};
		base.descriptor.interfaces={{"fes.video.fixed-720p60",1,0,true},
			{c64 ? "fes.expansion.c64-bus" : "fes.expansion.apple2-bus",1,0,false}};
		base.descriptor.target.device="5CSEBA6U23I7";
		base.descriptor.build.id=std::string(32,'b');base.descriptor.payload.sha256=Hash("base");
		for (unsigned slot : slots) {
			const std::string directory=root+"/slot"+std::to_string(slot);
			assert(mkdir(directory.c_str(),0700)==0);
			carts.push_back(std::string(40408,static_cast<char>('0'+slot)));
			manifests.push_back(Manifest(slot,carts.back()));
			Write(directory+"/cart.rbf",carts.back());
			request.expansions.push_back({static_cast<std::uint8_t>(slot),directory});
		}
		request.payload_path=root+"/composition/linked.rbf";
		request.composition.package_id=base.package_id;request.composition.shell_sha256=base.descriptor.payload.sha256;
		request.composition.payload_sha256=Hash(linked);request.composition.payload_size=linked.size();
		Write(request.payload_path,linked);
		Seal();
	}
	std::string Manifest(unsigned slot,const std::string& cart) const {
		const char* map=c64 ? "fes.c64-bus.sockets/1" : "fes.apple2-bus.slots/1";
		const char* bus=c64 ? "fes.expansion.c64-bus" : "fes.expansion.apple2-bus";
		return "{\"cart_sha256\":\""+Hash(cart)+"\",\"cart_size\":40408,\"device\":\"5CSEBA6U23I7\",\"format\":1,"
			"\"map\":\""+std::string(map)+"\",\"recipe_sha256\":\""+std::string(64,'c')+"\",\"revision\":\""+std::string(40,'d')+
			"\",\"shell_build_id\":\""+base.descriptor.build.id+"\",\"shell_package_id\":\""+base.package_id+
			"\",\"shell_sha256\":\""+base.descriptor.payload.sha256+"\",\"slot\":\""+std::string(bus)+"\",\"slot_index\":"+
			std::to_string(slot)+",\"slot_major\":1,\"slot_minor\":0}";
	}
	void Seal() {
		request.composition.expansions.clear();
		for (std::size_t i=0;i<slots.size();++i) {
			Write(request.expansions[i].path+"/manifest.json",manifests[i]);
			request.composition.expansions.push_back({request.expansions[i].slot,
				Hash(std::string("fes-expansion-v1\0",17)+manifests[i])});
		}
		request.composition.id=SlotCompositionId(base.package_id,request.composition.expansions,request.composition.payload_sha256);
	}
	Error Open(OpenedCoreComposition* out) {return OpenCoreComposition({root},base,request,out);}
	~SlotFixture() {
		for (unsigned slot : slots) {
			const std::string directory=root+"/slot"+std::to_string(slot);
			for (const auto* name : {"/manifest.json","/cart.rbf","/extra"}) unlink((directory+name).c_str());
			rmdir(directory.c_str());
		}
		unlink((root+"/composition/linked.rbf").c_str());unlink((root+"/composition/other.rbf").c_str());
		rmdir((root+"/composition").c_str());assert(rmdir(root.c_str())==0);
	}
};
void SlotCompositionAdmission() {
	{
		SlotFixture f;OpenedCoreComposition out;
		const auto error=f.Open(&out);
		if (!error.ok()) fprintf(stderr,"slot composition: %s\n",error.message.c_str());
		assert(error.ok());
		assert(out.info.id==f.request.composition.id && out.info.expansion_id.empty());
		assert(out.info.expansions.size()==2 && out.info.expansions[0].slot==4 && out.info.expansions[1].slot==6);
		assert(out.expansions.size()==2 && out.expansions[1].slot==6);
		assert(RecheckCoreComposition(out).ok());
		// Every card is rechecked before programming, not only the first.
		Write(f.request.expansions[1].path+"/cart.rbf",std::string(40408,'z'));
		assert(!RecheckCoreComposition(out).ok());
	}
	// The socket set of fes.apple2-bus.slots/1 is every slot until it is sealed.
	for (unsigned slot=1;slot<=7;++slot) {
		SlotFixture f({slot});OpenedCoreComposition out;assert(f.Open(&out).ok());
	}
	SlotFixture all({1,2,3,4,5,6,7});OpenedCoreComposition out;assert(all.Open(&out).ok());
	assert(out.expansions.size()==7);
	SlotFixture c64({1,2}, true);assert(c64.Open(&out).ok());
	assert(out.expansions.size()==2 && out.expansions[0].slot==1 && out.expansions[1].slot==2);
	SlotFixture io_only({2}, true);assert(io_only.Open(&out).ok());
	SlotFixture missing({3}, true);assert(!missing.Open(&out).ok());
}
void RejectSlotCompositionVariants() {
	for (unsigned test=0;test<24;++test) {
		SlotFixture f;OpenedCoreComposition out;
		auto& c=f.request.composition;
		auto replace=[&](std::size_t index,const std::string& from,const std::string& to) {
			const auto at=f.manifests[index].find(from);assert(at!=std::string::npos);
			f.manifests[index].replace(at,from.size(),to);f.Seal();
		};
		switch(test) {
		case 0:replace(0,"\"slot_index\":4","\"slot_index\":5");break;
		case 1:replace(0,"\"slot_index\":4,","");break; // single-socket manifest grammar
		case 2:replace(1,"fes.apple2-bus.slots/1","fes.zx81-bus.socket/1");break;
		case 3:replace(1,"\"slot\":\"fes.expansion.apple2-bus\"","\"slot\":\"fes.expansion.zx81-bus\"");break;
		case 4:replace(0,"\"slot_major\":1","\"slot_major\":2");break;
		case 5:replace(0,"\"slot_minor\":0","\"slot_minor\":1");break;
		case 6:f.manifests[0].insert(1,"\"boundary_patch\":{\"bits\":[{\"value\":0,\"x\":3332,\"y\":803},"
			"{\"value\":1,\"x\":3333,\"y\":802}],\"contract\":\"fes.coleco.response-boundary/4\"},");f.Seal();break;
		case 7:f.base.descriptor.interfaces[1].required=true;break;
		case 8:f.base.descriptor.interfaces.pop_back();break;
		case 9:f.base.descriptor.abi.id="fes.application";break;
		case 10:f.base.descriptor.interfaces.push_back({"fes.expansion.coleco-bus",1,0,false});break;
		case 11:c.id=std::string(64,'0');break;
		case 12:c.expansions[1].expansion_id=std::string(64,'0');
			c.id=SlotCompositionId(c.package_id,c.expansions,c.payload_sha256);break;
		case 13:std::swap(f.request.expansions[0],f.request.expansions[1]);std::swap(c.expansions[0],c.expansions[1]);
			c.id=SlotCompositionId(c.package_id,c.expansions,c.payload_sha256);break;
		case 14:c.expansions[1].slot=7;c.id=SlotCompositionId(c.package_id,c.expansions,c.payload_sha256);break;
		case 15:f.request.expansions.pop_back();break;
		case 16:f.request.expansion_path=f.request.expansions[0].path;break;
		case 17:c.expansion_id=c.expansions[0].expansion_id;break;
		case 18:Write(f.request.expansions[0].path+"/extra","x");break;
		case 19:c.shell_sha256=std::string(64,'0');break;
		case 20:c.payload_size++;break;
		case 21:f.request.payload_path=f.root+"/composition/other.rbf";Write(f.request.payload_path,f.linked);break;
		case 22:Write(f.request.expansions[1].path+"/cart.rbf",std::string(40408,'z'));break;
		case 23:f.base.descriptor.build.id=std::string(32,'0');break;
		}
		assert(!f.Open(&out).ok());
	}
	// Slot 0, slot 8 and more than seven cards are outside the map.
	for (const unsigned slot : {0u,8u}) {
		SlotFixture f({4});OpenedCoreComposition out;
		f.request.expansions[0].slot=static_cast<std::uint8_t>(slot);
		f.request.composition.expansions[0].slot=static_cast<std::uint8_t>(slot);
		f.request.composition.id=SlotCompositionId(f.base.package_id,f.request.composition.expansions,
			f.request.composition.payload_sha256);
		assert(!f.Open(&out).ok());
	}
	SlotFixture eight({1,2,3,4,5,6,7});OpenedCoreComposition out;
	eight.request.expansions.push_back(eight.request.expansions.back());
	eight.request.composition.expansions.push_back(eight.request.composition.expansions.back());
	assert(!eight.Open(&out).ok());
	// A single-socket request cannot compose an Apple II shell, and a slot
	// request cannot compose a ZX81 or Coleco shell.
	SlotFixture apple2;
	CoreCompositionRequest single;
	single.expansion_path=apple2.request.expansions[0].path;single.payload_path=apple2.request.payload_path;
	single.composition=apple2.request.composition;single.composition.expansions.clear();
	single.composition.expansion_id=apple2.request.composition.expansions[0].expansion_id;
	assert(!OpenCoreComposition({apple2.root},apple2.base,single,&out).ok());
	Fixture zx81;
	CoreCompositionRequest slots=zx81.request;
	slots.expansion_path.clear();slots.composition.expansion_id.clear();
	slots.expansions={{4,zx81.request.expansion_path}};
	slots.composition.expansions={{4,zx81.request.composition.expansion_id}};
	slots.composition.id=SlotCompositionId(zx81.base.package_id,slots.composition.expansions,slots.composition.payload_sha256);
	assert(!OpenCoreComposition({zx81.root},zx81.base,slots,&out).ok());
	assert(zx81.Open(&out).ok()); // single-socket ZX81 admission is unchanged
}
}
int main() {SharedGoIdentityVector();ValidAndRetained();ColecoBusAdmission();ColecoV2BusAdmission();ColecoBoundaryPatchAdmission();RejectColecoBoundaryPatchVariants();RejectBindings();RejectBytesAndPaths();
	SlotIdentityVector();SlotCompositionAdmission();RejectSlotCompositionVariants();
	puts("core_composition_test: single-socket and multi-slot admission passed");}
