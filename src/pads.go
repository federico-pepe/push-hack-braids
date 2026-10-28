package main

// pads.go — which pad presses play Braids.
//
// Push's pads reach Braids through its MIDI input (the I/O picker). The input
// "Live Port" carries every pad press whichever track is selected, so with it
// Braids plays on all tracks. The better way is a MIDI track in Live whose
// output is "Braids MIDI In": Braids then plays only when that track is
// selected. So there are two modes:
//
//	app     (default) the MIDI input is a port that carries no pad presses. Pads
//	        play Braids only while its app is open in Push Hack Home (then the
//	        pads are Braids' own), and through the Braids MIDI track otherwise.
//	always  the MIDI input is Live Port: pads play Braids on every track.

import (
	"fmt"
	"strings"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

const (
	padsApp    = "app"
	padsAlways = "always"
)

// acceptPushNote decides whether a note from Push's own hardware may play.
//
//	configured  it came from the port the MIDI input is set to
//	livePort    it came from Live Port (where the pads send)
//	appOpen     Braids owns the surface, so the pads are its own
//
// Notes outside the pad grid (36-99) never play: encoder touch is notes 0-7,
// the D-pad centre touch is 13, and so on (docs/push3-button-map.md).
func acceptPushNote(configured, livePort, appOpen bool, note byte) bool {
	if !configured && !(livePort && appOpen) {
		return false
	}
	return note >= 36 && note <= 99
}

// padsModeOf names the mode for the MIDI input in use.
func padsModeOf(client, port byte) string {
	if client == alsaseq.Push3ClientDefault && port == alsaseq.Push3PortDefault {
		return padsAlways
	}
	return padsApp
}

// padsLabel picks the label of the MIDI input option for a mode: Live Port for
// "always"; for "app" the first port that does not carry pad presses.
func padsLabel(mode string, labels []string) (int, error) {
	want := []string{"Live Port"}
	if mode == padsApp {
		want = []string{"External Port", "User Port"}
	} else if mode != padsAlways {
		return -1, fmt.Errorf("unknown pads mode %q", mode)
	}
	for _, w := range want {
		for i, l := range labels {
			if strings.Contains(l, w) {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("no MIDI input for mode %q among %v", mode, labels)
}

// SetPadsMode switches between the two modes by choosing the matching MIDI
// input, which is saved like any pick in the I/O picker.
func (io *ioState) SetPadsMode(mode string) error {
	opts := io.MIDIOptions()
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.Label
	}
	i, err := padsLabel(mode, labels)
	if err != nil {
		return err
	}
	return io.SetMIDIByIndex(i)
}

// PadsMode is the mode of the MIDI input now in use.
func (io *ioState) PadsMode() string {
	return padsModeOf(io.rt.getMIDI())
}
