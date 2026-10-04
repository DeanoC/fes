// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/media_data.hpp"
#include "native/sha256.hpp"
#include "daemon/json.hpp"
#include <iterator>
#include <map>
#include <cassert>
#include <fstream>
#include <unistd.h>
#include <sys/stat.h>
#if defined(__linux__)
#include <sys/syscall.h>
#include <cerrno>
static int fail_sync_kind = 0;
extern "C" int fsync(int descriptor)
{
    struct stat state {};
    if (fail_sync_kind && fstat(descriptor,&state)==0 &&
        ((fail_sync_kind==1 && S_ISREG(state.st_mode)) || (fail_sync_kind==2 && S_ISDIR(state.st_mode)))) {
        fail_sync_kind=0; errno=EIO; return -1;
    }
    return static_cast<int>(syscall(SYS_fsync,descriptor));
}
#endif
using namespace mister;
using namespace mister::native;
int main()
{
    MediaDataIdentity id{"fes.atari-st","atari-st-desktop",std::string(64,'a'),0};
    MediaDiskRecord record; record.identity=id; record.bytes.resize(kAtariStDiskBytes);
    for (unsigned at=0;at<record.bytes.size();++at) record.bytes[at]=static_cast<unsigned char>(at*17+(at>>9));
    std::vector<unsigned char> encoded;
    assert(EncodeMediaData(record,&encoded).ok());
    assert(encoded.size()==kAtariStDiskBytes+173);
    std::ifstream fixture("tests/fixtures/media-data-v1/atari-st.json");assert(fixture.good());
    const std::string fixture_bytes((std::istreambuf_iterator<char>(fixture)),std::istreambuf_iterator<char>());
    daemon::json::Value golden;std::string message;assert(daemon::json::ParseResponse(fixture_bytes,&golden,&message));
    std::map<std::string,daemon::json::Value> fields;for(const auto& pair:golden.object)fields[pair.first]=pair.second;
    const auto hex=[](const unsigned char* data,std::size_t size){std::string out;const char* digits="0123456789abcdef";for(std::size_t at=0;at<size;++at){out+=digits[data[at]>>4];out+=digits[data[at]&15];}return out;};
    const auto digest=[](const std::vector<unsigned char>& bytes){Sha256 hash;hash.Update(bytes.data(),bytes.size());return Sha256Hex(hash.Final());};
    assert(fields["core_id"].string_value==id.core_id&&fields["game_id"].string_value==id.game_id&&fields["base_media_id"].string_value==id.base_media_id);
    assert(fields["layout_id"].string_value=="fes.atari-st-floppy.image");
    assert(fields["payload_sha256"].string_value==digest(record.bytes));
    assert(fields["header_hex"].string_value==hex(encoded.data(),141));
    assert(fields["checksum_hex"].string_value==hex(encoded.data()+encoded.size()-32,32));
    assert(fields["revision"].string_value==digest(encoded));
    assert(fields["namespace"].string_value==MediaDataNamespace(id));
    // A valid checksum cannot turn another layout/version into this layout.
    for(unsigned at:{104u,106u,108u,110u,112u,116u}){
        auto wrong=encoded;wrong[at]^=1;Sha256 hash;hash.Update(wrong.data(),wrong.size()-32);const auto checksum=hash.Final();std::copy(checksum.begin(),checksum.end(),wrong.end()-32);
        MediaDiskRecord rejected;assert(DecodeMediaData(wrong,id,&rejected).code==ErrorCode::incompatible_data);
    }
    MediaDiskRecord decoded;
    assert(DecodeMediaData(encoded,id,&decoded).ok() && decoded.bytes==record.bytes && decoded.revision.size()==64);
    for (unsigned field=0;field<4;++field) {
        auto other=id;
        if(field==0) other.core_id="fes.other";
        if(field==1) other.game_id="atari-st-other";
        if(field==2) other.base_media_id=std::string(64,'b');
        if(field==3) other.unit=1;
        assert(!DecodeMediaData(encoded,other,&decoded).ok());
        assert(MediaDataNamespace(other)!=MediaDataNamespace(id));
    }
    for (unsigned at : {0u,104u,106u,108u,110u,112u,116u,4096u,737451u}) {
        auto bad=encoded; bad[at]^=1;
        assert(DecodeMediaData(bad,id,&decoded).code==ErrorCode::corrupt_data);
    }
    auto bad=encoded; bad.push_back(0); assert(!DecodeMediaData(bad,id,&decoded).ok());
    bad.pop_back(); bad.pop_back(); assert(!DecodeMediaData(bad,id,&decoded).ok());
    auto short_record=record; short_record.bytes.pop_back(); assert(!EncodeMediaData(short_record,&bad).ok());
    char tmp[]="/tmp/runtime-media-data-XXXXXX";
    const char* root=mkdtemp(tmp); assert(root);
    std::unique_ptr<MediaDataFile> file;
    assert(MediaDataFile::Open(root,id,&file).ok());
    assert(file->Read(&decoded).ok() && decoded.revision=="absent" && decoded.bytes.empty());
    assert(file->CheckWritable().ok());
    assert(file->Persist(record,"absent",&decoded).ok());
    const auto first=decoded.revision;
    assert(file->Persist(record,"absent",nullptr).code==ErrorCode::stale_revision);
    record.bytes[512]^=0x53;
#if defined(__linux__)
    fail_sync_kind=1;
    assert(file->Persist(record,first,nullptr).code==ErrorCode::save_failed);
    assert(file->Read(&decoded).ok() && decoded.revision==first);
    fail_sync_kind=2;
    assert(file->Persist(record,first,nullptr).code==ErrorCode::save_failed);
    assert(file->Read(&decoded).ok() && decoded.bytes==record.bytes && decoded.revision!=first);
#else
    assert(file->Persist(record,first,&decoded).ok());
#endif
    const auto second=decoded.revision;
    assert(file->Persist(record,second,&decoded).ok());
    // Reopen from another descriptor and reject corruption rather than
    // publishing defaults or overwriting it during a later save.
    std::unique_ptr<MediaDataFile> reopened;
    assert(MediaDataFile::Open(root,id,&reopened).ok());
    assert(reopened->Read(&decoded).ok() && decoded.bytes==record.bytes);
    const std::string path=std::string(root)+"/"+MediaDataNamespace(id)+"/record.bin";
    { std::ofstream corrupt(path,std::ios::binary|std::ios::trunc); corrupt << "bad"; }
    assert(reopened->Read(&decoded).code==ErrorCode::corrupt_data);
    assert(reopened->Persist(record,second,nullptr).code==ErrorCode::corrupt_data);
    assert(unlink(path.c_str())==0);
    assert(symlink("/dev/null",path.c_str())==0);
    assert(!reopened->Read(&decoded).ok());
    assert(!reopened->Persist(record,"absent",nullptr).ok());
    assert(unlink(path.c_str())==0);
    file.reset(); reopened.reset();
    assert(rmdir((std::string(root)+"/"+MediaDataNamespace(id)).c_str())==0);
    assert(rmdir(root)==0);
}
