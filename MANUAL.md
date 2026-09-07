# Push Braids Manual

This manual describes the on-screen controls of push-braids, a Braids macro-oscillator module for Ableton Push 3. It lists every page and every knob, and it explains what each one does.

## How to Open the On-Screen Controls

Hold Shift and press Device together. This turns the on-screen controls on or off.

The screen shows 6 buttons above it, one per page. Press a top button to jump to that page. The 8 encoders below the screen turn the knobs on the current page.

## OSC / AMP Page

This page holds the oscillator and the main amplitude envelope.

**Algorithm** picks the synthesis engine. Braids has 47 algorithms, from simple waveforms to noise, FM, and physical models.

**Timbre** and **Color** shape the sound of the current algorithm. Their effect changes with each algorithm.

**Attack**, **Decay**, **Sustain**, and **Release** form the amplitude envelope. This envelope controls how loud the sound is over time, from the moment you press a pad to the moment it fades out.

**Volume** sets the overall output level.

## FILTER Page

This page holds the low-pass filter and its own envelope.

**FM** applies frequency modulation from the mod wheel to the pitch of the oscillator.

**Cutoff** sets the filter frequency. **Resonance** adds emphasis around the cutoff frequency.

**Filt Env** sets how much the filter envelope moves the cutoff frequency.

**F.Attack**, **F.Decay**, **F.Sustain**, and **F.Release** form the filter envelope, separate from the amplitude envelope on the OSC / AMP page.

## PRESETS Page

The left knob browses presets. Turn it to highlight a preset, then press the bottom-left button to load it.

The right knob sets the octave transpose, from -3 to +3 octaves. This knob applies at once and needs no load step.

## CRUSH / QUANT Page

This page holds lo-fi effects and the pitch quantizer.

**Resolution** reduces the bit depth of the sound, from 16-bit (full quality) down to 2-bit. Lower settings add a rough, digital texture.

**Sample Rate** reduces the sample rate of the sound, from 96kHz (full quality) down to 4kHz. Lower settings add aliasing and a lo-fi character. The two highest settings, 48kHz and 96kHz, sound the same as full quality, because the module always runs at 44.1kHz.

**Signature** blends in a fixed waveshaping character. At 0, the sound is clean. At 1, the waveshaping is at full strength.

**Scale** picks a quantizer scale, such as a major scale or a minor pentatonic scale. At "Off", pitches pass through unchanged. At any other setting, pitches snap to the notes of that scale.

**Root** sets the root note of the current scale, from C to B.

**Trig Delay** delays the start of each note, up to 500ms. At 0, notes start at once.

## AD / DRIFT Page

This page holds a second envelope, called the AD envelope, and a pitch drift effect. The AD envelope has only an attack stage and a decay stage, and it can modulate the oscillator instead of only the amplitude.

**Meta Mod** turns the AD envelope's effect on Timbre on or off. At 0, **AD>Timbre** has no effect, even if its knob is turned up.

**AD>Timbre** sets how much the AD envelope modulates Timbre. This knob needs Meta Mod on to have an effect.

**AD>FM** sets how much the AD envelope modulates the pitch.

**AD>Color** sets how much the AD envelope modulates Color.

**AD>VCA** blends the amplitude envelope toward the AD envelope's own shape. At 0, the amplitude envelope from the OSC / AMP page is unchanged. At 1, the AD envelope's shape fully replaces it.

**AD Attack** and **AD Decay** set the speed of the AD envelope's two stages.

**VCO Drift** adds a slow, organic pitch instability, similar to an analog oscillator that has not settled to a stable temperature. At 0, the pitch is stable.

## SETTINGS Page

This page selects the MIDI input, the audio output device, and the audio channel pair. Turn the encoders to browse each list, then press the matching bottom button to select an option.

## Presets and These Controls

Every preset on the PRESETS page stores its own value for every knob in this manual, including Resolution, Sample Rate, Scale, and the AD envelope. When a preset does not set one of these controls, the module uses a safe default: full quality, no quantizer, no AD modulation.
