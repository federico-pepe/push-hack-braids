# Third-party code in `third_party/braids/`

This directory vendors the DSP source this hack hosts, unmodified except
where needed to build as a standalone `plugin_api_v2` module (see
`bridge.c`/`bridge.h` in the repo root for the host side).

## Mutable Instruments Braids

Copyright (c) 2012-2015 Emilie Gillet
License: MIT
Source: https://github.com/pichenettes/eurorack

The Braids macro oscillator DSP engine and associated lookup tables
(`dsp/braids/`).

## Mutable Instruments stmlib

Copyright (c) 2012-2015 Emilie Gillet
License: MIT
Source: https://github.com/pichenettes/eurorack

Utility library for DSP, math, and data structures (`dsp/stmlib/`).

## Move Everything port

Copyright (c) 2025 charlesvestal
License: MIT
Source: https://github.com/charlesvestal/schwung-braids

The `plugin_api_v2` host bridge and preset format (`dsp/braids_plugin.cpp`,
`dsp/param_helper.h`, `presets/*.braids`) this hack's own `bridge.c` talks
to, adapted from the Move Everything port of Braids.

---

Full MIT license text (identical for all three, only the copyright line
differs):

```
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
