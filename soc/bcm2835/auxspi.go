// BCM2835 auxiliary SPI (SPI1) master driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"errors"
	"sync"
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// The SoC's SPI1 is one of the two "Universal SPI Masters" inside the AUX
// block, alongside the mini-UART. It is a different peripheral from SPI0, with
// its own register layout, and it is exposed on GPIO16-21 (CE2, CE1, CE0,
// MISO, MOSI, SCLK) at ALT4.
//
// # The datasheet is wrong about this peripheral
//
// BCM2835 §2.3.4 contradicts itself for AUXSPI_STAT: it lists bits 11:5 as
// reserved and puts TX Full / TX Empty / RX Empty at bits 4/3/2, then two rows
// later places Busy at bit 6 and Bit count at 5:0 -- overlapping the bits it
// just called reserved. It also gives the wrong addresses for the IO and PEEK
// registers.
//
// The values below are the ones that work on silicon (and that Linux's
// spi-bcm2835aux uses in production). Deviations from the printed manual are
// marked. A driver written faithfully from the document initialises, transmits,
// and then hangs forever on a status bit that never sets.
const (
	AUXSPI1_BASE = 0x215080

	AUXSPI_CNTL0 = 0x00
	AUXSPI_CNTL1 = 0x04
	AUXSPI_STAT  = 0x08
	AUXSPI_PEEK  = 0x0c // datasheet says 0x14
	AUXSPI_IO    = 0x20 // datasheet says 0x10
	AUXSPI_TXHOLD = 0x30
)

// CNTL0 fields. These the manual gets right.
const (
	AUXSPI_CNTL0_SPEED_SHIFT = 20
	AUXSPI_CNTL0_CS_SHIFT    = 17
	AUXSPI_CNTL0_POSTINPUT   = 1 << 16
	AUXSPI_CNTL0_VAR_CS      = 1 << 15
	AUXSPI_CNTL0_VAR_WIDTH   = 1 << 14
	AUXSPI_CNTL0_ENABLE      = 1 << 11
	AUXSPI_CNTL0_IN_RISING   = 1 << 10
	AUXSPI_CNTL0_CLEAR_FIFO  = 1 << 9
	AUXSPI_CNTL0_OUT_RISING  = 1 << 8
	AUXSPI_CNTL0_INVERT_CLK  = 1 << 7
	AUXSPI_CNTL0_MSB_OUT     = 1 << 6

	// DOUT hold time, bits 13:12: 0, 1, 4 or 7 system clocks of extra MOSI
	// hold against the clock edge.
	AUXSPI_CNTL0_HOLD_0 = 0 << 12
	AUXSPI_CNTL0_HOLD_1 = 1 << 12
	AUXSPI_CNTL0_HOLD_4 = 2 << 12
	AUXSPI_CNTL0_HOLD_7 = 3 << 12
)

// auxSPIShift is where a transmitted byte sits in a FIFO word.
//
// The peripheral is ASYMMETRIC, which is the trap here: transmit data is
// left-justified to bit 31, while received data arrives right-justified in bits
// 7:0. Moving both ends together never works, and the manual does not say so --
// it offers only "shifted out starting with the MS bit (bit 15 or bit 11)",
// which is ambiguous and, for fixed-width 8-bit transfers, wrong.
//
// Determined empirically by looping MOSI back to MISO and sweeping the four
// byte positions (see ProbeShift): only <<24 produced data on the wire, and it
// returned in the low byte. Writing to any other offset gives a textbook clock
// and chip-select with a flat data line -- the engine shifts the zero bits
// sitting where the data was expected.
const auxSPIShift = 24

// CNTL1 fields.
const (
	AUXSPI_CNTL1_CS_HIGH_SHIFT = 8
	AUXSPI_CNTL1_MSB_IN        = 1 << 1
	AUXSPI_CNTL1_KEEP_INPUT    = 1 << 0
)

// STAT fields -- the corrected layout.
const (
	AUXSPI_STAT_TX_FULL  = 1 << 10 // datasheet says bit 4
	AUXSPI_STAT_TX_EMPTY = 1 << 9  // datasheet says bit 3
	AUXSPI_STAT_RX_FULL  = 1 << 8
	AUXSPI_STAT_RX_EMPTY = 1 << 7 // datasheet says bit 2
	AUXSPI_STAT_BUSY     = 1 << 6
)

