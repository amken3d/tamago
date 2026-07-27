// BCM2835 SoC PWM (hardware pulse-width modulation) driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"fmt"

	"github.com/usbarmory/tamago/internal/reg"
)

// The PWM0 block has two channels. Mark-space mode gives a standard fixed-
// frequency PWM (duty = DAT/RNG), suited to fans. Channel outputs are on
// GPIO12 (ch1, ALT0), GPIO13 (ch2, ALT0), GPIO18 (ch1, ALT5), GPIO19 (ch2,
// ALT5). The block is clocked from the clock manager's PWM clock.
const (
	PWM_BASE = 0x20c000

	pwmCTL  = 0x00
	pwmRNG1 = 0x10
	pwmDAT1 = 0x14
	pwmRNG2 = 0x20
	pwmDAT2 = 0x24

	pwmPWEN1 = 1 << 0 // channel 1 enable
	pwmMSEN1 = 1 << 7 // channel 1 mark-space mode
	pwmPWEN2 = 1 << 8 // channel 2 enable
	pwmMSEN2 = 1 << 15

	cmPWMCTL = CM_BASE + 0xa0
	cmPWMDIV = CM_BASE + 0xa4
)

// PWM is the BCM2835 PWM0 block.
type PWM struct {
	base   uint32
	rng    uint32
	inited bool
}

// PWM0 is the PWM controller.
var PWM0 = &PWM{}

// Init programs the PWM clock so each channel runs at freqHz with rng steps of
// duty resolution (the source clock is freqHz*rng, from the 19.2 MHz
// oscillator), and sets the channel ranges. Call EnableChannel per output.
func (p *PWM) Init(freqHz, rng uint32) error {
	if freqHz == 0 || rng == 0 {
		return fmt.Errorf("pwm: bad freq/range")
	}
	src := freqHz * rng
	if src > OSC_FREQ {
		return fmt.Errorf("pwm: freq*range %d exceeds oscillator %d", src, OSC_FREQ)
	}
	p.base = PeripheralAddress(PWM_BASE)
	p.rng = rng

	ctl := PeripheralAddress(cmPWMCTL)
	div := PeripheralAddress(cmPWMDIV)

	// stop the PWM clock before reprogramming
	reg.Write(ctl, CM_PASSWD|CM_CTL_SRC_OSC)
	for reg.Read(ctl)&CM_CTL_BUSY != 0 {
	}

	divi := OSC_FREQ / src
	if divi < 1 {
		divi = 1
	}
	if divi > 4095 {
		divi = 4095
	}
	reg.Write(div, CM_PASSWD|divi<<12) // integer divide (no MASH -> stable edges)
	reg.Write(ctl, CM_PASSWD|CM_CTL_ENAB|CM_CTL_SRC_OSC)

	reg.Write(p.base+pwmRNG1, rng)
	reg.Write(p.base+pwmRNG2, rng)
	p.inited = true
	return nil
}

func pwmAlt(gpio int) GPIOFunction {
	if gpio == 18 || gpio == 19 {
		return GPIO_FN5 // ALT5
	}
	return GPIO_FN0 // ALT0 (GPIO12/13)
}

// EnableChannel muxes gpio to its PWM function and enables the channel (1 or 2)
// in mark-space mode.
func (p *PWM) EnableChannel(ch, gpio int) error {
	if !p.inited {
		return fmt.Errorf("pwm: not initialized")
	}
	g, err := NewGPIO(gpio)
	if err != nil {
		return err
	}
	g.SelectFunction(pwmAlt(gpio))

	v := reg.Read(p.base + pwmCTL)
	if ch == 2 {
		v |= pwmPWEN2 | pwmMSEN2
	} else {
		v |= pwmPWEN1 | pwmMSEN1
	}
	reg.Write(p.base+pwmCTL, v)
	return nil
}

// SetDuty sets the duty ratio (0..1) of a channel (1 or 2).
func (p *PWM) SetDuty(ch int, ratio float64) {
	if !p.inited {
		return
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	dat := uint32(ratio * float64(p.rng))
	if ch == 2 {
		reg.Write(p.base+pwmDAT2, dat)
	} else {
		reg.Write(p.base+pwmDAT1, dat)
	}
}
