package main

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io/ioutil"
	"os"
	"sync"
	"unsafe"
)

func processEntrance(
	initEmu *System,
	g *Entrance,
	wg *sync.WaitGroup,
) {
	var err error

	e := &System{}
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

	eID := g.EntranceID
	fmt.Printf("entrance $%02x load start\n", eID)

	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID

	// load the entrance and draw the room:
	if eID > 0 {
		//e.LoggerCPU = os.Stdout
	}
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}
	e.LoggerCPU = nil

	fmt.Printf("entrance $%02x load complete\n", eID)

	g.Supertile = Supertile(read16(e.WRAM[:], 0xA0))

	{
		// if this is the entrance, Link should be already moved to his starting position:
		wram := e.WRAM[:]
		linkX := read16(wram, 0x22)
		linkY := read16(wram, 0x20)
		linkLayer := read16(wram, 0xEE)
		g.EntryCoord = AbsToMapCoord(linkX, linkY, linkLayer)
		//fmt.Printf("  link coord = {%04x, %04x, %04x}\n", linkX, linkY, linkLayer)
	}

	g.Rooms = make([]*RoomState, 0, 0x20)
	g.Supertiles = make(map[Supertile]*RoomState, 0x128)

	// build a stack (LIFO) of supertile entry points to visit:
	lifo := make([]EntryPoint, 0, 0x100)
	lifo = append(lifo, EntryPoint{g.Supertile, g.EntryCoord, DirNone, ExitPoint{}})

	// process the LIFO:
	for len(lifo) != 0 {
		// pop off the stack:
		lifoEnd := len(lifo) - 1
		ep := lifo[lifoEnd]
		lifo = lifo[0:lifoEnd]

		this := ep.Supertile

		//fmt.Printf("  ep = %s\n", ep)

		// create a room:
		var room *RoomState

		g.SupertilesLock.Lock()
		var ok bool
		if room, ok = g.Supertiles[this]; ok {
			//fmt.Printf("    reusing room %s\n", this)
			//if eID != room.Entrance.EntranceID {
			//	panic(fmt.Errorf("conflicting entrances for room %s", this))
			//}
		} else {
			// create new room:
			room = CreateRoom(g, this, e)
			g.Rooms = append(g.Rooms, room)
			g.Supertiles[this] = room
		}
		g.SupertilesLock.Unlock()

		// emulate loading the room:
		room.Lock()
		//fmt.Printf("entrance $%02x supertile %s discover from entry %s start\n", eID, room.Supertile, ep)

		if err = room.Init(ep); err != nil {
			panic(err)
		}

		if !staticEntranceMap {
			// check if room causes pit damage vs warp:
			// RoomsWithPitDamage#_00990C [0x70]uint16
			pitDamages := roomsWithPitDamage[this]

			warpExitTo := room.WarpExitTo
			stairExitTo := &room.StairExitTo
			warpExitLayer := room.WarpExitLayer
			stairTargetLayer := &room.StairTargetLayer

			pushEntryPoint := func(ep EntryPoint, name string) {
				// for EG2:
				if this >= 0x100 {
					ep.Supertile |= 0x100
				}

				room.EntryPoints = append(room.EntryPoints, ep)
				room.ExitPoints = append(room.ExitPoints, ExitPoint{
					Supertile:    ep.Supertile,
					Point:        ep.From.Point,
					Direction:    ep.From.Direction,
					WorthMarking: ep.From.WorthMarking,
				})

				lifo = append(lifo, ep)
				//fmt.Printf("    %s to %s\n", name, ep)
			}

			// dont need to read interroom stair list from $06B0; just link stair tile number to STAIRnTO exit

			// flood fill to find reachable tiles:
			tiles := &room.Tiles
			room.FindReachableTiles(
				ep,
				func(s ScanState, v uint8) {
					t := s.t
					d := s.d

					exit := ExitPoint{
						ep.Supertile,
						t,
						d,
						false,
					}

					// here we found a reachable tile:
					room.Reachable[t] = v

					if v == 0x00 {
						// detect edge walkways:
						if ok, edir, _, _ := t.IsEdge(); ok {
							if sn, _, ok := this.MoveBy(edir); ok {
								pushEntryPoint(EntryPoint{sn, t.OppositeEdge(), edir, exit}, fmt.Sprintf("%s walkway", edir))
							}
						}
						return
					}

					// door objects:
					if v >= 0xF0 {
						//fmt.Printf("    door tile $%02x at %s\n", v, t)
						// dungeon exits are already patched out, so this should be a normal door
						lyr, row, col := t.RowCol()
						if row >= 0x3A {
							// south:
							if sn, sd, ok := this.MoveBy(DirSouth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x06 << 6) | col), sd, exit}, "south door")
							}
						} else if row <= 0x06 {
							// north:
							if sn, sd, ok := this.MoveBy(DirNorth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x3A << 6) | col), sd, exit}, "north door")
							}
						} else if col >= 0x3A {
							// east:
							if sn, sd, ok := this.MoveBy(DirEast); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x06), sd, exit}, "east door")
							}
						} else if col <= 0x06 {
							// west:
							if sn, sd, ok := this.MoveBy(DirWest); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x3A), sd, exit}, "west door")
							}
						}

						return
					}

					// interroom doorways:
					if (v >= 0x80 && v <= 0x8D) || (v >= 0x90 && v <= 97) {
						if ok, edir, _, _ := t.IsDoorEdge(); ok && edir == d {
							// at or beyond the door edge zones:
							swapLayers := MapCoord(0)

							{
								// search the doorway for layer-swaps:
								tn := t
								for i := 0; i < 8; i++ {
									vd := tiles[tn]
									room.Reachable[tn] = vd
									if vd >= 0x90 && vd <= 0x9F {
										swapLayers = 0x1000
									}
									if vd >= 0xA8 && vd <= 0xAF {
										swapLayers = 0x1000
									}
									if _, ok := room.SwapLayers[tn]; ok {
										swapLayers = 0x1000
									}

									// advance into the doorway:
									tn, _, ok = tn.MoveBy(edir, 1)
									if !ok {
										break
									}
								}
							}

							if v&1 == 0 {
								// north-south normal doorway (no teleport doorways for north-south):
								if sn, _, ok := this.MoveBy(edir); ok {
									pushEntryPoint(EntryPoint{sn, t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "north-south doorway")
								}
							} else {
								// east-west doorway:
								if v == 0x89 {
									// teleport doorway:
									if edir == DirWest {
										pushEntryPoint(EntryPoint{stairExitTo[2], t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "west teleport doorway")
									} else if edir == DirEast {
										pushEntryPoint(EntryPoint{stairExitTo[3], t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "east teleport doorway")
									} else {
										panic("invalid direction approaching east-west teleport doorway")
									}
								} else {
									// normal doorway:
									if sn, _, ok := this.MoveBy(edir); ok {
										pushEntryPoint(EntryPoint{sn, t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "east-west doorway")
									}
								}
							}
						}
						return
					}

					if v >= 0x30 && v < 0x38 {
						var vn uint8
						vn = tiles[t-0x40]
						if vn == 0x80 || vn == 0x26 {
							vn = tiles[t+0x40]
						}

						if vn == 0x5E || vn == 0x5F {
							// spiral staircase
							tgtLayer := stairTargetLayer[v&3]
							dt := t
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									dt += 0x80
								}
								if tgtLayer != 0 {
									dt += 0x80
								}
								pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d.Opposite(), exit}, fmt.Sprintf("spiralStair(%s)", t))
							} else {
								// going down
								if t&0x1000 != 0 {
									dt -= 0x80
								}
								if tgtLayer != 0 {
									dt -= 0x80
								}
								pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d.Opposite(), exit}, fmt.Sprintf("spiralStair(%s)", t))
							}
							return
						} else if vn == 0x38 {
							// north stairs:
							tgtLayer := stairTargetLayer[v&3]
							dt := t.Col() + 0xFC0 - 2<<6
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
							} else {
								// going down
								// module #$07 submodule #$12 is going down north stairs (e.g. $042)
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
							}
							pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d, exit}, fmt.Sprintf("northStair(%s)", t))
							return
						} else if vn == 0x39 {
							// south stairs:
							tgtLayer := stairTargetLayer[v&3]
							dt := t.Col() + 2<<6
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
							} else {
								// going down
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
							}
							pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d, exit}, fmt.Sprintf("southStair(%s)", t))
							return
						} else if vn == 0x00 {
							// straight stairs:
							pushEntryPoint(EntryPoint{stairExitTo[v&3], t&0x0FFF | stairTargetLayer[v&3], d.Opposite(), exit}, fmt.Sprintf("stair(%s)", t))
							return
						}
						panic(fmt.Errorf("unhandled stair exit at %s %s", t, d))
						return
					}

					// pit exits:
					if !pitDamages {
						if v == 0x20 {
							// pit tile
							exit.WorthMarking = !room.markedPit
							room.markedPit = true
							pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("pit(%s)", t))
							return
						} else if v == 0x62 {
							// bombable floor tile
							exit.WorthMarking = !room.markedFloor
							room.markedFloor = true
							pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("bombableFloor(%s)", t))
							return
						}
					}
					if v == 0x4B {
						// warp floor tile
						exit.WorthMarking = t&0x40 == 0 && t&0x01 == 0
						pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("warp(%s)", t))
						return
					}

					if true {
						// manipulables (pots, hammer pegs, push blocks):
						if v&0xF0 == 0x70 {
							// find gfx tilemap position:
							j := (uint32(v) & 0x0F) << 1
							p := read16(room.WRAM[:], 0x0500+j)
							//fmt.Printf("    manip(%s) %02x = %04x\n", t, v, p)
							if p == 0 {
								//fmt.Printf("    pushBlock(%s)\n", t)

								// push block flips 0x0641
								write8(room.WRAM[:], 0x0641, 0x01)
								if read8(room.WRAM[:], 0xAE)|read8(room.WRAM[:], 0xAF) != 0 {
									// handle tags if there are any after the push to see if it triggers a secret:
									room.HandleRoomTags()
									// TODO: properly determine which tag was activated
									room.TilesVisited = room.TilesVisitedTag0
								}
							}
							return
						}

						v16 := read16(room.Tiles[:], uint32(t))
						if v16 == 0x3A3A || v16 == 0x3B3B {
							//fmt.Printf("    star(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							room.HandleRoomTags()

							// swap out visited maps:
							if read8(room.WRAM[:], 0x04BC) == 0 {
								//fmt.Printf("    star0\n")
								room.TilesVisited = room.TilesVisitedStar0
								//ioutil.WriteFile(fmt.Sprintf("%03X.cmap0", uint16(this)), room.Tiles[:], 0644)
							} else {
								//fmt.Printf("    star1\n")
								room.TilesVisited = room.TilesVisitedStar1
								//ioutil.WriteFile(fmt.Sprintf("%03X.cmap1", uint16(this)), room.Tiles[:], 0644)
							}
							return
						}

						// floor or pressure switch:
						if v16 == 0x2323 || v16 == 0x2424 {
							//fmt.Printf("    switch(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							if room.HandleRoomTags() {
								// reset current room visited state:
								for i := range room.TilesVisited {
									delete(room.TilesVisited, i)
								}
								//ioutil.WriteFile(fmt.Sprintf("%03X.cmap0", uint16(this)), room.Tiles[:], 0644)
							}
							return
						}
					}
				},
			)

			//ioutil.WriteFile(fmt.Sprintf("%03X.rch", uint16(this)), room.Reachable[:], 0644)

			//fmt.Printf("entrance $%02x supertile %s discover from entry %s complete\n", eID, room.Supertile, ep)
		}

		room.Unlock()
	}

	// render all supertiles found:
	for _, room := range g.Rooms {
		if supertileGifs || animateRoomDrawing {
			wg.Add(1)
			go func(r *RoomState) {
				r.Lock()
				defer r.Unlock()

				fmt.Printf("entrance $%02x supertile %s draw start\n", g.EntranceID, r.Supertile)

				if supertileGifs {
					RenderGIF(&r.GIF, fmt.Sprintf("%03x.%02x.gif", uint16(r.Supertile), r.Entrance.EntranceID))
				}

				if animateRoomDrawing {
					RenderGIF(&r.Animated, fmt.Sprintf("%03x.%02x.room.gif", uint16(r.Supertile), r.Entrance.EntranceID))
				}

				fmt.Printf("entrance $%02x supertile %s draw complete\n", g.EntranceID, r.Supertile)
				wg.Done()
			}(room)
		}

		// render VRAM BG tiles to a PNG:
		if false {
			cgram := (*(*[0x100]uint16)(unsafe.Pointer(&room.WRAM[0xC300])))[:]
			pal := cgramToPalette(cgram)

			tiles := 0x4000 / 32
			g := image.NewPaletted(image.Rect(0, 0, 16*8, (tiles/16)*8), pal)
			for t := 0; t < tiles; t++ {
				// palette 2
				z := uint16(t) | (2 << 10)
				draw4bppBGTile(
					g,
					z,
					(&room.VRAMTileSet)[:],
					t%16,
					t/16,
				)
			}

			if err = exportPNG(fmt.Sprintf("%03X.vram.png", uint16(room.Supertile)), g); err != nil {
				panic(err)
			}
		}
	}
}

