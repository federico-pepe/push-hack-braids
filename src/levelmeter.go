package main

// levelmeter.go — the level the Volume fader shows.
//
// Two sources. The one that counts is Live's own meter for the "Braids" audio
// track (pushhack.go), which is what the user actually hears after Live's
// routing and mixer. When that link is down, or the track is not there, the
// fader falls back to Braids' own peak, measured in the render loop.

import (
	"math"
	"sync/atomic"
	"time"
)

// liveStale is how long a Live meter value stays good with no update. The
// "meters" topic sends on change, ten times a second at most, and a silent
// track stops changing: stale means the link is down, not that it is quiet.
const liveStale = 1500 * time.Millisecond

// levelMeter is lock-free: the render thread writes bits every block, the
// PushHack follower writes live, and the display loop reads both.
type levelMeter struct {
	bits   atomic.Uint64 // Braids' own peak, 0-1 linear
	live   atomic.Uint64 // Live's meter for the Braids track
	liveAt atomic.Int64  // unix nanoseconds of the last live value, 0 = none
}

func (m *levelMeter) set(v float64) { m.bits.Store(math.Float64bits(v)) }
func (m *levelMeter) get() float64  { return math.Float64frombits(m.bits.Load()) }

func (m *levelMeter) setLive(v float64) {
	m.live.Store(math.Float64bits(v))
	m.liveAt.Store(time.Now().UnixNano())
}

func (m *levelMeter) clearLive() { m.liveAt.Store(0) }

// hasLive reports whether Live's value is fresh.
func (m *levelMeter) hasLive(now time.Time) bool {
	at := m.liveAt.Load()
	return at != 0 && now.Sub(time.Unix(0, at)) < liveStale
}

// frac is the fader fill, 0-1: Live's meter when it is fresh, else Braids' own
// peak. Live's value is already on the scale Live's own meters use (about 0.85
// is 0 dB, checked on a Push: a peak of 0.31 linear read 0.80), so it is shown
// as it is: the fader then matches the Braids track's meter in Live. Braids'
// own peak is a linear amplitude and goes through the dB scale.
func (m *levelMeter) frac(now time.Time) float64 {
	if m.hasLive(now) {
		return min(max(math.Float64frombits(m.live.Load()), 0), 1)
	}
	return dbFrac(m.get())
}
