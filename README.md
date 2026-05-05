# Pix

Turn photos into abstract art.

![Road in the Winter Forest by Olga Malamud Pavlovich](img/winter.png)

Install the command-line tool with `go get`:

```
go get -u github.com/yurivish/pix/cmd/pix
```

Run it like so:

```
pix -in picture.jpg
```

WAV files can be used as an input source too:

```
pix -in song.wav -width 800 -height 600 -out song.png
```

For WAV input, pix decodes the audio, extracts frame-based loudness and
frequency features, maps those features to a synthetic row-major source image,
then hands those colors to the normal pix sampling, sorting, and placement
pipeline. The placement engine is unchanged, so existing flags like
`-colorsort`, `-random`, `-reverse`, `-seeds`, `-sweep`, and `-variations`
still apply.

Audio-specific flags:

```
-audio-palette natural
-audio-offset 30s
-audio-duration 2m
-audio-mono=true
-audio-fft-size 2048
-audio-hop-size 512
```

`-audio-offset` and `-audio-duration` accept Go duration strings like `30s`,
`1m30s`, or plain seconds like `90`. A duration of `0s` uses the rest of the
file. The first audio palette is `natural`, which maps bass/mid/high energy to
hue, spectral flatness to saturation, and RMS loudness to brightness.

WAV support is intentionally modest in the first version: local RIFF/WAVE files
with PCM integer samples or IEEE float samples are supported, mono is used by
default, and stereo files are downmixed unless `-audio-mono=false` is set.

Generate multiple outputs by sweeping the parameter space:

```
pix -in picture.jpg -sweep
```

Pix is capable of generating 8,000×8,000 outputs in around a minute. 

The pixel-placement process is inherently serial and performs one nearest-neighbor search per output pixel, so the time taken depends significantly on the placement order and color distribution since those affect the size of the dynamic search tree and the shape of the frontier. 

When the `-sweep` or `-variations` flags are used, variations are generated in parallel.
