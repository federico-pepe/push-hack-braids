package main

// display.go — draws the current page and pushes it to push-manager's
// display API. Same pattern as hacks/keyboard-visualizer/src/render.go: an
// HTTP client of push-manager only, never touching the shared-memory
// framebuffer directly — see CLAUDE.md's "Display-owning hacks" section
// for why that discipline matters.
//
// Off by default: the UI only takes the screen (and enables push-manager's
// MIDI intercept, so pad hits drive Braids instead of also reaching Live)
// while toggled on via Shift+Device — see chord.go and toggleUI below.
//
// Page identity is shown by the top-screen buttons' own LEDs (leds.go),
// not an on-screen header — there's no top-of-screen title on any page.

import (
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
	"github.com/federico-pepe/ableton-push-hack/core/pmclient"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	screenW = push3.VisW
	screenH = push3.VisH

	cellW  = screenW / 8
	knobCX = 60 // knob center within its cell (cellW/2)
	knobCY = 78
	knobR  = 28

	botStripH = 16 // bottom label/action strip height, all pages
	topStripH = 16 // top page-tab strip height, all pages
)

// Mutable Instruments' own panel palette (the user's exact hex values),
// used on the 3 knob-bearing pages (OSC/AMP, FILTER, PRESETS) — beige
// panel, black print, green for Timbre/Modulation-style knobs, magenta for
// Color. SETTINGS and the "not ready" OSD stay on the framework's usual
// black background (widgets.Default) — nothing in the request asked for
// those to change, and SETTINGS' list rows already lean on Default.Select
// for its cursor highlight.
var (
	mutableBeige   = color.NRGBA{R: 0xF2, G: 0xEF, B: 0xE9, A: 255}
	mutableBlack   = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 255}
	mutableMagenta = color.NRGBA{R: 0xB3, G: 0x17, B: 0x48, A: 255}
	mutableGreen   = color.NRGBA{R: 0x1A, G: 0x78, B: 0x77, A: 255}
	// mutableTrack: a muted darker beige for a knob's empty track / the
	// bottom strip's background — plain beige-on-beige would make both
	// invisible against the panel fill.
	mutableTrack  = color.NRGBA{R: 0xD9, G: 0xD3, B: 0xC4, A: 255}
	mutableYellow = func() color.NRGBA {
		idx, ok := push3.ColorByName("yellow")
		if !ok {
			panic("display: unknown push3 palette color yellow")
		}
		return push3.ColorForIndex(idx).RGB
	}()
)

// mutableTheme maps the palette above onto widgets.Theme's fields: Select
// (a knob's default fill/pointer color) is Black, DarkGray (a knob's empty
// track, and DrawBotStrip's own background bar) is the muted track beige,
// White/Gray (value/label text) are Black so they read against the beige
// panel instead of the framework's usual black background.
var mutableTheme = widgets.Theme{
	Black:    mutableBeige,
	White:    mutableBlack,
	Gray:     mutableBlack,
	DarkGray: mutableTrack,
	Select:   mutableBlack,
	Accent:   mutableMagenta,
	OnColor:  mutableGreen,
	OffColor: mutableMagenta,
}

// knobColor picks a per-param override color: Timbre and FM (this host's
// closest equivalent to Braids' modulation amount) are Green, Color is
// Magenta, per the user's exact palette request — every other knob falls
// back to mutableTheme.Select (Black) via Knob.Color's own zero-value rule.
func knobColor(key string) color.NRGBA {
	switch key {
	case "timbre", "fm":
		return mutableGreen
	case "color":
		return mutableMagenta
	default:
		return color.NRGBA{}
	}
}

var (
	uiMu     sync.Mutex
	uiOn     bool
	lastPage = -1 // last page LEDs were synced for, see runDisplayLoop
)

