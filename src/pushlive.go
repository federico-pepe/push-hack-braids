package main

import "github.com/federico-pepe/ableton-push-hack/core/alsaseq"

// pushLive is where Push 3's Live Port is. The ALSA client number moves with the
// sound cards: 16 while Push 3 is card 0, 20 when the loopback card is card 0.
// watchBraidsPort refreshes it before each subscribe attempt. The saved MIDI
// choice keeps the default client (16) to mean "Push 3, wherever it is"; see
// sharedConfig.getMIDI.
var pushLive alsaseq.Push3
