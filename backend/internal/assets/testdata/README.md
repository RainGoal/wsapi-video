# Media validation samples

These tiny, original fixtures were generated locally with FFmpeg 9.0.2. They
contain a 64×36 flat amber image (four frames at 10 fps for video), or a 440 Hz
sine tone at 8 kHz for 0.2 seconds. They contain no third-party creative media.

Generation uses `lavfi` inputs `color=c=0x8b653b:s=64x36:r=10:d=0.4` and
`sine=frequency=440:sample_rate=8000:duration=0.2`. The MP4 uses H.264/yuv420p
with `+faststart`; WebM uses VP9; WebP is one image. MP3, Ogg and FLAC use the
container's default encoder. Tests run the real ffprobe decoder and require
decoded frames, including rejection of a metadata-only MP4 and an MP4 whose
media payload has been zeroed while its container metadata remains valid.

Set `FFPROBE_PATH` to the installed executable or put `ffprobe` on PATH.
Without it, the codec integration test explicitly skips; PNG/JPEG/GIF payload
validation still runs through Go's image decoders.