// renderTopTabs draws the 4 page names across the top of the screen, in
// the same column positions as the physical top-screen buttons that jump
// to them (CCScreenTop1-4) — the LEDs alone (leds.go) say which button is
// live, but not what it's called, so this is the on-screen page label the
// original request asked for. t supplies the text colors so this reads
// correctly on both the beige knob pages (mutableTheme: black text) and
// SETTINGS' black background (widgets.Default: light text).
func renderTopTabs(img *image.NRGBA, t widgets.Theme, current int) {
	var top [8]widgets.SoftButton
	for i, name := range pageNames {
		b := widgets.SoftButton{Label: strings.ToUpper(name)}
		if i == current {
			b.State = widgets.SoftOn
		}
		top[i] = b
	}
	widgets.DrawBotStrip(img, t, 0, screenW, cellW, topStripH, top, "")
}

// renderParamPage draws the current page: the "not ready" OSD first if
// astatus reports the audio session isn't running (no page has any real
// controls to show until it is), else the knob grid, PRESETS, or SETTINGS.
func renderParamPage(st *paramState, io *ioState, astatus *audioStatus, level *levelMeter) *image.NRGBA {
	if ready, msg := astatus.get(); !ready {
		return renderWaitingScreen(msg)
	}
	switch st.Page() {
	case pagePresets:
		return renderPresetsPage(st)
	case pageSettings:
		return io.render()
	default:
		return renderKnobGrid(st, level)
	}
}

// renderKnobGrid draws OSC/AMP or FILTER: one cell per encoder slot (0-7)
// on the Mutable beige panel — a knob for a plain float param, a dark
// yellow-text inset for the "engine" enum (mirroring the original
// hardware's small OLED-style readout), or a live-level fader for Volume
// (see levelMeter — shows actual output loudness, not just the parameter's
// own position). Labels live in the bottom strip, not above each knob —
// see DrawBotStrip below.
func renderKnobGrid(st *paramState, level *levelMeter) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, mutableBeige)
	renderTopTabs(img, mutableTheme, st.Page())

	st.mu.Lock()
	page := paramPages[st.page]
	type cell struct {
		key  string
		slot *paramSlot
	}
	cells := make([]cell, len(page))
	for i, key := range page {
		cells[i] = cell{key: key, slot: st.slots[key]}
	}
	st.mu.Unlock()

	var bottom [8]widgets.SoftButton
	for i, c := range cells {
		if c.slot == nil {
			continue
		}
		bottom[i] = widgets.SoftButton{Label: strings.ToUpper(c.slot.meta.Name), Color: knobColor(c.key)}
		cx := i*cellW + knobCX

		switch {
		case c.key == "volume":
			renderVolumeFader(img, cx, level)
		case c.slot.meta.Type == "enum":
			renderEngineInset(img, cx, c.slot)
		default:
			widgets.DrawKnobArc(img, mutableTheme, cx, knobCY, knobR, widgets.Knob{
				Value:      c.slot.value * 100,
				Min:        c.slot.meta.Min * 100,
				Max:        c.slot.meta.Max * 100,
				Color:      knobColor(c.key),
				ValueScale: 2,
			})
		}
	}
	widgets.DrawBotStrip(img, mutableTheme, screenH-botStripH, screenW, cellW, botStripH, bottom, "")
	return img
}

// renderEngineInset draws the "engine" enum as a small dark box with
// yellow text, centered in its cell — the original module's own small
// algorithm-name OLED, reused via push3's "yellow" palette entry rather
// than a raw color literal.
func renderEngineInset(img *image.NRGBA, cx int, slot *paramSlot) {
	name := formatValue(slot)
	const boxW, boxH = cellW - 8, 36
	const scale = 2
	x, y := cx-boxW/2, knobCY-boxH/2
	gfx.FillRect(img, x, y, boxW, boxH, mutableBlack)
	tw := text.WidthScaled(name, scale)
	if tw > boxW-4 {
		// text.Truncate measures at 1x, so truncate against the 1x budget
		// (boxW-4)/scale before scaling up, rather than the scaled width.
		name = text.Truncate(name, (boxW-4)/scale)
		tw = text.WidthScaled(name, scale)
	}
	text.DrawScaled(img, x+(boxW-tw)/2, y+boxH-11, scale, name, mutableYellow)
}