// auxSPI is a Universal SPI Master.
type auxSPI struct {
	sync.Mutex

	base uint32
	cs   int

	// csPin, when set, is driven low around a transfer instead of relying on a
	// muxed hardware chip-select. A carrier is free to wire the link's select
	// to any pin, and driving it explicitly also makes the frame boundary
	// unambiguous: exactly one falling edge before the first byte and one
	// rising edge after the last, which is what a slave watching for a CS edge
	// needs to delimit a frame.
	csPin *GPIO
}

// SetCS makes Transfer drive the given pin as an active-low chip-select.
//
// Pass nil to go back to whatever the peripheral's own CE lines do. Note that
// Init does not mux the CE pins, so without this a transfer clocks data with no
// chip-select asserted at all -- the bytes go out and the slave never sees a
// frame.
func (hw *auxSPI) SetCS(pin *GPIO) {
	hw.Lock()
	defer hw.Unlock()

	if pin != nil {
		pin.Out()
		pin.High() // idle high: active-low select
	}

	hw.csPin = pin
}

// AUXSPI1 is the SoC's SPI1, on GPIO16-21.
var AUXSPI1 = &auxSPI{base: AUXSPI1_BASE}

func (hw *auxSPI) reg(off uint32) uint32 { return PeripheralAddress(hw.base + off) }

// stat reads the status register. The whole word is taken at once: the flags
// are checked in combination, and re-reading between them would sample
// different instants of a live FIFO.
func (hw *auxSPI) stat() uint32 { return reg.Read(hw.reg(AUXSPI_STAT)) }

// ErrSPITimeout is returned when a status flag never settles.
var ErrSPITimeout = errors.New("aux SPI: timeout waiting on status")

// spiTimeout bounds every wait. Even the slowest sensible clock moves a byte in
// well under a millisecond, so anything beyond this is a wedged peripheral or a
// misread status bit -- and spinning forever on one would take down whatever
// goroutine called Transfer, console included. Erring out lets the caller say
// what happened instead of the machine simply stopping.
const spiTimeout = 50 * time.Millisecond

// wait spins until want(status) is true, or gives up.
func (hw *auxSPI) wait(want func(uint32) bool) error {
	deadline := time.Now().Add(spiTimeout)

	for {
		if want(hw.stat()) {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrSPITimeout
		}
	}
}

