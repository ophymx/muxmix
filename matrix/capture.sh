#!/bin/sh
# capture.sh — run ffprobe over the fixture media in every output mode and
# record the JSON, plus the binary's version and section listing.
#
# Runs on the host or inside a matrix container (POSIX sh, no bash-isms).
#
# usage: matrix/capture.sh <media-dir> <out-root>
# Output lands in <out-root>/<ffprobe-version>/.
set -eu
media=$1
root=$2

full=$(ffprobe -version 2>&1 | head -n1)
# "ffprobe version 4.4.2-0ubuntu0.22.04.1 Copyright ..." -> "4.4.2"
ver=$(printf '%s\n' "$full" | sed -E 's/^ffprobe version ([nN]?)([0-9]+(\.[0-9]+)*).*/\2/')
case "$ver" in
  [0-9]*) ;;
  *) ver=$(printf '%s\n' "$full" | awk '{print $3}' | tr '/:' '__') ;;
esac
out="$root/$ver"
mkdir -p "$out"
echo "capturing ffprobe $ver -> $out"

ffprobe -version > "$out/version.txt" 2>&1 || true
ffprobe -hide_banner -sections > "$out/sections.txt" 2>&1 || true
ffprobe -hide_banner -loglevel quiet -print_format json=compact=1 \
  -show_program_version -show_library_versions > "$out/versions.json" 2>/dev/null || true
ffprobe -hide_banner -loglevel quiet -print_format json=compact=1 \
  -show_pixel_formats > "$out/pixel_formats.json" 2>/dev/null || true
ffprobe -hide_banner -loglevel quiet -print_format json=compact=1 \
  -show_error -show_format "$media/does-not-exist.mp4" > "$out/error-missing.json" 2>/dev/null || true
ffprobe -hide_banner -loglevel quiet -print_format json=compact=1 \
  -show_error -show_format "$media/../../../go.mod" > "$out/error-invalid.json" 2>/dev/null || true

# Feature detection: -show_stream_groups appeared in 7.0.
groups=""
if ffprobe -hide_banner -h 2>/dev/null | grep -q show_stream_groups; then
  groups="-show_stream_groups"
fi

probe() {
  # probe <name> <file> <args...>
  name=$1; file=$2; shift 2
  ffprobe -hide_banner -loglevel quiet -print_format json=compact=1 -show_error "$@" "$file" \
    > "$out/$name.json" 2>/dev/null || true
}

for f in "$media"/*; do
  [ -f "$f" ] || continue
  base=$(basename "$f")
  stem=$(printf '%s' "$base" | tr '.' '_')
  probe "$stem.basic"    "$f" -show_format -show_streams -show_chapters -show_programs $groups
  probe "$stem.counts"   "$f" -show_format -show_streams -count_frames -count_packets
  probe "$stem.packets"  "$f" -show_packets
  probe "$stem.frames"   "$f" -show_frames
  probe "$stem.both"     "$f" -show_packets -show_frames
  probe "$stem.data"     "$f" -show_packets -show_data -show_data_hash md5 -read_intervals '%+#2'
  probe "$stem.entries"  "$f" -show_entries 'format=filename,duration:stream=index,codec_type,codec_name:stream_tags=language'
  probe "$stem.select"   "$f" -show_streams -select_streams v
  probe "$stem.optional" "$f" -show_format -show_streams -show_optional_fields always
done

# Drop outputs of options this version does not support.
find "$out" -name '*.json' -size 0 -delete
ls "$out" | wc -l | sed 's/^/files: /'
