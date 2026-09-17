# API usability review (2026-09-17)

Status: every item below is addressed except where noted; hardware decoding
remains the one deliberate deferral (see ffmpeg/hwaccel/README.md).

Question: is this the API an integrator would reach for? Three reviewers each wrote
5-7 realistic programs against the real API (all compiled, most run live against
ffmpeg 7.1) and reported friction. Pre-v0.1, so breaking changes are on the table.
Scratch programs: scratchpad/review-{ffmpeg,ffprobe,tasks}/ (session-local).

## Cross-cutting

- DONE. No `Example` functions anywhere. Release checklist item; also the best fix for most
  "had to look it up" friction below.
- DONE (RunnerOption/RunOption). `ffmpeg` has three functional-option types: `Opt` (command), `Option` (runner),
  `RunOption` (per-run). `Env` appends, `WithEnv` replaces; `Stderr` vs `WithStderr`.
  Proposal: keep `Opt`; rename `Option` -> `RunnerOption`; give runner-constructor
  options a `With` prefix and run-scoped ones none, consistently.
- DONE (Analyzer, TwoPassOptions.Runner). Nil-runner positional arg on `analyze.*` and `ffmpeg.TwoPass` calls
  (`analyze.Silence(ctx, nil, in, ...)`). Proposal: `analyze.Analyzer{Runner}` with
  methods plus package funcs bound to the default; `TwoPassOptions.Runner`.
- DONE (TotalDuration, plan.Run). Progress lacks fraction/ETA everywhere except TwoPass, which has its own `Duration`
  + `Fraction`. Proposal: `ffmpeg.TotalDuration(d) RunOption` filling
  `Progress.Fraction` and `Progress.ETA`; tasks plans inject it from `Info.Duration`.
- DONE (documented: -1, NaN for dB). "Unknown" sentinels vary: -1 ints, NaN floats, `Open`, invalid `Rat`. Pick one rule.
- DONE (ErrInvalidCommand, ErrNoResult, AVError). Untyped errors in several places (`Command.Validate`, analyze "no JSON block",
  ffprobe AVERROR codes unexported). Add sentinels so callers can `errors.Is/As`.

## HIGH

1. DONE (Tools.System + hwaccel.Policy/Selection). tasks: `TranscodeOptions.HW = VAAPI/QSV` builds a command that cannot run: no
   `-vaapi_device`, no `hwupload`, software `scale` feeding a hw encoder, `-pix_fmt
   yuv420p` forced. `hwaccel.Backend.InputArgs/Filter` exist but tasks never calls
   them. Works for NVENC only by luck. (tasks/transcode.go:359, packaging.go:197)
2. DONE (Auto removed; Policy resolves against System). hwaccel/encode: `hwaccel.Auto` silently resolves to software in `encode`
   (encode.go:104) and errors in `hwaccel.BuildInputArgs`. Nothing resolves it.
   Delete `Auto`, or have tasks resolve via `SystemSupport.Select` with the reason
   recorded in `PlannedStream.Reason`.
3. DONE (doc + CRF rounding). Scales deliberately stay each family's native one (CRF, VBR 1-10, JPEG 1-100), documented. encode: `Video.Quality` doc says "0 lossless" but 0 means encoder default
   (video.go:19, 101). Three quality scales disagree in direction and type: Video
   0-51 lower-better float64, Audio.VBR 1-10 higher-better int, Image 1-100
   higher-better int. Proposal: one `Quality int` 1..100 higher-better on all three,
   `Video.CRF float64` as the expert override. Round rendered CRF.
4. DONE. filtergraph: `Validate` rejects real stream specifiers. Regex is
   `^[0-9]+:[vaspd]$` (filtergraph.go:98), so `0:v:0`, `1:a:0`, `0:a?`,
   `0:m:language:eng` all fail "input label has no corresponding output". Every
   concat/overlay graph hits this.
