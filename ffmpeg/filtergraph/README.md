# filtergraph

Build and validate ffmpeg filtergraph strings.

```go
g := filtergraph.NewFilterGraph().WithSwsFlags("lanczos")
g.NewChain().Input("0:v").Scale(1280, -2).FPS(23.976).Format("yuv420p").Output("v")
g.NewChain().Input("0:a").Volume(0.8).Output("a")
fmt.Println(g)
// sws_flags=lanczos;[0:v]scale=w=1280:h=-2,fps=23.976,format=yuv420p[v];[0:a]volume=0.8[a]

cmd.GlobalOptions(ffmpeg.FilterComplex(g))
```

A graph is chains separated by `;`; a chain is filters separated by `,`;
`[labels]` link chains. `Input` labels that start with a digit are ffmpeg
stream specifiers (`0:v`, `1:a:0`, `0:a?`, `0:m:language:eng`) and need no
matching output.

## Builders

`Scale`, `ScaleExpression`, `Crop`, `CropExpression`, `FPS`, `Format`,
`Fade`, `Volume`, `Overlay`, `Split` and `DrawText` render the way people
write them by hand: named arguments in the order given
(`crop=w=100:h=100:x=10:y=10`), numbers printed exactly (`fps=23.976`,
`volume=0.8`).

Anything else is a `Filter`:

```go
f := filtergraph.NewFilter("unsharp").WithArg("lx", "5").WithArg("la", "1.2")
g.NewChain().Add(f)                       // unsharp=lx=5:la=1.2
filtergraph.NewFilter("format").WithPositionalArgs("yuv420p", "yuv444p")
filtergraph.NewFilter("scale").WithInstance("main")  // scale@main=...
```

`WithArg` keeps the order arguments were added; `WithNamedArgs(map)` sorts
its keys. Values containing `,`, `:`, `[`, `]`, `=`, `;`, `'`, `\` or
whitespace are quoted and escaped, so the filter receives them byte for
byte — quoting alone carries the graph separators, while `:`, `=`, `\`, `'`
and whitespace need the backslash as well. `=` matters most to a positional
argument, which ffmpeg reads as an option name the moment it finds one:
unescaped, `movie=/tmp/a=b.srt` looks like the option `/tmp/a` and the file
is never opened.

Keys are the exception. ffmpeg reads an option name with no escaping of its
own — quoting one does not survive the graph parser — so a key holding any
of those characters cannot be delivered however it is written, and
`Validate` rejects it rather than render something ffmpeg would mis-read.
Real option names are `[a-zA-Z0-9_]`, so this only ever catches a mistake.

## Overlay

```go
g := filtergraph.NewFilterGraph()
g.NewChain().Input("1:v").Scale(320, 240).Output("pip")
g.NewChain().Input("0:v", "pip").Overlay("W-w-10", "H-h-10").Output("out")
// [1:v]scale=w=320:h=240[pip];[0:v][pip]overlay=x=W-w-10:y=H-h-10[out]
```

## Validation and JSON

`Validate` checks filter and label names, argument keys and argument order,
that every non-specifier input label has a producing output, and that output
labels are unique. Graphs marshal to and from JSON with argument order
preserved: named arguments alone are an object, positional arguments alone
an array of strings, and anything else — a mix, or a key used twice — an
array in argument order with each named argument as a one-key object.
`filtergraph.schema.json` describes that wire format, and `schema_test.go`
holds it to what the encoder writes.
