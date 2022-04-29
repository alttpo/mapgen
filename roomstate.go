package main

import (
	"fmt"
	"image"
	"io/ioutil"
)

type ScanState struct {
	t MapCoord
	d Direction
	s LinkState
}

type ExitPoint struct {
	Supertile
	Point MapCoord
	Direction
	WorthMarking bool
}

type EntryPoint struct {
	Supertile
	Point MapCoord
	Direction
	//LinkState
	From ExitPoint
}

func (ep EntryPoint) String() string {
	//return fmt.Sprintf("{%s, %s, %s, %s}", ep.Supertile, ep.Point, ep.Direction, ep.LinkState)
	return fmt.Sprintf("{%s, %s, %s}", ep.Supertile, ep.Point, ep.Direction)
}

type RoomState struct {
	Supertile

	Rendered image.Image

	EntryPoints []EntryPoint
	ExitPoints  []ExitPoint

	WarpExitTo       Supertile
	StairExitTo      [4]Supertile
	WarpExitLayer    MapCoord
	StairTargetLayer [4]MapCoord

	Doors      []Door
	Stairs     []MapCoord
	SwapLayers map[MapCoord]empty // $06C0[size=$044E >> 1]

	TilesVisited map[MapCoord]empty

	TilesVisitedStar0 map[MapCoord]empty
	TilesVisitedStar1 map[MapCoord]empty
	TilesVisitedTag0  map[MapCoord]empty
	TilesVisitedTag1  map[MapCoord]empty

	Tiles     [0x2000]byte
	Reachable [0x2000]byte
	Hookshot  map[MapCoord]byte

	e           System
	WRAM        [0x20000]byte
	VRAMTileSet [0x4000]byte

	markedPit   bool
	markedFloor bool
	lifoSpace   [0x2000]ScanState
	lifo        []ScanState
	IsLoaded    bool
}

func CreateRoom(st Supertile, initEmu *System) (room *RoomState) {
	var err error

	fmt.Printf("    creating room %s\n", st)

	room = &RoomState{
		Supertile:         st,
		Rendered:          nil,
		Hookshot:          make(map[MapCoord]byte, 0x2000),
		TilesVisitedStar0: make(map[MapCoord]empty, 0x2000),
		TilesVisitedStar1: make(map[MapCoord]empty, 0x2000),
		TilesVisitedTag0:  make(map[MapCoord]empty, 0x2000),
		TilesVisitedTag1:  make(map[MapCoord]empty, 0x2000),
	}
	room.TilesVisited = room.TilesVisitedStar0

	e := &room.e

	// have the emulator's WRAM refer to room.WRAM
	e.WRAM = &room.WRAM
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

	return
}

