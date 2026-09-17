package filtergraph_test

import (
	"fmt"

	"github.com/ophymx/muxmix/ffmpeg/filtergraph"
)

func ExampleFilterGraph() {
	g := filtergraph.NewFilterGraph().WithSwsFlags("lanczos")
	g.NewChain().Input("0:v").Scale(1280, -2).FPS(23.976).Format("yuv420p").Output("v")
	g.NewChain().Input("0:a").Volume(0.8).Output("a")
	fmt.Println(g)
	// Output: sws_flags=lanczos;[0:v]scale=w=1280:h=-2,fps=23.976,format=yuv420p[v];[0:a]volume=0.8[a]
}

func ExampleFilterChain_Overlay() {
	g := filtergraph.NewFilterGraph()
	g.NewChain().Input("1:v").Scale(320, 240).Output("pip")
	g.NewChain().Input("0:v", "pip").Overlay("W-w-10", "H-h-10").Output("out")
	fmt.Println(g.Validate(), g)
	// Output: <nil> [1:v]scale=w=320:h=240[pip];[0:v][pip]overlay=x=W-w-10:y=H-h-10[out]
}

func ExampleNewFilter() {
	f := filtergraph.NewFilter("unsharp").WithArg("lx", "5").WithArg("la", "1.2")
	fmt.Println(f)
	// Output: unsharp=lx=5:la=1.2
}
