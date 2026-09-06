package main

// params.go — parameter metadata and live value/page state for the
// on-screen control UI (see display.go) and the 8 encoders + D-Pad
// Left/Right that drive it (see main.go's midiHandler).
//
// Metadata (name/type/min/max/enum options) comes straight from the DSP
// plugin itself via bridge_plugin_get_param("chain_params") — a JSON list
// the plugin already builds for its own generic-UI support (see
// braids_plugin.cpp's v2_get_param). Reading it here means this host never
// hardcodes Braids-specific ranges or the engine's shape names.

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

type paramMeta struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Type    string   `json:"type"` // "float", "int", "enum"
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Options []string `json:"options"`
}

// paramPages curates which params sit on which page and in which encoder
// slot (index 0-7, left to right, matching CC 71-78). A page may leave
// trailing encoders unused — the filter envelope page below only fills 4.
var paramPages = [][]string{
	{"engine", "timbre", "color", "attack", "decay", "sustain", "release", "volume"},
	{"fm", "cutoff", "resonance", "filt_env", "f_attack", "f_decay", "f_sustain", "f_release"},
	{"preset", "octave_transpose"},
}

// pageNames has one more entry than paramPages: the last page is the I/O
// picker (iopage.go), not a param page — see IsIOPage.
var pageNames = []string{"OSC / AMP", "FILTER", "PATCH", "I/O"}

// paramSlot is one parameter's live state: its metadata plus the Go-side
// value driving the plugin. The plugin's get_param has no "current value"
// query for individual params (only the bulk "state" JSON), so this host's
// own value is the single source of truth for what an encoder set last.
type paramSlot struct {
	meta  paramMeta
	value float64
	// accum carries leftover encoder delta between messages for an enum
	// slot with reduced sensitivity (see enumSensitivity) — a turn that
	// hasn't yet crossed its threshold accumulates here instead of being
	// dropped, so slow deliberate turning still eventually lands a step.
	accum int
}

// enumSensitivity maps an enum param's key to how much accumulated encoder
// delta it takes to advance one option — 1 (the default, via
// sensitivityFor) steps on every message like every other enum; a value
// above that makes the knob "heavier". Push's own tethered app offers this
// per-knob feel for exactly the same reason: "engine" flips through 47
// Braids algorithms, so the lightest graze of the encoder used to jump
// several algorithms past the one you wanted.
var enumSensitivity = map[string]int{
	"engine": 4,
	"preset": 4,
}

// sensitivityFor returns how much accumulated delta enum key needs before
// stepping once — see enumSensitivity.
func sensitivityFor(key string) int {
	if d, ok := enumSensitivity[key]; ok && d > 0 {
		return d
	}
	return 1
}

// paramState guards the current page and every param's value against
// concurrent access: the render loop goroutine (main.go) writes it on every
// encoder/page event, the display loop goroutine (display.go) reads it at
// ~30fps to redraw.
type paramState struct {
	mu    sync.Mutex
	page  int
	slots map[string]*paramSlot
	dirty bool
}

// fetchChainParams calls the plugin's get_param("chain_params") and parses
// the resulting JSON metadata list.
func fetchChainParams(plugin *C.bridge_plugin_t) ([]paramMeta, error) {
	const bufLen = 8192
	buf := make([]byte, bufLen)
	key := C.CString("chain_params")
	defer C.free(unsafe.Pointer(key))
	n := C.bridge_plugin_get_param(plugin, key, (*C.char)(unsafe.Pointer(&buf[0])), C.int(bufLen))
	if n < 0 {
		return nil, fmt.Errorf("get_param(chain_params) failed")
	}
	var metas []paramMeta
	if err := json.Unmarshal(buf[:n], &metas); err != nil {
		return nil, fmt.Errorf("parse chain_params JSON: %w", err)
	}
	for i := range metas {
		// "engine" (and any other enum) carries its range as "options",
		// not "min"/"max" — the plugin's chain_params JSON omits min/max
		// for enum entries entirely (see braids_plugin.cpp's chain_params
		// handler). Left at their zero value, every encoder turn clamped
		// straight back to 0 (CSAW) — the reported "engine stuck on
		// CSAW, won't scroll" bug.
		if metas[i].Type == "enum" && len(metas[i].Options) > 0 {
			metas[i].Min = 0
			metas[i].Max = float64(len(metas[i].Options) - 1)
		}
	}
	return metas, nil
}

// braidsPresetFile is the subset of a .braids preset JSON file this host
// reads — just enough to label the preset picker (see fetchPresetMeta).
type braidsPresetFile struct {
	Name string `json:"name"`
}

// fetchPresetMeta builds a synthetic enum paramMeta for "preset" — the
// plugin never lists it in chain_params (it's exposed only through its own
// ui_hierarchy browser convention, which this host doesn't use). Reading
// the .braids files directly off disk also avoids the alternative of
// cycling the live instance through every preset to read back its name:
// that would call v2_apply_preset for each one, overwriting the
// defaultParams values already sent to the instance by the time this runs.
func fetchPresetMeta(moduleDir string) (paramMeta, error) {
	dir := filepath.Join(moduleDir, "presets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return paramMeta{}, fmt.Errorf("reading presets dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".braids") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // load_presets (braids_plugin.cpp) loads in this same sorted order
	names := make([]string, 0, len(files))
	for _, fn := range files {
		data, err := os.ReadFile(filepath.Join(dir, fn))
		if err != nil {
			return paramMeta{}, fmt.Errorf("reading %s: %w", fn, err)
		}
		var pf braidsPresetFile
		if err := json.Unmarshal(data, &pf); err != nil {
			return paramMeta{}, fmt.Errorf("parsing %s: %w", fn, err)
		}
		if pf.Name == "" {
			pf.Name = strings.TrimSuffix(fn, ".braids")
		}
		names = append(names, pf.Name)
	}
	if len(names) == 0 {
		return paramMeta{}, fmt.Errorf("no .braids presets found in %s", dir)
	}
	return paramMeta{
		Key:     "preset",
		Name:    "Preset",
		Type:    "enum",
		Min:     0,
		Max:     float64(len(names) - 1),
		Options: names,
	}, nil
}

