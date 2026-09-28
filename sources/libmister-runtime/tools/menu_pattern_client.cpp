// SPDX-License-Identifier: GPL-3.0-or-later
#include "daemon/menu_frame_transport.hpp"
#include "daemon/json.hpp"
#include "native/generated/fes_application.hpp"
#include "native/menu_underflow.hpp"
#include <chrono>
#include <algorithm>
#include <cstring>
#include <fcntl.h>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <sys/mman.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>
using namespace mister::daemon;
using namespace mister::native::generated;
namespace {
const json::Value& Field(const json::Value& object,const char* name) {
 for(const auto& member:object.object)if(member.first==name)return member.second;
 throw std::runtime_error(std::string("missing response field: ")+name);
}
std::uint64_t Number(const json::Value& value) {
 if(value.type==json::Type::unsigned_integer)return value.unsigned_value;
 if(value.type==json::Type::integer&&value.integer_value>=0)return value.integer_value;
 throw std::runtime_error("invalid unsigned response value");
}
std::string Quote(const std::string& value) {
 std::string output="\"";const char* hex="0123456789abcdef";
 for(unsigned char c:value){
  if(c=='"'||c=='\\'){output+='\\';output+=c;}
  else if(c<32){output+="\\u00";output+=hex[c>>4];output+=hex[c&15];}
  else output+=c;
 }
 return output+'"';
}
void Check(const mister::Error& error){if(!error.ok())throw std::runtime_error(error.message);}
json::Value Response(const ReceivedFrame& frame) {
 json::Value value;std::string message;
 if(!json::ParseResponse(frame.line,&value,&message))throw std::runtime_error(message);
 const auto& ok=Field(value,"ok");
 if(ok.type!=json::Type::boolean||!ok.boolean_value)throw std::runtime_error("request rejected: "+frame.line);
 return value;
}
FrameDeadline Deadline(){return std::chrono::steady_clock::now()+std::chrono::seconds(60);}
class Connection {
public:
 explicit Connection(const std::string& path){
  sockaddr_un address{};address.sun_family=AF_UNIX;
  if(path.empty()||path.size()>=sizeof(address.sun_path))throw std::runtime_error("invalid socket path");
  std::memcpy(address.sun_path,path.c_str(),path.size()+1);
  fd_=socket(AF_UNIX,SOCK_STREAM|SOCK_CLOEXEC,0);if(fd_<0)throw std::runtime_error("create socket failed");
  if(connect(fd_,reinterpret_cast<sockaddr*>(&address),sizeof(address))<0){close(fd_);fd_=-1;throw std::runtime_error("connect socket failed");}
 }
 ~Connection(){if(fd_>=0)close(fd_);}
 int fd() const{return fd_;}
 Connection(const Connection&)=delete;
 Connection& operator=(const Connection&)=delete;
private:int fd_=-1;
};
json::Value Exchange(const std::string& socket,const std::string& request){
 Connection connection(socket);Check(SendFrame(connection.fd(),request,-1,Deadline()));
 ReceivedFrame response;Check(ReceiveFrame(connection.fd(),Deadline(),&response));
 if(!response.fds.empty())throw std::runtime_error("unexpected response descriptor");
 return Response(response);
}
void Paint(unsigned char* pixels,std::uint32_t sequence) {
 for(std::uint32_t y=0;y<FesApplicationMenuHeight;++y)for(std::uint32_t x=0;x<FesApplicationMenuWidth;++x){
  const auto offset=(y*FesApplicationMenuWidth+x)*4;
  const unsigned bar=x*6/FesApplicationMenuWidth;
  const bool alternate=sequence&1;
  unsigned char r=(bar==0||bar==3||bar==5)?255:0;
  unsigned char g=(bar==1||bar==3||bar==4)?255:0;
  unsigned char b=(bar==2||bar==4||bar==5)?255:0;
  if(alternate){r=255-r;g=255-g;b=255-b;}
  // Fine pixel/row boundaries and a 32-bit sequence marker across the top.
  if(y>=64&&((x%32)==0||(y%32)==0)){r=255;g=255;b=255;}
  if(y<64){const bool bit=(sequence>>(x/40))&1;r=bit?255:0;g=bit?255:0;b=bit?255:0;}
  if(x==FesApplicationMenuWidth-1){r=y&255;g=y>>8;b=sequence&255;}
  pixels[offset]=r;pixels[offset+1]=g;pixels[offset+2]=b;pixels[offset+3]=255;
 }
}
std::uint64_t Argument(const char* text,std::uint64_t maximum){
 const std::string value(text);std::size_t end=0;
 if(value.empty()||value.find_first_not_of("0123456789")!=std::string::npos)throw std::runtime_error("invalid numeric option");
 const auto number=std::stoull(value,&end);if(end!=value.size()||number>maximum)throw std::runtime_error("numeric option out of range");return number;
}
}
int main(int argc,char** argv){
 try {
  std::string socket="/run/mister-runtime.sock",package,id;
  std::uint64_t frames=120,seconds=0,interval=33;bool frame_option=false;
  for(int i=1;i<argc;++i){
   const std::string option=argv[i];
   if(option=="--help"){std::cout<<"menu-pattern-client [--socket PATH] [--package PATH --package-id ID] [--frames N | --seconds N] [--interval-ms N]\n";return 0;}
   if(i+1==argc)throw std::runtime_error("missing option value");
   const char* value=argv[++i];
   if(option=="--socket")socket=value;
   else if(option=="--package")package=value;
   else if(option=="--package-id")id=value;
   else if(option=="--frames"){frames=Argument(value,1000000);frame_option=true;if(!frames)throw std::runtime_error("frame count must be positive");}
   else if(option=="--seconds"){seconds=Argument(value,3600);if(!seconds)throw std::runtime_error("duration must be positive");}
   else if(option=="--interval-ms")interval=Argument(value,1000);
   else throw std::runtime_error("unknown option: "+option);
  }
  if(seconds&&frame_option)throw std::runtime_error("choose frame count or duration");
  if(package.empty()!=id.empty())throw std::runtime_error("package path and identity are required together");
  if(!package.empty())Exchange(socket,"{\"protocol\":2,\"operation\":\"configure_menu\",\"package_path\":"+Quote(package)+",\"package_id\":"+Quote(id)+"}");
  const auto status=Exchange(socket,"{\"protocol\":2,\"operation\":\"status\"}");const auto& menu=Field(status,"menu_display");
  const auto& available=Field(menu,"available");if(available.type!=json::Type::boolean||!available.boolean_value)throw std::runtime_error("idle menu unavailable");
  for(const auto& pair:{std::pair<const char*,std::uint64_t>{"width",FesApplicationMenuWidth},{"height",FesApplicationMenuHeight},{"stride",FesApplicationMenuStride},{"byte_count",FesApplicationMenuFrameBytes},{"slot_bytes",FesApplicationMenuSlotBytes}})
   if(Number(Field(menu,pair.first))!=pair.second)throw std::runtime_error("unsupported menu geometry");
  const auto generation=Number(Field(menu,"generation"));if(!generation)throw std::runtime_error("zero menu generation");
  auto last=Number(Field(menu,"displayed_sequence"));double total_ms=0,max_ms=0;std::uint64_t count=0,last_underflows=0;
  const auto start=std::chrono::steady_clock::now();auto tick=start;
  while(seconds?std::chrono::steady_clock::now()-start<std::chrono::seconds(seconds):count<frames){
   if(last>=std::numeric_limits<std::uint32_t>::max())throw std::runtime_error("menu sequence exhausted");
   Connection connection(socket);
   const auto begin="{\"protocol\":2,\"operation\":\"menu_frame_begin\",\"expected_generation\":"+std::to_string(generation)+",\"byte_count\":"+std::to_string(FesApplicationMenuFrameBytes)+"}";
   Check(SendFrame(connection.fd(),begin,-1,Deadline()));ReceivedFrame staging;Check(ReceiveFrame(connection.fd(),Deadline(),&staging));
   const auto begun=Response(staging);const auto& prepared=Field(begun,"menu_frame");
   if(staging.fds.size()!=1||Number(Field(prepared,"generation"))!=generation||Number(Field(prepared,"byte_count"))!=FesApplicationMenuFrameBytes||Field(prepared,"staging_format").string_value!="rgba8888")throw std::runtime_error("invalid staging response");
   const int fd=staging.fds[0];struct stat info{};
   if(fstat(fd,&info)<0||!S_ISREG(info.st_mode)||info.st_size!=FesApplicationMenuFrameBytes)throw std::runtime_error("invalid staging file");
   auto mapped=mmap(nullptr,FesApplicationMenuFrameBytes,PROT_READ|PROT_WRITE,MAP_SHARED,fd,0);
   if(mapped==MAP_FAILED)throw std::runtime_error("map staging failed");
   Paint(static_cast<unsigned char*>(mapped),static_cast<std::uint32_t>(last+1));
   if(munmap(mapped,FesApplicationMenuFrameBytes)<0||fcntl(fd,F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)<0)throw std::runtime_error("seal staging failed");
   const auto commit="{\"protocol\":2,\"operation\":\"menu_frame_commit\",\"generation\":"+std::to_string(generation)+",\"byte_count\":"+std::to_string(FesApplicationMenuFrameBytes)+"}";
   const auto submitted=std::chrono::steady_clock::now();Check(SendFrame(connection.fd(),commit,fd,Deadline()));
   ReceivedFrame completed;Check(ReceiveFrame(connection.fd(),Deadline(),&completed));const auto reply=Response(completed);const auto& displayed=Field(reply,"menu_frame");
   const auto underflows=Number(Field(displayed,"underflows"));
   if(!completed.fds.empty()||Number(Field(displayed,"generation"))!=generation||Number(Field(displayed,"displayed_sequence"))!=last+1||underflows>mister::native::kMenuUnderflowPresentCap)throw std::runtime_error("frame completion mismatch or underflow");
   last_underflows=underflows;
   const double ms=std::chrono::duration<double,std::milli>(std::chrono::steady_clock::now()-submitted).count();total_ms+=ms;max_ms=std::max(max_ms,ms);++last;++count;
   tick+=std::chrono::milliseconds(interval);std::this_thread::sleep_until(tick);
  }
  rusage usage{};if(getrusage(RUSAGE_SELF,&usage)<0)throw std::runtime_error("read client resource usage failed");
  const double cpu=usage.ru_utime.tv_sec+usage.ru_stime.tv_sec+(usage.ru_utime.tv_usec+usage.ru_stime.tv_usec)/1000000.0;
  const auto elapsed=std::chrono::duration<double>(std::chrono::steady_clock::now()-start).count();
  std::cout<<"frames="<<count<<" generation="<<generation<<" displayed_sequence="<<last<<" underflows="<<last_underflows<<" elapsed_s="<<elapsed<<" present_avg_ms="<<(count?total_ms/count:0)<<" present_max_ms="<<max_ms<<" client_cpu_s="<<cpu<<" client_maxrss_kib="<<usage.ru_maxrss<<"\n";
  return 0;
 } catch(const std::exception& error){std::cerr<<"menu-pattern-client: "<<error.what()<<"\n";return 1;}
}
