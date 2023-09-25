package main

import (
	"bufio"
	"fmt"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"sync"
	"unsafe"
)

func renderAll(fname string, entranceGroups []Entrance, rowStart int, rowCount int) {
	var err error

	const divider = 1
	supertilepx := 512 / divider

	wga := &sync.WaitGroup{}

	all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (rowCount*0x10*supertilepx)/0x10))
	// clear the image and remove alpha layer
	draw.Draw(
		all,
		all.Bounds(),
		image.NewUniform(color.NRGBA{0, 0, 0, 255}),
		image.Point{},
		draw.Src)

	greenTint := image.NewUniform(color.NRGBA{0, 255, 0, 64})
	redTint := image.NewUniform(color.NRGBA{255, 0, 0, 56})
	cyanTint := image.NewUniform(color.NRGBA{0, 255, 255, 64})
	blueTint := image.NewUniform(color.NRGBA{0, 0, 255, 64})

	black := image.NewUniform(color.RGBA{0, 0, 0, 255})
	yellow := image.NewUniform(color.RGBA{255, 255, 0, 255})
	white := image.NewUniform(color.RGBA{255, 255, 255, 255})

	for i := range entranceGroups {
		g := &entranceGroups[i]
		for _, room := range g.Rooms {
			st := int(room.Supertile)

			row := st/0x10 - rowStart
			col := st % 0x10
			if row < 0 || row >= rowCount {
				continue
			}

			wga.Add(1)
			go func(room *RoomState) {
				defer wga.Done()

				fmt.Printf("entrance $%02x supertile %s render start\n", g.EntranceID, room.Supertile)

				stx := col * supertilepx
				sty := row * supertilepx

				if room.Rendered != nil {
					draw.Draw(
						all,
						image.Rect(stx, sty, stx+supertilepx, sty+supertilepx),
						room.Rendered,
						image.Point{},
						draw.Src,
					)
				}

				// highlight tiles that are reachable:
				if drawOverlays {
					maxRange := 0x2000
					if room.IsDarkRoom() {
						maxRange = 0x1000
					}

					// draw supertile over pits, bombable floors, and warps:
					for j := range room.ExitPoints {
						ep := &room.ExitPoints[j]
						if !ep.WorthMarking {
							continue
						}

						_, er, ec := ep.Point.RowCol()
						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x + 1), fixed.I(sty + y + 1)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x), fixed.I(sty + y)},
						}
						stStr := fmt.Sprintf("%02X", uint16(ep.Supertile))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					// draw supertile over stairs:
					for j := range room.Stairs {
						sn := room.StairExitTo[j]
						_, er, ec := room.Stairs[j].RowCol()

						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x + 1), fixed.I(sty - 8 + y + 1 + 12)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x), fixed.I(sty - 8 + y + 12)},
						}
						stStr := fmt.Sprintf("%02X", uint16(sn))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					for t := 0; t < maxRange; t++ {
						v := room.Reachable[t]
						if v == 0x01 {
							continue
						}

						tt := MapCoord(t)
						lyr, tr, tc := tt.RowCol()
						overlay := greenTint
						if lyr != 0 {
							overlay = cyanTint
						}
						if v == 0x20 || v == 0x62 {
							overlay = redTint
						}

						x := int(tc) << 3
						y := int(tr) << 3
						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}

					for t, d := range room.Hookshot {
						_, tr, tc := t.RowCol()
						x := int(tc) << 3
						y := int(tr) << 3

						overlay := blueTint
						_ = d

						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}
				}

				fmt.Printf("entrance $%02x supertile %s render complete\n", g.EntranceID, room.Supertile)
			}(room)
		}
	}
	wga.Wait()

	if drawNumbers {
		for r := 0; r < 0x128; r++ {
			wga.Add(1)
			go func(st int) {
				defer wga.Done()

				row := st/0x10 - rowStart
				col := st % 0x10
				if row < 0 || row >= rowCount {
					return
				}

				stx := col * supertilepx
				sty := row * supertilepx

				// draw supertile number in top-left:
				var stStr string
				if st < 0x100 {
					stStr = fmt.Sprintf("%02X", st)
				} else {
					stStr = fmt.Sprintf("%03X", st)
				}
				(&font.Drawer{
					Dst:  all,
					Src:  black,
					Face: inconsolata.Bold8x16,
					Dot:  fixed.Point26_6{fixed.I(stx + 5), fixed.I(sty + 5 + 12)},
				}).DrawString(stStr)
				(&font.Drawer{
					Dst:  all,
					Src:  white,
					Face: inconsolata.Bold8x16,
					Dot:  fixed.Point26_6{fixed.I(stx + 4), fixed.I(sty + 4 + 12)},
				}).DrawString(stStr)
			}(r)
		}
		wga.Wait()
	}

	if err = exportPNG(fmt.Sprintf("data/%s.png", fname), all); err != nil {
		panic(err)
	}
}

