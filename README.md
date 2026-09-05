# push-hack-braids

A [push-hack](https://github.com/federico-pepe/ableton-push-hack) module for
Ableton Push 3. A standalone DSP host: reads pad/button MIDI straight off
Push3's own ALSA sequencer, feeds notes into a Move Anything `plugin_api_v2`
DSP module (Braids, a Mutable Instruments macro-oscillator port), and writes
the rendered audio into
[push-hack-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback)'s
virtual sound card.

Has an on-screen control UI for Braids' own parameters, and an on-screen
picker for which MIDI port / audio device / channel pair to use — see
"On-screen controls" below.

Install via [Push Hack Catalog](https://github.com/federico-pepe/ableton-push-hack/tree/main/catalog),
the on-device installer built into every push-hack setup. Requires
`push-hack-audio-loopback` (installed automatically first if missing) and
`push-manager` + `push-display` (part of the base push-hack setup) for its
on-screen controls.

## Build

```bash
make        # cross-builds via Docker golang:1.25-bookworm, needs libasound2-dev
```

cgo (dlopen + libasound) means this can't cross-compile with a plain
`go build`.

## Build the Braids DSP plugin

The DSP plugin itself (`dsp.so`) is a separate project, not bundled here —
build it and copy it in after install:

```bash
cd path/to/schwung-braids-main
mkdir -p build
docker run --rm --platform linux/amd64 -v "$PWD":/build -w /build debian:bullseye sh -c '
  apt-get update -qq && apt-get install -qq -y g++ >/dev/null
  BRAIDS_SRCS="src/dsp/braids/macro_oscillator.cc src/dsp/braids/analog_oscillator.cc \
    src/dsp/braids/digital_oscillator.cc src/dsp/braids/resources.cc \
    src/dsp/braids/quantizer.cc src/dsp/stmlib/utils/random.cc"
  for s in $BRAIDS_SRCS; do g++ -O3 -fPIC -std=c++14 -DTEST -Isrc/dsp -c "$s" -o "build/$(basename "$s" .cc).o"; done
  g++ -O3 -fPIC -std=c++14 -DTEST -Isrc/dsp -c src/dsp/braids_plugin.cpp -o build/braids_plugin.o
  g++ -shared build/*.o -o build/dsp.so -lm
'
```

Then, after installing this hack via the catalog:

```bash
ssh root@push.local 'mkdir -p /data/push-hack/hacks/push-braids/module/presets'
scp path/to/schwung-braids-main/build/dsp.so \
  root@push.local:/data/push-hack/hacks/push-braids/dsp.so
scp path/to/schwung-braids-main/src/presets/*.braids \
  root@push.local:/data/push-hack/hacks/push-braids/module/presets/
ssh root@push.local '/etc/init.d/push-hack-push-braids restart'
```

To actually hear it: an audio track in Live's own Set, Input = "Push Hack
Virtual Audio", Monitor = In, routed to Master. Pressing a pad on Push3
should now trigger Braids and come out the real speaker/headphone output.

## On-screen controls

Hold **Shift + Device** to toggle a param UI on Push's own screen: a
gauge knob per parameter on the current page (Algorithm, Timbre, Color,
Attack/Decay/Sustain/Release, Volume on page 1; the filter envelope on
page 2), driven by the 8 encoders above the screen. **D-Pad Left/Right**
switches pages. Turning the UI on also enables push-manager's MIDI
intercept, so pad hits drive Braids only — they stop reaching Live for
as long as the UI is on. Toggling it off (same chord) hands the screen
and MIDI back to normal.

### I/O picker (page 3)

**D-Pad Right** twice from page 1 reaches a third page, "I/O" — a
scrollable list to pick:

- **MIDI INPUT** — which of Push3's own three MIDI ports to read
  pad/button presses from (normally "Live Port", the default).
- **AUDIO OUTPUT** — which playback device on the system to render to.
- **AUDIO CHANNEL** — which channel pair of that device (1-2, 3-4, ...)
  the stereo signal lands on, so it can line up with whatever channel
  pair Live's own track is set to read from.

**D-Pad Up/Down** moves the list cursor, **Select** confirms the
highlighted row. A change applies immediately (MIDI resubscribes, the
audio device reopens if needed) and is saved to `braids-config.json`, so
it survives a restart. Picking the wrong MIDI port breaks Shift+Device
itself (no pad/button events reach this hack at all) — if that happens,
edit `/data/push-hack/hacks/push-braids/braids-config.json` directly
over SSH and restart the service.

## Persistent install

Runs as a real sysvinit service, installed like any other catalog hack,
and needs no manual steps after a reboot:

- Waits for `push-hack-audio-loopback`'s virtual card to appear, then
  waits for Live to actually open its side, before opening any audio
  device — works even if the two services start in any order.
- Reads channels, sample rate, period, and buffer size straight from
  what Live negotiated, every few seconds, for as long as it runs — so
  if Live restarts with a different buffer size, this hack reopens its
  audio device to match, with no restart needed.
- If it crashes (a DSP plugin bug, an ALSA error), a small supervisor
  built into the same binary restarts it on its own, with a short
  growing delay between tries.
- Its own settings (MIDI port, audio device, channel pair) live in
  `braids-config.json`, next to `hack.json`, so they survive a reboot
  and a catalog update.

## Known limits

- The DSP plugin (`dsp.so`) and its presets aren't installed by the
  catalog yet — copy them in by hand once, per "Build the Braids DSP
  plugin" above.
- Beyond the 8 encoders, D-Pad (param/I-O pages), and Note On/Off (pad
  grid), no other MIDI is wired up — pitch bend and aftertouch are
  ignored.

## Releasing

```bash
# bump hack.json's "version", commit, then:
git tag v0.1.1
git push origin v0.1.1
```

`.github/workflows/release.yml` builds (cgo, via the same Docker image
the Makefile uses), packages, publishes the GitHub Release, and updates
`release.json` — Push Hack Catalog reads that file live, so no further
step is needed for the catalog to pick up a new version.
