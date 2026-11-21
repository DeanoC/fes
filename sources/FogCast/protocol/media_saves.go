package protocol

import "github.com/DeanoC/FogCast/corepackage"

// DescriptorMediaWrite reports a package that binds medium as a writable
// floppy: the ST drive A, or the PC/XT drive A on ABI 1.1 and later. The
// base interface and its write companion must both be required.
func DescriptorMediaWrite(d corepackage.Descriptor, medium RuntimeContract) bool {
	base, write, pcxt, pcxtWrite := false, false, false, false
	for _, i := range d.Interfaces {
		if !i.Required {
			continue
		}
		if i.ID == AtariStFloppyInterface().ID && i.Major == 1 && i.Minor == 0 {
			base = true
		}
		if i.ID == AtariStFloppyWriteInterface().ID && i.Major == 1 && i.Minor == 0 {
			write = true
		}
		if i.ID == PcxtFloppyInterface().ID && i.Major == 1 && i.Minor == 0 {
			pcxt = true
		}
		if i.ID == PcxtFloppyWriteInterface().ID && i.Major == 1 && i.Minor == 0 {
			pcxtWrite = true
		}
	}
	return ComputerMediaABI(d.ABI.ID, d.ABI.Major, d.ABI.Minor) && ((medium == AtariStFloppyInterface() && base && write) || (medium == PcxtFloppyInterface() && d.ABI.Minor >= ComputerExtendedMinor && pcxt && pcxtWrite))
}

// CoreMediaSaves reports whether what the computer writes to this media role
// is kept: a writable floppy, or the pc386 hard disk, whose changes are kept
// per game. Every other unit only reads its medium. A ZX81 SAVE makes a new
// cassette and leaves the one in the drive unchanged, so its cassette does
// not save either.
func CoreMediaSaves(d corepackage.Descriptor, c CoreMediaCapability) bool {
	if c.Interface == AtaDiskInterface() {
		return c.Role == HardDiskRole && DeclaresAtaDisk(d)
	}
	return DescriptorMediaWrite(d, c.Interface)
}
