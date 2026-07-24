// Qualcomm GENI serial engine I2C master driver (polled, FIFO mode)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"errors"
	"fmt"

	"github.com/usbarmory/tamago/internal/reg"
)

// GENI serial engine I2C registers and command encoding, offsets from
// the SE base (drivers/i2c/busses/i2c-qcom-geni.c, geni-se.h)
const (
	SE_I2C_TX_TRANS_LEN = 0x26c
	SE_I2C_RX_TRANS_LEN = 0x270
	SE_I2C_SCL_COUNTERS = 0x278

	// M_CMD0 opcodes
	I2C_WRITE = 0x1
	I2C_READ  = 0x2

	// M_CMD0 parameters
	STOP_STRETCH  = 1 << 2 // hold the bus: no stop, next command repeated-starts
	SLV_ADDR_SHFT = 9

	// M_IRQ_STATUS error bits (M_GP_IRQ_n carry the I2C error codes)
	M_CMD_OVERRUN   = 1 << 1
	M_ILLEGAL_CMD   = 1 << 2
	M_I2C_NACK      = 1 << 10
	M_I2C_BUS_PROTO = 1 << 12
	M_I2C_ARB_LOST  = 1 << 13

	i2cErrMask = M_CMD_OVERRUN | M_ILLEGAL_CMD | M_I2C_NACK |
		M_I2C_BUS_PROTO | M_I2C_ARB_LOST

	GENI_SE_PROTO_I2C = 3

	// i2cPollSpins bounds register polling: ~60 ms of MMIO reads, ample
	// for any 100 kHz transaction (a 16-byte transfer is ~1.5 ms) while
	// keeping a full 112-address bus scan interactive.
	i2cPollSpins = 200000

	// SER_M_CLK_CFG divider and SE_I2C_SCL_COUNTERS values for 100 kHz
	// SCL from the 19.2 MHz XO (geni_i2c_clk_map_19p2mhz)
	i2cClkDiv = 7
	i2cTHigh  = 10
	i2cTLow   = 12
	i2cTCycle = 26
)

// GENII2C represents a GENI serial engine in I2C master mode.
type GENII2C struct {
	Base uint32

	// Proto is the serial engine protocol found at Init (3 = I2C).
	Proto uint32
}

func (s *GENII2C) poll(mask uint32) (status uint32, ok bool) {
	for i := 0; i < i2cPollSpins; i++ {
		status = reg.Read(s.Base + SE_GENI_M_IRQ_STATUS)

		if status&mask != 0 {
			return status, true
		}
	}

	return status, false
}

func (s *GENII2C) abort() {
	reg.Write(s.Base+SE_GENI_M_CMD_CTRL_REG, GENI_CMD_ABORT)
	s.poll(M_CMD_ABORT_DONE)
	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
}

// Probe returns the serial engine protocol and clock configuration
// without touching engine state, for bring-up diagnostics.
func (s *GENII2C) Probe() (proto, clkCfg, sclCounters uint32) {
	proto = (reg.Read(s.Base+GENI_FW_REVISION_RO) >> FW_REV_PROTOCOL_SHFT) & FW_REV_PROTOCOL_MASK
	clkCfg = reg.Read(s.Base + GENI_SER_M_CLK_CFG)
	sclCounters = reg.Read(s.Base + SE_I2C_SCL_COUNTERS)

	return
}

// Init prepares the serial engine for polled FIFO-mode I2C master
// operation at 100 kHz. The SE firmware must already hold the I2C
// protocol, and the SE's GCC branch must be running (EnableSEClock).
//
// Unlike the boot-chain-configured SPI/UART engines, the serial clock
// configuration (GENI_SER_M_CLK_CFG, in the sub-0x200 GENI4_CFG region)
// is written here when unset: such writes are safe once the SE clock
// runs (U-Boot does the same on this engine), while writes to a gated
// engine hang the bus and ramdump the SoC.
func (s *GENII2C) Init() error {
	s.Proto = (reg.Read(s.Base+GENI_FW_REVISION_RO) >> FW_REV_PROTOCOL_SHFT) & FW_REV_PROTOCOL_MASK

	if s.Proto != GENI_SE_PROTO_I2C {
		return fmt.Errorf("serial engine protocol %d, want %d (I2C)", s.Proto, GENI_SE_PROTO_I2C)
	}

	// FIFO mode, no GSI events, no interrupt signaling (polled)
	reg.Write(s.Base+SE_GENI_DMA_MODE_EN, 0)
	reg.Write(s.Base+SE_GSI_EVENT_EN, 0)
	reg.Write(s.Base+SE_GENI_M_IRQ_EN, 0)
	reg.Write(s.Base+SE_GENI_S_IRQ_EN, 0)
	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(s.Base+SE_GENI_S_IRQ_CLEAR, 0xffffffff)

	// one byte per FIFO word, both directions
	reg.Write(s.Base+SE_GENI_BYTE_GRAN, 0)
	reg.Write(s.Base+SE_GENI_TX_PACKING_CFG0, packing1x8)
	reg.Write(s.Base+SE_GENI_TX_PACKING_CFG1, 0)
	reg.Write(s.Base+SE_GENI_RX_PACKING_CFG0, packing1x8)
	reg.Write(s.Base+SE_GENI_RX_PACKING_CFG1, 0)

	// 100 kHz SCL: serial clock divider and high/low/cycle counters
	if clk := reg.Read(s.Base + GENI_SER_M_CLK_CFG); clk != (i2cClkDiv<<CLK_DIV_SHFT | SER_CLK_EN) {
		reg.Write(s.Base+GENI_SER_M_CLK_CFG, i2cClkDiv<<CLK_DIV_SHFT|SER_CLK_EN)
	}

	reg.Write(s.Base+SE_I2C_SCL_COUNTERS, i2cTHigh<<20|i2cTLow<<10|i2cTCycle)

	reg.Write(s.Base+SE_GENI_TX_WATERMARK, 1)

	return nil
}