func (room *RoomState) CaptureRoomDrawFrame() {
	var tileMap [0x4000]byte
	copy(tileMap[:], room.WRAM[0x2000:0x6000])
	room.AnimatedTileMap = append(room.AnimatedTileMap, tileMap)
	room.AnimatedLayers = append(room.AnimatedLayers, room.AnimatedLayer)
}

func (room *RoomState) RenderAnimatedRoomDraw(frameDelay int) {
	wram := (&room.WRAM)[:]

	// assume WRAM has rendering state as well:
	isDark := room.IsDarkRoom()
	doBG2 := !isDark

	// INIDISP contains PPU brightness
	brightness := read8(wram, 0x13) & 0xF
	_ = brightness

	//subdes := read8(wram, 0x1D)
	n0414 := read8(wram, 0x0414)
	addColor := n0414 == 0x07
	halfColor := n0414 == 0x04
	//flip := n0414 == 0x03

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]

	tileset := (&room.VRAMTileSet)[:]
	var lastFrame *image.Paletted = nil

	for i, tileMap := range room.AnimatedTileMap {
		bg1wram := (*(*[0x1000]uint16)(unsafe.Pointer(&tileMap[0])))[:]
		bg2wram := (*(*[0x1000]uint16)(unsafe.Pointer(&tileMap[0x2000])))[:]

		pal := cgramToPalette(cgram)

		bg1p := [2]*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
			image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		}
		bg2p := [2]*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
			image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		}

		// render all separate BG1 and BG2 priority layers:
		layer := room.AnimatedLayers[i]
		if layer != 2 {
			renderBGsep(bg1p, bg1wram, tileset, drawBG1p0, drawBG1p1)
		}
		if doBG2 {
			renderBGsep(bg2p, bg2wram, tileset, drawBG2p0, drawBG2p1)
		}

		// swap layers depending on color math:
		if !addColor && !halfColor {
			bg1p, bg2p = bg2p, bg1p
		}

		// switch everything but the first layer to have 0 as transparent:
		//palTransp := make(color.Palette, len(pal))
		//copy(palTransp, pal)
		//palTransp[0] = color.Transparent
		//bg1p[1].Palette = palTransp
		//bg2p[0].Palette = palTransp
		//bg2p[1].Palette = palTransp

		frame := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		ComposeToPaletted(frame, pal, bg1p, bg2p, addColor, halfColor)

		delta := frame
		dirty := false
		disposal := byte(0)
		if lastFrame != nil && optimizeGIFs {
			delta, dirty = generateDeltaFrame(lastFrame, frame)
			disposal = gif.DisposalNone
		}

		// TODO
		_ = dirty
		room.Animated.Image = append(room.Animated.Image, delta)
		room.Animated.Delay = append(room.Animated.Delay, frameDelay)
		room.Animated.Disposal = append(room.Animated.Disposal, disposal)

		lastFrame = frame
	}
}

