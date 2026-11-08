package main

// bus.go — the audio bus client. When the push-audio-loopback mixer runs,
// Braids sends its audio to it (core/audiobus) instead of opening the card:
// the mixer adds all audio hacks and writes the one stream Live reads.
// Without a mixer the direct path in audiosession.go is unchanged.

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
)

const busName = "Braids"

var (
	busSocket = audiobus.SocketPath(audiobus.DefaultDir)
	busClient atomic.Pointer[audiobus.Client]
	busMode   atomic.Bool // the mixer is in use
	linkProbe = audiobus.NewLinkProbe()
)

// linkMode says the mixer sends audio to Live over Link. Then the card input
// the Create tracks button sets is wrong, so the page hides the audio setup.
func linkMode() bool { return busMode.Load() && linkProbe.Link() }

// busUp says whether a mixer answers on the bus socket.
func busUp() bool {
	c, err := net.DialTimeout("unix", busSocket, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// busConnected says whether the mixer takes our audio now.
func busConnected() bool {
	c := busClient.Load()
	return busMode.Load() && c != nil && c.Connected()
}

// busChannels is the card's channel count as the mixer reports it, or 0.
func busChannels() int {
	if c := busClient.Load(); c != nil && busMode.Load() {
		return c.Format().Channels
	}
	return 0
}
