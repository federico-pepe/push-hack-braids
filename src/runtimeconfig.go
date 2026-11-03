package main

import (
	"sync"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// sharedConfig is persistedConfig's live, in-memory counterpart: the
// values watchBraidsPort/watchHWParams actually act on, and the on-screen I/O
// page (iopage.go) writes to when the user picks a new port/device.
// Changing it takes effect on those supervisors' next poll tick — no
// process restart needed — and iopage.go persists it to braids-config.json
// right after, so the choice survives a restart too.
type sharedConfig struct {
	mu             sync.Mutex
	midiClient     byte
	midiPort       byte
	pcmDevice      string
	channelOffset  int
	busPair        int
	pushManagerURL string
}

func newSharedConfig(cfg persistedConfig) *sharedConfig {
	return &sharedConfig{
		midiClient:     cfg.MidiClient,
		midiPort:       cfg.MidiPort,
		pcmDevice:      cfg.PCMDevice,
		channelOffset:  cfg.ChannelOffset,
		busPair:        cfg.BusPair,
		pushManagerURL: cfg.PushManagerURL,
	}
}

func (s *sharedConfig) getMIDI() (client, port byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	client = s.midiClient
	if client == alsaseq.Push3ClientDefault {
		// Saved as the default: Push 3, at its client number now.
		client = pushLive.Addr().Client
	}
	return client, s.midiPort
}

func (s *sharedConfig) setMIDI(client, port byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client == pushLive.Addr().Client {
		client = alsaseq.Push3ClientDefault // keep "Push 3" stable when its number moves
	}
	s.midiClient, s.midiPort = client, port
}

func (s *sharedConfig) getPCM() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pcmDevice
}

func (s *sharedConfig) setPCM(device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcmDevice = device
}

// getChannelOffset is the first channel of the pair Braids plays on: the pair
// the mixer gave us in bus mode, the configured one otherwise.
func (s *sharedConfig) getChannelOffset() int {
	if busConnected() {
		return busClient.Load().Pair()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelOffset
}

func (s *sharedConfig) setChannelOffset(offset int) {
	s.mu.Lock()
	s.channelOffset = offset
	s.busPair = offset
	s.mu.Unlock()
	if c := busClient.Load(); c != nil {
		c.SetPair(offset)
	}
}

func (s *sharedConfig) getBusPair() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busPair
}

// snapshot returns the current values as a persistedConfig, ready to save.
func (s *sharedConfig) snapshot() persistedConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return persistedConfig{
		MidiClient:     s.midiClient,
		MidiPort:       s.midiPort,
		PCMDevice:      s.pcmDevice,
		ChannelOffset:  s.channelOffset,
		BusPair:        s.busPair,
		PushManagerURL: s.pushManagerURL,
	}
}
