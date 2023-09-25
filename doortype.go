package main

import "fmt"

type DoorType uint8

func (t DoorType) IsExit() bool {
	if t >= 0x04 && t <= 0x06 {
		// exit door:
		return true
	}
	if t >= 0x0A && t <= 0x12 {
		// exit door:
		return true
	}
	//if t == 0x22 {
	//	// supertile 0b6, north key door covering stairwell?
	//	return true
	//}
	if t == 0x2A {
		// bombable cave exit:
		return true
	}
	//if t == 0x2E {
	//	// bombable door exit(?):
	//	return true
	//}
	return false
}

func (t DoorType) IsLayer2() bool {
	if t == 0x02 {
		return true
	}
	if t == 0x04 {
		return true
	}
	if t == 0x06 {
		return true
	}
	if t == 0x0C {
		return true
	}
	if t == 0x10 {
		return true
	}
	if t == 0x24 {
		return true
	}
	if t == 0x26 {
		return true
	}
	if t == 0x3A {
		return true
	}
	if t == 0x3C {
		return true
	}
	if t == 0x3E {
		return true
	}
	if t == 0x40 {
		return true
	}
	if t == 0x44 {
		return true
	}
	if t >= 0x48 && t <= 0x66 {
		return true
	}
	return false
}

func (t DoorType) IsStairwell() bool {
	return t >= 0x20 && t <= 0x26
}

func (t DoorType) String() string {
	return fmt.Sprintf("$%02x", uint8(t))
}
