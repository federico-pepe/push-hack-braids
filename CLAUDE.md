# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A [push-hack](https://github.com/federico-pepe/ableton-push-hack) module for Ableton Push 3. A standalone Go host that reads pad/button MIDI from Push 3's own ALSA sequencer, sends notes into a vendored Braids DSP plugin (macro oscillator by Emilie Gillet, via cgo/dlopen), and writes rendered audio into [push-hack-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback)'s virtual sound card. It also draws an on-screen control UI on Push's own screen.

## Build

```bash
make            # builds both push-braids (cgo) and dsp.so (native x86_64), both via Docker
make build      # just the Go binary
make build-dsp  # just the DSP plugin
make clean
```

Both steps need Docker. `push-braids` needs real ALSA headers (`libasound2-dev`) and cgo, so a plain `go build` on macOS fails with `alsa/asoundlib.h not found` — this is expected, not a bug. Build inside Docker instead, or use `make`.

There are no lint or test commands in this repo. Verify a change by building it (`make`) and, where possible, deploying it to real Push 3 hardware (see below).

## Deploy to Push 3 hardware for a manual test

No script for this exists in the repo yet. These are the exact steps used in practice:

```bash
make build build-dsp   # produces ./push-braids and ./dsp.so at repo root

ssh root@push.local "/etc/init.d/push-hack-push-braids stop"

scp push-braids                       ableton@push.local:/data/push-hack/hacks/push-braids/push-braids
scp dsp.so                            ableton@push.local:/data/push-hack/hacks/push-braids/dsp.so
scp hack.json                         ableton@push.local:/data/push-hack/hacks/push-braids/hack.json
scp third_party/braids/presets/*.braids ableton@push.local:/data/push-hack/hacks/push-braids/module/presets/
ssh ableton@push.local "chmod +x /data/push-hack/hacks/push-braids/push-braids"

ssh root@push.local "/etc/init.d/push-hack-push-braids start"
```

Key facts:
- SSH as `ableton@push.local` for normal files, `root@push.local` for service control (no `sudo` on the device).
- Install path on the device: `/data/push-hack/hacks/push-braids/`.
- Service name: `push-hack-push-braids` (sysvinit, `/etc/init.d/push-hack-push-braids {start|stop|restart|status}`). Stop it before copying the binary — a running binary is locked on Linux.
- Logs: `/data/push-hack/logs/push-braids.log`. Also useful: `push-manager.log`, `push-display.log` for the on-screen UI, `push-audio-loopback.log` for the virtual card.
- `braids-config.json` (MIDI port, audio device, channel pair) lives next to `hack.json` on the device and survives a redeploy — don't overwrite it by accident.

## Release process

```bash
# bump hack.json's "version" to match the tag, exactly (no leading "v")
git tag v0.2.2-alpha
git push origin main
git push origin v0.2.2-alpha
```

`.github/workflows/release.yml` triggers on any `v*` tag push. It **hard-fails** if the tag version doesn't exactly match `hack.json`'s `"version"` field — always bump `hack.json` first, in the same commit. The workflow builds the Go binary and `dsp.so` (same Docker images as the local `Makefile`), packages them with `hack.json` and the presets into `push-braids.tar.gz`, publishes a GitHub Release, and commits an updated `release.json` back to `main` — Push Hack Catalog reads `release.json` live, so no further step is needed for an install to see the new version.

## Architecture

### Two independent goroutine "supervisor loops"

The pattern used throughout: a loop that watches for a target to change (a config value, a hardware state) and opens/closes a resource to match, so the on-screen pickers can take effect without a process restart.