func generateDeltaFrame(prev, curr *image.Paletted) (delta *image.Paletted, dirty bool) {
	// make a special delta palette with 255 (never used) as transparent:
	pal := make(color.Palette, len(curr.Palette))
	copy(pal, curr.Palette)

	transparentIndex := uint8(255)
	pal[transparentIndex] = color.Transparent

	delta = image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
	dirty = false
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			cp := prev.ColorIndexAt(x, y)
			cc := curr.ColorIndexAt(x, y)

			if cp == cc {
				// set as transparent since nothing changed:
				delta.SetColorIndex(x, y, transparentIndex)
				continue
			}

			// use the current frame's color if it differs:
			delta.SetColorIndex(x, y, cc)
			dirty = true
		}
	}

	return
}

func (room *RoomState) DrawSupertile() {
	// gfx output is:
	//  s.VRAM: $4000[0x2000] = 4bpp tile graphics
	//  s.WRAM: $2000[0x2000] = BG1 64x64 tile map  [64][64]uint16
	//  s.WRAM: $4000[0x2000] = BG2 64x64 tile map  [64][64]uint16
	//  s.WRAM:$12000[0x1000] = BG1 64x64 tile type [64][64]uint8
	//  s.WRAM:$12000[0x1000] = BG2 64x64 tile type [64][64]uint8
	//  s.WRAM: $C300[0x0200] = CGRAM palette

	wram := (&room.WRAM)[:]

	// assume WRAM has rendering state as well:
	//isDark := room.IsDarkRoom()

	// INIDISP contains PPU brightness
	brightness := read8(wram, 0x13) & 0xF
	_ = brightness

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	// render BG layers:
	pal, bg1p, bg2p, addColor, halfColor := room.RenderBGLayers()

	//subdes := read8(wram, 0x1D)
	//n0414 := read8(wram, 0x0414)
	//flip := n0414 == 0x03

	if room.Rendered != nil {
		// subsequent GIF frames:
		frame := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		ComposeToPaletted(frame, pal, bg1p, bg2p, addColor, halfColor)

		room.GIF.Image = append(room.GIF.Image, frame)
		room.GIF.Delay = append(room.GIF.Delay, 50)
		room.GIF.Disposal = append(room.GIF.Disposal, gif.DisposalNone)

		return
	}

	// switch everything but the first layer to have 0 as transparent:
	//order[0].Palette = pal
	//palTransp := make(color.Palette, len(pal))
	//copy(palTransp, pal)
	//palTransp[0] = color.Transparent
	//for p := 1; p < 4; p++ {
	//	order[p].Palette = palTransp
	//}

	// first GIF frames build up the layers:
	frames := [4]*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
	}

	ComposeToPaletted(frames[0], pal, bg1p, bg2p, addColor, halfColor)
	ComposeToPaletted(frames[1], pal, bg1p, bg2p, addColor, halfColor)
	ComposeToPaletted(frames[2], pal, bg1p, bg2p, addColor, halfColor)
	ComposeToPaletted(frames[3], pal, bg1p, bg2p, addColor, halfColor)

	//renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], blankFrame, blankFrame, blankFrame}, addColor, halfColor),
	//renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], blankFrame, blankFrame}, addColor, halfColor),
	//renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], order[2], blankFrame}, addColor, halfColor),
	//renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], order[2], order[3]}, addColor, halfColor),

	room.GIF.Image = append(room.GIF.Image, frames[:]...)
	room.GIF.Delay = append(room.GIF.Delay, 50, 50, 50, 50)
	room.GIF.Disposal = append(room.GIF.Disposal, 0, 0, 0, 0)

	g := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	ComposeToNonPaletted(g, pal, bg1p, bg2p, addColor, halfColor)

	//if isDark {
	//	// darken the room
	//	draw.Draw(
	//		g,
	//		g.Bounds(),
	//		image.NewUniform(color.RGBA64{0, 0, 0, 0x8000}),
	//		image.Point{},
	//		draw.Over,
	//	)
	//}

	//if brightness < 15 {
	//	draw.Draw(
	//		g,
	//		g.Bounds(),
	//		image.NewUniform(color.RGBA64{0, 0, 0, uint16(brightness) << 12}),
	//		image.Point{},
	//		draw.Over,
	//	)
	//}

	// store full underworld rendering for inclusion into EG map:
	room.Rendered = g

	if drawRoomPNGs {
		if err := exportPNG(fmt.Sprintf("data/%03X.png", uint16(room.Supertile)), g); err != nil {
			panic(err)
		}
	}

	if drawBGLayerPNGs {
		if err := exportPNG(fmt.Sprintf("data/%03X.bg1.0.png", uint16(room.Supertile)), bg1p[0]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg1.1.png", uint16(room.Supertile)), bg1p[1]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg2.0.png", uint16(room.Supertile)), bg2p[0]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg2.1.png", uint16(room.Supertile)), bg2p[1]); err != nil {
			panic(err)
		}
	}
}