func (room *RoomState) Init() (err error) {
	if room.IsLoaded {
		return
	}

	st := room.Supertile

	e := &room.e
	wram := (e.WRAM)[:]
	vram := (e.VRAM)[:]
	tiles := room.Tiles[:]

	// load and draw current supertile:
	write16(wram, 0xA0, uint16(st))
	//e.LoggerCPU = e.Logger
	if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
		return
	}
	e.LoggerCPU = nil

	copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])
	copy(tiles, wram[0x12000:0x14000])

	// make a map full of $01 Collision and carve out reachable areas:
	for i := range room.Reachable {
		room.Reachable[i] = 0x01
	}

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(st)), wram, 0644)
	ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(st)), tiles, 0644)

	room.WarpExitTo = Supertile(read8(wram, 0xC000))
	room.StairExitTo = [4]Supertile{
		Supertile(read8(wram, uint32(0xC001))),
		Supertile(read8(wram, uint32(0xC002))),
		Supertile(read8(wram, uint32(0xC003))),
		Supertile(read8(wram, uint32(0xC004))),
	}
	room.WarpExitLayer = MapCoord(read8(wram, uint32(0x063C))&2) << 11
	room.StairTargetLayer = [4]MapCoord{
		MapCoord(read8(wram, uint32(0x063D))&2) << 11,
		MapCoord(read8(wram, uint32(0x063E))&2) << 11,
		MapCoord(read8(wram, uint32(0x063F))&2) << 11,
		MapCoord(read8(wram, uint32(0x0640))&2) << 11,
	}

	fmt.Fprintf(e.Logger, "    TAG1 = %02x\n", read8(wram, 0xAE))
	fmt.Fprintf(e.Logger, "    TAG2 = %02x\n", read8(wram, 0xAF))
	//fmt.Fprintf(s.Logger, "    WARPTO   = %s\n", Supertile(read8(wram, 0xC000)))
	//fmt.Fprintf(s.Logger, "    STAIR0TO = %s\n", Supertile(read8(wram, 0xC001)))
	//fmt.Fprintf(s.Logger, "    STAIR1TO = %s\n", Supertile(read8(wram, 0xC002)))
	//fmt.Fprintf(s.Logger, "    STAIR2TO = %s\n", Supertile(read8(wram, 0xC003)))
	//fmt.Fprintf(s.Logger, "    STAIR3TO = %s\n", Supertile(read8(wram, 0xC004)))
	//fmt.Fprintf(s.Logger, "    DARK     = %v\n", room.IsDarkRoom())

	// process doors first:
	doors := make([]Door, 0, 16)
	for m := 0; m < 16; m++ {
		tpos := read16(wram, uint32(0x19A0+(m<<1)))
		// stop marker:
		if tpos == 0 {
			//fmt.Fprintf(s.Logger, "    door stop at marker\n")
			break
		}

		door := Door{
			Pos:  MapCoord(tpos >> 1),
			Type: DoorType(read16(wram, uint32(0x1980+(m<<1)))),
			Dir:  Direction(read16(wram, uint32(0x19C0+(m<<1)))),
		}
		doors = append(doors, door)

		fmt.Fprintf(e.Logger, "    door: %v\n", door)

		isDoorEdge, _, _, _ := door.Pos.IsDoorEdge()

		{
			// open up doors that are in front of interroom stairwells:
			var stair MapCoord

			switch door.Dir {
			case DirNorth:
				stair = door.Pos + 0x01
				break
			case DirSouth:
				stair = door.Pos + 0xC1
				break
			case DirEast:
				stair = door.Pos + 0x43
				break
			case DirWest:
				stair = door.Pos + 0x40
				break
			}

			v := tiles[stair]
			if v >= 0x30 && v <= 0x39 {
				tiles[door.Pos+0x41+0x00] = 0x00
				tiles[door.Pos+0x41+0x01] = 0x00
				tiles[door.Pos+0x41+0x40] = 0x00
				tiles[door.Pos+0x41+0x41] = 0x00
			}
		}

		if door.Type.IsExit() {
			lyr, row, col := door.Pos.RowCol()
			// patch up the door tiles to prevent reachability from exiting:
			for y := uint16(0); y < 4; y++ {
				for x := uint16(0); x < 4; x++ {
					t := lyr | (row+y)<<6 | (col + x)
					if tiles[t] >= 0xF0 {
						tiles[t] = 0x00
					}
				}
			}
			continue
		}

		if door.Type == 0x30 {
			// exploding wall:
			pos := int(door.Pos)
			fmt.Printf("    exploding wall %s\n", door.Pos)
			for c := 0; c < 11; c++ {
				for r := 0; r < 12; r++ {
					tiles[pos+(r<<6)-c] = 0
					tiles[pos+(r<<6)+1+c] = 0
				}
			}
			continue
		}

		if isDoorEdge {
			// blow open edge doorways:
			var (
				start        MapCoord
				tn           MapCoord
				doorTileType uint8
				doorwayTile  uint8
				adj          int
			)

			var ok bool
			lyr, _, _ := door.Pos.RowCol()

			switch door.Dir {
			case DirNorth:
				start = door.Pos + 0x81
				doorwayTile = 0x80 | uint8(lyr>>10)
				adj = 1
				break
			case DirSouth:
				start = door.Pos + 0x41
				doorwayTile = 0x80 | uint8(lyr>>10)
				adj = 1
				break
			case DirEast:
				start = door.Pos + 0x41
				doorwayTile = 0x81 | uint8(lyr>>10)
				adj = 0x40
				break
			case DirWest:
				start = door.Pos + 0x42
				doorwayTile = 0x81 | uint8(lyr>>10)
				adj = 0x40
				break
			}

			doorTileType = tiles[start]
			if doorTileType < 0xF0 {
				// don't blow this doorway; it's custom:
				continue
			}

			tn = start
			canBlow := func(v uint8) bool {
				if v == 0x01 || v == 0x00 {
					return true
				}
				if v == doorTileType {
					return true
				}
				if v >= 0x28 && v <= 0x2B {
					return true
				}
				if v == 0x10 {
					// slope?? found in sanctuary $002
					return true
				}
				return false
			}
			for i := 0; i < 12; i++ {
				v := tiles[tn]
				if canBlow(v) {
					fmt.Printf("    blow open %s\n", tn)
					tiles[tn] = doorwayTile
					fmt.Printf("    blow open %s\n", MapCoord(int(tn)+adj))
					tiles[int(tn)+adj] = doorwayTile
				} else {
					panic(fmt.Errorf("something blocking the doorway at %s: $%02x", tn, v))
					break
				}

				tn, _, ok = tn.MoveBy(door.Dir, 1)
				if !ok {
					break
				}
			}
			continue
		}

		//if !isDoorEdge
		{
			var (
				start        MapCoord
				tn           MapCoord
				doorTileType uint8
				maxCount     int
				count        int
				doorwayTile  uint8
				adj          int
			)

			var ok bool
			lyr, _, _ := door.Pos.RowCol()

			switch door.Dir {
			case DirNorth:
				start = door.Pos + 0x81
				maxCount = 12
				doorwayTile = 0x80 | uint8(lyr>>10)
				adj = 1
				break
			case DirSouth:
				start = door.Pos + 0x41
				maxCount = 12
				doorwayTile = 0x80 | uint8(lyr>>10)
				adj = 1
				break
			case DirEast:
				start = door.Pos + 0x42
				maxCount = 10
				doorwayTile = 0x81 | uint8(lyr>>10)
				adj = 0x40
				break
			case DirWest:
				start = door.Pos + 0x42
				maxCount = 10
				doorwayTile = 0x81 | uint8(lyr>>10)
				adj = 0x40
				break
			}

			var mustStop func(uint8) bool

			doorTileType = tiles[start]
			if doorTileType >= 0x80 && doorTileType <= 0x8D {
				mustStop = func(v uint8) bool {
					if v == 0x01 {
						return false
					}
					if v >= 0x28 && v <= 0x2B {
						return false
					}
					if v == doorwayTile {
						return false
					}
					if v >= 0xF0 {
						return false
					}
					return true
				}
			} else if doorTileType >= 0xF0 {
				oppositeDoorType := uint8(0)
				if doorTileType >= 0xF8 {
					oppositeDoorType = doorTileType - 8
				} else if doorTileType >= 0xF0 {
					oppositeDoorType = doorTileType + 8
				}

				mustStop = func(v uint8) bool {
					if v == 0x01 {
						return false
					}
					if v == doorwayTile {
						return false
					}
					if v == oppositeDoorType {
						return false
					}
					if v == doorTileType {
						return false
					}
					if v >= 0x28 && v <= 0x2B {
						// ledge tiles can be found in doorway in fairy cave $008:
						return false
					}
					return true
				}
			} else {
				// bad door starter tile type
				fmt.Fprintf(e.Logger, fmt.Sprintf("unrecognized door tile at %s: $%02x\n", start, doorTileType))
				continue
			}

			// check many tiles behind door for opposite door tile:
			i := 0
			tn = start
			for ; i < maxCount; i++ {
				v := tiles[tn]
				if mustStop(v) {
					break
				}
				tn, _, ok = tn.MoveBy(door.Dir, 1)
				if !ok {
					break
				}
			}
			count = i

			// blow open the doorway:
			tn = start
			for i := 0; i < count; i++ {
				v := tiles[tn]
				if mustStop(v) {
					break
				}

				//fmt.Printf("    blow open %s\n", tn)
				tiles[tn] = doorwayTile
				//fmt.Printf("    blow open %s\n", mapCoord(int(tn)+adj))
				tiles[int(tn)+adj] = doorwayTile
				tn, _, _ = tn.MoveBy(door.Dir, 1)
			}
		}
	}
	room.Doors = doors

	// find layer-swap tiles in doorways:
	swapCount := read16(wram, 0x044E)
	room.SwapLayers = make(map[MapCoord]empty, swapCount*4)
	for i := uint16(0); i < swapCount; i += 2 {
		t := MapCoord(read16(wram, uint32(0x06C0+i)))

		// mark the 2x2 tile as a layer-swap:
		room.SwapLayers[t+0x00] = empty{}
		room.SwapLayers[t+0x01] = empty{}
		room.SwapLayers[t+0x40] = empty{}
		room.SwapLayers[t+0x41] = empty{}
		// have to put it on both layers? ew
		room.SwapLayers[t|0x1000+0x00] = empty{}
		room.SwapLayers[t|0x1000+0x01] = empty{}
		room.SwapLayers[t|0x1000+0x40] = empty{}
		room.SwapLayers[t|0x1000+0x41] = empty{}
	}

	// find interroom stair objects:
	stairCount := uint32(0)
	for _, n := range []uint32{0x0438, 0x043A, 0x047E, 0x0482, 0x0480, 0x0484, 0x04A2, 0x04A6, 0x04A4, 0x04A8} {
		index := uint32(read16(wram, n))
		if index > stairCount {
			stairCount = index
		}
	}
	for i := uint32(0); i < stairCount; i += 2 {
		t := MapCoord(read16(wram, 0x06B0+i))
		room.Stairs = append(room.Stairs, t)
		fmt.Fprintf(e.Logger, "    interroom stair at %s\n", t)
	}

	for i := uint32(0); i < 0x20; i += 2 {
		pos := MapCoord(read16(wram, 0x0540+i) >> 1)
		if pos == 0 {
			break
		}
		fmt.Printf(
			"    manipulable(%s): %02x, %04x @%04x -> %04x%04x,%04x%04x\n",
			pos,
			i,
			read16(wram, 0x0500+i), // MANIPPROPS
			read16(wram, 0x0520+i), // MANIPOBJX
			read16(wram, 0x0560+i), // MANIPRTNW
			read16(wram, 0x05A0+i), // MANIPRTNE
			read16(wram, 0x0580+i), // MANIPRTSW
			read16(wram, 0x05C0+i), // MANIPRTSE
		)
	}

	for i := uint32(0); i < 6; i++ {
		gt := read16(wram, 0x06E0+i<<1)
		if gt == 0 {
			break
		}

		fmt.Printf("    chest($%04x)\n", gt)

		if gt&0x8000 != 0 {
			// locked cell door:
			t := MapCoord((gt & 0x7FFF) >> 1)
			if tiles[t] == 0x58+uint8(i) {
				tiles[t+0x00] = 0x00
				tiles[t+0x01] = 0x00
				tiles[t+0x40] = 0x00
				tiles[t+0x41] = 0x00
			}
			if tiles[t|0x1000] == 0x58+uint8(i) {
				tiles[t|0x1000+0x00] = 0x00
				tiles[t|0x1000+0x01] = 0x00
				tiles[t|0x1000+0x40] = 0x00
				tiles[t|0x1000+0x41] = 0x00
			}
		}
	}

	// clear all enemy health to see if this triggers something:
	for i := uint32(0); i < 16; i++ {
		write8(room.WRAM[:], 0x0DD0+i, 0)
	}

	room.HandleRoomTags()

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap", uint16(st)), (&room.Tiles)[:], 0644)

	room.IsLoaded = true

	return
}

