package main

import (
	"fmt"
	"image"
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

	WRAM        [0x20000]byte
	VRAMTileSet [0x4000]byte

	markedPit   bool
	markedFloor bool
	lifoSpace   [0x2000]ScanState
	lifo        []ScanState
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

func (r *RoomState) HandleRoomTags(e *System) bool {
	// if no tags present, don't check them:
	oldAE, oldAF := read8(r.WRAM[:], 0xAE), read8(r.WRAM[:], 0xAF)
	if oldAE == 0 && oldAF == 0 {
		return false
	}

	old04BC := read8(r.WRAM[:], 0x04BC)

	// prepare emulator for execution within this supertile:
	copy(e.WRAM[:], r.WRAM[:])
	copy(e.WRAM[0x12000:0x14000], r.Tiles[:])

	if err := e.ExecAt(b00HandleRoomTagsPC, 0); err != nil {
		panic(err)
	}

	// update room state:
	copy(r.WRAM[:], e.WRAM[:])
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