// cmdErr maps M_IRQ error status to an error.
func cmdErr(status uint32) error {
	switch {
	case status&M_I2C_NACK != 0:
		return errors.New("NACK")
	case status&M_I2C_ARB_LOST != 0:
		return errors.New("arbitration lost")
	case status&M_I2C_BUS_PROTO != 0:
		return errors.New("bus protocol error")
	case status&(M_CMD_OVERRUN|M_ILLEGAL_CMD) != 0:
		return fmt.Errorf("command error (status %#x)", status)
	}

	return nil
}

// Write transmits buf to the 7-bit slave address. With stop false the
// bus is held for a repeated start by the next command.
func (s *GENII2C) Write(addr uint8, buf []byte, stop bool) error {
	n := len(buf)

	if n == 0 || n > 16 {
		return errors.New("transfer length must be 1-16 bytes")
	}

	param := uint32(addr) << SLV_ADDR_SHFT

	if !stop {
		param |= STOP_STRETCH
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(s.Base+SE_I2C_TX_TRANS_LEN, uint32(n))
	reg.Write(s.Base+SE_GENI_M_CMD0, I2C_WRITE<<M_OPCODE_SHFT|param)

	if _, ok := s.poll(M_TX_FIFO_WM | i2cErrMask); !ok {
		s.abort()
		return errors.New("timeout awaiting TX FIFO")
	}

	for _, c := range buf[:n] {
		reg.Write(s.Base+SE_GENI_TX_FIFO, uint32(c))
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, M_TX_FIFO_WM)

	status, ok := s.poll(M_CMD_DONE | i2cErrMask)

	if err := cmdErr(status); err != nil {
		s.abort()
		return err
	}

	if !ok {
		s.abort()
		return errors.New("timeout awaiting command completion")
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)

	return nil
}

// Read receives len(buf) bytes from the 7-bit slave address. With stop
// false the bus is held for a repeated start by the next command.
func (s *GENII2C) Read(addr uint8, buf []byte, stop bool) error {
	n := len(buf)

	if n == 0 || n > 16 {
		return errors.New("transfer length must be 1-16 bytes")
	}

	param := uint32(addr) << SLV_ADDR_SHFT

	if !stop {
		param |= STOP_STRETCH
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(s.Base+SE_I2C_RX_TRANS_LEN, uint32(n))
	reg.Write(s.Base+SE_GENI_M_CMD0, I2C_READ<<M_OPCODE_SHFT|param)

	for i := 0; i < n; i++ {
		if !s.pollRx() {
			status := reg.Read(s.Base + SE_GENI_M_IRQ_STATUS)
			s.abort()

			if err := cmdErr(status); err != nil {
				return err
			}

			return errors.New("timeout awaiting RX FIFO")
		}

		buf[i] = byte(reg.Read(s.Base + SE_GENI_RX_FIFO))
	}

	status, ok := s.poll(M_CMD_DONE | i2cErrMask)

	if err := cmdErr(status); err != nil {
		s.abort()
		return err
	}

	if !ok {
		s.abort()
		return errors.New("timeout awaiting command completion")
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)

	return nil
}

func (s *GENII2C) pollRx() bool {
	for i := 0; i < i2cPollSpins; i++ {
		if reg.Read(s.Base+SE_GENI_RX_FIFO_STATUS)&RX_FIFO_WC_MASK != 0 {
			return true
		}

		if reg.Read(s.Base+SE_GENI_M_IRQ_STATUS)&i2cErrMask != 0 {
			return false
		}
	}

	return false
}

// ReadReg performs a register read: a one-byte register-address write,
// a repeated start, then an n-byte read.
func (s *GENII2C) ReadReg(addr, regAddr uint8, buf []byte) error {
	if err := s.Write(addr, []byte{regAddr}, false); err != nil {
		return err
	}

	return s.Read(addr, buf, true)
}
