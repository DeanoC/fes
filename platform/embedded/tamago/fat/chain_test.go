package fat

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

type chainDisk struct {
	table []byte
	reads []uint64
	fail  uint64
}

func (d *chainDisk) SectorCount() uint64 { return 4096 }
func (d *chainDisk) ReadSectors(lba uint64, dst []byte) error {
	d.reads = append(d.reads, lba)
	if lba == d.fail {
		return errChainRead
	}
	copy(dst, d.table[(lba-8)*512:])
	return nil
}
func (d *chainDisk) WriteSectors(uint64, []byte) error { return block.ErrReadOnly }
func (d *chainDisk) Flush() error                      { return nil }

var errChainRead = errors.New("FAT sector unavailable")

func chainFixture(clusters []uint32) (*Volume, *chainDisk) {
	d := &chainDisk{table: make([]byte, 4096), fail: ^uint64(0)}
	for i, cluster := range clusters {
		next := uint32(0x0fffffff)
		if i+1 < len(clusters) {
			next = clusters[i+1]
		}
		binary.LittleEndian.PutUint32(d.table[cluster*4:], next)
	}
	return &Volume{device: d, fatStart: 8, clusterCount: 1024, sectorsPerCluster: 1}, d
}

func TestChainBoundsSectorReadsAndRetainsFragmentedOrder(t *testing.T) {
	sequential := make([]uint32, 300)
	for i := range sequential {
		sequential[i] = uint32(i + 2)
	}
	for _, test := range []struct {
		name     string
		clusters []uint32
		reads    []uint64
	}{
		{"sequential", sequential, []uint64{8, 9, 10}},
		{"fragmented", []uint32{2, 130, 3}, []uint64{8, 9, 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			v, d := chainFixture(test.clusters)
			got, err := v.chain(test.clusters[0], len(test.clusters)*512)
			if err != nil || !reflect.DeepEqual(got, test.clusters) || !reflect.DeepEqual(d.reads, test.reads) {
				t.Fatalf("chain %v, reads %v: %v", got, d.reads, err)
			}
		})
	}
}

func TestChainRevalidatesMediaOnEveryWalk(t *testing.T) {
	v, d := chainFixture([]uint32{2, 3, 4})
	if _, err := v.chain(2, 3*512); err != nil {
		t.Fatal(err)
	}
	// A later walk must see a newly cyclic chain even when its first FAT
	// sector is the same one read by the earlier successful open.
	binary.LittleEndian.PutUint32(d.table[3*4:], 2)
	if got, err := v.chain(2, 3*512); !errors.Is(err, ErrCorrupt) || got != nil {
		t.Fatalf("changed media accepted: %v %v", got, err)
	}
	if !reflect.DeepEqual(d.reads, []uint64{8, 8}) {
		t.Fatalf("previous walk supplied cached metadata: %v", d.reads)
	}
	d.fail = 8
	if got, err := v.chain(2, 3*512); !errors.Is(err, errChainRead) || got != nil {
		t.Fatalf("later failed read accepted: %v %v", got, err)
	}
}

func TestChainCorruptionBoundsAndReadFailureRemainTerminal(t *testing.T) {
	for _, test := range []struct {
		name string
		next uint32
	}{
		{"cycle", 2}, {"reserved", 0x0ffffff0}, {"bad", 0x0ffffff7},
		{"free", 0}, {"outside", 1026},
	} {
		t.Run(test.name, func(t *testing.T) {
			v, d := chainFixture([]uint32{2, 3})
			binary.LittleEndian.PutUint32(d.table[3*4:], test.next)
			if got, err := v.chain(2, 3*512); !errors.Is(err, ErrCorrupt) || got != nil {
				t.Fatalf("invalid chain accepted: %v %v", got, err)
			}
		})
	}
	v, _ := chainFixture([]uint32{2, 3, 4})
	if got, err := v.chain(2, 2*512); !errors.Is(err, ErrCorrupt) || got != nil {
		t.Fatalf("oversized chain accepted: %v %v", got, err)
	}
	v, d := chainFixture([]uint32{2, 130})
	d.fail = 9
	if got, err := v.chain(2, 2*512); !errors.Is(err, errChainRead) || got != nil || !reflect.DeepEqual(d.reads, []uint64{8, 9}) {
		t.Fatalf("partial chain/read failure accepted: %v %v %v", got, d.reads, err)
	}
}
