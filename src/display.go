package main

// display.go — draws the current parameter page (8 encoder-slot cells,
// each either a knob for a plain 0-1 float or a text readout for the
// "engine" enum) and pushes it to push-manager's display API. Same
// pattern as hacks/keyboard-visualizer/src/render.go: an HTTP client of
// push-manager only, never touching the shared-memory framebuffer
// directly — see CLAUDE.md's "Display-owning hacks" section for why that
// discipline matters.
//
// Off by default: the UI only takes the screen (and enables push-manager's
// MIDI intercept, so pad hits drive Braids instead of also reaching Live)
// while toggled on via Shift+Device — see chord.go and toggleUI below.

import (
	"fmt"
	"image"
	"log"
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
	knobCY = 88
	knobR  = 30
)

var (
	uiMu sync.Mutex
	uiOn bool
)

// renderParamPage draws the current page: the I/O picker (iopage.go) if
// that's what's selected, otherwise one cell per encoder slot (0-7) — a
// knob for a plain 0-1 float param, or a plain centered readout for an
// enum ("engine") — DrawKnob's numeric center doesn't fit a shape name, so
// that one slot draws differently.
func renderParamPage(st *paramState, io *ioState, astatus *audioStatus) *image.NRGBA {
	if ready, msg := astatus.get(); !ready {
		return renderWaitingScreen(msg)
	}
	if st.IsIOPage() {
		return io.render()
	}
	if pageNames[st.page] == "PATCH" {
		return renderPatchPage(st)
	}

	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, widgets.Default.Black)

	st.mu.Lock()
	page := paramPages[st.page]
	title := fmt.Sprintf("BRAIDS - PAGE %d/%d - %s", st.page+1, len(pageNames), pageNames[st.page])
	type cell struct {
		slot *paramSlot
	}
	cells := make([]cell, len(page))
	for i, key := range page {
		cells[i] = cell{slot: st.slots[key]}
	}
	st.mu.Unlock()

	text.Draw(img, 8, 16, title, widgets.Default.Gray)

	for i, c := range cells {
		if c.slot == nil {
			continue
		}
		cx := i*cellW + knobCX
		if c.slot.meta.Type == "enum" {
			name := c.slot.meta.Name
			text.Draw(img, cx-text.Width(name)/2, knobCY-knobR-4, name, widgets.Default.Gray)
			val := formatValue(c.slot)
			text.DrawScaled(img, cx-text.WidthScaled(val, 2)/2, knobCY+8, 2, val, widgets.Default.White)
			continue
		}
		widgets.DrawKnobArc(img, widgets.Default, cx, knobCY, knobR, widgets.Knob{
			Label: c.slot.meta.Name,
			Value: c.slot.value * 100,
			Min:   c.slot.meta.Min * 100,
			Max:   c.slot.meta.Max * 100,
		})
	}
	return img
}

// renderPatchPage draws the PATCH page (preset + octave transpose) as a
// full-screen scrollable list of every preset name, rather than the
// generic per-cell rendering the other pages use — a plain centered
// readout only ever shows the currently selected preset, and the whole
// point of the encoder's reduced sensitivity (see enumSensitivity) is
// deliberate browsing, which needs to see the neighboring options too, not
// just where the cursor currently sits. Reuses iopage.go's own list
// widget/scroll-centering pattern.
func renderPatchPage(st *paramState) *image.NRGBA {
	st.mu.Lock()
	presetSlot := st.slots["preset"]
	octSlot := st.slots["octave_transpose"]
	st.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, widgets.Default.Black)

	if presetSlot == nil {
		text.Draw(img, 8, 16, "BRAIDS - PATCH - no presets found", widgets.Default.Gray)
		return img
	}

	cur := int(presetSlot.value + 0.5)
	rows := make([]widgets.ListRow, len(presetSlot.meta.Options))
	for i, name := range presetSlot.meta.Options {
		mark, col := "  ", widgets.Default.Gray
		if i == cur {
			mark, col = "> ", widgets.Default.White
		}
		rows[i] = widgets.ListRow{Text: fmt.Sprintf("%s%02d  %s", mark, i+1, name), TextCol: col}
	}

	status := ""
	if octSlot != nil {
		status = fmt.Sprintf("Octave %+d", int(octSlot.value+0.5))
	}

	const patchRowH = 13
	visRows := (screenH - patchRowH) / patchRowH
	scroll := cur - visRows/2
	if scroll < 0 {
		scroll = 0
	}
	if maxScroll := len(rows) - visRows; maxScroll < 0 {
		scroll = 0
	} else if scroll > maxScroll {
		scroll = maxScroll
	}

	v := widgets.ListView{
		Rows:       rows,
		Cursor:     cur,
		Scroll:     scroll,
		Breadcrumb: "BRAIDS - PRESET - encoder 1 select, encoder 2 octave",
		Status:     status,
	}
	widgets.RenderList(img, widgets.Default, v, 0, screenW, patchRowH, screenH)
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
// UI reads them as controls, not notes), and forcing an immediate frame —
// or releasing both back to the native Push UI / normal Live routing.
func toggleUI(pmURL string, st *paramState, io *ioState, astatus *audioStatus) {
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
		if err := client.PushImage(renderParamPage(st, io, astatus)); err != nil {
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
		log.Printf("push-braids: UI OFF (Shift+Device) — MIDI intercept disabled")
	}
}

// shutdownUI best-effort releases the display and MIDI intercept on exit,
// regardless of the UI's last toggled state — leaving push-manager's
// global MIDI intercept stuck on after this process exits would silently
// block all pad input to Live with no process left to blame.
func shutdownUI(pmURL string) {
	client := pmclient.New(pmURL)
	_ = client.SetMode(0)
	_ = client.SetMidiFilter(false)
}

// runDisplayLoop redraws when the UI is on and either an encoder turn/page
// flip marked the state dirty, or astatus's readiness flipped — the latter
// needs its own check since it changes with no encoder event at all (Live
// opening the loopback card mid-session), polled at ~30fps, the same rate
// keyboard-visualizer's own render loop uses.
func runDisplayLoop(pmURL string, st *paramState, io *ioState, astatus *audioStatus) {
	client := pmclient.New(pmURL)
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()
	lastReady := false
	for range ticker.C {
		st.mu.Lock()
		dirty := st.dirty
		st.dirty = false
		st.mu.Unlock()

		ready, _ := astatus.get()
		if ready != lastReady {
			lastReady = ready
			dirty = true
		}

		uiMu.Lock()
		on := uiOn
		uiMu.Unlock()

		if !on || !dirty {
			continue
		}
		if err := client.PushImage(renderParamPage(st, io, astatus)); err != nil {
			log.Printf("display: push frame: %v", err)
		}
	}
}
