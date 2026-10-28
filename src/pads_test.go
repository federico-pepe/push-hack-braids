package main

import (
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

func TestAcceptPushNote(t *testing.T) {
	cases := []struct {
		name                       string
		configured, livePort, open bool
		note                       byte
		want                       bool
	}{
		{"configured port, pad", true, false, false, 60, true},
		{"live port, app closed, input elsewhere: pads do not play", false, true, false, 60, false},
		{"live port, app open: pads play", false, true, true, 60, true},
		{"another port, app open: no", false, false, true, 60, false},
		{"configured, below the pad grid (encoder touch)", true, false, false, 3, false},
		{"configured, above the pad grid", true, false, false, 100, false},
		{"app open, below the grid", false, true, true, 13, false},
		{"grid edges", true, false, false, 36, true},
		{"grid top edge", true, false, false, 99, true},
	}
	for _, c := range cases {
		if got := acceptPushNote(c.configured, c.livePort, c.open, c.note); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPadsModeOf(t *testing.T) {
	if padsModeOf(alsaseq.Push3ClientDefault, alsaseq.Push3PortDefault) != padsAlways {
		t.Error("Live Port is the always mode")
	}
	if padsModeOf(alsaseq.Push3ClientDefault, alsaseq.Push3PortDefault+2) != padsApp {
		t.Error("another Push port is the app mode")
	}
	if padsModeOf(140, 0) != padsApp {
		t.Error("an external client is the app mode")
	}
}

func TestPadsLabel(t *testing.T) {
	labels := []string{"Ableton Push 3 Live Port", "Ableton Push 3 User Port", "Ableton Push 3 External Port"}
	if i, err := padsLabel(padsAlways, labels); err != nil || i != 0 {
		t.Errorf("always: %d %v", i, err)
	}
	if i, err := padsLabel(padsApp, labels); err != nil || i != 2 {
		t.Errorf("app prefers External Port: %d %v", i, err)
	}
	if i, err := padsLabel(padsApp, labels[:2]); err != nil || i != 1 {
		t.Errorf("app falls back to User Port: %d %v", i, err)
	}
	if _, err := padsLabel(padsApp, labels[:1]); err == nil {
		t.Error("app with only Live Port must fail, it would play on every track")
	}
	if _, err := padsLabel("loud", labels); err == nil {
		t.Error("unknown mode accepted")
	}
}

func TestLevelMeterPrefersFreshLiveValue(t *testing.T) {
	var m levelMeter
	now := time.Now()
	m.set(0.5)
	own := m.frac(now)
	if own <= 0 {
		t.Fatalf("own peak 0.5 gave %v", own)
	}
	m.setLive(0.1)
	live := m.frac(time.Now())
	if live >= own {
		t.Errorf("Live's 0.1 should read lower than own 0.5: live %v own %v", live, own)
	}
	if !m.hasLive(time.Now()) {
		t.Error("fresh live value not seen")
	}
	if m.hasLive(time.Now().Add(liveStale + time.Second)) {
		t.Error("stale live value still used")
	}
	if got := m.frac(time.Now().Add(liveStale + time.Second)); got != own {
		t.Errorf("stale live must fall back to own peak: %v vs %v", got, own)
	}
	m.clearLive()
	if m.hasLive(time.Now()) {
		t.Error("cleared live value still used")
	}
}

func TestLiveMeterIsShownAsLiveShowsIt(t *testing.T) {
	var m levelMeter
	m.setLive(0.8)
	if got := m.frac(time.Now()); got != 0.8 {
		t.Errorf("Live's 0.8 must fill 0.8, got %v", got)
	}
	m.setLive(1.7)
	if got := m.frac(time.Now()); got != 1 {
		t.Errorf("over 1 must clamp to 1, got %v", got)
	}
	m.setLive(-0.2)
	if got := m.frac(time.Now()); got != 0 {
		t.Errorf("below 0 must clamp to 0, got %v", got)
	}
}