5. DONE. ffprobe: missing ffprobe binary is reported as `*ExitError{ExitCode:-1}` AND
   `errors.Is(err, fs.ErrNotExist)` is true, so integrators classify it as a missing
   input file (probe.go:311). Return `ErrFFProbeNotFound` on start failure.
6. DONE (`ffprobe.Color`). ffprobe: `IsHDR()` is stream-only; HDR10 mastering/CLL SEI is frame-only on
   7.1 so it returns false on hdr.mp4 (sidedata.go:283). Add
   `Prober.Color(ctx, input) (*ColorInfo, error)` that falls back to first frame, or
   document loudly.

## MEDIUM

ffmpeg
- DONE. `FPS`/`Volume` render `%.2f` (23.976 -> 23.98); named filter args sort
  alphabetically (`scale=h=-2:w=1280`). Use FormatFloat(-1) and ordered args.
- DONE. Global-only opts (`FilterComplex`, `InitHWDevice`, `LogLevel`) compile inside
  `Output(...)` and pass `Validate`. Add a global-only/input-only table to Validate.
- DONE. Version parsing: git builds (`N-118000-g...`) parse to 0.0 so `AtLeast(4,4)` is
  false on master. Add `Snapshot bool`, `Patch`, `LibraryAtLeast(name, maj, min)`.
- DONE. Runner-injected `-progress pipe:3`/`-stats_period` land after the first input's
  options in `Result.Args` (ffmpeg.go:498). Prepend at index 0.
- DONE. filtergraph doc code blocks not indented (render as prose); README overlay
  example stale; `FilterChain.parent` set but never read.
- DONE. `Run` returns non-nil `*Result` with the error, undocumented; `run` accepts nil ctx.

ffprobe
- DONE. `Result.Format` is `*Format` but `Streams` is `[]Stream` and helpers return
  `[]*Stream`; nil deref when only `ShowStreams()` passed; `&res.Streams[i]` needed
  for pointer-receiver helpers. Generate `[]*Stream` and value `Format`.
- DONE. No tag fallback for Matroska bitrate/frame count (`BPS`, `NUMBER_OF_FRAMES`),
  though `DurationOf()` already does this for `DURATION`. Add `Bitrate()`,
  `FrameCount()`.
- DONE (also fixed a sign bug in Degrees). Two rotation APIs: `Rotation()` raw signed, `DisplayMatrix().Degrees()` normalised;
  neither reads legacy `rotate` tag. Collapse to one normalised `Rotation()`.
- DONE (`Secs` suffix on raw fields). `DurationOf()`/`StartOf()` naming reads like it takes an argument; rename the
  generated seconds fields so `Duration()`/`StartTime()` are free.
- DONE (`AVError`). AVERROR codes unexported; `Is` covers 5 of 7; ETIMEDOUT (-110) and EOF not
  matchable. Export `AVError` consts, map errno generically.
- DONE. Version tolerance is documented per field but not queryable: add cached
  `Prober.Version(ctx)` + `VersionInfo.AtLeast`; mention `Sections()` in doc.go.

tasks / encode / caps / hwaccel
- DONE (Tools.Run, plan.Run, res.Run). Progress cannot be attached per job: `RunOptions` lives on `Tools`. Add
  `Tools.Run(ctx, cmd, ...RunOption)` and `TranscodePlan.Run(...)`.
- DONE. Two capability detections, two caches: `caps.Detect` (10 sequential runs) and
  `hwaccel.DetectSystem` overlap; caps has no cache; hwaccel cache `DetectedAt`
  never consulted. Have hwaccel take an existing `*caps.Set`; add
  `caps.DetectCached`; parallelise listings; add a TTL.
- DONE (mergeVideo). `Rendition.Video` override replaces the base `encode.Video` instead of merging;
  `{Height:360, Video:&encode.Video{Profile:"main"}}` errors "needs a Codec".
- DONE (Skipped). Package silently upscales and reports wrong Width for rungs above source height.
- DONE. No per-stream filter hook (`VideoRule.Filters`, `AudioRule.Filters`); loudnorm
  needs a global `-af` via `Extra` that collides with per-stream `-filter:v:0`.