// Init brings up SPI1 at the argument mode (0-3) and clock frequency, muxing
// GPIO19/20/21 (MISO, MOSI, SCLK) to ALT4.
//
// The chip-selects are NOT muxed. Three devices' worth of CE pins is rarely
// what a board wants, and a carrier is free to wire GPIO16-18 to something
// else; drive whichever chip-select you need as a plain GPIO. cs selects the
// pattern the peripheral drives on any CE pin that IS muxed, and is otherwise
// harmless.
func (hw *auxSPI) Init(mode int, hz uint32, cs int) error {
	hw.Lock()
	defer hw.Unlock()

	if mode < 0 || mode > 3 {
		return errors.New("invalid SPI mode")
	}
	if cs < 0 || cs > 2 {
		return errors.New("invalid chip select")
	}
	if hz == 0 || hz > coreFreq/2 {
		return errors.New("invalid SPI clock frequency")
	}

	// Enable the SPI1 half of the AUX block WITHOUT disturbing the mini-UART:
	// all three auxiliary peripherals share AUX_ENABLES, and a blind write here
	// takes the console down.
	reg.Set(PeripheralAddress(AUX_ENABLES), AUX_ENABLE_SPI1)

	for _, num := range []int{19, 20, 21} { // MISO, MOSI, SCLK
		gpio, err := NewGPIO(num)
		if err != nil {
			return err
		}
		if err = gpio.SelectFunction(GPIO_FN4); err != nil { // ALT4
			return err
		}
	}

	// spi_clk = core / (2 * (speed + 1))
	speed := coreFreq/(2*hz) - 1
	if speed > 0xfff {
		speed = 0xfff
	}

	cntl0 := speed<<AUXSPI_CNTL0_SPEED_SHIFT |
		uint32(cs)<<AUXSPI_CNTL0_CS_SHIFT |
		AUXSPI_CNTL0_ENABLE |
		AUXSPI_CNTL0_CLEAR_FIFO |
		AUXSPI_CNTL0_MSB_OUT |
		// Maximum MOSI hold. The manual warns that this peripheral's hold time
		// against the clock is very short "because the interface runs of fast
		// silicon", and that it causes considerable problems on SPI slaves. It
		// costs seven system clocks per bit and buys margin on a link we are
		// still bringing up; there is no reason to be miserly here.
		AUXSPI_CNTL0_HOLD_7 |
		// CS stays high a little longer between frames too, so a slave that
		// keys off the CS edge has time to notice it.
		8 // fixed 8-bit shifts

	// Clock polarity and phase.
	//
	// CPOL sets the idle level. CPHA=0 samples on the leading edge, so with an
	// idle-low clock that is the rising edge; CPHA=1 shifts out on the leading
	// edge instead. This mapping is DERIVED, not measured -- verify with
	// Loopback or a scope before trusting a mode other than 0.
	if mode&2 != 0 { // CPOL
		cntl0 |= AUXSPI_CNTL0_INVERT_CLK
	}
	if mode&1 != 0 { // CPHA
		cntl0 |= AUXSPI_CNTL0_OUT_RISING
	} else {
		cntl0 |= AUXSPI_CNTL0_IN_RISING
	}

	reg.Write(hw.reg(AUXSPI_CNTL0), cntl0)
	reg.Write(hw.reg(AUXSPI_CNTL1),
		AUXSPI_CNTL1_MSB_IN|7<<AUXSPI_CNTL1_CS_HIGH_SHIFT)

	// Release the FIFOs from reset now that the configuration is in place.
	reg.Write(hw.reg(AUXSPI_CNTL0), cntl0&^AUXSPI_CNTL0_CLEAR_FIFO)

	hw.cs = cs

	return nil
}

// Probe sends one byte and returns the RAW 32-bit receive word.
//
// It exists because the datasheet is unclear about where an 8-bit result sits
// in the FIFO entry. §2.3.4 says data is shifted out "starting with the MS bit
// (bit 15 or bit 11)", which implies the entry is justified to a 16- or 12-bit
// field rather than to bit 0 -- so taking the low byte of a read may be taking
// the wrong end. Rather than guess between three plausible alignments, show the
// whole word and read the answer off it.
func (hw *auxSPI) Probe(b byte) (uint32, error) {
	hw.Lock()
	defer hw.Unlock()

	if hw.csPin != nil {
		hw.csPin.Low()
	}

	for hw.stat()&AUXSPI_STAT_RX_EMPTY == 0 {
		reg.Read(hw.reg(AUXSPI_IO))
	}

	if err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_TX_FULL == 0 }); err != nil {
		hw.release()
		return 0, err
	}

	reg.Write(hw.reg(AUXSPI_IO), uint32(b)<<auxSPIShift)

	if err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_RX_EMPTY == 0 }); err != nil {
		hw.release()
		return 0, err
	}

	raw := reg.Read(hw.reg(AUXSPI_IO))

	err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_BUSY == 0 })
	hw.release()

	return raw, err
}

// Regs dumps the control and status registers, plus the words at both candidate
// data-register offsets.
//
// The manual and the Linux driver disagree about where AUX_SPI0_IO lives -- the
// document says +0x10, spi-bcm2835aux uses +0x20 -- and this driver had to pick
// one. Reading CNTL0 back is the test: if it returns what Init wrote, the base
// address and the register map are right and the fault is elsewhere. If it
// returns zeros, we are writing into empty space and every other conclusion
// drawn from this peripheral is void.
func (hw *auxSPI) Regs() (cntl0, cntl1, stat, io10, io20 uint32) {
	return reg.Read(hw.reg(AUXSPI_CNTL0)),
		reg.Read(hw.reg(AUXSPI_CNTL1)),
		reg.Read(hw.reg(AUXSPI_STAT)),
		reg.Read(hw.reg(0x10)),
		reg.Read(hw.reg(0x20))
}

