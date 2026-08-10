// BCM2835 SoC on-die sensors
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import "encoding/binary"

// The SoC's own health readings, via the VideoCore property mailbox.
//
// The die temperature sensor (AVS_RO_TEMSTATUS) and the clock/voltage
// controls are owned by the VideoCore firmware, not by the ARM: the firmware
// runs the thermal governor and will throttle the ARM clock on its own. Reading
// through the mailbox is therefore not merely the easy path, it is the correct
// one -- the raw sensor register needs a calibration slope the firmware holds,
// and a value read behind the governor's back can disagree with the decisions
// the governor is making.

// VideoCore clock IDs for the clock-rate tags. (ClockV3D is with the QPU
// support it was added for.)
const (
	ClockARM  = 3
	ClockCore = 4
	ClockEMMC = VC_CLOCK_ID_EMMC
)

// Voltage domain IDs for the voltage tags.
const (
	VoltageCore = 1
)

// Temperature returns the SoC die temperature in millidegrees Celsius, or zero
// if the firmware does not answer.
//
// Millidegrees rather than a float because that is the unit the firmware
// reports; rounding belongs with whatever displays it.
func Temperature() uint32 {
	return temperatureTag(VC_TEMP_GET, VC_TEMP_GET_LEN)
}

// MaxTemperature returns the die temperature at which the firmware will start
// throttling, in millidegrees Celsius.
//
// Worth reading alongside Temperature: an absolute reading means little without
// the limit it is approaching, and the limit is a firmware policy that differs
// between boards rather than a constant worth compiling in.
func MaxTemperature() uint32 {
	return temperatureTag(VC_TEMP_GET_MAX, VC_TEMP_GET_MAX_LEN)
}

func temperatureTag(code, length uint32) uint32 {
	buf := make([]byte, length)
	binary.LittleEndian.PutUint32(buf[0:], 0) // sensor id; only 0 exists

	resp := exchangeSingleTagMessage(code, buf)
	if len(resp) < 8 {
		return 0
	}

	return binary.LittleEndian.Uint32(resp[4:])
}

// ClockRate returns the current rate of a VideoCore-managed clock in Hz, or
// zero if the clock does not exist.
//
// On ClockARM this is the live rate, so it moves: the firmware scales the ARM
// clock for both idle and thermal throttling, which makes a reading well below
// the maximum a symptom worth surfacing rather than a curiosity.
func ClockRate(clockID uint32) uint32 {
	buf := make([]byte, VC_CLOCK_GET_RATE_LEN)
	binary.LittleEndian.PutUint32(buf[0:], clockID)

	resp := exchangeSingleTagMessage(VC_CLOCK_GET_RATE, buf)
	if len(resp) < 8 {
		return 0
	}

	return binary.LittleEndian.Uint32(resp[4:])
}

// Voltage returns a voltage domain's setting in microvolts, or zero if the
// domain does not exist.
//
// The firmware reports an offset from 1.2V in units of 2.5mV, which is an
// encoding rather than a measurement; it is converted here so callers do not
// each have to know it.
func Voltage(domainID uint32) int32 {
	buf := make([]byte, VC_VOLT_GET_LEN)
	binary.LittleEndian.PutUint32(buf[0:], domainID)

	resp := exchangeSingleTagMessage(VC_VOLT_GET, buf)
	if len(resp) < 8 {
		return 0
	}

	off := int32(binary.LittleEndian.Uint32(resp[4:]))

	// 0x80000000 and 0 are the firmware's "not supported" answers, and both
	// would otherwise decode to a plausible-looking voltage.
	if off == 0 || uint32(off) == 0x80000000 {
		return 0
	}

	return 1200000 + off*2500
}
