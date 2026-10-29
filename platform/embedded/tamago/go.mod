module github.com/DeanoC/fes/platform/embedded/tamago

go 1.26.5

require (
	github.com/DeanoC/FogCast v0.0.0
	github.com/usbarmory/tamago v1.26.5
)

require (
	github.com/DeanoC/FogCast/appliance v0.0.0 // indirect
	github.com/DeanoC/misteross/expansion v0.0.0 // indirect
	github.com/geoffgarside/ber v1.1.0 // indirect
	github.com/hirochachacha/go-smb2 v1.1.0 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/yuin/gopher-lua v1.1.2 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/image v0.31.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/DeanoC/FogCast => ../../../sources/FogCast

replace github.com/DeanoC/FogCast/appliance => ../../../sources/FogCast/appliance

replace github.com/DeanoC/misteross/expansion => ../../../sources/misteross/expansion
