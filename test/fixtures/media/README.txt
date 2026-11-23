Synthetic media fixtures

These 440 Hz sine-wave recordings were generated for GoWarp tests, without
third-party game assets. Tests read the committed files; ffmpeg is only needed
to regenerate them. Generated with ffmpeg 4.4.2 (libmp3lame/libvorbis).

From the project root, regenerate with absolute output paths:

fixture_dir="$(pwd)/test/fixtures/media"
mkdir -p "$fixture_dir"
ffmpeg -hide_banner -loglevel error -f lavfi -i sine=frequency=440:sample_rate=44100:duration=10 -ac 2 -c:a libvorbis -q:a 0 -map_metadata -1 -y "$fixture_dir/tone.ogg"
ffmpeg -hide_banner -loglevel error -f lavfi -i sine=frequency=440:sample_rate=44100:duration=1 -ac 2 -c:a libmp3lame -b:a 32k -reservoir 0 -map_metadata -1 -y "$fixture_dir/tone.mp3"
ffmpeg -hide_banner -loglevel error -f lavfi -i sine=frequency=440:sample_rate=22050:duration=348.5 -ac 1 -c:a libmp3lame -b:a 8k -map_metadata -1 -y "$fixture_dir/long-tone.mp3"

Files:
- tone.ogg: 26,396 bytes; stereo 44.1 kHz, ten seconds. Also decoded with a WAV
  format hint to exercise sniffing of mislabeled Vorbis data and allocation limits.
- tone.mp3: 4,433 bytes; stereo 44.1 kHz, one second. Used for exact streamed vs
  eager PCM comparisons, including seeking and looping. The bit reservoir is
  disabled because go-mp3 seeks with only one preceding frame of history.
- long-tone.mp3: 348,805 bytes; mono 22.05 kHz, 348.5 seconds before MP3 padding.
  MPEG-2 avoids the decoder's unsupported MPEG-2.5 format. Low bitrate keeps
  the fixture small while exercising long-track metadata,
  bounded streaming startup, independent playback cursors, and cache eviction.

Project/SVG/PNG/WAV fixtures are generated directly in the media tests and packed
into temporary SB3 ZIP archives. The custom Korean font test uses the existing
app/assets PretendardRegular font and its bundled license, without duplicating it.
