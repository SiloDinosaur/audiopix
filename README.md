# Audiopix

[![Go](https://img.shields.io/badge/Go-1.17%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE.md)

Turn WAV files into abstract, organic pixel art.

Audiopix listens to a local WAV file, extracts loudness and frequency features,
maps those features into a color field, then grows the final image one
neighboring pixel at a time. The result is part spectrogram, part crystal
growth, part generative album-art machine.

![Audiopix example output](img/winter.png)

## Contents

- [Features](#features)
- [Install](#install)
- [Quick Start](#quick-start)
- [Examples](#examples)
- [How It Works](#how-it-works)
- [Output Naming](#output-naming)
- [Options](#options)
- [Supported WAV Files](#supported-wav-files)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Contributing](#contributing)
- [Credit](#credit)
- [License](#license)

## Features

- Generate PNG artwork from WAV audio.
- Read a full track or render only a selected time range.
- Choose image size, output path, white space, and variation count.
- Generate repeatable studies with fixed random seeds.
- Sweep through preset creative parameters for quick exploration.
- Use the inherited `pix` image pipeline with PNG/JPG inputs and image URLs.
- Run locally as a small Go command-line tool.

## Install

Audiopix requires Go 1.17 or newer.

Install from a local checkout:

```sh
git clone https://github.com/SiloDinosaur/audiopix.git
cd audiopix
go install ./cmd/pix
```

Or run it directly from this repository:

```sh
go run ./cmd/pix -i song.wav
```

After installation, the command is:

```sh
pix -i song.wav
```

## Quick Start

Generate a default 300x300 PNG:

```sh
pix -i song.wav
```

Choose a larger canvas and a specific output file:

```sh
pix -i song.wav -w 1200 -H 800 -o song.pix.png
```

Render a section of a track:

```sh
pix -i song.wav --audio-offset 45s --audio-duration 90s -o excerpt.png
```

Make several variations from the same audio:

```sh
pix -i song.wav -o study.png -v 8
```

## Examples

Leave 15% of the canvas white:

```sh
pix -i song.wav --white-percent 15 -o airy.png
```

Create square cover art:

```sh
pix -i song.wav -w 1024 -H 1024 -o cover.png
```

Generate a broad parameter study:

```sh
pix -i song.wav -o sweep.png --sweep
```

Favor faster PNG writing over smaller file size:

```sh
pix -i song.wav --compress -2 -o fast.png
```

Use a source image instead of audio:

```sh
pix -i source.jpg -o grown.png
```

## How It Works

Audiopix reads `.wav` files, crops them if requested, analyzes overlapping FFT
frames, then resamples the resulting audio features to match the requested image
size. The default `natural` palette maps low, low-mid, mid, and high-frequency
energy into hue. Spectral flatness, bandwidth, and zero-crossing rate influence
saturation, while RMS loudness, high-frequency energy, and 85% spectral rolloff
influence brightness.

From there, Audiopix hands the generated colors to the crystallization pipeline
from [yurivish/pix](https://github.com/yurivish/pix). Colors are sorted by a mix
of source position, color similarity, and optional randomness. The canvas starts
from one or more seed pixels, then grows by repeatedly placing the next color on
a neighboring empty pixel.

Pixel placement is inherently serial and does one nearest-neighbor search per
output pixel, so render time depends on output size, placement order, and color
distribution. When `--sweep` or `--variations` is used, independent outputs are
generated in parallel.

## Output Naming

If `--output` is omitted, Audiopix writes to `pix.<input-name>.png` in the current
working directory. For example, `song.wav` becomes `pix.song.png`.

When multiple outputs are generated, the first output uses the requested output
path and later outputs add numeric suffixes before the extension:

```text
study.png
study.2.png
study.3.png
```

## Options

Most first experiments only need `-i`/`--input`, `-o`/`--output`, `-w`/`--width`,
`-H`/`--height`, `--audio-offset`, `--audio-duration`, and maybe
`-v`/`--variations`. The advanced
controls are there for deeper tuning once you know what kind of image the audio
wants to become.

### Essential

| Flag | Default | Description |
| --- | --- | --- |
| `-i`, `--input <path>` | required | Input file. WAV files use the audio pipeline. PNG, JPG, JPEG, and image URLs use the inherited image pipeline. |
| `-o`, `--output <path>` | `pix.<input>.png` | Output PNG path. Multiple outputs add `.2`, `.3`, and so on before the extension. |
| `-w`, `--width <int>` | `300` | Output image width in pixels for WAV input. Must be positive. |
| `-H`, `--height <int>` | `300` | Output image height in pixels for WAV input. Must be positive. |

### Audio Range

| Flag | Default | Description |
| --- | --- | --- |
| `--audio-offset <duration>` | `0s` | Start offset within the WAV file. Accepts Go durations like `30s` or `1m30s`, plus plain seconds like `90`. |
| `--audio-duration <duration>` | `0s` | Amount of audio to read. Accepts the same format as `--audio-offset`; `0s` uses the rest of the file. |

### Output And Batches

| Flag | Default | Description |
| --- | --- | --- |
| `--white-percent <0-100>` | `0` | Percentage of the output canvas to leave white by sampling fewer source colors. |
| `-v`, `--variations <int>` | `1` | Number of outputs to generate for each selected parameter set. |
| `-c`, `--compress <-3|-2|-1|0>` | `0` | PNG compression level. `0` is default compression, `-1` is no compression, `-2` is best speed, and `-3` is best compression. |

### Advanced

These options are useful when you are comparing looks, chasing a specific
texture, or trying to understand how the growth algorithm responds to different
sort orders and audio analysis windows.

#### Color Sorting And Growth

| Flag | Default | Description |
| --- | --- | --- |
| `--color-sort <0-100>` | `90` | Sort weighting between source position and color similarity. Higher values favor color similarity; lower values preserve more source position. |
| `--random <int>` | `0` | Randomness weight used during similarity sorting. |
| `--reverse[=true|false]` | `true` | Reverse the sorted color order. Use `--reverse=false` to disable. |
| `--random-seed <int>` | `0` | Base random seed for reproducible placement. Each variation adds its variation number to this seed. |
| `--seeds "x y[ x y...]"` | center pixel | One or more seed coordinates for the initial placed pixels. Pass an even number of integers, usually quoted by the shell. |
| `--sweep[=true|false]` | `false` | Generate a preset parameter sweep, ignoring explicitly supplied `--color-sort`, `--random`, `--reverse`, and `--seeds` values. |

Example seed layout:

```sh
pix -i song.wav -w 800 -H 600 --seeds "400 300 0 300 799 300"
```

#### FFT And Audio Analysis

| Flag | Default | Description |
| --- | --- | --- |
| `--audio-palette <name>` | `natural` | Audio-to-color palette. The available palette is `natural`. |
| `--audio-mono[=true|false]` | `true` | Downmix WAV input to mono. Use `--audio-mono=false` to preserve source channels during decoding; feature analysis still mixes channels when reading frames. |
| `--audio-fft-size <int>` | `2048` | FFT frame size for audio analysis. Must be positive and a power of two. Larger values smooth time and sharpen frequency detail. |
| `--audio-hop-size <int>` | `512` | Hop size between analysis frames. Must be positive and no larger than `--audio-fft-size`. Smaller values create denser overlapping analysis frames. |

#### Sweep Presets

`--sweep` renders the Cartesian product of these presets:

| Parameter | Values |
| --- | --- |
| `--color-sort` | `90`, `10` |
| `--random` | `0`, `10` |
| `--reverse` | `true`, `false` |
| `--seeds` | center, bottom-left, four-point cross |

That produces 24 parameter sets before applying `--variations`.

## Supported WAV Files

WAV support is intentionally modest. Audiopix supports local RIFF/WAVE files
with PCM integer samples or IEEE float samples.

| Encoding | Supported depths |
| --- | --- |
| PCM integer | 8, 16, 24, and 32 bit |
| IEEE float | 32 and 64 bit |

## Troubleshooting

| Problem | What to try |
| --- | --- |
| `please specify an input image or wav file` | Add `-i path/to/file.wav` or `--input path/to/file.wav`. |
| `invalid wav file` | Check that the input is a RIFF/WAVE file, not MP3, AIFF, FLAC, or a renamed file. |
| `audio fft size must be a power of two` | Use a value like `512`, `1024`, `2048`, or `4096`. |
| `audio hop size must not exceed fft size` | Lower `--audio-hop-size` or raise `--audio-fft-size`. |
| Render is slow | Try a smaller `--width` and `--height` while exploring, then render large once the settings feel right. |
| Output is too dense | Try `--white-percent 10` or `--white-percent 20`. |

## Development

Run the test suite:

```sh
go test ./...
```

Run the command during development:

```sh
go run ./cmd/pix -i song.wav -o test.png
```

Useful files:

- [`cmd/pix/main.go`](cmd/pix/main.go) contains the CLI flags and render loop.
- [`audio.go`](audio.go) handles WAV decoding, FFT analysis, and audio-to-color mapping.
- [`pix.go`](pix.go) runs the placement pipeline.
- [`image.go`](image.go) loads PNG/JPG sources and image URLs.

## Contributing

Issues, experiments, and small focused pull requests are welcome. Good changes
for this project usually include:

- A short explanation of the visual or behavioral goal.
- Before/after commands or sample settings.
- Tests for parser, audio, sorting, or placement behavior when the change is not purely documentation.

## Credit

Audiopix is based on [yurivish/pix](https://github.com/yurivish/pix) for the
crystallization and visualization pipeline. This repository adds the WAV-to-color
step: it decodes audio, turns frame-based frequency and loudness features into
colors, then hands those colors to the original pix-style placement algorithm.

## License

MIT. See [LICENSE.md](LICENSE.md).
