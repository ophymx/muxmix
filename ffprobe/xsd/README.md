# ffprobe XSD copies

The files in this directory are unmodified copies of `doc/ffprobe.xsd` from
the FFmpeg source tree, one per release tag listed in `../ffprobe.versions`.
Each was fetched from

    https://raw.githubusercontent.com/FFmpeg/FFmpeg/refs/tags/<tag>/doc/ffprobe.xsd

FFmpeg is licensed under the GNU Lesser General Public License version 2.1 or
later; these copies are redistributed under that license, not under the MIT
license that covers the rest of this repository. See
https://ffmpeg.org/legal.html and the `COPYING.LGPLv2.1` file in the FFmpeg
source tree.

They are read by `../internal/gen` to produce the Go types in
`../types.gen.go`; they are not compiled into the package.
