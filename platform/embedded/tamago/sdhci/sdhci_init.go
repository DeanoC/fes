// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

func (d *Device) initialize(config Config) error {
	d.expectedHost = baseHost33V
	d.busWidth4 = false
	d.io.Write32(signalEnable, 0)
	d.io.Write32(clock, 1<<24) // SDHCI software reset all, including DMA state
	if err := d.wait(clock, 1<<24, 0, false); err != nil {
		return err
	}
	if d.io.Read32(capabilities)&cap33V == 0 {
		return fmt.Errorf("sdhci: 3.3V bus power unsupported")
	}
	d.io.Write32(signalEnable, 0)
	d.io.Write32(statusEnable, 0xffff8033)
	d.io.Write32(host, 0) // 1-bit, default speed, bus power off
	if err := d.pause(10 * time.Millisecond); err != nil {
		return err
	}
	d.io.Write32(host, 7<<9) // select 3.3V before enabling bus power
	d.io.Write32(host, baseHost33V)
	if err := d.pause(time.Millisecond); err != nil {
		return err
	}
	// SDHCI divided-clock mode: divisor encoding 0 means /1, otherwise /2N.
	divisor := (uint64(config.InputClockHz) + 799999) / 800000
	if divisor == 0 {
		divisor = 1
	}
	d.identClock = uint32(divisor)<<8 | 14<<16
	d.dataClock = d.identClock
	d.dataClockHz = uint32(uint64(config.InputClockHz) / (2 * divisor))
	d.io.Write32(clock, d.identClock|1)
	if err := d.wait(clock, 2, 2, false); err != nil {
		return err
	}
	if config.PrepareClock != nil {
		if err := config.PrepareClock(); err != nil {
			return fmt.Errorf("sdhci: prepare controller clock: %w", err)
		}
		if d.io.Read32(clock)&(7|7<<24) != 3 || d.io.Read32(signalEnable) != 0 {
			return fmt.Errorf("sdhci: clock or interrupt state changed during platform preparation")
		}
	}
	d.io.Write32(clock, d.io.Read32(clock)|4)
	if err := d.pause(time.Millisecond); err != nil {
		return err
	} // >=74 startup clocks
	if err := d.issue(0, 0, responseNone, noData); err != nil {
		return err
	}
	if err := d.issue(8, 0x1aa, responseR7, noData); err != nil {
		return err
	}
	if d.io.Read32(response)&0xfff != 0x1aa {
		return fmt.Errorf("sdhci: unsupported CMD8 voltage/echo")
	}
	deadline := d.io.Now().Add(time.Second)
	ready := false
	for n := 0; n < 100; n++ {
		if err := d.issue(55, 0, responseR1, noData); err != nil {
			return err
		}
		if d.io.Read32(response)&(1<<5) == 0 {
			return fmt.Errorf("sdhci: card refused APP_CMD")
		}
		// HCS, 3.2-3.4V; no 1.8V switch or extended-power request.
		if err := d.issue(41, 1<<30|0x00300000, responseR3, noData); err != nil {
			return err
		}
		ocr := d.io.Read32(response)
		if ocr&(1<<31) != 0 {
			if ocr&(1<<30) == 0 || ocr&0x00300000 == 0 {
				return fmt.Errorf("sdhci: non-SDHC or incompatible OCR")
			}
			ready = true
			break
		}
		if !d.io.Now().Before(deadline) {
			break
		}
		if err := d.pause(10 * time.Millisecond); err != nil {
			return err
		}
	}
	if !ready {
		return fmt.Errorf("sdhci: card power-up timeout")
	}
	if err := d.issue(2, 0, responseR2, noData); err != nil {
		return err
	}
	// R2 strips the final CRC/end byte and shifts the remaining bits down.
	top := d.io.Read32(response + 12)
	d.cid[0], d.cid[1], d.cid[2] = byte(top>>16), byte(top>>8), byte(top)
	binary.BigEndian.PutUint32(d.cid[3:7], d.io.Read32(response+8))
	binary.BigEndian.PutUint32(d.cid[7:11], d.io.Read32(response+4))
	binary.BigEndian.PutUint32(d.cid[11:15], d.io.Read32(response))
	var nonzero, notFF byte
	for _, value := range d.cid {
		nonzero |= value
		notFF |= ^value
	}
	if nonzero == 0 || notFF == 0 {
		return fmt.Errorf("sdhci: invalid card CID")
	}
	if err := d.issue(3, 0, responseR6, noData); err != nil {
		return err
	}
	r6 := d.io.Read32(response)
	d.rca = uint16(r6 >> 16)
	if d.rca == 0 || r6&0xe000 != 0 {
		return fmt.Errorf("sdhci: invalid card RCA/R6 %#x", r6)
	}
	if err := d.issue(9, uint32(d.rca)<<16, responseR2, noData); err != nil {
		return err
	}
	// SDHCI R2 registers hold CSD[127:8] shifted down by 8 bits.
	csdTop := d.io.Read32(response + 12)
	d.csd[0], d.csd[1], d.csd[2] = byte(csdTop>>16), byte(csdTop>>8), byte(csdTop)
	binary.BigEndian.PutUint32(d.csd[3:7], d.io.Read32(response+8))
	binary.BigEndian.PutUint32(d.csd[7:11], d.io.Read32(response+4))
	binary.BigEndian.PutUint32(d.csd[11:15], d.io.Read32(response))
	if (csdTop>>22)&3 != 1 {
		return fmt.Errorf("sdhci: unsupported CSD structure")
	}
	csize := (d.io.Read32(response+4) >> 8) & 0x3fffff
	// CSD[13] PERM_WRITE_PROTECT and [12] TMP_WRITE_PROTECT, shifted down
	// by eight bits in SDHCI's R2 response registers like the other CSD fields.
	d.cardWriteProtected = d.io.Read32(response)&(1<<5|1<<4) != 0
	// CSD[103:96] is R2 register 2's high byte after SDHCI's eight-bit
	// shift. SDHC/SDXC Default Speed advertises TRAN_SPEED 0x32 (25 MHz).
	d.defaultSpeed = byte(d.io.Read32(response+8)>>24) == 0x32
	d.sectors = (uint64(csize) + 1) * 1024
	if err := d.issue(7, uint32(d.rca)<<16, responseR1Busy, noData); err != nil {
		return err
	}
	return d.ready()
}

