#!/bin/sh
# test.sh — run the Go test suite inside every matrix container, so the live
# tests exercise each ffmpeg release line, not just the host's.
#
# The host Go toolchain is mounted read-only (it is statically linked, so it
# runs on Alpine as well) along with shared module and build caches under
# matrix/.cache, which keeps a run to roughly the time of the tests.
#
# usage: matrix/test.sh [image-dir ...]   (default: all)
#        GOTESTFLAGS="-run Live -v" matrix/test.sh debian-13
set -eu
cd "$(dirname "$0")/.."
repo=$(pwd)
goroot=$(go env GOROOT)
cache="$repo/matrix/.cache"
mkdir -p "$cache/mod" "$cache/build"

if [ $# -eq 0 ]; then
  set -- matrix/*/
fi

status=0
for dir in "$@"; do
  name=$(basename "${dir%/}")
  tag="muxmix-matrix:$name"
  echo "== $name"
  docker build -q -t "$tag" "matrix/$name" >/dev/null
  if docker run --rm --user "$(id -u):$(id -g)" \
      -v "$repo:/src" -w /src \
      -v "$goroot:/usr/local/go:ro" \
      -v "$cache/mod:/go/pkg/mod" -v "$cache/build:/go/cache" \
      -e HOME=/tmp -e GOPATH=/go -e GOCACHE=/go/cache -e GOMODCACHE=/go/pkg/mod \
      -e GOFLAGS=-mod=mod -e GOTOOLCHAIN=local -e CGO_ENABLED=0 -e PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin \
      "$tag" sh -c "ffmpeg -version | head -n1 && go test -count=1 ${GOTESTFLAGS:-} ./..."; then
    echo "== $name: ok"
  else
    echo "== $name: FAIL"
    status=1
  fi
done
exit $status
