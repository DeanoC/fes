package main
import ("testing";"strings")
func TestReadOnlySDMounts(t *testing.T) {
 good:="/dev/root /.fes-bootstrap ext4 ro,relatime 0 0\n/dev/loop0 / ext4 ro 0 0\n/dev/mmcblk0p1 /media/fat vfat ro,relatime 0 0\n/dev/mmcblk0p3 /run/fogcast/fesdata3 ext4 ro,relatime 0 0\ntmpfs /run tmpfs rw 0 0\n"
 cases:=[]struct{name,raw string;ok bool}{
 {"expected",good,true},
 {"missing",strings.Replace(good,"/dev/mmcblk0p1 /media/fat vfat ro,relatime 0 0\n","",1),false},
 {"writable-data",strings.Replace(good,"fesdata3 ext4 ro","fesdata3 ext4 rw",1),false},
 {"writable-extra",good+"/dev/mmcblk0p2 /another ext4 rw 0 0\n",false},
 {"wrong-mountpoint",strings.Replace(good," /media/fat "," /wrong ",1),false},
 {"ambiguous-options",strings.Replace(good,"vfat ro,","vfat ro,rw,",1),false},
 {"read-only-alias",good+"/dev/mmcblk0p3 /media/fat/fogcast/cache ext4 ro 0 0\n",true},
 {"writable-alias",good+"/dev/mmcblk0p3 /media/fat/fogcast/cache ext4 rw 0 0\n",false},
 {"writable-bootstrap",strings.Replace(good,".fes-bootstrap ext4 ro",".fes-bootstrap ext4 rw",1),false},
 {"writable-root-image",strings.Replace(good,"/ ext4 ro","/ ext4 rw",1),false},
 }
 for _,c:=range cases {t.Run(c.name,func(t *testing.T){if (readOnlySDMounts([]byte(c.raw))==nil)!=c.ok {t.Fatal("incorrect mount admission")}})}
}
func TestWatchdogCanStop(t *testing.T) {
 good:="# CONFIG_WATCHDOG_NOWAYOUT is not set\nCONFIG_DW_WATCHDOG=y\n"
 cases:=[]struct{name,config,args string;ok bool}{
 {"pinned-running-config",good,"console=ttyS0,115200 root=/dev/mmcblk0p2 rw rootwait",true},
 {"nowayout-on","CONFIG_WATCHDOG_NOWAYOUT=y\nCONFIG_DW_WATCHDOG=y\n","",false},
 {"missing-policy","CONFIG_DW_WATCHDOG=y\n","",false},
 {"module-driver","# CONFIG_WATCHDOG_NOWAYOUT is not set\nCONFIG_DW_WATCHDOG=m\n","",false},
 {"contradictory-policy",good+"CONFIG_WATCHDOG_NOWAYOUT=y\n","",false},
 {"override",good,"dw_wdt.nowayout=1",false},
 {"alias-override",good,"dw-wdt.nowayout=1",false},
 {"bare-override",good,"dw_wdt.nowayout",false},
 }
 for _,c:=range cases {t.Run(c.name,func(t *testing.T){if (watchdogCanStop([]byte(c.config),[]byte(c.args))==nil)!=c.ok {t.Fatal("incorrect stoppability admission")}})}
}
