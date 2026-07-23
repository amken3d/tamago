// BCM2835 SoC Arasan SDHCI (EMMC) driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// SDHCI registers, offsets from the controller base
// (BCM2835 ARM Peripherals and SD Host Controller Specification 3.0;
// the Arasan core allows 32-bit register access only).
const (
	EMMC_BASE = 0x300000

	SDHCI_ARG2        = 0x00
	SDHCI_BLKSIZECNT  = 0x04
	SDHCI_ARG1        = 0x08
	SDHCI_CMDTM       = 0x0c
	SDHCI_RESP0       = 0x10
	SDHCI_RESP1       = 0x14
	SDHCI_RESP2       = 0x18
	SDHCI_RESP3       = 0x1c
	SDHCI_DATA        = 0x20
	SDHCI_STATUS      = 0x24
	SDHCI_CONTROL0    = 0x28
	SDHCI_CONTROL1    = 0x2c
	SDHCI_INTERRUPT   = 0x30
	SDHCI_IRPT_MASK   = 0x34
	SDHCI_IRPT_EN     = 0x38
	SDHCI_CONTROL2    = 0x3c
	SDHCI_SLOTISR_VER = 0xfc

	// STATUS (present state)
	STATUS_CMD_INHIBIT = 1 << 0
	STATUS_DAT_INHIBIT = 1 << 1

	// CONTROL0
	CONTROL0_BUS_POWER = 0xf00 // 3.3V + power on

	// CONTROL1
	CONTROL1_SRST_DATA   = 1 << 26
	CONTROL1_SRST_CMD    = 1 << 25
	CONTROL1_SRST_HC     = 1 << 24
	CONTROL1_DATA_TOUNIT = 16 // 4 bits
	CONTROL1_CLK_EN      = 1 << 2
	CONTROL1_CLK_STABLE  = 1 << 1
	CONTROL1_CLK_INTLEN  = 1 << 0

	// INTERRUPT
	INT_CMD_DONE  = 1 << 0
	INT_DATA_DONE = 1 << 1
	INT_WRITE_RDY = 1 << 4
	INT_READ_RDY  = 1 << 5
	INT_ERR       = 1 << 15
	INT_CTO_ERR   = 1 << 16
	INT_CCRC_ERR  = 1 << 17
	INT_CEND_ERR  = 1 << 18
	INT_CBAD_ERR  = 1 << 19
	INT_DTO_ERR   = 1 << 20
	INT_DCRC_ERR  = 1 << 21
	INT_DEND_ERR  = 1 << 22
	INT_ERR_MASK  = 0xffff0000

	// CMDTM fields (transfer mode [15:0], command [31:16])
	TM_DIR_READ     = 1 << 4
	TM_BLKCNT_EN    = 1 << 1
	TM_MULTI_BLOCK  = 1 << 5
	CMD_RSPNS_NONE  = 0 << 16
	CMD_RSPNS_136   = 1 << 16
	CMD_RSPNS_48    = 2 << 16
	CMD_RSPNS_48B   = 3 << 16
	CMD_CRCCHK_EN   = 1 << 19
	CMD_IXCHK_EN    = 1 << 20
	CMD_ISDATA      = 1 << 21
	CMD_INDEX_SHIFT = 24
)

// VideoCore clock IDs (Mailbox property interface)
const VC_CLOCK_ID_EMMC = 0x1

// Response types for (*SDHCI).Command.
const (
	// RspNone for commands without response (CMD0)
	RspNone = iota
	// Rsp136 for R2 (136-bit) responses
	Rsp136
	// Rsp48 for R1/R5/R6 responses (48-bit, CRC and index checked)
	Rsp48
	// Rsp48Busy for R1b responses (48-bit with busy)
	Rsp48Busy
	// Rsp48NoCheck for R3/R4 responses (48-bit, no CRC or index check)
	Rsp48NoCheck
)

// SDHCI represents an SD host controller instance.
type SDHCI struct {
	// controller base offset from the peripheral base
	Base uint32

	// BaseClockHz is the controller input clock (queried from the
	// VideoCore when left zero).
	BaseClockHz uint32

	// Divider is the last programmed 10-bit clock divider
	// (SD clock = BaseClockHz / (2 * Divider)).
	Divider uint32

	initialized bool
}

// SDHCI1 is the Arasan eMMC controller. On stock Raspberry Pi firmware
// configurations its pins (SD1: GPIO 34-39, ALT3) route to the onboard
// wireless chip, while the SD card slot is served by the SDHOST
// controller.
var SDHCI1 = &SDHCI{
	Base: EMMC_BASE,
}

func (hc *SDHCI) reg(off uint32) uint32 {
	return PeripheralAddress(hc.Base + off)
}