func (d *Device) configureADMA() error {
	if !d.config.UseADMA2 {
		return nil
	}
	caps := d.io.Read32(capabilities)
	eng := d.adma
	allocated := false
	if eng == nil {
		var err error
		eng, err = newADMAEngine(d.config.DMAPool, caps)
		if err != nil {
			return err
		}
		allocated = true
	}
	desired := d.expectedHost | eng.hostBits
	d.io.Write32(host, desired)
	if d.io.Read32(host) != desired {
		if allocated {
			_ = eng.close()
		}
		return errors.New("sdhci: ADMA2 host control readback differs")
	}
	d.adma = eng
	d.expectedHost = desired
	return nil
}

func (d *Device) readSCR() ([8]byte, error) {
	if d.hasSCR {
		return d.scr, nil
	}
	// Query 64-bit SCR via CMD55 + ACMD51 (8-byte PIO read).
	if err := d.issue(55, uint32(d.rca)<<16, responseR1, noData); err != nil {
		return [8]byte{}, err
	}
	if d.io.Read32(response)&(1<<5) == 0 {
		return [8]byte{}, errors.New("sdhci: card refused APP_CMD for ACMD51")
	}
	if err := d.issueTransfer(51, 0, responseR1, readData, 1, 8, false); err != nil {
		return [8]byte{}, err
	}
	if err := d.wait(status, 1<<5, 1<<5, true); err != nil {
		return [8]byte{}, err
	}
	var scr [8]byte
	binary.LittleEndian.PutUint32(scr[0:4], d.io.Read32(dataPort))
	binary.LittleEndian.PutUint32(scr[4:8], d.io.Read32(dataPort))
	d.io.Write32(status, 1<<5)
	if err := d.wait(status, 2, 2, true); err != nil {
		return [8]byte{}, err
	}
	d.io.Write32(status, 2)
	d.scr = scr
	d.hasSCR = true
	return scr, nil
}

