#!/bin/sh
# run.sh — build every image under matrix/ and run capture.sh inside each,
# writing fixtures into ffprobe/testdata/probe/<ffprobe-version>/.
#
# usage: matrix/run.sh [image-dir ...]   (default: all)
set -eu
cd "$(dirname "$0")/.."
repo=$(pwd)

if [ $# -eq 0 ]; then
  set -- matrix/*/
fi

for dir in "$@"; do
  name=$(basename "${dir%/}")
  tag="muxmix-matrix:$name"
  echo "== $name"
  docker build -q -t "$tag" "matrix/$name" >/dev/null
  docker run --rm --user "$(id -u):$(id -g)" \
    -v "$repo:/src" -w /src "$tag" \
    sh matrix/capture.sh ffprobe/testdata/media ffprobe/testdata/probe
done