// ProbeShift sends one byte placed at the given bit offset in the FIFO word and
// returns the raw receive word.
//
// Where an 8-bit datum belongs is the one thing about this peripheral that
// neither the manual nor a readback has settled: the document says data shifts
// out "starting with the MS bit (bit 15 or bit 11)", which is ambiguous, and two
// reasoned guesses have now produced a clean clock with a flat data line. With
// MOSI looped back to MISO the hardware can simply be asked -- whichever offset
// returns the byte is the right one.
func (hw *auxSPI) ProbeShift(b byte, shift uint) (uint32, error) {
	hw.Lock()
	defer hw.Unlock()

	if hw.csPin != nil {
		hw.csPin.Low()
	}

	for hw.stat()&AUXSPI_STAT_RX_EMPTY == 0 {
		reg.Read(hw.reg(AUXSPI_IO))
	}

	reg.Write(hw.reg(AUXSPI_IO), uint32(b)<<shift)

	err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_RX_EMPTY == 0 })
	if err != nil {
		hw.release()
		return 0, err
	}

	raw := reg.Read(hw.reg(AUXSPI_IO))

	_ = hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_BUSY == 0 })
	hw.release()

	return raw, nil
}

// Stat exposes the status register for diagnostics.
func (hw *auxSPI) Stat() uint32 { return hw.stat() }

// release deasserts the chip-select. A transfer that gives up part-way must
// still let it go, or the slave is left believing a frame is still open.
func (hw *auxSPI) release() {
	if hw.csPin != nil {
		hw.csPin.High()
	}
}

// Transfer clocks out tx and returns the bytes clocked in, full duplex.
//
// Bytes are shifted one at a time. The peripheral bursts at most 32 bits, and
// §2.3.3 handles longer streams through two transmit addresses: writing to
// TXHOLD keeps the chip-select asserted, writing to IO releases it at the end
// of the burst. So every byte but the last goes to TXHOLD, which keeps CS low
// across the whole frame.
//
// One byte per FIFO entry is deliberately the slow arrangement. The packed
// 32-bit variable-width mode moves four times the data per entry, and is worth
// having once this is known-good -- but the shift-length and chip-select
// handling get considerably fiddlier, and this is a bus whose own datasheet has
// already been shown to be wrong.
func (hw *auxSPI) Transfer(tx []byte) (rx []byte, err error) {
	hw.Lock()
	defer hw.Unlock()

	if len(tx) == 0 {
		return nil, nil
	}

	rx = make([]byte, len(tx))

	if hw.csPin != nil {
		hw.csPin.Low()
	}

	for i, b := range tx {
		// Drain anything stale so a leftover entry cannot be mistaken for this
		// byte's reply.
		for hw.stat()&AUXSPI_STAT_RX_EMPTY == 0 {
			reg.Read(hw.reg(AUXSPI_IO))
		}

		if err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_TX_FULL == 0 }); err != nil {
			hw.release()
			return rx, err
		}

		addr := hw.reg(AUXSPI_TXHOLD) // keep CS asserted
		if i == len(tx)-1 {
			addr = hw.reg(AUXSPI_IO) // last byte: release CS
		}

		reg.Write(addr, uint32(b)<<auxSPIShift)

		// Wait for the reply to this byte.
		if err := hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_RX_EMPTY == 0 }); err != nil {
			hw.release()
			return rx, err
		}

		// The receive word's alignment is not stated as plainly as the
		// transmit side's; Probe (and `link raw`) exist to show the whole word
		// so this can be read off the hardware rather than assumed.
		rx[i] = byte(reg.Read(hw.reg(AUXSPI_IO)))
	}

	// Let the module finish, including the minimum CS-high time, so a caller
	// that immediately reconfigures does not do so mid-transfer.
	err = hw.wait(func(s uint32) bool { return s&AUXSPI_STAT_BUSY == 0 })

	// Release the select only once the module is idle, so the rising edge lands
	// after the last bit rather than in the middle of it.
	if hw.csPin != nil {
		hw.csPin.High()
	}

	return rx, err
}
