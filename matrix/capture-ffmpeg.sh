#!/bin/sh
# capture-ffmpeg.sh — record ffmpeg's version and capability listings, its
# -progress stream and stats line from one encode, and how it reports a
# missing input.
#
# usage: matrix/capture-ffmpeg.sh <out-root>
# Output lands in <out-root>/<ffmpeg-version>/.
set -eu
root=$1
full=$(ffmpeg -version 2>&1 | head -n1)
ver=$(printf '%s\n' "$full" | sed -E 's/^ffmpeg version ([nN]?)([0-9]+(\.[0-9]+)*).*/\2/')
out="$root/$ver"
mkdir -p "$out"
echo "capturing ffmpeg $ver -> $out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

ffmpeg -version > "$out/version.txt" 2>&1 || true
for l in encoders decoders muxers demuxers filters pix_fmts sample_fmts hwaccels bsfs protocols; do
  ffmpeg -hide_banner -$l > "$out/$l.txt" 2>/dev/null || true
done
ffmpeg -hide_banner -h encoder=libx264 > "$out/help-encoder-libx264.txt" 2>/dev/null || true
ffmpeg -hide_banner -h decoder=h264 > "$out/help-decoder-h264.txt" 2>/dev/null || true
ffmpeg -hide_banner -h muxer=mp4 > "$out/help-muxer-mp4.txt" 2>/dev/null || true
ffmpeg -hide_banner -h demuxer=mov > "$out/help-demuxer-mov.txt" 2>/dev/null || true
ffmpeg -hide_banner -h filter=scale > "$out/help-filter-scale.txt" 2>/dev/null || true
ffmpeg -hide_banner -h bsf=h264_mp4toannexb > "$out/help-bsf-h264_mp4toannexb.txt" 2>/dev/null || true
# Progress on a pipe (fd 3) plus the stats line on stderr, same run.
ffmpeg -hide_banner -loglevel info -stats -stats_period 0.05 -y -nostdin \
  -f lavfi -i testsrc2=size=320x240:rate=25:duration=4 -f lavfi -i sine=duration=4 \
  -c:v libx264 -preset veryslow -pix_fmt yuv420p -c:a aac \
  -progress pipe:3 "$tmp/out.mp4" 3> "$out/progress.txt" 2> "$out/stderr.txt" || true

# Analysis filters: deterministic synthetic media, one log per filter.
A="$tmp/a.wav"; V="$tmp/v.mp4"; I="$tmp/i.mp4"; F="$tmp/f.mp4"
enc="-c:v libx264 -preset ultrafast -pix_fmt yuv420p"
# 3 s of 440 Hz tone with silence from 1 s to 2 s.
ffmpeg -hide_banner -nostdin -y -loglevel error -f lavfi \
  -i "aevalsrc=sin(440*2*PI*t)*(lt(t\,1)+gt(t\,2)):s=48000:d=3" -c:a pcm_s16le "$A" 2>/dev/null || true
# 320x240 test pattern in a 360x280 black frame, black from 1 s to 2 s, hard cut at 3 s.
ffmpeg -hide_banner -nostdin -y -loglevel error \
  -f lavfi -i "testsrc2=size=320x240:rate=25:duration=3,drawbox=c=black:t=fill:enable='between(t,1,2)',pad=360:280:20:20" \
  -f lavfi -i "rgbtestsrc=size=360x280:rate=25:duration=1" \
  -filter_complex "[0][1]concat=n=2:v=1:a=0" $enc "$V" 2>/dev/null || true
# Interlaced (top field first) test pattern.
ffmpeg -hide_banner -nostdin -y -loglevel error \
  -f lavfi -i "testsrc2=size=320x240:rate=25:duration=2,tinterlace=mode=interleave_top,setfield=tff" \
  $enc -flags +ildct+ilme "$I" 2>/dev/null || true
# Motion, then a frozen gray frame from 1 s to 3 s, then motion again.
ffmpeg -hide_banner -nostdin -y -loglevel error \
  -f lavfi -i "testsrc2=size=64x64:rate=25:duration=1" -f lavfi -i "color=c=gray:size=64x64:rate=25:duration=2" \
  -f lavfi -i "testsrc2=size=64x64:rate=25:duration=1" \
  -filter_complex "[0][1][2]concat=n=3:v=1:a=0" $enc "$F" 2>/dev/null || true

analyze() {
  # analyze <name> <args...>: stderr of an analysis run to analysis-<name>.txt
  name=$1; shift
  ffmpeg -hide_banner -nostdin -loglevel info "$@" -f null - > /dev/null 2> "$out/analysis-$name.txt" || true
}
analyze loudnorm      -i "$A" -af "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json"
analyze ebur128       -i "$A" -af "ebur128=peak=true+sample"
analyze volumedetect  -i "$A" -af volumedetect
analyze silencedetect -i "$A" -af "silencedetect=n=-50dB:d=0.5"
analyze astats        -i "$A" -af "astats=measure_perchannel=none"
analyze blackdetect   -i "$V" -vf "blackdetect=d=0.5:pix_th=0.10"
analyze blackframe    -i "$V" -vf "blackframe=amount=98"
analyze cropdetect    -t 3 -i "$V" -vf "cropdetect=limit=24:round=2:reset=0"
analyze scdet         -i "$V" -vf "scdet=threshold=10"
analyze scene         -i "$V" -vf "select='gt(scene,0.3)',metadata=print"
analyze idet          -i "$I" -vf idet
analyze freezedetect  -i "$F" -vf "freezedetect=n=-60dB:d=0.5"

# A failing run, for error capture.
ffmpeg -hide_banner -loglevel error -y -nostdin -i "$tmp/missing.mp4" "$tmp/x.mp4" \
  > /dev/null 2> "$out/stderr-error.txt" || echo "exit=$?" >> "$out/stderr-error.txt"

find "$out" -type f -size 0 -delete
ls "$out" | wc -l | sed 's/^/files: /'
