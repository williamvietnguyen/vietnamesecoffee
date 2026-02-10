package main

import "os"

// ============================================================
// Section 15: Main
// ============================================================

func main() {
	initAttacks()
	initZobrist()
	initTT(DefaultTTSize)

	for _, arg := range os.Args[1:] {
		if arg == "--bench" || arg == "--perft" {
			runPerftSuite()
			return
		}
		if arg == "--lichess" {
			lichessMain()
			return
		}
	}

	uciLoop()
}
