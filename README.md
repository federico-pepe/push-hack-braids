# push-hack-braids

A [push-hack](https://github.com/federico-pepe/ableton-push-hack) module for
Ableton Push 3. A standalone DSP host: takes notes from a MIDI track in Live
(or from Push3's pads while its app is open), feeds them into a Move Anything `plugin_api_v2`
DSP module — Braids, the macro oscillator by Emilie Gillet (Mutable
Instruments), ported from Schwung DSP — and writes the rendered audio into
[push-hack-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback)'s
virtual sound card.

Has an on-screen control UI for Braids' own parameters, and an on-screen
picker for which MIDI port / audio device / channel pair to use — see
"On-screen controls" below.

Install via [Push Hack Catalog](https://github.com/federico-pepe/ableton-push-hack/tree/main/catalog),
the on-device installer built into every push-hack setup. Fully
self-contained — the Braids DSP plugin and its presets ship in the
release, no manual copy step. Requires `push-hack-audio-loopback`
(installed automatically first if missing) and `push-manager` +
`push-display` (part of the base push-hack setup) for its on-screen
controls, and the PushHack Remote Script (part of push-hack) for the tracks
and the level meter.

## Build

```bash
make        # builds both the Go binary (cgo, via Docker) and dsp.so (native x86_64, via Docker)
```

cgo (dlopen + libasound) means the Go binary can't cross-compile with a
plain `go build`. `dsp.so` is portable C++ with no ARM-specific code, so
it builds native x86_64 in a plain container — no cross toolchain needed.

## The Braids DSP plugin

`third_party/braids/` vendors the DSP source this hack hosts — the
Braids macro oscillator engine by Emilie Gillet (Mutable Instruments),
via its Move Everything port by Charles Vestal — both MIT-licensed, see
`third_party/braids/THIRD_PARTY_LICENSES.md`. `make` (or the release
workflow) builds it into `dsp.so` and bundles it with its presets in the
release tarball, so a catalog install needs no manual step.

## Set up in Live

1. In Push's audio preferences, enable the **Push Hack Virtual Audio** card (one
   stereo input pair, for example 3/4).
2. Open Braids' web page (Push Hack Catalog, or `http://push.local:7712`) and press
   **Create the Braids tracks in Live**. It makes two tracks and sets their routing:
   - **Braids**: an audio track, input `Ext. In` on the card's pair, monitor In,
     output Main. Its meter drives the Volume fader on Push's screen.
   - **Braids MIDI**: a MIDI track, input All Ins, output **Braids MIDI In**.
   If Live offers exactly one stereo pair, Braids switches its own channel pair to
   it. If Live offers several, the page lists them and asks you to set the pair in
   Braids' I/O settings to match.
3. Select the **Braids MIDI** track and press the pads. Braids plays only while that
   track is selected, so pads on your other tracks do not trigger it.

The page also has **Pads play**: "Braids only on its own track" (the default) or
"Braids on every track" (the old behaviour: the MIDI input is Live Port, so every
pad press plays Braids).

Braids needs the PushHack Remote Script, which ships with Push Hack, selected in
Live (Preferences, a control-surface slot, Input and Output None). The audio
track has to exist for the Volume fader to show Live's level; without it the
fader shows Braids' own peak.

## On-screen controls

Braids is an app of **Push Hack Home**: press **Shift + Help**, pick **BRAIDS**.
Its param UI then owns Push's screen, encoders and buttons: a gauge knob per
parameter on the current page (Algorithm, Timbre, Color,
Attack/Decay/Sustain/Release, Volume on page 1; the filter envelope on
page 2), driven by the 8 encoders above the screen. **D-Pad Left/Right**
switches pages. While the app is open the pads play Braids directly (and stop
reaching Live). **Help** goes back to Live; hold it half a second for Home. The
Volume fader shows the level of the **Braids** audio track in Live, as Live's own
meter shows it.

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
it survives a restart. The on-screen controls always come from Push's own
port, so picking another MIDI input never breaks them; it only changes which
pad presses play Braids (see "Set up in Live"). If something goes wrong, edit
`/data/push-hack/hacks/push-braids/braids-config.json` over SSH and restart the
service.

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
