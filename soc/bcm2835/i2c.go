// BCM2835 SoC BSC (I2C) master driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"fmt"
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// BSC0 is the I2C controller on GPIO0 (SDA0) / GPIO1 (SCL0), and BSC1 the one
// on GPIO2 (SDA1) / GPIO3 (SCL1).
//
// GPIO0/1 are the HAT ID pins. On a board carrying an ID EEPROM to the HAT
// specification they are the only way to ask the hardware what it is, rather
// than being told by configuration that may have come from a different board.
const (
	BSC0_BASE = 0x205000
	BSC1_BASE = 0x804000

	bscC    = 0x00 // control
	bscS    = 0x04 // status
	bscDLEN = 0x08 // data length
	bscA    = 0x0c // slave address
	bscFIFO = 0x10 // data FIFO
	bscDIV  = 0x14 // clock divider

	bscCI2CEN = 1 << 15 // controller enable
	bscCST    = 1 << 7  // start transfer
	bscCCLEAR = 1 << 4  // clear FIFO
	bscCREAD  = 1 << 0  // read (else write)

	bscSCLKT = 1 << 9 // clock stretch timeout
	bscSERR  = 1 << 8 // ack error
	bscSRXD  = 1 << 5 // FIFO contains data
	bscSTXD  = 1 << 4 // FIFO accepts data
	bscSDONE = 1 << 1 // transfer done
)

// I2C is a BCM2835 BSC (I2C) master.
type I2C struct {
	offset   uint32 // BSCn_BASE
	sda, scl int    // the pins ALT0 puts the bus on
	base     uint32
	inited   bool
}

// I2C is a BCM2835 BSC (I2C) master.
//
// I2C0 is BSC0 on GPIO0/GPIO1 (the HAT ID pins) and I2C1 is BSC1 on
// GPIO2/GPIO3 (the general-purpose bus on the 40-way header).
var (
	I2C0 = &I2C{offset: BSC0_BASE, sda: 0, scl: 1}
	I2C1 = &I2C{offset: BSC1_BASE, sda: 2, scl: 3}
)

// Init muxes the pins and programs the bus clock (Hz).
func (b *I2C) Init(hz uint32) error {
	// Zero values keep the pre-existing behaviour: an I2C built by a caller
	// rather than taken from I2C0/I2C1 is BSC1 on GPIO2/3, as it always was.
	if b.offset == 0 {
		b.offset, b.sda, b.scl = BSC1_BASE, 2, 3
	}

	b.base = PeripheralAddress(b.offset)

	for _, pin := range []int{b.sda, b.scl} {
		g, err := NewGPIO(pin)
		if err != nil {
			return err
		}
		g.SelectFunction(GPIO_FN0) // ALT0
	}

	core := coreClockRate()
	if core == 0 {
		core = 250000000
	}
	div := core / hz
	if div < 2 {
		div = 2
	}
	reg.Write(b.base+bscDIV, div&0xfffe)

	b.inited = true
	return nil
}

func (b *I2C) waitDone(deadline time.Time) (uint32, error) {
	for {
		s := reg.Read(b.base + bscS)
		if s&bscSDONE != 0 {
			return s, nil
		}
		if s&bscSERR != 0 {
			return s, fmt.Errorf("i2c: no ack")
		}
		if s&bscSCLKT != 0 {
			return s, fmt.Errorf("i2c: clock stretch timeout")
		}
		if time.Now().After(deadline) {
			return s, fmt.Errorf("i2c: timeout (s=%#x)", s)
		}
	}
}

// Write sends data to the 7-bit slave address.
func (b *I2C) Write(addr uint8, data []byte) error {
	if !b.inited {
		return fmt.Errorf("i2c: not initialized")
	}
	reg.Write(b.base+bscA, uint32(addr))
	reg.Write(b.base+bscC, bscCCLEAR)
	reg.Write(b.base+bscDLEN, uint32(len(data)))
	reg.Write(b.base+bscS, bscSCLKT|bscSERR|bscSDONE) // clear status

	i := 0
	for ; i < len(data) && i < 16; i++ { // preload the 16-byte FIFO
		reg.Write(b.base+bscFIFO, uint32(data[i]))
	}
	reg.Write(b.base+bscC, bscCI2CEN|bscCST)

	deadline := time.Now().Add(100 * time.Millisecond)
	for i < len(data) {
		if reg.Read(b.base+bscS)&bscSTXD != 0 {
			reg.Write(b.base+bscFIFO, uint32(data[i]))
			i++
		} else if time.Now().After(deadline) {
			return fmt.Errorf("i2c: write FIFO timeout")
		}
	}
	_, err := b.waitDone(deadline)
	return err
}

// Read receives len(buf) bytes from the 7-bit slave address.
func (b *I2C) Read(addr uint8, buf []byte) error {
	if !b.inited {
		return fmt.Errorf("i2c: not initialized")
	}
	reg.Write(b.base+bscA, uint32(addr))
	reg.Write(b.base+bscC, bscCCLEAR)
	reg.Write(b.base+bscDLEN, uint32(len(buf)))
	reg.Write(b.base+bscS, bscSCLKT|bscSERR|bscSDONE)
	reg.Write(b.base+bscC, bscCI2CEN|bscCST|bscCREAD)

	deadline := time.Now().Add(100 * time.Millisecond)
	i := 0
	for i < len(buf) {
		s := reg.Read(b.base + bscS)
		if s&bscSRXD != 0 {
			buf[i] = byte(reg.Read(b.base + bscFIFO))
			i++
			continue
		}
		if s&bscSDONE != 0 {
			break
		}
		if s&bscSERR != 0 {
			return fmt.Errorf("i2c: no ack")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("i2c: read timeout (s=%#x)", s)
		}
	}
	for i < len(buf) && reg.Read(b.base+bscS)&bscSRXD != 0 {
		buf[i] = byte(reg.Read(b.base + bscFIFO))
		i++
	}
	if i < len(buf) {
		return fmt.Errorf("i2c: short read %d/%d", i, len(buf))
	}
	return nil
}

// WriteRead writes then reads (separate transactions), for register-pointer
// devices such as the ADS1x15.
func (b *I2C) WriteRead(addr uint8, w, r []byte) error {
	if err := b.Write(addr, w); err != nil {
		return err
	}
	return b.Read(addr, r)
}
