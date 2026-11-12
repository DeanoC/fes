// Native DE25 watchdog qualification; only Linux watchdog ioctls, no MMIO.
package main
import("bytes";"compress/gzip";"io";"crypto/sha256";"encoding/hex";"encoding/json";"fmt";"os";"os/exec";"path/filepath";"runtime";"strconv";"strings";"time";"unsafe";"golang.org/x/sys/unix")
const getSupport=0x80285700
const setTimeout=0xc0045706
const getTimeout=0x80045707
const getTimeLeft=0x8004570a
const keepalive=0x80045705
type support struct{Options,Firmware uint32;Identity [32]byte}
func must(err error){if err!=nil{panic(err)}}
func check(ok bool,what string){if !ok{panic(what)}}
func read(path string)[]byte{b,e:=os.ReadFile(path);must(e);return b}
func ioctl(fd int,request uintptr,arg unsafe.Pointer)error{_,_,e:=unix.Syscall(unix.SYS_IOCTL,uintptr(fd),request,uintptr(arg));if e!=0{return e};return nil}
func event(kind string,fields map[string]any){fields["event"]=kind;fields["utc"]=time.Now().UTC().Format(time.RFC3339Nano);must(json.NewEncoder(os.Stdout).Encode(fields))}
func main(){
 check(len(os.Args)==3 && (os.Args[1]=="ping-stop" || os.Args[1]=="expire"),"usage: ping-stop|expire EXPECTED_BOOT")
 check(os.Geteuid()==0 && runtime.GOARCH=="arm64","requires native AArch64 root")
 boot:=strings.TrimSpace(string(read("/proc/sys/kernel/random/boot_id")));check(boot==os.Args[2],"boot changed")
 check(bytes.Equal(read("/etc/fes-board"),[]byte("de25-nano\n")),"wrong image board")
 check(bytes.Equal(read("/sys/firmware/devicetree/base/model"),[]byte("SoCFPGA Agilex5 Terasic DE25-Nano\x00")),"wrong physical model")
 hash:=sha256.Sum256(read("/sys/kernel/notes"));check(hex.EncodeToString(hash[:])=="b0c2f6b1ec6f79bf12d1b1e724fd8a5f5ba36e7f32e302ddf5adda74124f1a8e","kernel notes differ")
 pid,err:=exec.Command("pidof","mister-runtime","mister-agent","fogcast-tenfoot","fes-kit-software").Output();exit,ok:=err.(*exec.ExitError);check(ok && exit.ExitCode()==1 && len(bytes.TrimSpace(pid))==0,"FES services must be stopped")
 overlays,err:=os.ReadDir("/sys/kernel/config/device-tree/overlays");must(err);check(len(overlays)==0,"baseline proof requires no FPGA overlays")
 check(bytes.Equal(read("/sys/class/watchdog/watchdog0/device/of_node/compatible"),[]byte("snps,dw-wdt\x00")),"watchdog compatible differs")
 check(bytes.Equal(read("/sys/class/watchdog/watchdog0/device/of_node/reg"),[]byte{0x10,0xd0,0x02,0,0,0,1,0}),"watchdog is not stock10d00200")
 driver,err:=filepath.EvalSymlinks("/sys/class/watchdog/watchdog0/device/driver");must(err);check(filepath.Base(driver)=="dw_wdt","wrong watchdog driver")
 dev:=strings.Split(strings.TrimSpace(string(read("/sys/class/watchdog/watchdog0/dev"))),":");check(len(dev)==2,"invalid sysfs device")
 major,err:=strconv.ParseUint(dev[0],10,32);must(err);minor,err:=strconv.ParseUint(dev[1],10,32);must(err)
 lease,err:=unix.Open("/run/fes-de25-computer.lock",unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC,0);must(err);defer unix.Close(lease);must(unix.Flock(lease,unix.LOCK_EX|unix.LOCK_NB))
 must(readOnlySDMounts(read("/proc/mounts")))
 cfgReader,err:=gzip.NewReader(bytes.NewReader(read("/proc/config.gz")));must(err)
 cfg,err:=io.ReadAll(cfgReader);must(err);must(cfgReader.Close())
 must(watchdogCanStop(cfg,read("/proc/cmdline")))
 fd,err:=unix.Open("/dev/watchdog0",unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC,0);must(err)
 closed:=false
 defer func(){if !closed{_,_=unix.Write(fd,[]byte("V"));_ =unix.Close(fd)}}()
 var st unix.Stat_t;must(unix.Fstat(fd,&st));check(st.Mode&unix.S_IFMT==unix.S_IFCHR && uint64(unix.Major(uint64(st.Rdev)))==major && uint64(unix.Minor(uint64(st.Rdev)))==minor,"watchdog descriptor differs from sysfs")
 var info support;must(ioctl(fd,getSupport,unsafe.Pointer(&info)));check(info.Options&0x100!=0,"magic-close unsupported")
 timeout:=int32(30);must(ioctl(fd,setTimeout,unsafe.Pointer(&timeout)));check(timeout>=15 && timeout<=120,"unexpected actual device timeout")
 var actual int32;must(ioctl(fd,getTimeout,unsafe.Pointer(&actual)));check(actual==timeout,"timeout readback differs")
 event("watchdog-open",map[string]any{"pid":os.Getpid(),"boot_id":boot,"identity":strings.TrimRight(string(info.Identity[:]),"\x00"),"options":info.Options,"timeout_seconds":actual,"mode":os.Args[1]})
 must(ioctl(fd,keepalive,nil))
 if os.Args[1]=="ping-stop"{
  var first int32;must(ioctl(fd,getTimeLeft,unsafe.Pointer(&first)));time.Sleep(2*time.Second);var left int32;must(ioctl(fd,getTimeLeft,unsafe.Pointer(&left)));check(first>0 && left>0 && left<first,"hardware timeleft did not count")
  must(ioctl(fd,keepalive,nil));n,err:=unix.Write(fd,[]byte("V"));must(err);check(n==1,"partial magic close");closed=true;must(unix.Close(fd))
  event("magic-close-completed",map[string]any{"initial_hardware_timeleft_seconds":first,"timeleft_seconds":left,"timeout_seconds":actual,"boot_id":boot})
  return
 }
 event("expiry-armed-no-more-pings",map[string]any{"timeout_seconds":actual,"boot_id":boot,"software_reboot_calls":0})
 time.Sleep(time.Duration(2*actual+15)*time.Second)
 event("expiry-failed-no-reset",map[string]any{"boot_id":boot,"timeout_seconds":actual})
 panic(fmt.Sprintf("hardware did not reset within %d seconds",2*actual+15))
}