// meterMinDB is the fader's bottom of scale — standard dB-meter practice,
// same reason a real mixer fader isn't linear-amplitude: linear peak
// spends nearly all its range near zero for normal program material (a
// typical mix sits well under full scale), reading as "stuck near empty"
// even when audible. -48dB floor maps to an empty fader, 0dB (full scale)
// to a full one.
const meterMinDB = -48.0

// dbFrac converts a linear 0-1 peak to a 0-1 fader fraction on that dB
// scale — math.Log10(0) is -Inf, so peak<=0 short-circuits to silent
// rather than letting that propagate.
func dbFrac(peak float64) float64 {
	if peak <= 0 {
		return 0
	}
	db := 20 * math.Log10(peak)
	if db < meterMinDB {
		return 0
	}
	return (db - meterMinDB) / -meterMinDB
}

// renderVolumeFader draws the Volume column as a fader whose fill tracks
// the live output level (levelMeter) on a dB scale, not the Volume
// parameter's own position — the parameter itself still turns via its
// encoder as before (audiosession.go's drainCtl still calls applyEncoder
// for it unchanged); this is purely what the fader visualizes.
func renderVolumeFader(img *image.NRGBA, cx int, level *levelMeter) {
	const w = 22
	x := cx - w/2
	y := knobCY - knobR
	h := 2 * knobR
	widgets.DrawFader(img, mutableTheme, x, y, w, h, widgets.Knob{
		Value: dbFrac(level.get()) * 100,
		Min:   0,
		Max:   100,
	})
}

// renderPresetsPage draws PRESETS: the preset browser in column 1 only
// (not full-screen — encoder 1 moves a staged highlight, distinct from
// the actually-loaded preset until Load/bottom-1 commits it), and octave
// transpose as a pan-style knob in column 2, applied immediately.
func renderPresetsPage(st *paramState) *image.NRGBA {
	st.mu.Lock()
	presetSlot := st.slots["preset"]
	octSlot := st.slots["octave_transpose"]
	st.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, mutableBeige)
	renderTopTabs(img, mutableTheme, pagePresets)

	if presetSlot == nil {
		text.Draw(img, 8, topStripH+14, "no presets found", mutableBlack)
		return img
	}

	loaded := int(presetSlot.value + 0.5)
	cursor := st.PresetCursor()

	const rowH = 13
	top, bot := topStripH+2, screenH-botStripH
	visRows := (bot - top) / rowH
	scroll := cursor - visRows/2
	if scroll < 0 {
		scroll = 0
	}
	if maxScroll := len(presetSlot.meta.Options) - visRows; maxScroll < 0 {
		scroll = 0
	} else if scroll > maxScroll {
		scroll = maxScroll
	}
	for i := 0; i < visRows; i++ {
		idx := scroll + i
		if idx >= len(presetSlot.meta.Options) {
			break
		}
		y := top + i*rowH
		name := presetSlot.meta.Options[idx]
		col := mutableBlack
		if idx == cursor {
			gfx.FillRect(img, 0, y, cellW-4, rowH, mutableBlack)
			col = mutableBeige
		} else if idx == loaded {
			col = mutableYellow
		}
		text.Draw(img, 4, y+rowH-3, name, col)
	}

	if octSlot != nil {
		widgets.DrawKnobArc(img, mutableTheme, cellW+knobCX, knobCY, knobR, widgets.Knob{
			Bipolar:    true,
			Value:      octSlot.value,
			Min:        octSlot.meta.Min,
			Max:        octSlot.meta.Max,
			ValueScale: 2,
		})
	}

	var bottom [8]widgets.SoftButton
	bottom[0] = widgets.SoftButton{Label: "LOAD", State: widgets.SoftConfirm}
	bottom[1] = widgets.SoftButton{Label: "OCTAVE"}
	widgets.DrawBotStrip(img, mutableTheme, screenH-botStripH, screenW, cellW, botStripH, bottom, "")
	return img
}