func scanForTileTypes(e *System) {
	var err error

	// scan underworld for certain tile types:
	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC-0x5000] = 0x00
	// load the entrance and draw the room:
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}

	for st := uint16(0); st < 0x128; st++ {
		// load and draw current supertile:
		write16(e.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, st)
		if err = e.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
			panic(err)
		}

		found := false
		for t, v := range e.WRAM[0x12000:0x14000] {
			if v == 0x0A {
				found = true
				fmt.Printf("%s: %s = $0A\n", Supertile(st), MapCoord(t))
			}
		}

		if found {
			ioutil.WriteFile(fmt.Sprintf("%03x.tmap", st), e.WRAM[0x12000:0x14000], 0644)
		}
	}

	return

}

type empty = struct{}

type LinkState uint8

const (
	StateWalk LinkState = iota
	StateFall
	StateSwim
	StatePipe
)

func (s LinkState) String() string {
	switch s {
	case StateWalk:
		return "walk"
	case StateFall:
		return "fall"
	case StateSwim:
		return "swim"
	case StatePipe:
		return "pipe"
	default:
		panic("bad LinkState")
	}
}

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
	sync.Mutex

	Supertile

	Entrance *Entrance

	IsLoaded bool

	Rendered image.Image
	GIF      gif.GIF

	Animated        gif.GIF // single room drawing animation
	AnimatedTileMap [][0x4000]byte
	AnimatedLayers  []int

	AnimatedLayer int

	EnemyMovementGIF gif.GIF

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

	e               System
	WRAM            [0x20000]byte
	WRAMAfterLoaded [0x20000]byte
	VRAMTileSet     [0x4000]byte

	markedPit   bool
	markedFloor bool
	lifoSpace   [0x2000]ScanState
	lifo        []ScanState
}