func (d *Device) selectBusWidth() error {
	if !d.config.BusWidth4Bit {
		return nil
	}
	scr, err := d.readSCR()
	if err != nil {
		return err
	}
	// SCR[51:48] is SD_BUS_WIDTHS (wire byte 1 low nibble); bit 2 (0x04) = 4-bit.
	if scr[1]&0x04 == 0 {
		return fmt.Errorf("sdhci: card SCR (%x) does not advertise 4-bit bus width", scr)
	}
	if err := d.issue(55, uint32(d.rca)<<16, responseR1, noData); err != nil {
		return err
	}
	if d.io.Read32(response)&(1<<5) == 0 {
		return errors.New("sdhci: card refused APP_CMD for ACMD6")
	}
	if err := d.issue(6, 2, responseR1, noData); err != nil {
		return err
	}
	desired := d.expectedHost | host4BitWidth
	d.io.Write32(host, desired)
	if d.io.Read32(host) != desired {
		return errors.New("sdhci: 4-bit host control readback differs")
	}
	d.expectedHost = desired
	d.busWidth4 = true
	return d.ready()
}

func (d *Device) selectCMD23() error {
	if !d.config.UseCMD23 {
		return nil
	}
	scr, err := d.readSCR()
	if err != nil {
		return err
	}
	// SCR[33] (wire byte 3 bit 1) advertises CMD23 (SET_BLOCK_COUNT) support.
	if scr[3]&0x02 == 0 {
		return fmt.Errorf("sdhci: card SCR (%x) does not advertise CMD23 support", scr)
	}
	d.cmd23 = true
	return nil
}

func (d *Device) issueCMD6(arg uint32) ([64]byte, error) {
	var st [64]byte
	if err := d.issueTransfer(6, arg, responseR1, readData, 1, 64, false); err != nil {
		return st, err
	}
	if err := d.wait(status, 1<<5, 1<<5, true); err != nil {
		return st, err
	}
	for i := 0; i < 64; i += 4 {
		binary.LittleEndian.PutUint32(st[i:i+4], d.io.Read32(dataPort))
	}
	d.io.Write32(status, 1<<5)
	if err := d.wait(status, 2, 2, true); err != nil {
		return st, err
	}
	d.io.Write32(status, 2)
	return st, nil
}

func (d *Device) switchHighSpeed() error {
	if d.io.Read32(capabilities)&capHighSpeed == 0 {
		return errors.New("sdhci: controller does not advertise High-Speed support")
	}
	if !d.defaultSpeed {
		return errors.New("sdhci: card does not declare default-speed base timing")
	}
	scr, err := d.readSCR()
	if err != nil {
		return err
	}
	// SD Physical Layer v1.10+ (SD_SPEC >= 1) is required for CMD6 Switch Function.
	if scr[0]&0x0f < 1 {
		return fmt.Errorf("sdhci: card SCR (%x) does not support CMD6 High-Speed", scr)
	}
	// Mode 0 check: verify Access Mode Group 1 Function 1 (High Speed, 25 MB/s)
	// is supported and advertised current fits within board supply budget.
	maxCurrentLimit := d.config.HighSpeedMaxCurrentMA
	if maxCurrentLimit == 0 {
		maxCurrentLimit = defaultHighSpeedMaxCurrMA
	}
	checkStatus, err := d.issueCMD6(0x00fffff1)
	if err != nil {
		return err
	}
	checkCurr := binary.BigEndian.Uint16(checkStatus[0:2])
	if checkCurr == 0 || checkCurr > maxCurrentLimit ||
		binary.BigEndian.Uint16(checkStatus[12:14])&(1<<1) == 0 ||
		checkStatus[16]&0x0f != 1 {
		return fmt.Errorf("sdhci: card CMD6 High-Speed check refused (current=%dmA limit=%dmA)", checkCurr, maxCurrentLimit)
	}
	// Mode 1 switch: commit Group 1 Function 1.
	switchStatus, err := d.issueCMD6(0x80fffff1)
	if err != nil {
		return err
	}
	switchCurr := binary.BigEndian.Uint16(switchStatus[0:2])
	if switchCurr == 0 || switchCurr > maxCurrentLimit || switchStatus[16]&0x0f != 1 {
		return fmt.Errorf("sdhci: card failed CMD6 High-Speed switch (current=%dmA limit=%dmA)", switchCurr, maxCurrentLimit)
	}
	// SD Physical Layer §4.3.10 requires at least 8 card clocks after the Mode 1
	// CMD6 status block before configuring switched host timing or issuing another command.
	switchDelay := time.Millisecond
	if d.dataClockHz > 0 {
		eightClocks := (8*time.Second + time.Duration(d.dataClockHz) - 1) / time.Duration(d.dataClockHz)
		if eightClocks > switchDelay {
			switchDelay = eightClocks
		}
	}
	if err := d.pause(switchDelay); err != nil {
		return err
	}
	desired := d.expectedHost | hostHighSpeed
	d.io.Write32(host, desired)
	if d.io.Read32(host) != desired {
		return errors.New("sdhci: High-Speed host control readback differs")
	}
	d.expectedHost = desired
	d.highSpeed = true
	return nil
}

