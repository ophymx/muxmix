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

# A failing run, for error capture.
ffmpeg -hide_banner -loglevel error -y -nostdin -i "$tmp/missing.mp4" "$tmp/x.mp4" \
  > /dev/null 2> "$out/stderr-error.txt" || echo "exit=$?" >> "$out/stderr-error.txt"

find "$out" -type f -size 0 -delete
ls "$out" | wc -l | sed 's/^/files: /'