func (room *RoomState) RenderBGLayers() (
	pal color.Palette,
	bg1p [2]*image.Paletted,
	bg2p [2]*image.Paletted,
	addColor, halfColor bool,
) {
	return renderBGLayers(&room.WRAM, (&room.VRAMTileSet)[:])
}

func renderBGLayers(wramArray *WRAMArray, tileset []uint8) (
	pal color.Palette,
	bg1p [2]*image.Paletted,
	bg2p [2]*image.Paletted,
	addColor, halfColor bool,
) {
	wram := wramArray[:]

	// assume WRAM has rendering state as well:
	//isDark := room.IsDarkRoom()
	isDark := read8(wram, 0xC005) != 0

	// INIDISP contains PPU brightness
	//brightness := read8(wram, 0x13) & 0xF
	//_ = brightness

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	// extract palette:
	cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
	pal = cgramToPalette(cgram)

	// render BG layers:
	bg1p = [2]*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
	}
	bg2p = [2]*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
	}

	// render all separate BG1 and BG2 priority layers:
	{
		bg1wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:]
		renderBGsep(bg1p, bg1wram, tileset, drawBG1p0, drawBG1p1)
		if !isDark {
			bg2wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:]
			renderBGsep(bg2p, bg2wram, tileset, drawBG2p0, drawBG2p1)
		}
	}

	//subdes := read8(wram, 0x1D)
	n0414 := read8(wram, 0x0414)
	addColor = n0414 == 0x07
	halfColor = n0414 == 0x04
	flip := n0414 == 0x03

	// swap bg1 and bg2 if color math is involved:
	if !addColor && !halfColor && !flip {
		bg1p, bg2p = bg2p, bg1p
	}

	return
}

func drawShadowedString(g draw.Image, clr image.Image, dot fixed.Point26_6, s string) {
	// shadow:
	for oy := -1; oy <= 1; oy++ {
		for ox := -1; ox <= 1; ox++ {
			(&font.Drawer{
				Dst:  g,
				Src:  image.Black,
				Face: inconsolata.Bold8x16,
				Dot:  fixed.Point26_6{X: dot.X + fixed.I(ox), Y: dot.Y + fixed.I(oy)},
			}).DrawString(s)
		}
	}

	// regular label:
	(&font.Drawer{
		Dst:  g,
		Src:  clr,
		Face: inconsolata.Bold8x16,
		Dot:  dot,
	}).DrawString(s)
}

