# Audiopix

Turn WAV files into abstract, organic pixel art.

Audiopix decodes WAV audio, turns frame-based frequency and loudness features
into a color field, sorts those colors by a mix of feature position, color
similarity, and randomness, then grows a new canvas one neighboring pixel at a
time.

## Install

Install the command-line tool:

```sh
go install github.com/SiloDinosaur/audiopix@latest
```

Or run it from this repository:

```sh
go run ./cmd/pix -in song.wav
```

## Quick Start

Generate a default 300x300 PNG from a WAV file:

```sh
pix -in song.wav
```

Choose a size and output path:

```sh
pix -in song.wav -width 1200 -height 800 -out song.pix.png
```

Generate a family of variations:

```sh
pix -in song.wav -out study.png -variations 8
```

Sweep through preset sort, randomness, reverse, and seed combinations:

```sh
pix -in song.wav -out sweep.png -sweep
```

## How It Works

Audiopix reads local `.wav` files, extracts frame-based loudness and frequency
features, maps those features to a row-major color field, then hands those
colors to the sampling, sorting, and placement pipeline. Sort and placement
flags such as `-colorsort`, `-random`, `-reverse`, `-seeds`, `-sweep`, and
`-variations` all apply.

The pixel-placement process is inherently serial and performs one
nearest-neighbor search per output pixel, so render time depends on output size,
placement order, and color distribution. When `-sweep` or `-variations` is used,
independent outputs are generated in parallel.

## Output Naming

If `-out` is omitted, Audiopix writes to `pix.<input-name>.png` in the current
working directory. For example, `song.wav` becomes `pix.song.png`.

When multiple outputs are generated, the first output uses the requested output
path and later outputs add numeric suffixes before the extension:

```text
study.png
study.2.png
study.3.png
```

## Options

| Flag | Default | Description |
| --- | --- | --- |
| `-in <path>` | required | Input WAV file. Local `.wav` files are supported. |
| `-out <path>` | `pix.<input>.png` | Output PNG path. Multiple outputs add `.2`, `.3`, and so on before the extension. |
| `-width <int>` | `300` | Output image width in pixels. Must be positive. |
| `-height <int>` | `300` | Output image height in pixels. Must be positive. |
| `-white-percent <0-100>` | `0` | Percentage of the output canvas to leave white by sampling fewer audio-derived colors. |
| `-colorsort <0-100>` | `90` | Sort weighting between audio-feature position and color similarity. Higher values favor color similarity; lower values preserve more feature position. |
| `-random <int>` | `0` | Randomness weight used during similarity sorting. |
| `-reverse[=true|false]` | `true` | Reverse the sorted color order. Use `-reverse=false` to disable. |
| `-random-seed <int>` | `0` | Base random seed for reproducible placement. Each variation adds its variation number to this seed. |
| `-seeds "x y[ x y...]"` | center pixel | One or more seed coordinates for the initial placed pixels. Pass an even number of integers, usually quoted by the shell. |
| `-variations <int>` | `1` | Number of outputs to generate for each selected parameter set. |
| `-sweep[=true\|false]` | `false` | Generate a preset parameter sweep, ignoring explicitly supplied `-colorsort`, `-random`, `-reverse`, and `-seeds` values. |
| `-compress <-3\|-2\|-1\|0>` | `0` | PNG compression level. `0` is default compression, `-1` is no compression, `-2` is best speed, and `-3` is best compression. |

### Audio Options

| Flag | Default | Description |
| --- | --- | --- |
| `-audio-palette <name>` | `natural` | Audio-to-color palette. The available palette is `natural`. |
| `-audio-offset <duration>` | `0s` | Start offset within the WAV file. Accepts Go duration strings like `30s` or `1m30s`, plus plain seconds like `90`. |
| `-audio-duration <duration>` | `0s` | Amount of audio to read. Accepts the same format as `-audio-offset`; `0s` uses the rest of the file. |
| `-audio-mono[=true\|false]` | `true` | Downmix WAV input to mono. Use `-audio-mono=false` to preserve source channels during decoding; feature analysis still mixes channels when reading frames. |
| `-audio-fft-size <int>` | `2048` | FFT frame size for audio analysis. Must be positive and a power of two. |
| `-audio-hop-size <int>` | `512` | Hop size between analysis frames. Must be positive and no larger than `-audio-fft-size`. |

The `natural` audio palette maps sub and bass energy, low mids, mids, and air
energy into hue. Spectral flatness, bandwidth, and zero-crossing rate influence
saturation, while RMS loudness, high-frequency energy, and 85% spectral rolloff
influence brightness.

WAV support is intentionally modest: local RIFF/WAVE files with PCM integer
samples or IEEE float samples are supported. PCM bit depths of 8, 16, 24, and
32 bits are accepted; float WAV files may be 32-bit or 64-bit.

## Sweep Presets

`-sweep` renders the Cartesian product of these presets:

| Parameter | Values |
| --- | --- |
| `-colorsort` | `90`, `10` |
| `-random` | `0`, `10` |
| `-reverse` | `true`, `false` |
| `-seeds` | center, bottom-left, four-point cross |

That produces 24 parameter sets before applying `-variations`.

## Examples

Leave 15% of the canvas white:

```sh
pix -in song.wav -white-percent 15 -out airy.png
```

Start growth from several seed points:

```sh
pix -in song.wav -seeds "400 300 0 300 799 300" -width 800 -height 600
```

Render a specific section of a song:

```sh
pix -in song.wav -audio-offset 45s -audio-duration 90s -width 1024 -height 1024
```

Favor speed over PNG file size:

```sh
pix -in song.wav -compress -2 -out fast.png
```