// renderWaitingScreen is the "not ready" OSD: full black screen, no knobs —
// shown instead of the real param UI whenever astatus.get() reports not
// ready, so Shift+Device never shows controls for a session with no audio
// running yet. msg may be multi-line (\n-separated).
func renderWaitingScreen(msg string) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, widgets.Default.Black)
	text.DrawScaled(img, 8, 26, 2, "Braids not ready - setup needed", widgets.Default.White)
	y := 46
	for _, line := range strings.Split(msg, "\n") {
		text.Draw(img, 8, y, line, widgets.Default.Gray)
		y += 16
	}
	return img
}

// toggleUI flips the on-screen param UI: entering takeover mode, enabling
// push-manager's MIDI intercept (so pad hits stop reaching Live while this
// UI reads them as controls, not notes), syncing LEDs for the current
// page, and forcing an immediate frame — or releasing all three back to
// the native Push UI / normal Live routing.
func toggleUI(pmURL string, st *paramState, io *ioState, astatus *audioStatus, level *levelMeter) {
	uiMu.Lock()
	uiOn = !uiOn
	on := uiOn
	uiMu.Unlock()

	client := pmclient.New(pmURL)
	if on {
		if err := client.SetMode(2); err != nil {
			log.Printf("display: enable takeover: %v", err)
		}
		if err := client.SetMidiFilter(true); err != nil {
			log.Printf("display: enable midi filter: %v", err)
		}
		page := st.Page()
		syncUILEDs(pmURL, page)
		uiMu.Lock()
		lastPage = page
		uiMu.Unlock()
		if err := client.PushImage(renderParamPage(st, io, astatus, level)); err != nil {
			log.Printf("display: push frame: %v", err)
		}
		log.Printf("push-braids: UI ON (Shift+Device) — MIDI intercept enabled")
	} else {
		if err := client.SetMode(0); err != nil {
			log.Printf("display: disable takeover: %v", err)
		}
		if err := client.SetMidiFilter(false); err != nil {
			log.Printf("display: disable midi filter: %v", err)
		}
		releaseUILEDs(pmURL)
		log.Printf("push-braids: UI OFF (Shift+Device) — MIDI intercept disabled")
	}
}

// shutdownUI best-effort releases the display, MIDI intercept, and LEDs on
// exit, regardless of the UI's last toggled state — leaving any of those
// stuck on after this process exits would silently break Push with no
// process left to blame.
func shutdownUI(pmURL string) {
	client := pmclient.New(pmURL)
	_ = client.SetMode(0)
	_ = client.SetMidiFilter(false)
	releaseUILEDs(pmURL)
}

// runDisplayLoop redraws at ~10fps while the UI is on — bounded, not the
// full 33ms tick rate, since every redraw allocates a fresh PNG frame (GC
// is enabled precisely because of this loop, see main.go, but a steady
// allocation rate still means steady GC work and network/CPU load worth
// keeping modest). 10fps is still smooth enough for the Volume fader's
// live meter, the only thing that needs continuous redraws with no other
// trigger of its own. Also re-syncs LEDs whenever the page changes (a
// top-button press elsewhere), since that has no other trigger either.
func runDisplayLoop(pmURL string, st *paramState, io *ioState, astatus *audioStatus, level *levelMeter) {
	client := pmclient.New(pmURL)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		uiMu.Lock()
		on := uiOn
		uiMu.Unlock()
		if !on {
			continue
		}

		page := st.Page()
		uiMu.Lock()
		changed := page != lastPage
		if changed {
			lastPage = page
		}
		uiMu.Unlock()
		if changed {
			syncUILEDs(pmURL, page)
		}

		if err := client.PushImage(renderParamPage(st, io, astatus, level)); err != nil {
			log.Printf("display: push frame: %v", err)
		}
	}
}