func readOnlySDMounts(raw []byte) error {
 expected:=map[string]string{"/dev/root":"/.fes-bootstrap","/dev/loop0":"/","/dev/mmcblk0p1":"/media/fat","/dev/mmcblk0p3":"/run/fogcast/fesdata3"}
 seen:=map[string]bool{}
 for _,line:=range strings.Split(string(raw),"\n") {
  fields:=strings.Fields(line);if len(fields)<4 {continue}
  if !strings.HasPrefix(fields[0],"/dev/mmcblk0p") && fields[0]!="/dev/root" && fields[0]!="/dev/loop0" {continue}
  opts:=","+fields[3]+",";if !strings.Contains(opts,",ro,") || strings.Contains(opts,",rw,") {return fmt.Errorf("SD mount remains writable: %s",fields[0])}
  if path,required:=expected[fields[0]];required && fields[1]==path {seen[fields[0]]=true}
 }
 for device:=range expected {if !seen[device] {return fmt.Errorf("required SD mount missing: %s",device)}}
 return nil
}

func watchdogCanStop(config,commandLine []byte) error {
 lines:=map[string]bool{};for _,line:=range strings.Split(string(config),"\n") {lines[line]=true}
 if !lines["# CONFIG_WATCHDOG_NOWAYOUT is not set"] || lines["CONFIG_WATCHDOG_NOWAYOUT=y"] {return fmt.Errorf("running kernel does not prove stoppable watchdog")}
 if !lines["CONFIG_DW_WATCHDOG=y"] || lines["CONFIG_DW_WATCHDOG=m"] {return fmt.Errorf("watchdog driver must be built in")}
 for _,arg:=range strings.Fields(string(commandLine)) {if strings.HasPrefix(strings.ReplaceAll(arg,"-","_"),"dw_wdt.nowayout") {return fmt.Errorf("watchdog boot parameter override present")}}
 return nil
}
