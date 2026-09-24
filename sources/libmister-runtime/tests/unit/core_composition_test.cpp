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
	Fixture(bool coleco=false) {
		char path[]="/tmp/fes-composition.XXXXXX";root=mkdtemp(path);
		assert(mkdir((root+"/expansion").c_str(),0700)==0);
		assert(mkdir((root+"/composition").c_str(),0700)==0);
		base.package_id=std::string(64,'a');base.descriptor.abi={coleco ? "fes.application" : "fes.simple-computer",1,0};
		const std::string slot=coleco ? "fes.expansion.coleco-bus" : "fes.expansion.zx81-bus";
		const std::string map=coleco ? "fes.coleco-bus.socket/1" : "fes.zx81-bus.socket/1";
		base.descriptor.interfaces={{slot,1,0,false}};
		base.descriptor.target.device="5CSEBA6U23I7";
		base.descriptor.build.id=std::string(32,'b');base.descriptor.payload.sha256=Hash("base");
		manifest="{\"cart_sha256\":\""+Hash(cart)+"\",\"cart_size\":40408,\"device\":\"5CSEBA6U23I7\",\"format\":1,"
			"\"map\":\""+map+"\",\"recipe_sha256\":\""+std::string(64,'c')+"\",\"revision\":\""+std::string(40,'d')+
			"\",\"shell_build_id\":\""+base.descriptor.build.id+"\",\"shell_package_id\":\""+base.package_id+
			"\",\"shell_sha256\":\""+base.descriptor.payload.sha256+"\",\"slot\":\""+slot+"\",\"slot_major\":1,\"slot_minor\":0}";
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
int main() {SharedGoIdentityVector();ValidAndRetained();ColecoBusAdmission();ColecoBoundaryPatchAdmission();RejectColecoBoundaryPatchVariants();RejectBindings();RejectBytesAndPaths();}