- DONE (ensureDir everywhere). Output-dir creation inconsistent: Trickplay/Package `MkdirAll`, Thumbnail/
  Preview/Waveform don't, and the failure is "exit 251 Input/output error".
- DONE. Two-pass not reachable from `Transcode`; add `TranscodeOptions.TwoPass bool`.
- DONE (documented). `caps.Check` with nil runner silently skips option-value checks (check.go:190).

## LOW

- DONE (aliases removed in ffmpeg, filtergraph, encode and ffprobe). Alias bloat: `EncoderOption`/`FormatOption` = `Set`; `KeyframeInterval`;
  filtergraph `Resize`/`SetFrameRate`/`SetPixelFormat`; `encode.PNG/WebP/GIF` vs
  `PNGImage/WebPImage/AnimatedGIF`; ffprobe `ErrUnsupportedFile`, `Prober.Exists`,
  `Result.Error`.
- DONE (KeepAll/MainOnly, NoFastStart doc, Speed.String; AllCodecs kept as documented sentinel). Polarity/naming: `VideoRule.All` vs `AudioRule.First`; `NoFastStart` doc describes
  `FastStart`; `AllCodecs = []Codec{"*"}` sentinel (prefer a `CopyPolicy` type);
  `tasks.Copy` (Action) vs `encode.Copy` (Codec); `Speed`/`Kind` lack `String()`.
- DONE. hwaccel exports many undocumented identifiers (`Detect`, `DetectSystem`, `Probe`,
  `Support`, `ParseDetection`, `SystemSortProbes`, ...); `baseffmpeg` alias leaks
  into godoc; `InputOpt` swallows errors while `EncodeOpt` returns them; unexport the
  `Build*Args` string builders.
- DONE. Package-level vs `Tools` surface uneven (`PlanTranscode`/`Inspect` only on Tools).
- DONE. Missing docs: `BufSize`, `Tune`, `Profile`, `Level`, `PassLogFile`, `No*`,
  `TimeSpec`, all `Concat*` types, `ParseConcat`; `OutputTSOffset` and `NewCommand`
  docs reference wrong names; ffprobe `MinNits` shares `MaxNits` comment.
- DONE. `Concat` needs `Version: "1.0"` by hand; add `NewConcat(files ...string)`.
- DONE. `FrameRate()` returns 90000 fps for attached pictures.
- DONE. `Packet.IsKeyframe`/`IsDiscard` positional on the flags string; use ContainsRune.
- DONE (value type). `Disposition` is a pointer with only three accessors; make it a value or generate
  `Has(flag)`.
- DONE (`WithTimeout` + wrapped error). ffprobe `Timeout(d)` duplicates context and loses the "ffprobe:" prefix.
- DONE (documented). `SilenceOptions{NoiseDB:0}` means default; 0 dB is legal. Document or use pointer.

## Keep

- `Command` model: correct global/input/output arg order, `-map` order preserved,
  shell-quoted `String()`, `Raw` escape hatch, `PerStream`.
- Progress transport over `pipe:3` with stderr fallback; cancel -> SIGINT ->
  trailer, `Done=true`, `errors.Is(err, context.Canceled)`.
- `*ffmpeg.Error` carrying `Result`, `LogLines`, `LastLogLine`.
- ffprobe lenient value types with `Valid()`; `iter.Seq2` packet/frame streaming with
  kill-on-break; `ProbeError.Is` onto `fs.ErrNotExist`; generated "Added in / Dropped
  after FFmpeg X" comments; `SideDataAs[T]`.
- `TranscodePlan.String()` and `PlannedStream.Reason`: the plan explains itself.
- Plan/Build split with `Command` exposed; dropping to `ffmpeg.TwoPass` is clean.
- `caps.CheckError` listing every problem with "available:" suggestions.
- `encode.Video.OptsFor(encoder)` as a pure function; README per-encoder table
  matches output.
- Config structs with sane zero values throughout tasks/encode/hwaccel.