// regDelay spaces consecutive register writes: the Arasan integration
// loses back-to-back writes that arrive within a few core clock cycles
// of each other.
func regDelay() {
	time.Sleep(10 * time.Microsecond)
}

// EMMCClockRate returns the EMMC base clock rate from the VideoCore.
func EMMCClockRate() uint32 {
	buf := make([]byte, VC_CLOCK_GET_RATE_LEN)
	binary.LittleEndian.PutUint32(buf, VC_CLOCK_ID_EMMC)

	res := exchangeSingleTagMessage(VC_CLOCK_GET_RATE, buf)

	if len(res) < 8 {
		return 0
	}

	return binary.LittleEndian.Uint32(res[4:])
}

// Init resets the host controller and configures the bus clock at the
// argument rate (typically 400 kHz for card identification).
func (hc *SDHCI) Init(hz uint32) error {
	if hc.BaseClockHz == 0 {
		hc.BaseClockHz = EMMCClockRate()
	}

	if hc.BaseClockHz == 0 {
		return fmt.Errorf("could not determine EMMC base clock")
	}

	// software reset, host controller
	reg.Set(hc.reg(SDHCI_CONTROL1), 24)
	regDelay()

	if !reg.WaitFor(10*time.Millisecond, hc.reg(SDHCI_CONTROL1), 24, 1, 0) {
		return fmt.Errorf("host controller reset timeout")
	}

	// maximum data timeout
	reg.SetN(hc.reg(SDHCI_CONTROL1), CONTROL1_DATA_TOUNIT, 0xf, 0xe)
	regDelay()

	// bus power: the Arasan card power is hardwired on Raspberry Pi
	// boards, but the controller still gates its output drivers on the
	// power control register
	reg.Write(hc.reg(SDHCI_CONTROL0), CONTROL0_BUS_POWER)
	regDelay()

	if err := hc.SetClock(hz); err != nil {
		return err
	}

	// enable all status bits, no interrupt signals (polled operation)
	reg.Write(hc.reg(SDHCI_IRPT_EN), 0)
	regDelay()
	reg.Write(hc.reg(SDHCI_IRPT_MASK), 0xffffffff)
	regDelay()
	reg.Write(hc.reg(SDHCI_INTERRUPT), 0xffffffff)
	regDelay()

	hc.initialized = true

	return nil
}

// Version returns the host controller specification version.
func (hc *SDHCI) Version() uint32 {
	return (reg.Read(hc.reg(SDHCI_SLOTISR_VER)) >> 16) & 0xff
}

// LineState returns the CMD line level and the DAT[3:0] line levels
// from the present state register: with a powered card and pull-ups all
// lines idle high.
func (hc *SDHCI) LineState() (cmd bool, dat uint32) {
	s := reg.Read(hc.reg(SDHCI_STATUS))
	return s&(1<<24) != 0, (s >> 20) & 0xf
}

// SetClock programs the SD bus clock using the SDHCI 3.0 10-bit divided
// clock mode.
func (hc *SDHCI) SetClock(hz uint32) error {
	if hz == 0 {
		return fmt.Errorf("invalid clock rate")
	}

	// disable the bus clock
	reg.Clear(hc.reg(SDHCI_CONTROL1), 2)
	regDelay()

	// freq = base / (2 * div), rounded up so we never exceed hz
	div := (hc.BaseClockHz + 2*hz - 1) / (2 * hz)

	if div > 0x3ff {
		div = 0x3ff
	}

	hc.Divider = div

	c1 := reg.Read(hc.reg(SDHCI_CONTROL1))
	c1 &^= 0xffff & ^uint32(CONTROL1_CLK_EN|CONTROL1_CLK_STABLE|CONTROL1_CLK_INTLEN)
	c1 |= (div & 0xff) << 8
	c1 |= ((div >> 8) & 0x3) << 6
	c1 |= CONTROL1_CLK_INTLEN

	reg.Write(hc.reg(SDHCI_CONTROL1), c1)
	regDelay()

	if !reg.WaitFor(10*time.Millisecond, hc.reg(SDHCI_CONTROL1), 1, 1, 1) {
		return fmt.Errorf("clock did not stabilize")
	}

	reg.Set(hc.reg(SDHCI_CONTROL1), 2)
	regDelay()

	return nil
}

// recoverLines resets the CMD and DAT state machines after a command
// error: the controller latches the inhibit bits until the lines are
// software reset (SD Host Controller spec 3.10.1).
func (hc *SDHCI) recoverLines() {
	reg.Set(hc.reg(SDHCI_CONTROL1), 25) // SRST_CMD
	reg.Set(hc.reg(SDHCI_CONTROL1), 26) // SRST_DATA
	regDelay()

	reg.WaitFor(10*time.Millisecond, hc.reg(SDHCI_CONTROL1), 25, 1, 0)
	reg.WaitFor(10*time.Millisecond, hc.reg(SDHCI_CONTROL1), 26, 1, 0)

	reg.Write(hc.reg(SDHCI_INTERRUPT), 0xffffffff)
	regDelay()
}