func CreateRoom(ent *Entrance, st Supertile, initEmu *System) (room *RoomState) {
	var err error

	//fmt.Printf("    creating room %s\n", st)

	room = &RoomState{
		Supertile:         st,
		Entrance:          ent,
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

func (room *RoomState) Init(ep EntryPoint) (err error) {
	if room.IsLoaded {
		return
	}

	st := room.Supertile

	namePrefix := fmt.Sprintf("t%03x.e%02x", uint16(st), room.Entrance.EntranceID)

	e := &room.e
	wram := (e.WRAM)[:]
	vram := (e.VRAM)[:]
	tiles := room.Tiles[:]

	// load and draw current supertile:
	write16(wram, 0xA0, uint16(st))
	write16(wram, 0x048E, uint16(st))

	if animateRoomDrawing {
		// clear tile map first:
		tilemap := e.WRAM[0x2000:0x6000]
		for i := range tilemap {
			tilemap[i] = 0x00
		}

		captureStart := false
		room.AnimatedLayer = 0

		doCapture := func() {
			if captureStart {
				room.CaptureRoomDrawFrame()
			}
		}

		e.CPU.OnPC = make(map[uint32]func())

		//#_018834: JSR RoomDraw_DrawAllObjects
		//#_018837: PLY
		e.CPU.OnPC[0x01_8837] = func() {
			// start capturing after basic room layout (template) is drawn:
			captureStart = true
			room.AnimatedLayer++
			doCapture()
		}

		// draw layer 2:
		e.CPU.OnPC[0x01_885F] = func() {
			room.AnimatedLayer++
			//doCapture()
		}
		// draw layer 3 (aka doors):
		e.CPU.OnPC[0x01_8874] = func() {
			room.AnimatedLayer++
			//doCapture()
		}

		//RoomDraw_A_Many32x32Blocks:#_018A44
		//#_018A88: RTS
		e.CPU.OnPC[0x01_8A88] = doCapture

		//#_01880F: JSR RoomDraw_DrawFloors
		//#_018812: LDY.b $BA
		e.CPU.OnPC[0x01_8812] = doCapture

		//#_0188F8: JSR RoomData_DrawObject
		//#_0188FB: BRA .next
		e.CPU.OnPC[0x01_88FB] = doCapture

		//#_01890D: JSR RoomData_DrawObject_Door
		//#_018910: INC.b $BA
		e.CPU.OnPC[0x01_8910] = doCapture
	}

	//e.LoggerCPU = e.Logger
	if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
		return
	}
	//e.LoggerCPU = nil

	// make a copy of WRAM after loading supertile:
	room.WRAMAfterLoaded = room.WRAM

	if animateRoomDrawing {
		// capture final frame:
		room.CaptureRoomDrawFrame()

		// copy final frame to first frame so it works as a preview image:
		frames := make([][0x4000]byte, 0, len(room.AnimatedTileMap)+1)
		frames = append(frames, room.AnimatedTileMap[len(room.AnimatedTileMap)-1])
		frames = append(frames, room.AnimatedTileMap[1:]...)

		room.AnimatedTileMap = frames

		e.CPU.OnPC = nil
	}

	copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])
	copy(tiles, wram[0x12000:0x14000])

	// make a map full of $01 Collision and carve out reachable areas:
	for i := range room.Reachable {
		room.Reachable[i] = 0x01
	}

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(st)), wram, 0644)
	//ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(st)), tiles, 0644)

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

	//fmt.Printf("    TAG1 = %02x\n", read8(wram, 0xAE))
	//fmt.Printf("    TAG2 = %02x\n", read8(wram, 0xAF))
	//fmt.Printf("    WARPTO   = %s\n", Supertile(read8(wram, 0xC000)))
	//fmt.Printf("    STAIR0TO = %s\n", Supertile(read8(wram, 0xC001)))
	//fmt.Printf("    STAIR1TO = %s\n", Supertile(read8(wram, 0xC002)))
	//fmt.Printf("    STAIR2TO = %s\n", Supertile(read8(wram, 0xC003)))
	//fmt.Printf("    STAIR3TO = %s\n", Supertile(read8(wram, 0xC004)))
	//fmt.Printf("    DARK     = %v\n", room.IsDarkRoom())

	if !staticEntranceMap {
		// process doors first:
		doors := make([]Door, 0, 16)
		for m := 0; m < 16; m++ {
			tpos := read16(wram, uint32(0x19A0+(m<<1)))
			// stop marker:
			if tpos == 0 {
				//fmt.Printf("    door stop at marker\n")
				break
			}

			door := Door{
				Pos:  MapCoord(tpos >> 1),
				Type: DoorType(read16(wram, uint32(0x1980+(m<<1)))),
				Dir:  Direction(read16(wram, uint32(0x19C0+(m<<1)))),
			}
			doors = append(doors, door)

			//fmt.Printf("    door: %v\n", door)

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
				//fmt.Printf("    exploding wall %s\n", door.Pos)
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
						//fmt.Printf("    blow open %s\n", tn)
						tiles[tn] = doorwayTile
						//fmt.Printf("    blow open %s\n", MapCoord(int(tn)+adj))
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
					//fmt.Printf(fmt.Sprintf("unrecognized door tile at %s: $%02x\n", start, doorTileType))
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
			//fmt.Printf("    interroom stair at %s\n", t)
		}

		for i := uint32(0); i < 0x20; i += 2 {
			pos := MapCoord(read16(wram, 0x0540+i) >> 1)
			if pos == 0 {
				break
			}
			//fmt.Printf(
			//	"    manipulable(%s): %02x, %04x @%04x -> %04x%04x,%04x%04x\n",
			//	pos,
			//	i,
			//	read16(wram, 0x0500+i), // MANIPPROPS
			//	read16(wram, 0x0520+i), // MANIPOBJX
			//	read16(wram, 0x0560+i), // MANIPRTNW
			//	read16(wram, 0x05A0+i), // MANIPRTNE
			//	read16(wram, 0x0580+i), // MANIPRTSW
			//	read16(wram, 0x05C0+i), // MANIPRTSE
			//)
		}

		for i := uint32(0); i < 6; i++ {
			gt := read16(wram, 0x06E0+i<<1)
			if gt == 0 {
				break
			}

			//fmt.Printf("    chest($%04x)\n", gt)

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
	}

	// clear all enemy health to see if this triggers something:
	//for i := uint32(0); i < 16; i++ {
	//	write8(room.WRAM[:], 0x0DD0+i, 0)
	//}

	if false {
		// start GIF with solid black frame:
		room.GIF.BackgroundIndex = 0
		room.GIF.Image = append(room.GIF.Image, image.NewPaletted(
			image.Rect(0, 0, 512, 512),
			color.Palette{
				color.Black,
			},
		))
		room.GIF.Delay = append(room.GIF.Delay, 2)
		room.GIF.Disposal = append(room.GIF.Disposal, gif.DisposalNone)
	}

	// dump enemy state:
	//fmt.Println(hex.Dump(wram[0x0D00:0x0FA0]))

	// capture first room state:
	room.DrawSupertile()

	room.RenderAnimatedRoomDraw(animateRoomDrawingDelay)

	room.HandleRoomTags()

	f := len(room.GIF.Delay) - 1
	if f >= 0 {
		room.GIF.Delay[f] += 200
	}

	if enemyMovementFrames > 0 {
		isInteresting := false
		spriteDead := [16]bool{}
		spriteID := [16]uint8{}
		for j := 0; j < 16; j++ {
			spriteDead[j] = read8(wram, uint32(0x0DD0+j)) == 0
			spriteID[j] = read8(wram, uint32(0x0E20+j))
		}

		// reset WRAM to initial supertile load:
		room.WRAM = room.WRAMAfterLoaded

		// place Link at the entrypoint:
		{
			linkX, linkY := ep.Point.ToAbsCoord(st)
			// nudge link within visible bounds:
			if linkX&0x1FF < 0x20 {
				linkX += 0x20
			}
			if linkX&0x1FF > 0x1E0 {
				linkX -= 0x20
			}
			if linkY&0x1FF < 0x20 {
				linkY += 0x20
			}
			if linkY&0x1FF > 0x1E0 {
				linkY -= 0x20
			}
			linkY += 14
			write16(wram, 0x22, linkX)
			write16(wram, 0x20, linkY)
		}

		gifName := fmt.Sprintf("data/%s.move.gif", namePrefix)
		fmt.Printf("rendering %s\n", gifName)

		// first frame of enemy movement GIF:
		var lastFrame *image.Paletted
		{
			pal, bg1p, bg2p, addColor, halfColor := room.RenderBGLayers()
			if false {
				if err := exportPNG(fmt.Sprintf("data/%03x.%02x.bg1.0.png", uint16(room.Supertile), room.Entrance.EntranceID), bg1p[0]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("data/%03x.%02x.bg1.1.png", uint16(room.Supertile), room.Entrance.EntranceID), bg1p[1]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("data/%03x.%02x.bg2.0.png", uint16(room.Supertile), room.Entrance.EntranceID), bg2p[0]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("data/%03x.%02x.bg2.1.png", uint16(room.Supertile), room.Entrance.EntranceID), bg2p[1]); err != nil {
					panic(err)
				}
			}
			g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
			ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
			room.RenderSprites(g)

			lastFrame = g
			room.EnemyMovementGIF.Image = append(room.EnemyMovementGIF.Image, g)
			room.EnemyMovementGIF.Delay = append(room.EnemyMovementGIF.Delay, 20)
			room.EnemyMovementGIF.Disposal = append(room.EnemyMovementGIF.Disposal, gif.DisposalNone)
			room.EnemyMovementGIF.LoopCount = 0
			room.EnemyMovementGIF.BackgroundIndex = 0
		}

		// run for N frames and render each frame into a GIF:
		sx, sy := room.Supertile.AbsTopLeft()
		fmt.Printf(
			"t%03x: abs=(%04x, %04x), bg1=(%04x,%04x), bg2=(%04x,%04x)\n",
			uint16(room.Supertile),
			sx,
			sy,
			read16(wram, 0xE0),
			read16(wram, 0xE6),
			read16(wram, 0xE2),
			read16(wram, 0xE8),
		)

	movement:
		for i := 0; i < enemyMovementFrames; i++ {
			//fmt.Println("FRAME")
			//e.LoggerCPU = os.Stdout
			// move camera to all four quadrants to get all enemies moving:
			for j := 0; j < 4; j++ {
				// BG1H
				write16(wram, 0xE0, uint16(j&1)<<8+sx)
				// BG2H
				write16(wram, 0xE2, uint16(j&1)<<8+sx)
				// BG1V
				write16(wram, 0xE6, uint16(j&2)<<7+sy)
				// BG2V
				write16(wram, 0xE8, uint16(j&2)<<7+sy)

				if err := e.ExecAtUntil(b00RunSingleFramePC, 0, 0x200000); err != nil {
					fmt.Fprintln(os.Stderr, err)
					break movement
				}
				//e.LoggerCPU = nil

				// sanity check:
				if supertileWram := read16(wram, 0xA0); supertileWram != uint16(room.Supertile) {
					panic(fmt.Sprintf("%s: supertile in wram does not match expected", namePrefix))
				}

				// update tile sets after NMI; e.g. animated tiles:
				copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])
				copy(tiles, wram[0x12000:0x14000])

				// check for any killed sprites:
				for j := 0; j < 16; j++ {
					if spriteDead[j] {
						continue
					}

					sDead := read8(wram, uint32(0x0DD0+j)) == 0
					if sDead {
						sID := read8(wram, uint32(0x0E20+j))
						fmt.Fprintf(os.Stderr, "%s: sprite %02x killed on frame %3d\n", namePrefix, sID, i)
						if sID == 0x9b {
							isInteresting = true
						}
						spriteDead[j] = true
					}
				}

				{
					pal, bg1p, bg2p, addColor, halfColor := room.RenderBGLayers()
					g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
					ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
					room.RenderSprites(g)

					delta := g
					dirty := false
					disposal := byte(0)
					if optimizeGIFs && room.EnemyMovementGIF.Image != nil {
						delta, dirty = generateDeltaFrame(lastFrame, g)
						//_ = exportPNG(fmt.Sprintf("data/%s.fr%03d.png", namePrefix, i), delta)
						disposal = gif.DisposalNone
					}

					if !dirty && room.EnemyMovementGIF.Image != nil {
						// just increment last frame's delay if nothing changed:
						room.EnemyMovementGIF.Delay[len(room.EnemyMovementGIF.Delay)-1] += 2
					} else {
						room.EnemyMovementGIF.Image = append(room.EnemyMovementGIF.Image, delta)
						room.EnemyMovementGIF.Delay = append(room.EnemyMovementGIF.Delay, 2)
						room.EnemyMovementGIF.Disposal = append(room.EnemyMovementGIF.Disposal, disposal)
					}
					lastFrame = g
				}
			}
		}

		fmt.Printf("rendered  %s\n", gifName)
		if isInteresting {
			RenderGIF(&room.EnemyMovementGIF, gifName)
			fmt.Printf("wrote     %s\n", gifName)
		}

		// reset WRAM:
		room.WRAM = room.WRAMAfterLoaded
	}

	// update tile sets after NMI; e.g. animated tiles:
	copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])
	copy(tiles, wram[0x12000:0x14000])

	//ioutil.WriteFile(fmt.Sprintf("data/%s.cmap", namePrefix), (&room.Tiles)[:], 0644)

	room.IsLoaded = true

	return
}