func drawOutlineBox(g draw.Image, clr image.Image, x, y int, w, h int) {
	// outline box:
	draw.Draw(g, image.Rect(x, y, x+w, y+1), clr, image.Point{}, draw.Over)
	draw.Draw(g, image.Rect(x+w, y, x+w+1, y+h), clr, image.Point{}, draw.Over)
	draw.Draw(g, image.Rect(x, y+h, x+w, y+h+1), clr, image.Point{}, draw.Over)
	draw.Draw(g, image.Rect(x, y, x+1, y+h), clr, image.Point{}, draw.Over)
}

func (room *RoomState) RenderSprites(g draw.Image) {
	wram := (&room.WRAM)[:]

	//black := image.NewUniform(color.RGBA{0, 0, 0, 255})
	yellow := image.NewUniform(color.RGBA{255, 255, 0, 255})
	red := image.NewUniform(color.RGBA{255, 48, 48, 255})

	// draw sprites:
	for i := uint32(0); i < 16; i++ {
		clr := yellow

		// Initial AI state on load:
		//initialAIState := read8(room.WRAMAfterLoaded[:], 0x0DD0+i)
		//if initialAIState == 0 {
		//	// nothing was ever here:
		//	continue
		//}

		// determine if in bounds:
		yl, yh := read8(wram, 0x0D00+i), read8(wram, 0x0D20+i)
		xl, xh := read8(wram, 0x0D10+i), read8(wram, 0x0D30+i)
		y := uint16(yl) | uint16(yh)<<8
		x := uint16(xl) | uint16(xh)<<8
		if !room.IsAbsInBounds(x, y) {
			continue
		}

		// AI state:
		st := read8(wram, 0x0DD0+i)
		// enemy type:
		et := read8(wram, 0x0E20+i)

		var lx, ly int
		if true {
			lx = int(x) & 0x1FF
			ly = int(y) & 0x1FF
		} else {
			coord := AbsToMapCoord(x, y, 0)
			_, row, col := coord.RowCol()
			lx = int(col << 3)
			ly = int(row << 3)
		}

		//fmt.Printf(
		//	"%02x @ abs(%04x, %04x) -> map(%04x, %04x)\n",
		//	et,
		//	x,
		//	y,
		//	col,
		//	row,
		//)

		hb := hitbox[read8(wram, 0x0F60+i)&0x1F]

		if st == 0 {
			// dead:
			clr = red
		}

		drawOutlineBox(g, clr, lx+hb.X, ly+hb.Y, hb.W, hb.H)

		// colored number label:
		drawShadowedString(g, clr, fixed.Point26_6{X: fixed.I(lx), Y: fixed.I(ly + 12)}, fmt.Sprintf("%02X", et))
	}

	// draw Link:
	{
		x := read16(wram, 0x22)
		y := read16(wram, 0x20)
		var lx, ly int
		if true {
			lx = int(x) & 0x1FF
			ly = int(y) & 0x1FF
		} else {
			coord := AbsToMapCoord(x, y, 0)
			_, row, col := coord.RowCol()
			lx = int(col << 3)
			ly = int(row << 3)
		}

		green := image.NewUniform(color.RGBA{0, 255, 0, 255})
		drawOutlineBox(g, green, lx, ly, 16, 16)
		drawShadowedString(g, green, fixed.Point26_6{X: fixed.I(lx), Y: fixed.I(ly + 12)}, "LK")
	}
}

func newBlankFrame() *image.Paletted {
	return image.NewPaletted(
		image.Rect(0, 0, 512, 512),
		color.Palette{color.Transparent},
	)
}

// saturate a 16-bit value:
func sat(v uint32) uint16 {
	if v > 0xffff {
		return 0xffff
	}
	return uint16(v)
}

// prefer p1's color unless it's zero:
func pick(c0, c1 uint8) uint8 {
	if c1 != 0 {
		return c1
	} else {
		return c0
	}
}