// Command issues an SD/SDIO command and returns RESP0..RESP3.
func (hc *SDHCI) Command(index uint32, arg uint32, rsp int) (resp [4]uint32, err error) {
	if !reg.WaitFor(100*time.Millisecond, hc.reg(SDHCI_STATUS), 0, 1, 0) {
		hc.recoverLines()

		if !reg.WaitFor(10*time.Millisecond, hc.reg(SDHCI_STATUS), 0, 1, 0) {
			return resp, fmt.Errorf("CMD%d: command line busy", index)
		}
	}

	if rsp == Rsp48Busy {
		if !reg.WaitFor(100*time.Millisecond, hc.reg(SDHCI_STATUS), 1, 1, 0) {
			return resp, fmt.Errorf("CMD%d: data line busy", index)
		}
	}

	// clear pending status
	reg.Write(hc.reg(SDHCI_INTERRUPT), 0xffffffff)
	regDelay()

	cmdtm := index << CMD_INDEX_SHIFT

	switch rsp {
	case RspNone:
		cmdtm |= CMD_RSPNS_NONE
	case Rsp136:
		cmdtm |= CMD_RSPNS_136 | CMD_CRCCHK_EN
	case Rsp48:
		cmdtm |= CMD_RSPNS_48 | CMD_CRCCHK_EN | CMD_IXCHK_EN
	case Rsp48Busy:
		cmdtm |= CMD_RSPNS_48B | CMD_CRCCHK_EN | CMD_IXCHK_EN
	case Rsp48NoCheck:
		cmdtm |= CMD_RSPNS_48
	}

	reg.Write(hc.reg(SDHCI_ARG1), arg)
	regDelay()
	reg.Write(hc.reg(SDHCI_CMDTM), cmdtm)

	var status uint32

	deadline := time.Now().Add(200 * time.Millisecond)

	for {
		status = reg.Read(hc.reg(SDHCI_INTERRUPT))

		if status&(INT_CMD_DONE|INT_ERR) != 0 {
			break
		}

		if time.Now().After(deadline) {
			hc.recoverLines()
			return resp, fmt.Errorf("CMD%d: timeout waiting for completion (status %#08x)", index, status)
		}

		time.Sleep(10 * time.Microsecond)
	}

	// acknowledge
	reg.Write(hc.reg(SDHCI_INTERRUPT), status)

	if status&INT_ERR_MASK != 0 {
		hc.recoverLines()
		return resp, fmt.Errorf("CMD%d: error status %#08x%s", index, status, decodeErrors(status))
	}

	resp[0] = reg.Read(hc.reg(SDHCI_RESP0))
	resp[1] = reg.Read(hc.reg(SDHCI_RESP1))
	resp[2] = reg.Read(hc.reg(SDHCI_RESP2))
	resp[3] = reg.Read(hc.reg(SDHCI_RESP3))

	return resp, nil
}

func decodeErrors(status uint32) (s string) {
	for _, e := range []struct {
		bit  uint32
		name string
	}{
		{INT_CTO_ERR, "cmd-timeout"},
		{INT_CCRC_ERR, "cmd-crc"},
		{INT_CEND_ERR, "cmd-end-bit"},
		{INT_CBAD_ERR, "cmd-index"},
		{INT_DTO_ERR, "data-timeout"},
		{INT_DCRC_ERR, "data-crc"},
		{INT_DEND_ERR, "data-end-bit"},
	} {
		if status&e.bit != 0 {
			s += " " + e.name
		}
	}

	return
}

// ---- SDIO (SD specifications part E1) command helpers ----

// GoIdle issues CMD0 (GO_IDLE_STATE).
func (hc *SDHCI) GoIdle() error {
	_, err := hc.Command(0, 0, RspNone)
	return err
}

// IOSendOpCond issues CMD5 (IO_SEND_OP_COND) with the argument operating
// conditions and returns the R4 response: card ready, number of I/O
// functions, memory present flag and I/O OCR.
func (hc *SDHCI) IOSendOpCond(ocr uint32) (ready bool, functions uint32, memory bool, ioOCR uint32, err error) {
	resp, err := hc.Command(5, ocr, Rsp48NoCheck)

	if err != nil {
		return
	}

	r := resp[0]

	return r&(1<<31) != 0, (r >> 28) & 0x7, r&(1<<27) != 0, r & 0xffffff, nil
}

// SendRelativeAddr issues CMD3 (SEND_RELATIVE_ADDR) and returns the
// card's relative address.
func (hc *SDHCI) SendRelativeAddr() (rca uint32, err error) {
	resp, err := hc.Command(3, 0, Rsp48)

	if err != nil {
		return 0, err
	}

	return resp[0] >> 16, nil
}