func (s *System) ExecAtUntil(startPC, donePC uint32, maxCycles uint64) (err error) {
	var stopPC uint32
	var expectedPC uint32
	var cycles uint64

	s.SetPC(startPC)
	if stopPC, expectedPC, cycles = s.RunUntil(donePC, maxCycles); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		return
	}

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

	// update last frame's delay:
	f := len(r.GIF.Delay) - 1
	if f >= 0 {
		r.GIF.Delay[f] = 200
	}

	// insert a blank GIF frame so its delay may be adjusted:
	r.GIF.Image = append(r.GIF.Image, newBlankFrame())
	r.GIF.Delay = append(r.GIF.Delay, 50)
	r.GIF.Disposal = append(r.GIF.Disposal, 0)

	lastCap := [0x4000]byte{}
	lastDelay := 167
	copy(lastCap[:], e.WRAM[0x2000:0x6000])

	e.CPU.OnWDM = func(wdm byte) {
		// capture frame to GIF:
		if wdm == 0xFF {
			// compare against last capture:
			diff := false
			for i := range lastCap {
				if lastCap[i] != e.WRAM[0x2000+i] {
					diff = true
					break
				}
			}

			if !diff {
				// increase last frame's delay:
				lastDelay += 167
				return
			}

			f := len(r.GIF.Delay) - 1
			if f >= 0 {
				if lastDelay <= 0 {
					lastDelay = 167
				}
				r.GIF.Delay[f] = lastDelay / 100
				if lastDelay%100 >= 50 {
					r.GIF.Delay[f]++
				}
			}
			lastDelay = 167
			r.DrawSupertile()

			copy(lastCap[:], e.WRAM[0x2000:0x6000])
		}
	}

	if err := e.ExecAt(b00HandleRoomTagsPC, 0); err != nil {
		panic(err)
	}

	e.CPU.OnWDM = nil

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