func ComposeToNonPaletted(
	dst draw.Image,
	pal color.Palette,
	bg1p [2]*image.Paletted,
	bg2p [2]*image.Paletted,
	addColor bool,
	halfColor bool,
) {
	mx := dst.Bounds().Min.X
	my := dst.Bounds().Min.Y
	if halfColor {
		// color math: add half
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				bg1c := pick(bg1p[0].ColorIndexAt(x, y), bg1p[1].ColorIndexAt(x, y))
				bg2c := pick(bg2p[0].ColorIndexAt(x, y), bg2p[1].ColorIndexAt(x, y))
				if bg2c != 0 {
					r1, g1, b1, _ := pal[bg1c].RGBA()
					r2, g2, b2, _ := pal[bg2c].RGBA()
					c := color.RGBA64{
						R: sat(r1>>1 + r2>>1),
						G: sat(g1>>1 + g2>>1),
						B: sat(b1>>1 + b2>>1),
						A: 0xffff,
					}
					dst.Set(mx+x, my+y, c)
				} else {
					dst.Set(mx+x, my+y, pal[bg1c])
				}
			}
		}
	} else if addColor {
		// color math: add
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				bg1c := pick(bg1p[0].ColorIndexAt(x, y), bg1p[1].ColorIndexAt(x, y))
				bg2c := pick(bg2p[0].ColorIndexAt(x, y), bg2p[1].ColorIndexAt(x, y))
				r1, g1, b1, _ := pal[bg1c].RGBA()
				r2, g2, b2, _ := pal[bg2c].RGBA()
				c := color.RGBA64{
					R: sat(r1 + r2),
					G: sat(g1 + g2),
					B: sat(b1 + b2),
					A: 0xffff,
				}
				dst.Set(mx+x, my+y, c)
			}
		}
	} else {
		// no color math:
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				c0 := bg1p[0].ColorIndexAt(x, y)
				c1 := bg1p[1].ColorIndexAt(x, y)
				c2 := bg2p[0].ColorIndexAt(x, y)
				c3 := bg2p[1].ColorIndexAt(x, y)
				c := pick(pick(c0, c1), pick(c2, c3))
				dst.Set(mx+x, my+y, pal[c])
			}
		}
	}
}

func ComposeToPaletted(
	dst *image.Paletted,
	pal color.Palette,
	bg1p [2]*image.Paletted,
	bg2p [2]*image.Paletted,
	addColor bool,
	halfColor bool,
) {
	// store mixed colors in second half of palette which is unused by BG layers:
	hc := uint8(128)
	mixedColors := make(map[uint16]uint8, 0x200)

	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			var c uint8
			c0 := bg1p[0].ColorIndexAt(x, y)
			c1 := bg1p[1].ColorIndexAt(x, y)
			c2 := bg2p[0].ColorIndexAt(x, y)
			c3 := bg2p[1].ColorIndexAt(x, y)

			m1 := pick(c0, c1)
			m2 := pick(c2, c3)

			if addColor || halfColor {
				if m2 == 0 {
					c = m1
				} else {
					key := uint16(m1) | uint16(m2)<<8

					var ok bool
					if c, ok = mixedColors[key]; !ok {
						c = hc
						r1, g1, b1, _ := pal[m1].RGBA()
						r2, g2, b2, _ := pal[m2].RGBA()
						if halfColor {
							pal[c] = color.RGBA64{
								R: sat(r1>>1 + r2>>1),
								G: sat(g1>>1 + g2>>1),
								B: sat(b1>>1 + b2>>1),
								A: 0xffff,
							}
						} else {
							pal[c] = color.RGBA64{
								R: sat(r1 + r2),
								G: sat(g1 + g2),
								B: sat(b1 + b2),
								A: 0xffff,
							}
						}
						mixedColors[key] = c
						hc++
					}
				}
			} else {
				c = pick(m1, m2)
			}

			dst.SetColorIndex(x, y, c)
		}
	}

	dst.Palette = pal
}

