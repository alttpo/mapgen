package main

type Direction uint8

const (
	DirNorth Direction = iota
	DirSouth
	DirWest
	DirEast
	DirNone
)

func (d Direction) MoveEG2(s Supertile) (Supertile, bool) {
	if s < 0x100 {
		return s, false
	}

	switch d {
	case DirNorth:
		return s - 0x10, s&0xF0 > 0
	case DirSouth:
		return s + 0x10, s&0xF0 < 0xF0
	case DirWest:
		return s - 1, s&0x0F > 0
	case DirEast:
		return s + 1, s&0x0F < 0x02
	}
	return s, false
}

func (d Direction) Opposite() Direction {
	switch d {
	case DirNorth:
		return DirSouth
	case DirSouth:
		return DirNorth
	case DirWest:
		return DirEast
	case DirEast:
		return DirWest
	}
	return d
}

func (d Direction) String() string {
	switch d {
	case DirNorth:
		return "north"
	case DirSouth:
		return "south"
	case DirWest:
		return "west"
	case DirEast:
		return "east"
	}
	return ""
}

func (d Direction) RotateCW() Direction {
	switch d {
	case DirNorth:
		return DirEast
	case DirEast:
		return DirSouth
	case DirSouth:
		return DirWest
	case DirWest:
		return DirNorth
	}
	return d
}

func (d Direction) RotateCCW() Direction {
	switch d {
	case DirNorth:
		return DirWest
	case DirWest:
		return DirSouth
	case DirSouth:
		return DirEast
	case DirEast:
		return DirNorth
	}
	return d
}