func (d *Device) selectTransferClock() error {
	if d.config.TransferClockHz == 0 {
		return nil
	}
	if d.config.TransferClockHz == 50000000 {
		if err := d.switchHighSpeed(); err != nil {
			return err
		}
	} else if !d.defaultSpeed {
		return errors.New("sdhci: card does not declare default-speed 25 MHz")
	}
	divisor := uint64(0)
	if d.config.InputClockHz > d.config.TransferClockHz {
		denominator := 2 * uint64(d.config.TransferClockHz)
		divisor = (uint64(d.config.InputClockHz) + denominator - 1) / denominator
	}
	selected := uint32(divisor)<<8 | 14<<16
	if err := d.programClock(d.identClock|7, selected, true); err != nil {
		return err
	}
	if err := d.ready(); err != nil {
		return err
	}
	d.dataClock = selected
	d.dataClockHz = d.config.InputClockHz
	if divisor != 0 {
		d.dataClockHz = uint32(uint64(d.config.InputClockHz) / (2 * divisor))
	}
	return nil
}

// Change only the SDHCI divider while the command/data engines are idle.
// Keep the firmware-owned input/PHY, 3.3V, configured bus/DMA mode and IRQ policy.
func (d *Device) programClock(expected, selected uint32, enable bool) error {
	value := d.io.Read32(clock)
	if value&clockModeMask != expected || d.io.Read32(host) != d.expectedHost ||
		d.io.Read32(signalEnable) != 0 || d.io.Read32(command)&1 != 0 || d.io.Read32(status)&statusErrors != 0 {
		return errors.New("sdhci: unexpected mode before clock transition")
	}
	if err := d.wait(present, busy, 0, true); err != nil {
		return err
	}
	d.io.Write32(clock, value&^4)
	if d.io.Read32(clock)&4 != 0 {
		return errors.New("sdhci: card clock did not stop")
	}
	if err := d.wait(present, busy, 0, true); err != nil {
		return err
	}
	d.io.Write32(clock, value&^7)
	if d.io.Read32(clock)&1 != 0 {
		return errors.New("sdhci: internal clock did not stop")
	}
	d.io.Write32(clock, selected|1)
	if err := d.wait(clock, 2, 2, true); err != nil {
		return err
	}
	if d.io.Read32(clock)&clockModeMask != selected|3 {
		return errors.New("sdhci: divider/stable readback differs")
	}
	if enable {
		d.io.Write32(clock, selected|5)
		if d.io.Read32(clock)&clockModeMask != selected|7 {
			return errors.New("sdhci: transfer clock readback differs")
		}
		return d.pause(time.Millisecond)
	}
	return nil
}
