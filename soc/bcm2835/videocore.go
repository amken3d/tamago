// BCM2835 SoC VideoCore support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import "encoding/binary"

const (
	GPU_MEMORY_FLAG_DISCARDABLE      = 1 << 0
	GPU_MEMORY_FLAG_NORMAL           = 0 << 2
	GPU_MEMORY_FLAG_DIRECT           = 1 << 2
	GPU_MEMORY_FLAG_COHERENT         = 2 << 2
	GPU_MEMORY_FLAG_L1_NONALLOCATING = 3 << 2
	GPU_MEMORY_FLAG_ZERO             = 1 << 4
	GPU_MEMORY_FLAG_NO_INIT          = 1 << 5
	GPU_MEMORY_FLAG_HINT_PERMALOCK   = 1 << 6
)

// FirmwareRevision gets the firmware rev of the VideoCore GPU
func FirmwareRevision() uint32 {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_REV, make([]byte, VC_BOARD_GET_REV_LEN))

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// BoardModel gets the board model
func BoardModel() uint32 {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_MODEL, make([]byte, VC_BOARD_GET_MODEL_LEN))

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// MACAddress gets the board's MAC address
func MACAddress() []byte {
	return exchangeSingleTagMessage(VC_BOARD_GET_MAC, make([]byte, VC_BOARD_GET_MAC_LEN))
}

// Serial gets the board's serial number
func Serial() uint32 {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_SERIAL, make([]byte, VC_BOARD_GET_SERIAL_LEN))

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// CPUMemory gets the memory ranges allocated to the ARM core(s)
func CPUMemory() (start uint32, size uint32) {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_ARM_MEMORY, make([]byte, VC_BOARD_GET_ARM_MEMORY_LEN))

	if len(buf) < 8 {
		return 0, 0
	}

	return binary.LittleEndian.Uint32(buf[0:]), binary.LittleEndian.Uint32(buf[4:])
}

// GPUMemory gets the memory ranges allocated to VideoCore
func GPUMemory() (start uint32, size uint32) {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_VC_MEMORY, make([]byte, VC_BOARD_GET_VC_MEMORY_LEN))

	if len(buf) < 8 {
		return 0, 0
	}

	return binary.LittleEndian.Uint32(buf[0:]), binary.LittleEndian.Uint32(buf[4:])
}