func (r *RoomState) push(s ScanState) {
	r.lifo = append(r.lifo, s)
}

func (r *RoomState) pushAllDirections(t MapCoord, s LinkState) {
	mn, ms, mw, me := false, false, false, false
	// can move in any direction:
	if tn, dir, ok := t.MoveBy(DirNorth, 1); ok {
		mn = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirWest, 1); ok {
		mw = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirEast, 1); ok {
		me = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirSouth, 1); ok {
		ms = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}

	// check diagonals at pits; cannot squeeze between solid areas though:
	if mn && mw && r.canDiagonal(r.Tiles[t-0x40]) && r.canDiagonal(r.Tiles[t-0x01]) {
		r.push(ScanState{t: t - 0x41, d: DirNorth, s: s})
	}
	if mn && me && r.canDiagonal(r.Tiles[t-0x40]) && r.canDiagonal(r.Tiles[t+0x01]) {
		r.push(ScanState{t: t - 0x3F, d: DirNorth, s: s})
	}
	if ms && mw && r.canDiagonal(r.Tiles[t+0x40]) && r.canDiagonal(r.Tiles[t-0x01]) {
		r.push(ScanState{t: t + 0x3F, d: DirSouth, s: s})
	}
	if ms && me && r.canDiagonal(r.Tiles[t+0x40]) && r.canDiagonal(r.Tiles[t+0x01]) {
		r.push(ScanState{t: t + 0x41, d: DirSouth, s: s})
	}
}