// newParamState builds the page/slot state from the plugin's own metadata
// and defaultParams' starting values (see main.go's defaultParams) — every
// key named in paramPages that the plugin doesn't report is skipped with a
// log line rather than a crash, so a future Braids build that renames a
// param degrades to a shorter page instead of failing to start.
func newParamState(metas []paramMeta) *paramState {
	byKey := make(map[string]paramMeta, len(metas))
	for _, m := range metas {
		byKey[m.Key] = m
	}
	defaults := make(map[string]float64, len(defaultParams))
	for _, kv := range defaultParams {
		var v float64
		fmt.Sscanf(kv[1], "%f", &v)
		defaults[kv[0]] = v
	}

	st := &paramState{slots: make(map[string]*paramSlot)}
	for _, page := range paramPages {
		for _, key := range page {
			meta, ok := byKey[key]
			if !ok {
				log.Printf("params: %q not found in plugin's chain_params, skipping", key)
				continue
			}
			st.slots[key] = &paramSlot{meta: meta, value: defaults[key]}
		}
	}
	return st
}

// stepFor is how much one encoder tick moves a param's value: exactly one
// unit for an enum/int (one shape, one semitone), or 1/100th of the
// param's full range for a float — full-range sweep in ~100 slow ticks,
// proportionally faster while the encoder is accelerating.
func stepFor(meta paramMeta) float64 {
	if meta.Type == "int" || meta.Type == "enum" {
		return 1
	}
	span := meta.Max - meta.Min
	if span <= 0 {
		span = 1
	}
	return span / 100.0
}

// applyEncoder nudges the param in encoder slot idx (0-7) on the current
// page by delta ticks, clamps it into the plugin's own reported range, and
// returns the key/value string pair ready for bridge_plugin_set_param. ok
// is false when that encoder has no param on the current page.
func (st *paramState) applyEncoder(idx, delta int) (key, val string, ok bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	page := paramPages[st.page]
	if idx < 0 || idx >= len(page) {
		return "", "", false
	}
	slot := st.slots[page[idx]]
	if slot == nil {
		return "", "", false
	}
	if slot.meta.Type == "enum" {
		// Push's encoders accelerate — a fast turn sends delta up to ±11,
		// not ±1 (core/push3/encoder.go's DecodeRel doc). That's fine for
		// a continuous float sweep, but for a discrete list like the
		// engine's shapes it made one brisk flick jump 11 algorithms at
		// once. Accumulate delta instead and step by exactly one option
		// per sensitivityFor(key) units of accumulated turn — 1 (the
		// default) steps on every message same as before; "engine" is
		// tuned heavier (see enumSensitivity) so it takes deliberate
		// turning to browse its 47 algorithms.
		div := sensitivityFor(page[idx])
		slot.accum += delta
		for slot.accum >= div {
			slot.value++
			slot.accum -= div
		}
		for slot.accum <= -div {
			slot.value--
			slot.accum += div
		}
	} else {
		slot.value += float64(delta) * stepFor(slot.meta)
	}
	if slot.value < slot.meta.Min {
		slot.value = slot.meta.Min
	}
	if slot.value > slot.meta.Max {
		slot.value = slot.meta.Max
	}
	st.dirty = true
	if slot.meta.Type == "float" {
		return page[idx], fmt.Sprintf("%.4f", slot.value), true
	}
	return page[idx], fmt.Sprintf("%d", int(slot.value+0.5)), true
}

// changePage moves by delta pages, clamped to the available pages —
// including the I/O picker page, which is one past the last param page
// (see pageNames and IsIOPage).
func (st *paramState) changePage(delta int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.page + delta
	if p < 0 {
		p = 0
	}
	if p > len(pageNames)-1 {
		p = len(pageNames) - 1
	}
	if p != st.page {
		st.page = p
		st.dirty = true
	}
}

// Page returns the current page index.
func (st *paramState) Page() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.page
}

// IsIOPage reports whether the current page is the I/O picker (iopage.go)
// rather than a param page.
func (st *paramState) IsIOPage() bool {
	return st.Page() == len(paramPages)
}

// MarkDirty flags the display loop to redraw on its next tick — used by
// the I/O picker (iopage.go), which changes its own state outside of
// applyEncoder/changePage.
func (st *paramState) MarkDirty() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.dirty = true
}

// formatValue renders a slot's current value as a short, human string: the
// enum's selected option name for "engine", a percentage for a plain 0-1
// float, or a plain number otherwise.
func formatValue(slot *paramSlot) string {
	m := slot.meta
	switch {
	case m.Type == "enum":
		i := int(slot.value + 0.5)
		if i >= 0 && i < len(m.Options) {
			return m.Options[i]
		}
		return fmt.Sprintf("%d", i)
	case m.Type == "float" && m.Min == 0 && m.Max == 1:
		return fmt.Sprintf("%d%%", int(slot.value*100+0.5))
	case m.Type == "int":
		return fmt.Sprintf("%d", int(slot.value+0.5))
	default:
		return fmt.Sprintf("%.3f", slot.value)
	}
}
