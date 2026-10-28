package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPairNames(t *testing.T) {
	for off, want := range map[int]string{0: "1/2", 2: "3/4", 6: "7/8"} {
		if got := pairName(off); got != want {
			t.Errorf("pairName(%d) = %q, want %q", off, got, want)
		}
		if back, ok := parsePair(want); !ok || back != off {
			t.Errorf("parsePair(%q) = %d %v", want, back, ok)
		}
	}
	for _, bad := range []string{"3", "3/5", "4/3", "0/1", "a/b", "", "3/4/5", "1/ 2"} {
		if _, ok := parsePair(bad); ok {
			t.Errorf("parsePair(%q) accepted", bad)
		}
	}
}

func TestOnlyStereoPair(t *testing.T) {
	if p, ok := onlyStereoPair([]string{"3/4", "3", "4"}); !ok || p != "3/4" {
		t.Errorf("one pair: %q %v", p, ok)
	}
	if _, ok := onlyStereoPair([]string{"1/2", "3/4", "3"}); ok {
		t.Error("two pairs are ambiguous")
	}
	if _, ok := onlyStereoPair([]string{"1", "2"}); ok {
		t.Error("mono only has no pair")
	}
	if _, ok := onlyStereoPair(nil); ok {
		t.Error("nothing offered")
	}
}

func TestBraidsTrackIndexAndMeterOf(t *testing.T) {
	tracks := []liveTrack{{"1-MIDI", "midi"}, {"Braids", "midi"}, {"Braids", "audio"}}
	if got := braidsTrackIndex(tracks); got != 2 {
		t.Errorf("index = %d, want 2 (a MIDI track of that name does not count)", got)
	}
	if braidsTrackIndex(tracks[:2]) != -1 {
		t.Error("no audio Braids track must be -1")
	}
	meters := [][]float64{{0.1, 0.2}, {0.5, 0.3}, {0.0, 0.7}}
	if v, ok := meterOf(meters, 2); !ok || v != 0.7 {
		t.Errorf("meterOf = %v %v", v, ok)
	}
	if _, ok := meterOf(meters, 3); ok {
		t.Error("an index past the list must not read")
	}
	if _, ok := meterOf(meters, -1); ok {
		t.Error("-1 must not read")
	}
	if _, ok := meterOf([][]float64{{0.1}}, 0); ok {
		t.Error("a short entry must not read")
	}
}

func TestRoutingOptionsAndFriendlyErrors(t *testing.T) {
	data := json.RawMessage(`{"side":"input","what":"channel","want":["3/4"],"options":["1/2","9/10"],"changed":[]}`)
	pe := &phError{Code: "bad_routing", Data: data}
	what, opts := routingOptions(pe)
	if what != "channel" || len(opts) != 2 || opts[1] != "9/10" {
		t.Errorf("routingOptions = %q %v", what, opts)
	}
	if msg := friendlyPHError("audio track", pe).Error(); !strings.Contains(msg, "1/2, 9/10") {
		t.Errorf("bad_routing message: %s", msg)
	}
	if msg := friendlyPHError("MIDI track", &phError{Code: "unknown_cmd"}).Error(); !strings.Contains(msg, "too old") {
		t.Errorf("old script message: %s", msg)
	}
	if msg := friendlyPHError("MIDI track", &phError{Code: "wrong_track_kind"}).Error(); !strings.Contains(msg, "rename") {
		t.Errorf("wrong kind message: %s", msg)
	}
	if msg := friendlyPHError("MIDI track", errors.New("connection refused")).Error(); !strings.Contains(msg, "selected in Live") {
		t.Errorf("unreachable message: %s", msg)
	}
}