func (r *RoomState) canDiagonal(v byte) bool {
	return v == 0x20 || // pit
		(v&0xF0 == 0xB0) // somaria/pipe
}

func (r *RoomState) IsDarkRoom() bool {
	return read8((&r.WRAM)[:], 0xC005) != 0
}

// isAlwaysWalkable checks if the tile is always walkable on, regardless of state
func (r *RoomState) isAlwaysWalkable(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x09 || // shallow water
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xA0 // north/south dungeon swap door (for HC to sewers)
}

// isMaybeWalkable checks if the tile could be walked on depending on what state it's in
func (r *RoomState) isMaybeWalkable(t MapCoord, v uint8) bool {
	return v&0xF0 == 0x70 || // pots/pegs/blocks
		v == 0x62 || // bombable floor
		v == 0x66 || v == 0x67 // crystal pegs (orange/blue):
}

func (r *RoomState) canHookThru(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x08 || v == 0x09 || // water
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x1C || v == 0x0C || // layer pass through
		v == 0x20 || // pit
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x28 && v <= 0x2B) || // ledge tiles
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xB6 || // somaria start
		v == 0xBC // somaria start
}

// isHookable determines if the tile can be attached to with a hookshot
func (r *RoomState) isHookable(v uint8) bool {
	return v == 0x27 || // general hookable object
		(v >= 0x58 && v <= 0x5D) || // chests (TODO: check $0500 table for kind)
		v&0xF0 == 0x70 // pot/peg/block
}