func RenderGIF(g *gif.GIF, fname string) {
	// present last frame for 3 seconds:
	f := len(g.Delay) - 1
	if f >= 0 {
		g.Delay[f] = 300
	}

	// render GIF:
	gw, err := os.OpenFile(
		fname,
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		panic(err)
	}
	defer gw.Close()

	err = gif.EncodeAll(gw, g)
	if err != nil {
		panic(err)
	}
}

func exportPNG(name string, g image.Image) (err error) {
	// export to PNG:
	var po *os.File

	po, err = os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() {
		err = po.Close()
		if err != nil {
			return
		}
	}()

	bo := bufio.NewWriterSize(po, 8*1024*1024)

	err = png.Encode(bo, g)
	if err != nil {
		return
	}

	err = bo.Flush()
	if err != nil {
		return
	}

	return
}

var gammaRamp = [...]uint8{
	0x00, 0x01, 0x03, 0x06, 0x0a, 0x0f, 0x15, 0x1c,
	0x24, 0x2d, 0x37, 0x42, 0x4e, 0x5b, 0x69, 0x78,
	0x88, 0x90, 0x98, 0xa0, 0xa8, 0xb0, 0xb8, 0xc0,
	0xc8, 0xd0, 0xd8, 0xe0, 0xe8, 0xf0, 0xf8, 0xff,
}

func cgramToPalette(cgram []uint16) color.Palette {
	pal := make(color.Palette, 256)
	for i, bgr15 := range cgram {
		// convert BGR15 color format (MSB unused) to RGB24:
		b := (bgr15 & 0x7C00) >> 10
		g := (bgr15 & 0x03E0) >> 5
		r := bgr15 & 0x001F
		if useGammaRamp {
			pal[i] = color.NRGBA{
				R: gammaRamp[r],
				G: gammaRamp[g],
				B: gammaRamp[b],
				A: 0xff,
			}
		} else {
			pal[i] = color.NRGBA{
				R: uint8(r<<3 | r>>2),
				G: uint8(g<<3 | g>>2),
				B: uint8(b<<3 | b>>2),
				A: 0xff,
			}
		}
	}
	return pal
}

func renderBG(g *image.Paletted, bg []uint16, tiles []uint8, prio uint8) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			// priority check:
			if (z&0x2000 != 0) != (prio != 0) {
				continue
			}

			draw4bppTile(g, z, tiles, tx, ty)
		}
	}
}

func renderBGsep(g [2]*image.Paletted, bg []uint16, tiles []uint8, p0 bool, p1 bool) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			// priority check:
			p := (z & 0x2000) >> 13
			if p == 0 && !p0 {
				continue
			}
			if p == 1 && !p1 {
				continue
			}
			draw4bppTile(g[p], z, tiles, tx, ty)
		}
	}
}

func draw4bppTile(g *image.Paletted, z uint16, tiles []uint8, tx int, ty int) {
	//High     Low          Legend->  c: Starting character (tile) number
	//vhopppcc cccccccc               h: horizontal flip  v: vertical flip
	//                                p: palette number   o: priority bit

	p := byte((z>>10)&7) << 4
	c := int(z & 0x03FF)
	for y := 0; y < 8; y++ {
		fy := y
		if z&0x8000 != 0 {
			fy = 7 - y
		}
		p0 := tiles[(c<<5)+(y<<1)]
		p1 := tiles[(c<<5)+(y<<1)+1]
		p2 := tiles[(c<<5)+(y<<1)+16]
		p3 := tiles[(c<<5)+(y<<1)+17]
		for x := 0; x < 8; x++ {
			fx := x
			if z&0x4000 == 0 {
				fx = 7 - x
			}

			i := byte((p0>>x)&1) |
				byte(((p1>>x)&1)<<1) |
				byte(((p2>>x)&1)<<2) |
				byte(((p3>>x)&1)<<3)

			// transparency:
			if i == 0 {
				continue
			}

			g.SetColorIndex(tx<<3+fx, ty<<3+fy, p+i)
		}
	}
}
