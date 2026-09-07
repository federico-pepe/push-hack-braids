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
// slot (index 0-7, left to right, matching CC 71-78). Only covers the
// generic knob-grid pages (pageOscAmp/pageFilter/pageCrush) — PRESETS and
// SETTINGS render and handle encoders their own way (see
// renderPatchPage/iopage.go). paramPages is indexed directly by page
// constant (renderKnobGrid does paramPages[st.page]), so pagePresets and
// pageSettings still need a (nil) placeholder here even though they never
// read it, to keep every other page's index aligned. pageCrush is filled
// in incrementally as more of the vendored-but-unused Braids Settings
// params get wired up (see git log on this branch) — it has empty slots
// for now.
var paramPages = [][]string{
	pageOscAmp:   {"engine", "timbre", "color", "attack", "decay", "sustain", "release", "volume"},
	pageFilter:   {"fm", "cutoff", "resonance", "filt_env", "f_attack", "f_decay", "f_sustain", "f_release"},
	pagePresets:  nil,
	pageCrush:    {"resolution", "sample_rate"},
	pageSettings: nil,
}

// Page indices, jumped to directly by top-screen button press (CCScreenTopN)
// — see main.go's Fixed() and pageNames below. SETTINGS is kept last on
// purpose (rightmost top-screen button) — any future page (e.g. more of
// the vendored-but-unused Braids Settings params landing on their own
// page) gets inserted here BEFORE pageSettings, not after, so SETTINGS
// keeps that position as more pages are added.
const (
	pageOscAmp = iota
	pageFilter
	pagePresets
	pageCrush
	pageSettings
)

var pageNames = []string{"OSC / AMP", "FILTER", "PRESETS", "CRUSH / QUANT", "SETTINGS"}

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
	"engine":           4,
	"preset":           4,
	"octave_transpose": 4,
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

	// presetCursor/presetAccum: PRESETS page's staged (not-yet-loaded)
	// highlight — separate from slots["preset"].value, which only changes
	// once Load (bottom-1) is pressed. See movePresetCursor/loadStagedPreset.
	presetCursor int
	presetAccum  int
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
	addSlot := func(key string) {
		meta, ok := byKey[key]
		if !ok {
			log.Printf("params: %q not found in plugin's chain_params, skipping", key)
			return
		}
		st.slots[key] = &paramSlot{meta: meta, value: defaults[key]}
	}
	for _, page := range paramPages {
		for _, key := range page {
			addSlot(key)
		}
	}
	// "preset" and "octave_transpose" aren't on any paramPages grid page —
	// PRESETS (pagePresets) renders and drives them itself (see
	// renderPatchPage, movePresetCursor, loadStagedPreset, NudgeOctave).
	addSlot("preset")
	addSlot("octave_transpose")
	if presetSlot, ok := st.slots["preset"]; ok {
		st.presetCursor = int(presetSlot.value + 0.5)
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

// nudgeSlotLocked applies one encoder's delta to slot (enum: accumulate to
// sensitivityFor(key) then step by one option; float/int: stepFor(meta)
// per tick), clamped to the plugin's own reported range. Caller must hold
// st.mu. Shared by applyEncoder (page-grid params) and NudgeOctave (a
// param outside any paramPages grid — see newParamState's addSlot doc).
func nudgeSlotLocked(slot *paramSlot, key string, delta int) (val string) {
	if slot.meta.Type == "enum" || slot.meta.Type == "int" {
		// Push's encoders accelerate — a fast turn sends delta up to ±11,
		// not ±1 (core/push3/encoder.go's DecodeRel doc). That's fine for
		// a continuous float sweep, but for a small discrete range it made
		// one brisk flick jump clean across it in a single message (an
		// enum's shape list, or octave_transpose's whole -3..3 span).
		// Accumulate delta instead and step by exactly one unit per
		// sensitivityFor(key) ticks of accumulated turn — 1 (the default)
		// steps on every message same as before a key was tuned; "engine"/
		// "octave_transpose" are heavier (see enumSensitivity) so browsing
		// them takes deliberate turning, not one graze.
		div := sensitivityFor(key)
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
	if slot.meta.Type == "float" {
		return fmt.Sprintf("%.4f", slot.value)
	}
	return fmt.Sprintf("%d", int(slot.value+0.5))
}

// applyEncoder nudges the param in encoder slot idx (0-7) on the current
// paramPages grid page (pageOscAmp/pageFilter) by delta ticks, and returns
// the key/value string pair ready for bridge_plugin_set_param. ok is false
// when the current page isn't a paramPages grid page, or that encoder has
// no param on it.
func (st *paramState) applyEncoder(idx, delta int) (key, val string, ok bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.page < 0 || st.page >= len(paramPages) {
		return "", "", false
	}
	page := paramPages[st.page]
	if idx < 0 || idx >= len(page) {
		return "", "", false
	}
	slot := st.slots[page[idx]]
	if slot == nil {
		return "", "", false
	}
	val = nudgeSlotLocked(slot, page[idx], delta)
	st.dirty = true
	return page[idx], val, true
}

// NudgeOctave applies one encoder tick to octave_transpose — PRESETS
// page's column-2 knob, immediate (unlike the staged preset browse in
// column 1) since it's not gated behind Load. Not on any paramPages grid
// page, so it can't go through applyEncoder.
func (st *paramState) NudgeOctave(delta int) (val string, ok bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	slot := st.slots["octave_transpose"]
	if slot == nil {
		return "", false
	}
	val = nudgeSlotLocked(slot, "octave_transpose", delta)
	st.dirty = true
	return val, true
}

// movePresetCursor moves PRESETS page's staged highlight by delta ticks
// (same accumulate-then-step feel as an enum param, via enumSensitivity's
// "preset" entry) — does not touch slots["preset"].value or the plugin;
// only loadStagedPreset (Load, bottom-1) does that.
func (st *paramState) movePresetCursor(delta int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	slot := st.slots["preset"]
	if slot == nil {
		return
	}
	div := sensitivityFor("preset")
	st.presetAccum += delta
	for st.presetAccum >= div {
		st.presetCursor++
		st.presetAccum -= div
	}
	for st.presetAccum <= -div {
		st.presetCursor--
		st.presetAccum += div
	}
	if n := len(slot.meta.Options); st.presetCursor >= n {
		st.presetCursor = n - 1
	}
	if st.presetCursor < 0 {
		st.presetCursor = 0
	}
	st.dirty = true
}

// PresetCursor returns PRESETS page's current staged highlight index.
func (st *paramState) PresetCursor() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.presetCursor
}

// loadStagedPreset commits the staged highlight as the actual preset
// (Load, bottom-1 on PRESETS) — the caller still owns sending it to the
// plugin (bridge_plugin_set_param("preset", idx)), same division of
// responsibility as applyEncoder/NudgeOctave.
func (st *paramState) loadStagedPreset() (idx int, ok bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	slot := st.slots["preset"]
	if slot == nil {
		return 0, false
	}
	slot.value = float64(st.presetCursor)
	st.dirty = true
	return st.presetCursor, true
}

// setPage jumps directly to page n (a top-screen button press), clamped to
// the 4 fixed pages (see pageNames) — no relative D-Pad delta anymore.
func (st *paramState) setPage(n int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n > len(pageNames)-1 {
		n = len(pageNames) - 1
	}
	if n != st.page {
		st.page = n
		st.dirty = true
	}
}

// Page returns the current page index.
func (st *paramState) Page() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.page
}

// MarkDirty flags the display loop to redraw on its next tick — used by
// the SETTINGS page (iopage.go), which changes its own state outside of
// applyEncoder/setPage.
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
