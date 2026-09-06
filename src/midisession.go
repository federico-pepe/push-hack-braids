package main

// midisession.go — owns the MIDI subscription lifecycles, so the on-screen
// I/O page (iopage.go) can retarget the note-input source at a different
// ALSA seq source without a process restart. Mirrors audiosession.go's
// watchHWParams/startAudioSession split: a supervisor loop that opens,
// tears down, and reopens as its target changes.
//
// Two independent subscriptions run side by side: the I/O picker's choice
// only ever retargets the notes one. Push3's own on-screen-control traffic
// (encoders, D-Pad, screen buttons) always comes from Push3's own ALSA seq
// port — main.go wraps handler so each subscription only feeds it the kind
// of event it owns (see notesOnlyHandler/controlsOnlyHandler). Without that
// split, picking a different note-input port used to take Push3's own
// control surface down with it, since both used to ride the one
// subscription being retargeted.

import (
	"log"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// midiSource is the minimal interface watchMIDI needs to find its target:
// *sharedConfig (retargetable by the I/O page) satisfies it directly, and
// fixedMIDISource lets the always-on control-surface subscription reuse
// the same connect/retry loop with a target that never changes.
type midiSource interface {
	getMIDI() (client, port byte)
}

// fixedMIDISource is a midiSource that never changes — used to pin the
// control-surface subscription to Push3's own port regardless of whatever
// the I/O picker has the note-input source set to.
type fixedMIDISource struct{ client, port byte }

func (f fixedMIDISource) getMIDI() (client, port byte) { return f.client, f.port }

// watchMIDI opens an ALSA seq subscription to whatever src's current MIDI
// source is, and reopens it whenever that changes. label is just for the
// log lines, so the two concurrent subscriptions (notes vs. control
// surface) are distinguishable. Runs until shutdown fires.
func watchMIDI(src midiSource, label string, handler alsaseq.Handler, shutdown <-chan struct{}) {
	var seq *alsaseq.Client
	var curClient, curPort byte
	haveSeq := false

	stop := func() {
		if haveSeq {
			seq.Close() // makes the ReadLoop goroutine's blocking read fail, ending it
			haveSeq = false
		}
	}
	defer stop()

	for {
		select {
		case <-shutdown:
			return
		default:
		}

		client, port := src.getMIDI()
		if !haveSeq || client != curClient || port != curPort {
			stop()
			newSeq, err := openMIDISource(client, port, handler)
			if err != nil {
				log.Printf("opening MIDI source %d:%d for %s: %v — will retry", client, port, label, err)
				if !sleepOrStop(waitPollInterval, shutdown) {
					return
				}
				continue
			}
			seq = newSeq
			curClient, curPort = client, port
			haveSeq = true
			log.Printf("subscribed to MIDI source %d:%d for %s", client, port, label)
		}

		if !sleepOrStop(steadyPollInterval, shutdown) {
			return
		}
	}
}

func openMIDISource(client, port byte, handler alsaseq.Handler) (*alsaseq.Client, error) {
	seq, err := alsaseq.Open()
	if err != nil {
		return nil, err
	}
	if _, err := seq.CreatePort("Push Braids Host In",
		alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		seq.Close()
		return nil, err
	}
	if err := seq.Subscribe(alsaseq.Addr{Client: client, Port: port}); err != nil {
		seq.Close()
		return nil, err
	}
	go func() {
		if err := seq.ReadLoop(handler); err != nil {
			log.Printf("MIDI read loop ended: %v", err)
		}
	}()
	return seq, nil
}
