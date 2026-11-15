package nativefabric

import (
 "bytes"
 "context"
 "strings"
 "testing"
)

func TestRetainedSourceCannotSupplyInitialTapePublication(t *testing.T) {
 barriers,calls:=0,0
 backing:=make([]byte,8<<20)
 a,err:=NewRetainedProgramArena(0x94800000,backing,func()error{barriers++;return nil})
 if err!=nil {t.Fatal(err)}
 region,err:=a.Reserve([]byte{1,2,3,4});if err!=nil{t.Fatal(err)}
 source,binding:=bindingFixture(t);header,err:=encodeProgramBinding(source,binding)
 if err!=nil{t.Fatal(err)}
 if err=region.PublishConfiguration(header);err!=nil{t.Fatal(err)}
 if err=region.Release();err!=nil{t.Fatal(err)}
 if !a.ready || a.failed || !a.retained || a.active!=region {t.Fatal("completed retained source required")}
 before:=bytes.Clone(backing);barriers=0
 scope,request:=tapePublicationRequest(917)
 _,err=a.PublishInitialTape(context.Background(),transferHashCaller(func([7]uint64)([4]uint64,error){calls++;return [4]uint64{},nil}),scope,request)
 if err==nil || !strings.Contains(err.Error(),"initial tape arena unavailable, outstanding or consumed") {t.Fatal("actual board failure did not reproduce",err)}
 if barriers!=0 || calls!=0 || !bytes.Equal(before,backing) || a.failed || !a.ready || a.active!=region {t.Fatal("refusal changed retained source or reached firmware",barriers,calls)}
 t.Log("reproduced completed retained configuration -> tape rejection; zero barriers/SMC calls; entire source and metadata unchanged")
}