// CPUAvailableDMAChannels gets the DMA channels available to the ARM core(s)
func CPUAvailableDMAChannels() (bitmask uint32) {
	buf := exchangeSingleTagMessage(VC_RES_GET_DMACHANNELS, make([]byte, VC_RES_GET_DMACHANNELS_LEN))

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// AllocateGPUMemory allocates space from the GPU address space
//
// The returned value is a handle, use LockMemory to convert
// to an address.
func AllocateGPUMemory(size uint32, alignment uint32, flags uint32) (handle uint32) {
	buf := make([]byte, VC_MEM_ALLOCATE_LEN)
	binary.LittleEndian.PutUint32(buf[0:], size)
	binary.LittleEndian.PutUint32(buf[4:], alignment)
	binary.LittleEndian.PutUint32(buf[8:], uint32(flags))

	buf = exchangeSingleTagMessage(VC_MEM_ALLOCATE, buf)

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// LockGPUMemory provides the address of previously allocated memory
func LockGPUMemory(handle uint32) (addr uint32) {
	buf := make([]byte, VC_MEM_LOCK_LEN)
	binary.LittleEndian.PutUint32(buf[0:], handle)

	buf = exchangeSingleTagMessage(VC_MEM_LOCK, buf)

	if len(buf) < 4 {
		return 0
	}

	return binary.LittleEndian.Uint32(buf)
}

// VideoCore-managed power domains (device IDs for the set-power-state tag).
const (
	PowerDeviceSDCard = 0
	PowerDeviceUART0  = 1
	PowerDeviceUART1  = 2
	PowerDeviceUSBHCD = 3 // the DWC2 OTG core
	PowerDeviceI2C0   = 4
	PowerDeviceI2C1   = 5
	PowerDeviceI2C2   = 6
	PowerDeviceSPI    = 7
	PowerDeviceCCP2TX = 8
)

// SetPowerState turns a VideoCore-managed power domain on or off, waiting for
// the rail to settle, and reports whether the device is powered afterwards.
// The DWC2 USB core (PowerDeviceUSBHCD) reads back as all-zero registers until
// it is powered, so this must run before touching it.
func SetPowerState(deviceID uint32, on bool) (powered bool) {
	state := uint32(1 << 1) // bit 1: wait for the rail to stabilize
	if on {
		state |= 1 << 0 // bit 0: power on
	}

	buf := make([]byte, VC_POWER_SET_STATE_LEN)
	binary.LittleEndian.PutUint32(buf[0:], deviceID)
	binary.LittleEndian.PutUint32(buf[4:], state)

	resp := exchangeSingleTagMessage(VC_POWER_SET_STATE, buf)
	if len(resp) < 8 {
		return false
	}

	// response: [device id][state]; bit 0 set = powered, bit 1 set = no device
	s := binary.LittleEndian.Uint32(resp[4:])
	return s&0x1 != 0 && s&0x2 == 0
}

// VC_GPIO_EXPANDER_BASE is the number the firmware gives the first expander pin.
// Expander line n is addressed as VC_GPIO_EXPANDER_BASE+n; below the base these
// tags address the SoC's own GPIOs, which the ARM can drive directly and should.
const VC_GPIO_EXPANDER_BASE = 128

// GPIO direction for SetGPIOConfig.
const (
	VC_GPIO_DIR_IN  = 0
	VC_GPIO_DIR_OUT = 1
)

// SetGPIOState drives a firmware-owned GPIO, reporting whether the firmware
// accepted the request.
//
// This exists for lines the ARM cannot reach. On boards where WL_REG_ON sits on
// the expander, driving the SoC GPIO of the same number succeeds, changes
// nothing, and leaves the wireless chip in reset -- with no error anywhere,
// because nothing failed; the write simply went elsewhere.
func SetGPIOState(gpio uint32, on bool) bool {
	buf := make([]byte, VC_GPIO_SET_STATE_LEN)

	var state uint32
	if on {
		state = 1
	}
	binary.LittleEndian.PutUint32(buf[0:], gpio)
	binary.LittleEndian.PutUint32(buf[4:], state)

	resp := exchangeSingleTagMessage(VC_GPIO_SET_STATE, buf)
	if len(resp) < 8 {
		return false
	}

	// The status comes back in the FIRST word, overwriting the gpio field -- not
	// in the second, which still holds the state that was sent. Reading the wrong
	// one reports every request as refused.
	return binary.LittleEndian.Uint32(resp[0:]) == 0
}

// GPIOState reads a firmware-owned GPIO.
func GPIOState(gpio uint32) (on bool, ok bool) {
	buf := make([]byte, VC_GPIO_GET_STATE_LEN)
	binary.LittleEndian.PutUint32(buf[0:], gpio)

	resp := exchangeSingleTagMessage(VC_GPIO_GET_STATE, buf)
	if len(resp) < 8 {
		return false, false
	}
	if binary.LittleEndian.Uint32(resp[0:]) != 0 { // status word
		return false, false
	}

	return binary.LittleEndian.Uint32(resp[4:]) != 0, true
}

// SetGPIOConfig configures a firmware-owned GPIO: direction, polarity,
// termination and initial state. An expander line must be made an output before
// SetGPIOState has any effect.
func SetGPIOConfig(gpio, direction, polarity, termEnable, termPullUp, state uint32) bool {
	buf := make([]byte, VC_GPIO_SET_CONFIG_LEN)

	binary.LittleEndian.PutUint32(buf[0:], gpio)
	binary.LittleEndian.PutUint32(buf[4:], direction)
	binary.LittleEndian.PutUint32(buf[8:], polarity)
	binary.LittleEndian.PutUint32(buf[12:], termEnable)
	binary.LittleEndian.PutUint32(buf[16:], termPullUp)
	binary.LittleEndian.PutUint32(buf[20:], state)

	resp := exchangeSingleTagMessage(VC_GPIO_SET_CONFIG, buf)
	if len(resp) < 8 {
		return false
	}

	return binary.LittleEndian.Uint32(resp[0:]) == 0 // status word
}

// BoardRevision returns the board revision word, which encodes the model.
//
// FirmwareRevision above uses this same tag (VC_BOARD_GET_REV, 0x00010002)
// despite its name -- the firmware revision is 0x00000001. This is the correctly
// named accessor; the other is left as it is so nothing depending on it breaks.
func BoardRevision() uint32 {
	buf := exchangeSingleTagMessage(VC_BOARD_GET_REV, make([]byte, VC_BOARD_GET_REV_LEN))

	if len(buf) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(buf)
}

// exchangeSingleTagMessageChecked is exchangeSingleTagMessage that returns
// nothing unless the firmware actually answered.
//
// The unchecked version is left as it is because everything that boots this SoC
// goes through it and its callers have been proven against real firmware. New
// readings should use this one: an unimplemented tag comes back with its buffer
// untouched, which is not zero but whatever the shared region last held, and
// silently decoding that produces a confident wrong number rather than a
// missing one. (Observed: a core-voltage read that came back as -1293.767 V.)
func exchangeSingleTagMessageChecked(code uint32, buf []byte) []byte {
	msg := &MailboxMessage{
		Tags: []MailboxTag{
			{
				ID:     code,
				Buffer: buf,
			},
		},
	}

	Mailbox.Call(VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	if msg.Error() {
		return nil
	}

	tag := msg.Tag(code)

	if tag == nil || !tag.Responded() {
		return nil
	}

	return tag.Buffer
}

func exchangeSingleTagMessage(code uint32, buf []byte) []byte {
	msg := &MailboxMessage{
		Tags: []MailboxTag{
			{
				ID:     code,
				Buffer: buf,
			},
		},
	}

	Mailbox.Call(VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	tag := msg.Tag(code)

	if tag == nil {
		return nil
	}

	return tag.Buffer
}