// SelectCard issues CMD7 (SELECT_CARD) for the argument relative address.
func (hc *SDHCI) SelectCard(rca uint32) error {
	_, err := hc.Command(7, rca<<16, Rsp48Busy)
	return err
}

// IORWExtended issues CMD53 (IO_RW_EXTENDED): a byte-mode multi-byte
// read (write == false) or write to the argument function and register
// address, with address increment when incr is set. len(buf) must be
// 1..512 and is transferred as a single data block.
func (hc *SDHCI) IORWExtended(write bool, fn uint32, addr uint32, incr bool, buf []byte) error {
	n := len(buf)

	if n == 0 || n > 512 {
		return fmt.Errorf("CMD53: invalid transfer size %d", n)
	}

	count := uint32(n)

	if count == 512 {
		count = 0 // byte mode encodes 512 as 0
	}

	arg := fn<<28 | (addr&0x1ffff)<<9 | count

	if write {
		arg |= 1 << 31
	}

	if incr {
		arg |= 1 << 26
	}

	if !reg.WaitFor(100*time.Millisecond, hc.reg(SDHCI_STATUS), 1, 1, 0) {
		hc.recoverLines()
		return fmt.Errorf("CMD53: data line busy")
	}

	reg.Write(hc.reg(SDHCI_BLKSIZECNT), 1<<16|uint32(n))
	regDelay()

	cmdtm := uint32(53)<<CMD_INDEX_SHIFT | CMD_RSPNS_48 | CMD_CRCCHK_EN | CMD_IXCHK_EN | CMD_ISDATA

	if !write {
		cmdtm |= TM_DIR_READ
	}

	if !reg.WaitFor(100*time.Millisecond, hc.reg(SDHCI_STATUS), 0, 1, 0) {
		hc.recoverLines()
		return fmt.Errorf("CMD53: command line busy")
	}

	reg.Write(hc.reg(SDHCI_INTERRUPT), 0xffffffff)
	regDelay()
	reg.Write(hc.reg(SDHCI_ARG1), arg)
	regDelay()
	reg.Write(hc.reg(SDHCI_CMDTM), cmdtm)

	// command phase
	if !hc.waitInt(INT_CMD_DONE, 200*time.Millisecond) {
		hc.recoverLines()
		return fmt.Errorf("CMD53: command timeout")
	}

	// data phase, single block PIO
	rdy := uint32(INT_WRITE_RDY)

	if !write {
		rdy = INT_READ_RDY
	}

	if !hc.waitInt(rdy, 200*time.Millisecond) {
		hc.recoverLines()
		return fmt.Errorf("CMD53: data ready timeout")
	}

	reg.Write(hc.reg(SDHCI_INTERRUPT), rdy)

	words := (n + 3) / 4

	for w := 0; w < words; w++ {
		if write {
			var v uint32

			for b := 0; b < 4; b++ {
				if i := w*4 + b; i < n {
					v |= uint32(buf[i]) << (8 * b)
				}
			}

			reg.Write(hc.reg(SDHCI_DATA), v)
		} else {
			v := reg.Read(hc.reg(SDHCI_DATA))

			for b := 0; b < 4; b++ {
				if i := w*4 + b; i < n {
					buf[i] = byte(v >> (8 * b))
				}
			}
		}
	}

	if !hc.waitInt(INT_DATA_DONE, 500*time.Millisecond) {
		hc.recoverLines()
		return fmt.Errorf("CMD53: data completion timeout")
	}

	reg.Write(hc.reg(SDHCI_INTERRUPT), 0xffffffff)

	return nil
}

// waitInt polls the interrupt status register for mask or an error.
func (hc *SDHCI) waitInt(mask uint32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for {
		status := reg.Read(hc.reg(SDHCI_INTERRUPT))

		if status&INT_ERR_MASK != 0 {
			return false
		}

		if status&mask != 0 {
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(10 * time.Microsecond)
	}
}

// IORWDirect issues CMD52 (IO_RW_DIRECT): a single byte read (write ==
// false) or write to the argument function and register address.
func (hc *SDHCI) IORWDirect(write bool, fn uint32, addr uint32, val byte) (byte, error) {
	arg := fn<<28 | (addr&0x1ffff)<<9 | uint32(val)

	if write {
		arg |= 1 << 31
	}

	resp, err := hc.Command(52, arg, Rsp48)

	if err != nil {
		return 0, err
	}

	if flags := (resp[0] >> 8) & 0xff; flags&0xcb != 0 {
		return 0, fmt.Errorf("CMD52 fn %d addr %#x: response flags %#02x", fn, addr, flags)
	}

	return byte(resp[0] & 0xff), nil
}
