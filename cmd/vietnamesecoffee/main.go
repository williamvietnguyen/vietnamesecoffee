package main

import (
	"os"

	"vietnamesecoffee/internal/engine"
	"vietnamesecoffee/internal/lichess"
	"vietnamesecoffee/internal/uci"
)

func main() {
	engine.InitAttacks()
	engine.InitZobrist()
	engine.InitTT(engine.DefaultTTSize)

	for _, arg := range os.Args[1:] {
		if arg == "--bench" || arg == "--perft" {
			engine.RunPerftSuite()
			return
		}
		if arg == "--lichess" {
			lichess.Run()
			return
		}
	}

	uci.Run()
}