- **`watchMIDI`** (`midisession.go`) — runs the loop **twice**, for two separate ALSA seq subscriptions:
  1. One retargets to whatever the I/O picker's MIDI-input choice is (`sharedConfig`), and only ever carries **note** events (pad presses) into the plugin.
  2. One is permanently pinned to Push 3's own port (`alsaseq.Push3ClientDefault`/`Push3PortDefault`) and only ever carries **control-surface** events (encoders, D-Pad, screen buttons).

  This split matters: the two kinds of event used to share one subscription, so picking a MIDI input other than Push 3's own port also broke the on-screen encoders/buttons. `main.go`'s `notesOnlyHandler`/`controlsOnlyHandler` wrap the shared `midiHandler` to gate which events each subscription is allowed to feed it — the actual channel/note-range filtering logic in `midiHandler.Fixed` is unchanged and shared by both.

- **`watchHWParams`** (`audiosession.go`) — waits for `push-audio-loopback`'s virtual card, waits for Live to actually open it, reads channels/rate/period/buffer straight off what Live negotiated, and reopens the `audioSession` whenever those change or the user picks a different output device.

### One goroutine owns the plugin instance

Braids' C++ instance state is not thread-safe. Every `bridge_plugin_*` call happens on `audioSession.run`'s single goroutine (`audiosession.go`), which drains two channels each render block:
- `midiCh` — raw MIDI bytes, produced by `midiHandler.Fixed` running on the ALSA read-loop goroutine.
- `ctlCh` — decoded `controlEvent`s (encoder turn / page change / I/O picker move), same producer.

Both are plain byte/struct channels, not method calls into the plugin, specifically so the ALSA read-loop goroutines never touch the plugin directly.

### Param UI: metadata comes from the plugin, curation is local

`params.go` reads the plugin's own parameter list via `bridge_plugin_get_param("chain_params")` (JSON: key/name/type/min/max/options per param) — this host never hardcodes a Braids param's range or the engine's shape names. `paramPages` then curates which of those params sit on which on-screen page and encoder slot; a page can leave trailing encoders unused.

One exception: **`preset`** is not part of `chain_params` at all — the plugin only exposes it via its own `ui_hierarchy` browser convention, unused by this host. `fetchPresetMeta` (`params.go`) instead builds a synthetic enum param by reading the `.braids` JSON files straight off disk (`<module_dir>/presets/*.braids`), so it never has to cycle the live plugin instance through every preset just to read names back (which would silently overwrite `defaultParams`' starting values via `v2_apply_preset`).

Some enum params are deliberately slowed down (`enumSensitivity` in `params.go`): Push's encoders send an accelerating delta per message, which is fine for a continuous float but made a single brisk turn skip many entries in a discrete list (`engine`'s 47 algorithms, the preset list). Delta accumulates in `paramSlot.accum` and only steps the value once enough of it has built up.

The PATCH page (preset + octave transpose) gets its own full-screen scrollable list rendering (`renderPatchPage` in `display.go`) instead of the generic per-cell knob view every other page uses — a single centered readout can't show the neighboring preset names, which is the point of slowing that knob down in the first place.

### Persisted vs. live config

`config.go` defines `persistedConfig`, loaded from `braids-config.json` (JSON file next to `hack.json` on the device, survives restarts/updates). `runtimeconfig.go`'s `sharedConfig` is its live, mutex-guarded, in-memory counterpart — the I/O picker (`iopage.go`) writes to `sharedConfig` immediately (so `watchMIDI`/`watchHWParams` pick the change up on their next poll tick, no restart needed) and then persists the same values back to `braids-config.json`.

### The C bridge

`bridge.h`/`bridge.c` is the only place cgo talks to non-Go code: `dlopen`-ing the Braids `plugin_api_v2` module and driving ALSA PCM playback. Two independent opaque handles (`bridge_plugin_t`, `bridge_pcm_t`) with no shared state between them — the plugin instance and the audio session are opened/closed on different lifecycles (see the two supervisor loops above).

### Vendored DSP source

`third_party/braids/` is vendored MIT-licensed C++ (Mutable Instruments' Braids, via a Move Anything port) — see its own `THIRD_PARTY_LICENSES.md`. It has no ARM-specific code, so `dsp.so` builds as native x86_64, no cross toolchain needed.