func (r *RoomState) FindReachableTiles(
	entryPoint EntryPoint,
	visit func(s ScanState, v uint8),
) {
	m := &r.Tiles

	// if we ever need to wrap
	f := visit

	r.lifo = r.lifoSpace[:0]
	r.push(ScanState{t: entryPoint.Point, d: entryPoint.Direction})

	// handle the stack of locations to traverse:
	for len(r.lifo) != 0 {
		lifoLen := len(r.lifo) - 1
		s := r.lifo[lifoLen]
		r.lifo = r.lifo[:lifoLen]

		if _, ok := r.TilesVisited[s.t]; ok {
			continue
		}

		v := m[s.t]

		if s.s == StatePipe {
			// allow 00 and 01 in pipes for TR $015 center area:
			if v == 0x00 || v == 0x01 {
				// continue in the same direction:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// straight:
			if v == 0xB0 || v == 0xB1 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// check for pipe exit 3 tiles in advance:
				// this is done to skip collision tiles between B0/B1 and BE
				if tn, dir, ok := s.t.MoveBy(s.d, 3); ok && m[tn] == 0xBE {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
					continue
				}

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					// if the pipe crosses another direction pipe skip over that bit of pipe:
					if m[tn] == v^0x01 {
						if tn, _, ok := tn.MoveBy(dir, 2); ok && v == m[tn] {
							r.push(ScanState{t: tn, d: dir, s: StatePipe})
							continue
						}
					}

					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// west to south or north to east:
			if v == 0xB2 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// south to east or west to north:
			if v == 0xB3 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// north to west or east to south:
			if v == 0xB4 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// east to north or south to west:
			if v == 0xB5 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}

			// line exit:
			if v == 0xB6 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// check for 2 pit tiles beyond exit:
				t := s.t
				var ok bool
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					continue
				}
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					continue
				}

				// continue in the same direction but not in pipe-follower state:
				if tn, dir, ok := t.MoveBy(s.d, 1); ok && m[tn] == 0x00 {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			// south, west, east junction:
			if v == 0xB7 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, west, east junction:
			if v == 0xB8 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, east, south junction:
			if v == 0xB9 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, west, south junction:
			if v == 0xBA {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// 4-way junction:
			if v == 0xBB {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// possible exit:
			if v == 0xBC {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}

				// check for exits across pits:
				if tn, dir, ok := s.t.MoveBy(s.d.RotateCW(), 1); ok && m[tn] == 0x20 {
					if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x20 {
						if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x00 {
							r.push(ScanState{t: tn, d: dir})
						}
					}
				}
				if tn, dir, ok := s.t.MoveBy(s.d.RotateCCW(), 1); ok && m[tn] == 0x20 {
					if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x20 {
						if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x00 {
							r.push(ScanState{t: tn, d: dir})
						}
					}
				}

				continue
			}

			// cross-over:
			if v == 0xBD {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// pipe exit:
			if v == 0xBE {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction but not in pipe-follower state:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			continue
		}

		if s.s == StateSwim {
			if s.t&0x1000 == 0 {
				panic("swimming in layer 1!")
			}

			if v == 0x02 || v == 0x03 {
				// collision:
				r.TilesVisited[s.t] = empty{}
				continue
			}

			if v == 0x0A {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			if v == 0x1D {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			if v == 0x3D {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			// can swim over mostly everything on layer 2:
			r.TilesVisited[s.t] = empty{}
			f(s, v)
			r.pushAllDirections(s.t, StateSwim)
			continue
		}

		if v == 0x08 {
			// deep water:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// flip to swimming layer and state:
			t := s.t ^ 0x1000
			if m[t] != 0x1C && m[t] != 0x0D {
				r.push(ScanState{t: t, d: s.d, s: StateSwim})
			}

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir, s: StateWalk})
				continue
			}
			continue
		}

		if r.isAlwaysWalkable(v) || r.isMaybeWalkable(s.t, v) {
			// no collision:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// can move in any direction:
			r.pushAllDirections(s.t, StateWalk)

			// check for water below us:
			t := s.t | 0x1000
			if t != s.t && m[t] == 0x08 {
				if v != 0x08 && v != 0x0D {
					r.pushAllDirections(t, StateSwim)
				}
			}
			continue
		}

		if v == 0x0A {
			// deep water ladder:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// transition to swim state on other layer:
			t := s.t | 0x1000
			r.TilesVisited[t] = empty{}
			f(ScanState{t: t, d: s.d, s: StateSwim}, v)

			if tn, dir, ok := t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir, s: StateSwim})
			}
			continue
		}

		// layer pass through:
		if v == 0x1C {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if s.t&0x1000 == 0 {
				// $1C falling onto $0C means scrolling floor:
				if m[s.t|0x1000] == 0x0C {
					// treat as regular floor:
					r.pushAllDirections(s.t, StateWalk)
				} else {
					// drop to lower layer:
					r.push(ScanState{t: s.t | 0x1000, d: s.d})
				}
			}

			// detect a hookable tile across this pit:
			r.scanHookshot(s.t, s.d)

			continue
		} else if v == 0x0C {
			panic(fmt.Errorf("what to do for $0C at %s", s.t))
		}

		// north-facing stairs:
		if v == 0x1D {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// north-facing stairs, layer changing:
		if v >= 0x1E && v <= 0x1F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// pit:
		if v == 0x20 {
			// Link can fall into pit but cannot move beyond it:

			// don't mark as visited since it's possible we could also fall through this pit tile from above
			// TODO: fix this to accommodate both position and direction in the visited[] check and introduce
			// a Falling direction
			//r.TilesVisited[s.t] = empty{}
			f(s, v)

			// check what's beyond the pit:
			func() {
				t := s.t
				var ok bool
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					return
				}
				if t, _, ok = t.MoveBy(s.d, 1); !ok {
					return
				}

				v = m[t]

				// somaria line start:
				if v == 0xB6 || v == 0xBC {
					r.TilesVisited[t] = empty{}
					f(ScanState{t: t, d: s.d}, v)

					// find corresponding B0..B1 directional line to follow:
					if tn, dir, ok := t.MoveBy(DirNorth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirWest, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirEast, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirSouth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					return
				}
			}()

			// detect a hookable tile across this pit:
			r.scanHookshot(s.t, s.d)

			continue
		}

		// ledge tiles:
		if v >= 0x28 && v <= 0x2B {
			// ledge much not be approached from its perpendicular direction:
			ledgeDir := Direction(v - 0x28)
			if ledgeDir != s.d && ledgeDir != s.d.Opposite() {
				continue
			}

			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// check for hookable tiles across from this ledge:
			r.scanHookshot(s.t, s.d)

			// check 4 tiles from ledge for pit:
			t, dir, ok := s.t.MoveBy(s.d, 4)
			if !ok {
				continue
			}

			// pit tile on same layer?
			v = m[t]
			if v == 0x20 {
				// visit it next:
				r.push(ScanState{t: t, d: dir})
			} else if v == 0x1C { // or 0x0C ?
				// swap layers:
				t ^= 0x1000

				// check again for pit tile on the opposite layer:
				v = m[t]
				if v == 0x20 {
					// visit it next:
					r.push(ScanState{t: t, d: dir})
				}
			} else if v == 0x0C {
				panic(fmt.Errorf("TODO handle $0C in pit case t=%s", t))
			} else if v == 0x00 {
				// open floor:
				r.push(ScanState{t: t, d: dir})
			}

			continue
		}

		// interroom stair exits:
		if v >= 0x30 && v <= 0x37 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// don't continue beyond a staircase unless it's our entry point:
			if len(r.lifo) == 0 {
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}

		// 38=Straight interroom stairs north/down edge (39= south/up edge):
		if v == 0x38 || v == 0x39 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// don't continue beyond a staircase unless it's our entry point:
			if len(r.lifo) == 0 {
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}

		// south-facing single-layer auto stairs:
		if v == 0x3D {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// south-facing layer-swap auto stairs:
		if v >= 0x3E && v <= 0x3F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// spiral staircase:
		// $5F is the layer 2 version of $5E (spiral staircase)
		if v == 0x5E || v == 0x5F {
			r.TilesVisited[s.t] = empty{}
			f(s, m[s.t])

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// doorways:
		if v >= 0x80 && v <= 0x87 {
			if v&1 == 0 {
				// north-south
				if s.d == DirNone {
					// scout in both directions:
					if _, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirNorth && s.d != DirSouth {
					panic(fmt.Errorf("north-south door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
					// don't move past door edge:
					continue
				}
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			} else {
				// east-west
				if s.d == DirNone {
					// scout in both directions:
					if _, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirEast && s.d != DirWest {
					panic(fmt.Errorf("east-west door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
					// don't move past door edge:
					continue
				}
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}
		// east-west teleport door
		if v == 0x89 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// entrance door (8E = north-south?, 8F = east-west??):
		if v == 0x8E || v == 0x8F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if s.d == DirNone {
				// scout in both directions:
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// Layer/dungeon toggle doorways:
		if v >= 0x90 && v <= 0xAF {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// TR pipe entrance:
		if v == 0xBE {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// find corresponding B0..B1 directional pipe to follow:
			if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			continue
		}

		// doors:
		if v >= 0xF0 {
			// determine a direction to head in if we have none:
			if s.d == DirNone {
				var ok bool
				var t MapCoord
				if t, _, ok = s.t.MoveBy(DirNorth, 1); ok && m[t] == 0x00 {
					s.d = DirSouth
				} else if t, _, ok = s.t.MoveBy(DirEast, 1); ok && m[t] == 0x00 {
					s.d = DirWest
				} else if t, _, ok = s.t.MoveBy(DirSouth, 1); ok && m[t] == 0x00 {
					s.d = DirNorth
				} else if t, _, ok = s.t.MoveBy(DirWest, 1); ok && m[t] == 0x00 {
					s.d = DirEast
				} else {
					// maybe we're too far in the door:
					continue
				}

				r.push(ScanState{t: t, d: s.d})
				continue
			}

			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if t, _, ok := s.t.MoveBy(s.d, 2); ok {
				r.push(ScanState{t: t, d: s.d})
			}
			continue
		}

		// anything else is considered solid:
		//r.TilesVisited[s.t] = empty{}
		continue
	}
}

func (r *RoomState) scanHookshot(t MapCoord, d Direction) {
	var ok bool
	i := 0
	pt := t
	st := t
	shot := false

	m := &r.Tiles

	// estimating 0x10 8x8 tiles horizontally/vertically as max stretch of hookshot:
	const maxTiles = 0x10

	if m[t] >= 0x28 && m[t] <= 0x2B {
		// find opposite ledge first:
		ledgeTile := m[t]
		for ; i < maxTiles; i++ {
			// advance 1 tile:
			if t, _, ok = t.MoveBy(d, 1); !ok {
				return
			}

			if m[t] == ledgeTile {
				break
			}
		}
		if m[t] != ledgeTile {
			return
		}
	}

	for ; i < maxTiles; i++ {
		// the previous tile technically doesn't need to be walkable but it prevents
		// infinite loops due to not taking direction into account in the visited[] map
		// and not marking pit tiles as visited
		if r.isHookable(m[t]) && r.isAlwaysWalkable(m[pt]) {
			shot = true
			r.push(ScanState{t: pt, d: d})
			break
		}

		if !r.canHookThru(m[t]) {
			return
		}

		// advance 1 tile:
		pt = t
		if t, _, ok = t.MoveBy(d, 1); !ok {
			return
		}
	}

	if shot {
		// mark range as hookshot:
		t = st
		for j := 0; j < i; j++ {
			r.Hookshot[t] |= 1 << d

			if t, _, ok = t.MoveBy(d, 1); !ok {
				return
			}
		}
	}
}

func (r *RoomState) HandleRoomTags() bool {
	e := &r.e

	// if no tags present, don't check them:
	oldAE, oldAF := read8(r.WRAM[:], 0xAE), read8(r.WRAM[:], 0xAF)
	if oldAE == 0 && oldAF == 0 {
		return false
	}

	old04BC := read8(r.WRAM[:], 0x04BC)

	// prepare emulator for execution within this supertile:
	copy(e.WRAM[0x12000:0x14000], r.Tiles[:])

	if err := e.ExecAt(b00HandleRoomTagsPC, 0); err != nil {
		panic(err)
	}

	// update room state:
	copy(r.Tiles[:], e.WRAM[0x12000:0x14000])

	// if $AE or $AF (room tags) are modified, then the tag was activated:
	newAE, newAF := read8(r.WRAM[:], 0xAE), read8(r.WRAM[:], 0xAF)
	if newAE != oldAE || newAF != oldAF {
		return true
	}

	new04BC := read8(r.WRAM[:], 0x04BC)
	if new04BC != old04BC {
		return true
	}

	return false
}
