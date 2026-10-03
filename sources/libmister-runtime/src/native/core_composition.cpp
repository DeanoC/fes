// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/core_composition.hpp"
#include "native/sha256.hpp"
#include <algorithm>
#include <array>
#include <dirent.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

namespace mister { namespace native { namespace {
constexpr std::uint64_t MaximumPayload = 32 * 1024 * 1024;
Error Invalid(const std::string& message) {
	return {ErrorCode::invalid_package, message, "admission"};
}
bool Hex(const std::string& text, std::size_t size) {
	if (text.size() != size) return false;
	for (char c : text) if (!(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f')) return false;
	return true;
}
std::string Hash(const std::string& bytes) {
	Sha256 hash; hash.Update(bytes.data(), bytes.size()); return Sha256Hex(hash.Final());
}
Error Read(const Artifact& file, std::string* bytes, std::string* digest) {
	struct stat st = {};
	if (fstat(file.fd(), &st) || !S_ISREG(st.st_mode) || st.st_size < 1 ||
		static_cast<std::uint64_t>(st.st_size) != file.size()) return Invalid("composition file size changed");
	Sha256 hash;
	std::array<char, 32768> buffer;
	if (bytes) bytes->clear();
	std::uint64_t offset = 0;
	while (offset < file.size()) {
		const std::size_t wanted = static_cast<std::size_t>(std::min<std::uint64_t>(buffer.size(), file.size()-offset));
		const ssize_t count = pread(file.fd(), buffer.data(), wanted, static_cast<off_t>(offset));
		if (count <= 0) return Invalid("cannot read retained composition file");
		hash.Update(buffer.data(), static_cast<std::size_t>(count));
		if (bytes) bytes->append(buffer.data(), static_cast<std::size_t>(count));
		offset += static_cast<std::uint64_t>(count);
	}
	if (digest) *digest = Sha256Hex(hash.Final());
	return {};
}
// Same containment policy as base packages: every directory is no-follow and
// each child is opened relative to its retained parent, never a checked path.
int Directory(const std::vector<std::string>& roots, const std::string& path) {
	if (path.empty() || path[0] != '/' || path.find('\0') != std::string::npos) return -1;
	for (std::string root : roots) {
		while (root.size()>1 && root.back()=='/') root.pop_back();
		const std::string prefix = root == "/" ? root : root + "/";
		if (root.empty() || path.compare(0, prefix.size(), prefix) != 0) continue;
		const std::string relative = path.substr(prefix.size());
		if (relative.empty() || relative.back()=='/') continue;
		int fd = open(root.c_str(), O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
		if (fd < 0) continue;
		std::size_t start=0;
		while (start < relative.size()) {
			const std::size_t end = relative.find('/', start);
			const std::string part = relative.substr(start, end-start);
			if (part.empty() || part=="." || part=="..") { close(fd); fd=-1; break; }
			const int next=openat(fd, part.c_str(), O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
			close(fd); fd=next;
			if (fd<0 || end==std::string::npos) break;
			start=end+1;
		}
		if (fd>=0) return fd;
	}
	return -1;
}
bool ClosedDirectory(int fd) {
	int scan = fcntl(fd, F_DUPFD_CLOEXEC, 0);
	if (scan<0) return false;
	DIR* dir=fdopendir(scan);
	if (!dir) {close(scan);return false;}
	unsigned entries=0; bool valid=true;
	while (dirent* entry=readdir(dir)) {
		std::string name=entry->d_name;
		if (name=="." || name=="..") continue;
		if (name!="manifest.json" && name!="cart.rbf") valid=false;
		++entries;
	}
	closedir(dir);return valid && entries==2;
}
// The shared contract is canonical JSON with fixed field order and only
// fixed identifiers, lowercase hex and bounded decimal integers. Reading that
// exact grammar rejects duplicates, unknown fields, escapes and whitespace.
class ManifestReader {
public:
	explicit ManifestReader(const std::string& bytes): bytes_(bytes) {}
	bool HasBoundaryPatch() const {return bytes_.find("{\"boundary_patch\":")==0;}
	bool ColecoBoundaryPatch() {
		if (!Take("{\"boundary_patch\":{\"bits\":[")) return false;
		const std::array<const char*,2> coordinates={{
			",\"x\":3332,\"y\":803}",
			",\"x\":3333,\"y\":802}",
		}};
		for (std::size_t i=0;i<coordinates.size();++i) {
			if (i && !Take(",")) return false;
			if (!Take("{\"value\":") || !(Take("0") || Take("1")) || !Take(coordinates[i])) return false;
		}
		return Take("],\"contract\":\"fes.coleco.response-boundary/4\"}");
	}
	bool Text(const std::string& key, std::string* value) {
		if (!Key(key) || !Take("\"")) return false;
		const auto end=bytes_.find('"', offset_);
		if (end==std::string::npos) return false;
		*value=bytes_.substr(offset_,end-offset_);offset_=end+1;return true;
	}
	bool Number(const std::string& key, std::uint64_t* value) {
		if (!Key(key)) return false;
		const auto start=offset_;*value=0;
		while (offset_<bytes_.size() && bytes_[offset_]>='0' && bytes_[offset_]<='9') {
			if (offset_-start>=10) return false;
			*value=*value*10+static_cast<unsigned>(bytes_[offset_++]-'0');
		}
		return offset_>start && (offset_-start==1 || bytes_[start]!='0');
	}
	bool End() {return Take("}") && offset_==bytes_.size();}
private:
	bool Take(const std::string& value) {
		if (bytes_.compare(offset_,value.size(),value)!=0) return false;
		offset_+=value.size();return true;
	}
	bool Key(const std::string& key) {return Take((offset_==0 ? "{\"" : ",\"")+key+"\":");}
	const std::string& bytes_;std::size_t offset_=0;
};
Error OpenExpansion(const std::vector<std::string>& roots, const std::string& path, OpenedCoreExpansion* out) {
	PosixArtifactOpener opener;
	const int dir=Directory(roots,path);
	if (dir<0) return Invalid("expansion path is outside trusted roots");
	Error error;
	if (!ClosedDirectory(dir)) error=Invalid("expansion directory must contain only manifest.json and cart.rbf");
	if (error.ok()) error=opener.OpenRelative(dir,path,"manifest.json",65536,&out->manifest);
	if (error.ok()) error=opener.OpenRelative(dir,path,"cart.rbf",MaximumPayload,&out->cart);
	close(dir);
	if (!error.ok()) return error;
	return Read(out->manifest,&out->manifest_bytes,nullptr);
}
Error OpenLinkedPayload(const std::vector<std::string>& roots, const std::string& path,
	std::uint64_t size, Artifact* payload) {
	PosixArtifactOpener opener;
	const auto slash=path.find_last_of('/');
	if (slash==std::string::npos) return Invalid("invalid linked payload path");
	const std::string parent=path.substr(0,slash), name=path.substr(slash+1);
	if (name!="linked.rbf") return Invalid("linked payload must be named linked.rbf");
	const int payload_dir=Directory(roots,parent);
	if (payload_dir<0) return Invalid("linked payload path is outside trusted roots");
	const Error error=opener.OpenRelative(payload_dir,parent,name,MaximumPayload,payload);close(payload_dir);
	if (!error.ok()) return error;
	if (payload->size()!=size) return Invalid("linked payload size does not match composition");
	return {};
}
// fes.apple2-bus.slots/1 socket set. The shell's physical sockets are not
// final, so every Apple II slot 1..7 is admitted until that table is sealed.
bool Apple2SlotSocket(std::uint64_t slot) {return slot>=1 && slot<=7;}
bool SpectrumSlotSocket(std::uint64_t slot) {return slot>=1 && slot<=4;}
// fes.c64-bus.sockets/1 is socket 1 (ROM window) and socket 2 (I/O window).
bool C64SlotSocket(std::uint64_t slot) {return slot==1 || slot==2;}
bool AtariStSlotSocket(std::uint64_t slot) {return slot==1;}
// The target agent links cards into their own reserved socket rectangles with
// the shared misteross implementation; this admission verifies its result.
Error OpenSlotComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage& base, const CoreCompositionRequest& request, OpenedCoreComposition* output) {
	const auto& descriptor=base.descriptor;
	const char* bus_id=nullptr;
	const char* bus_map=nullptr;
	bool (*socket_ok)(std::uint64_t)=nullptr;
	std::size_t max_slots=0;
	for (const auto& interface : descriptor.interfaces) {
		const bool multi=interface.id==kApple2ExpansionBusID ||
			interface.id==kC64ExpansionBusID || interface.id==kSpectrumExpansionBusID ||
			interface.id==kAtariStExpansionBusID;
		if (interface.id!="fes.expansion.zx81-bus" && interface.id!="fes.expansion.coleco-bus" && !multi)
			continue;
		if (bus_id || !multi || interface.major!=1 || interface.minor!=0 || interface.required)
			return Invalid("base package has an unsupported or ambiguous expansion bus");
		if (interface.id==kAtariStExpansionBusID) {
			bus_id=kAtariStExpansionBusID;
			bus_map=kAtariStExpansionMapID;
			socket_ok=AtariStSlotSocket;
			max_slots=1;
		} else if (interface.id==kC64ExpansionBusID) {
			bus_id=kC64ExpansionBusID;
			bus_map=kC64ExpansionMapID;
			socket_ok=C64SlotSocket;
			max_slots=2;
		} else if (interface.id==kSpectrumExpansionBusID) {
			bus_id=kSpectrumExpansionBusID;
			bus_map=kSpectrumExpansionMapID;
			socket_ok=SpectrumSlotSocket;
			max_slots=4;
		} else {
			bus_id=kApple2ExpansionBusID;
			bus_map=kApple2ExpansionMapID;
			socket_ok=Apple2SlotSocket;
			max_slots=7;
		}
	}
	if (!bus_id || descriptor.abi.id!="fes.computer" || descriptor.abi.major!=1 || descriptor.abi.minor!=0)
		return Invalid("base package does not declare an optional multi-slot bus");
	const auto& info=request.composition;
	if (!request.expansion_path.empty() || !info.expansion_id.empty() ||
		!Hex(info.id,64) || !Hex(info.package_id,64) || !Hex(info.shell_sha256,64) || !Hex(info.payload_sha256,64) ||
		info.package_id!=base.package_id || info.shell_sha256!=descriptor.payload.sha256 ||
		info.payload_size<40408 || info.payload_size>MaximumPayload)
		return Invalid("invalid composition identity or base binding");
	if (request.expansions.empty() || request.expansions.size()>max_slots ||
		request.expansions.size()!=info.expansions.size())
		return Invalid("multi-slot composition slot count is outside this shell");
	// Domain, NUL, package, NUL, then "<slot>:<expansion>" NUL per ascending
	// slot, then the linked payload digest (misteross SlotCompositionID).
	std::string canonical("fes-composition-v2\0",19);
	canonical+=info.package_id+std::string(1,'\0');
	unsigned previous=0;
	for (std::size_t i=0;i<info.expansions.size();++i) {
		const auto& slot=info.expansions[i];
		if (slot.slot<=previous || !socket_ok(slot.slot) || request.expansions[i].slot!=slot.slot ||
			!Hex(slot.expansion_id,64))
			return Invalid("composition slots must ascend, be unique sockets and match the request");
		previous=slot.slot;
		canonical+=std::to_string(slot.slot)+":"+slot.expansion_id+std::string(1,'\0');
	}
	canonical+=info.payload_sha256;
	if (Hash(canonical)!=info.id) return Invalid("composition ID does not match canonical identity");
	OpenedCoreComposition opened;opened.info=info;
	for (std::size_t i=0;i<request.expansions.size();++i) {
		OpenedCoreExpansion expansion;expansion.slot=request.expansions[i].slot;
		Error error=OpenExpansion(roots,request.expansions[i].path,&expansion);
		if (!error.ok()) return error;
		ManifestReader reader(expansion.manifest_bytes);
		std::string cart_hash,device,map,recipe,revision,build,package,shell,slot;
		std::uint64_t size=0,format=0,index=0,major=0,minor=0;
		if (reader.HasBoundaryPatch() ||
			!reader.Text("cart_sha256",&cart_hash) || !reader.Number("cart_size",&size) ||
			!reader.Text("device",&device) || !reader.Number("format",&format) || !reader.Text("map",&map) ||
			!reader.Text("recipe_sha256",&recipe) || !reader.Text("revision",&revision) ||
			!reader.Text("shell_build_id",&build) || !reader.Text("shell_package_id",&package) ||
			!reader.Text("shell_sha256",&shell) || !reader.Text("slot",&slot) ||
			!reader.Number("slot_index",&index) || !reader.Number("slot_major",&major) ||
			!reader.Number("slot_minor",&minor) || !reader.End())
			return Invalid("expansion manifest must use canonical JSON");
		if (!Hex(cart_hash,64) || !Hex(recipe,64) || !Hex(revision,40) || !Hex(build,32) ||
			format!=1 || device!="5CSEBA6U23I7" || descriptor.target.device!=device ||
			slot!=bus_id || map!=bus_map || major!=1 || minor!=0 ||
			index!=expansion.slot || !socket_ok(index) ||
			size<40408 || size!=expansion.cart.size() || package!=base.package_id ||
			build!=descriptor.build.id || shell!=descriptor.payload.sha256)
			return Invalid("expansion manifest does not match its slot and frozen shell");
		if (Hash(std::string("fes-expansion-v1\0",17)+expansion.manifest_bytes)!=info.expansions[i].expansion_id)
			return Invalid("expansion ID does not match canonical manifest");
		expansion.cart_sha256=cart_hash;
		opened.expansions.push_back(std::move(expansion));
	}
	Error error=OpenLinkedPayload(roots,request.payload_path,info.payload_size,&opened.payload);
	if (!error.ok()) return error;
	error=RecheckCoreComposition(opened);
	if (!error.ok()) return error;
	*output=std::move(opened);return {};
}
// Developer parts retain the base GP identity and add no operational capability.
Error OpenPartsComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage& base, const CoreCompositionRequest& request,
	OpenedCoreComposition* output) {
	const auto& descriptor = base.descriptor;
	bool video = false, cpu = false;
	for (const auto& interface : descriptor.interfaces) {
		if (interface.id == "fes.fabric.video.raster-rgb888")
			video = !interface.required && interface.major == 1 && interface.minor == 0;
		if (interface.id == "fes.expansion.coleco-bus")
			cpu = !interface.required && interface.major == 2 && interface.minor == 0;
	}
	if (descriptor.format != 2 || descriptor.core.id != "fes.coleco" ||
		descriptor.abi.id != "fes.application" || descriptor.abi.major != 1 ||
		descriptor.abi.minor != 0 || !video || !cpu)
		return Invalid("parts require the declared Coleco video developer shell");
	const auto& info = request.composition;
	if (!request.expansion_path.empty() || !request.expansions.empty() ||
		!info.expansion_id.empty() || !info.expansions.empty() ||
		info.layout != "fes.coleco-video.parts/1" || !Hex(info.id, 64) ||
		!Hex(info.package_id, 64) || !Hex(info.shell_sha256, 64) ||
		!Hex(info.payload_sha256, 64) || info.package_id != base.package_id ||
		info.shell_sha256 != descriptor.payload.sha256 || info.payload_size < 40408 ||
		info.payload_size > MaximumPayload || info.parts.empty() || info.parts.size() > 2 ||
		request.parts.size() != info.parts.size())
		return Invalid("invalid developer parts identity or base binding");
	std::string canonical = "fes-parts-composition-v1" + std::string(1, '\0') +
		info.package_id + std::string(1, '\0') + info.layout + std::string(1, '\0');
	std::string previous;
	bool have_video = false;
	OpenedCoreComposition opened;
	opened.info = info;
	for (std::size_t i = 0; i < info.parts.size(); ++i) {
		const auto& part = info.parts[i];
		if ((part.role != "expansion" && part.role != "video") || part.role <= previous ||
			!Hex(part.part_id, 64) || request.parts[i].role != part.role)
			return Invalid("parts must be unique supported roles in ascending order");
		previous = part.role;
		have_video |= part.role == "video";
		canonical += part.role + ":" + part.part_id + std::string(1, '\0');
		OpenedCoreExpansion asset;
		Error error = OpenExpansion(roots, request.parts[i].path, &asset);
		if (!error.ok()) return error;
		ManifestReader reader(asset.manifest_bytes);
		std::string hash, device, map, recipe, revision, build, package, shell, slot;
		std::uint64_t size = 0, format = 0, major = 0, minor = 0;
		if (reader.HasBoundaryPatch() || !reader.Text("cart_sha256", &hash) ||
			!reader.Number("cart_size", &size) || !reader.Text("device", &device) ||
			!reader.Number("format", &format) || !reader.Text("map", &map) ||
			!reader.Text("recipe_sha256", &recipe) || !reader.Text("revision", &revision) ||
			!reader.Text("shell_build_id", &build) || !reader.Text("shell_package_id", &package) ||
			!reader.Text("shell_sha256", &shell) || !reader.Text("slot", &slot) ||
			!reader.Number("slot_major", &major) || !reader.Number("slot_minor", &minor) ||
			!reader.End())
			return Invalid("parts manifest must use canonical JSON");
		const bool is_video = part.role == "video";
		if (!Hex(hash, 64) || !Hex(recipe, 64) || !Hex(revision, 40) || !Hex(build, 32) ||
			format != 1 || device != "5CSEBA6U23I7" || descriptor.target.device != device ||
			slot != (is_video ? "fes.fabric.video.raster-rgb888" : "fes.expansion.coleco-bus") ||
			map != (is_video ? "fes.coleco-video.socket/1" : "fes.coleco-bus.socket/2") ||
			major != (is_video ? 1u : 2u) || minor != 0 || size < 40408 ||
			size != asset.cart.size() || package != base.package_id ||
			build != descriptor.build.id || shell != descriptor.payload.sha256 ||
			Hash(std::string("fes-expansion-v1\0", 17) + asset.manifest_bytes) != part.part_id)
			return Invalid("part does not match its role and frozen shell");
		asset.cart_sha256 = hash;
		opened.expansions.push_back(std::move(asset));
	}
	canonical += info.payload_sha256;
	if (!have_video || Hash(canonical) != info.id)
		return Invalid("parts composition ID does not match canonical identity");
	Error error = OpenLinkedPayload(roots, request.payload_path, info.payload_size, &opened.payload);
	if (error.ok()) error = RecheckCoreComposition(opened);
	if (!error.ok()) return error;
	*output = std::move(opened);
	return {};
}

} // namespace

Error RecheckCoreComposition(const OpenedCoreComposition& opened) {
	std::string bytes, digest;
	if (opened.expansions.empty()) return Invalid("composition retains no expansion");
	for (const auto& expansion : opened.expansions) {
		Error error=Read(expansion.manifest,&bytes,nullptr);
		if (!error.ok()) return error;
		if (bytes!=expansion.manifest_bytes) return Invalid("expansion manifest changed after admission");
		error=Read(expansion.cart,nullptr,&digest);
		if (!error.ok()) return error;
		if (digest!=expansion.cart_sha256) return Invalid("expansion cart changed after admission");
	}
	const Error error=Read(opened.payload,nullptr,&digest);
	if (!error.ok()) return error;
	if (digest!=opened.info.payload_sha256) return Invalid("linked payload changed after admission");
	return {};
}

Error OpenCoreComposition(const std::vector<std::string>& roots,
	const OpenedCorePackage& base, const CoreCompositionRequest& request, OpenedCoreComposition* output) {
	if (!output) return Invalid("missing composition output");
	if (!request.parts.empty() || !request.composition.parts.empty())
		return OpenPartsComposition(roots,base,request,output);
	if (!request.expansions.empty() || !request.composition.expansions.empty())
		return OpenSlotComposition(roots,base,request,output);
	const auto& descriptor=base.descriptor;
	std::string socket;
	std::uint16_t socket_major=0;
	for (const auto& interface : descriptor.interfaces) {
		if (interface.id!="fes.expansion.zx81-bus" && interface.id!="fes.expansion.coleco-bus") continue;
		if (!socket.empty() || interface.minor!=0 || interface.required ||
			!(interface.major==1 || interface.major==2))
			return Invalid("base package has an unsupported or ambiguous expansion bus");
		socket=interface.id;
		socket_major=interface.major;
	}
	const std::string abi=socket=="fes.expansion.coleco-bus" ? "fes.application" : "fes.simple-computer";
	if (socket.empty() || descriptor.abi.id!=abi || descriptor.abi.major!=1 || descriptor.abi.minor!=0)
		return Invalid("base package does not declare a matching optional expansion bus");
	const auto& info=request.composition;
	if (!Hex(info.id,64) || !Hex(info.package_id,64) || !Hex(info.expansion_id,64) ||
		!Hex(info.shell_sha256,64) || !Hex(info.payload_sha256,64) || info.package_id!=base.package_id ||
		info.shell_sha256!=descriptor.payload.sha256 || info.payload_size<40408 || info.payload_size>MaximumPayload)
		return Invalid("invalid composition identity or base binding");
	const std::string canonical=std::string("fes-composition-v1\0",19)+info.package_id+std::string(1,'\0')+
		info.expansion_id+std::string(1,'\0')+info.payload_sha256;
	if (Hash(canonical)!=info.id) return Invalid("composition ID does not match canonical identity");
	OpenedCoreComposition opened;opened.info=info;
	OpenedCoreExpansion expansion;
	Error error=OpenExpansion(roots,request.expansion_path,&expansion);
	if (!error.ok()) return error;
	ManifestReader reader(expansion.manifest_bytes);
	if (reader.HasBoundaryPatch() &&
		(socket!="fes.expansion.coleco-bus" || !reader.ColecoBoundaryPatch()))
		return Invalid("expansion manifest must use canonical JSON");
	std::string cart_hash,device,map,recipe,revision,build,package,shell,slot;
	std::uint64_t size=0,format=0,major=0,minor=0;
	if (!reader.Text("cart_sha256",&cart_hash) || !reader.Number("cart_size",&size) ||
		!reader.Text("device",&device) || !reader.Number("format",&format) || !reader.Text("map",&map) ||
		!reader.Text("recipe_sha256",&recipe) || !reader.Text("revision",&revision) ||
		!reader.Text("shell_build_id",&build) || !reader.Text("shell_package_id",&package) ||
		!reader.Text("shell_sha256",&shell) || !reader.Text("slot",&slot) ||
		!reader.Number("slot_major",&major) || !reader.Number("slot_minor",&minor) || !reader.End())
		return Invalid("expansion manifest must use canonical JSON");
	if (!Hex(cart_hash,64) || !Hex(recipe,64) || !Hex(revision,40) || !Hex(build,32) ||
		format!=1 || device!="5CSEBA6U23I7" || descriptor.target.device!=device ||
		!(slot==socket && ((slot=="fes.expansion.zx81-bus" && ((map=="fes.zx81-bus.socket/1" && major==1) ||
				(map=="fes.zx81-bus.socket/2" && major==2))) ||
			(slot=="fes.expansion.coleco-bus" && ((map=="fes.coleco-bus.socket/1" && major==1) ||
				(map=="fes.coleco-bus.socket/2" && major==2))))) || minor!=0 ||
		(major==2 && reader.HasBoundaryPatch()) ||
		major!=socket_major ||
		size<40408 || size!=expansion.cart.size() || package!=base.package_id ||
		build!=descriptor.build.id || shell!=descriptor.payload.sha256)
		return Invalid("expansion manifest does not match the supported bus and frozen shell");
	if (Hash(std::string("fes-expansion-v1\0",17)+expansion.manifest_bytes)!=info.expansion_id)
		return Invalid("expansion ID does not match canonical manifest");
	expansion.cart_sha256=cart_hash;
	opened.expansions.push_back(std::move(expansion));
	error=OpenLinkedPayload(roots,request.payload_path,info.payload_size,&opened.payload);
	if (!error.ok()) return error;
	error=RecheckCoreComposition(opened);
	if (!error.ok()) return error;
	*output=std::move(opened);return {};
}
} }
