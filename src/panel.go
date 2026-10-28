package main

// panel.go — the Braids front panel look for the Push screen: a knob is a
// dark bezel with a coloured cap and a pointer notch, a label is a coloured
// pill, and the engine name sits on a slate LCD in yellow-green.

import (
	"image"
	"image/color"
	"math"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

var (
	panelBezel = color.NRGBA{R: 0x1E, G: 0x1E, B: 0x20, A: 255}
	panelRing  = color.NRGBA{R: 0x7A, G: 0x7A, B: 0x80, A: 255}
	panelWhite = color.NRGBA{R: 0xF4, G: 0xF4, B: 0xF2, A: 255}
	panelLCD   = color.NRGBA{R: 0x7E, G: 0x8B, B: 0x93, A: 255}
	panelSeg   = color.NRGBA{R: 0xE4, G: 0xF4, B: 0x1C, A: 255}
)

// fillDisc paints an anti-aliased filled circle.
func fillDisc(img *image.NRGBA, cx, cy int, r float64, col color.NRGBA) {
	b := int(math.Ceil(r)) + 1
	for dy := -b; dy <= b; dy++ {
		for dx := -b; dx <= b; dx++ {
			a := r + 0.5 - math.Hypot(float64(dx), float64(dy))
			blendAt(img, cx+dx, cy+dy, col, a)
		}
	}
}

func blendAt(img *image.NRGBA, x, y int, col color.NRGBA, a float64) {
	if a <= 0 || !(image.Point{x, y}).In(img.Bounds()) {
		return
	}
	if a > 1 {
		a = 1
	}
	bg := img.NRGBAAt(x, y)
	mix := func(s, d uint8) uint8 { return uint8(float64(s)*a + float64(d)*(1-a)) }
	img.SetNRGBA(x, y, color.NRGBA{R: mix(col.R, bg.R), G: mix(col.G, bg.G), B: mix(col.B, bg.B), A: 255})
}

// fillRoundRect paints a pill-shaped rectangle with corner radius r.
func fillRoundRect(img *image.NRGBA, x, y, w, h int, r float64, col color.NRGBA) {
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			// distance outside the inner rect that the corners are rounded from
			dx := math.Max(math.Max(r-float64(px)-0.5, float64(px)+0.5-(float64(w)-r)), 0)
			dy := math.Max(math.Max(r-float64(py)-0.5, float64(py)+0.5-(float64(h)-r)), 0)
			blendAt(img, x+px, y+py, col, r+0.5-math.Hypot(dx, dy))
		}
	}
}

// drawPanelKnob draws a bezel, a cap in capCol and a pointer notch at frac
// (0-1) of a 270 degree sweep, 7 o'clock to 5 o'clock — as on the module.
func drawPanelKnob(img *image.NRGBA, cx, cy, r int, frac float64, capCol color.NRGBA) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fillDisc(img, cx, cy, float64(r), panelBezel)
	fillDisc(img, cx, cy, float64(r)-4, panelRing)
	fillDisc(img, cx, cy, float64(r)-6, panelBezel)
	capR := float64(r) * 0.62
	fillDisc(img, cx, cy, capR, capCol)
	// highlight: lighter crescent, upper left
	fillDisc(img, cx-1, cy-1, capR-3, lighten(capCol, 0.18))
	fillDisc(img, cx, cy, capR-5, capCol)

	ang := (-135 + 270*frac) * math.Pi / 180 // 0 = 12 o'clock, clockwise
	for t := capR * 0.35; t <= float64(r)-1; t += 0.5 {
		fillDisc(img, cx+int(math.Round(math.Sin(ang)*t)), cy-int(math.Round(math.Cos(ang)*t)), 1.6, notchColor(capCol, t > capR))
	}
}

// notchColor: the bezel part of the pointer is light, the cap part is dark
// on a light cap and light on a dark one.
func notchColor(capCol color.NRGBA, onBezel bool) color.NRGBA {
	if onBezel {
		return panelWhite
	}
	if int(capCol.R)+int(capCol.G)+int(capCol.B) > 600 {
		return panelBezel
	}
	return panelWhite
}

func lighten(c color.NRGBA, f float64) color.NRGBA {
	m := func(v uint8) uint8 { return uint8(float64(v) + (255-float64(v))*f) }
	return color.NRGBA{R: m(c.R), G: m(c.G), B: m(c.B), A: 255}
}

// drawPill draws label centred on cx in a pill of fill, or as plain ink
// text when fill is the zero colour.
func drawPill(img *image.NRGBA, cx, y int, label string, fill color.NRGBA) {
	tw := text.Width(label)
	if fill == (color.NRGBA{}) {
		text.Draw(img, cx-tw/2, y+10, label, mutableBlack)
		return
	}
	w := tw + 12
	fillRoundRect(img, cx-w/2, y, w, 13, 3, fill)
	text.Draw(img, cx-tw/2, y+10, label, panelWhite)
}

// drawLCD paints the slate display with name in yellow-green, scaled to fit.
func drawLCD(img *image.NRGBA, x, y, w, h int, name string, scale int) {
	fillRoundRect(img, x, y, w, h, 4, panelLCD)
	tw := text.WidthScaled(name, scale)
	if tw > w-6 {
		name = text.Truncate(name, (w-6)/scale)
		tw = text.WidthScaled(name, scale)
	}
	text.DrawScaled(img, x+(w-tw)/2, y+h-11, scale, name, panelSeg)
}

// capColor: Timbre and FM wear the teal cap, Color the red one, the rest
// the white one — the same split knobColor makes for the label pills.
func capColor(key string) color.NRGBA {
	if c := knobColor(key); c != (color.NRGBA{}) {
		return c
	}
	return panelWhite
}

// slotFrac is the slot's value as a 0-1 place in its range.
func slotFrac(s *paramSlot) float64 {
	if s.meta.Max <= s.meta.Min {
		return 0
	}
	return (s.value - s.meta.Min) / (s.meta.Max - s.meta.Min)
}

// drawValueReadout writes the slot's value under its knob.
func drawValueReadout(img *image.NRGBA, cx int, s *paramSlot) {
	v := formatValue(s)
	text.Draw(img, cx-text.Width(v)/2, knobCY+knobR+14, v, mutableBlack)
}

// drawPillStrip draws the bottom labels as pills on the panel, in place of
// the framework's solid bar. A button's Color picks its pill; no colour is
// plain ink text. SoftConfirm gets the magenta pill.
func drawPillStrip(img *image.NRGBA, buttons [8]widgets.SoftButton) {
	gfx.FillRect(img, 0, screenH-botStripH, screenW, botStripH, mutableBeige)
	for i, b := range buttons {
		if b.Label == "" {
			continue
		}
		fill := b.Color
		if b.State == widgets.SoftConfirm {
			fill = mutableMagenta
		}
		drawPill(img, i*cellW+cellW/2, screenH-botStripH+1, b.Label, fill)
	}
}
