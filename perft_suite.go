package main

import (
	"fmt"
	"time"
)

// ============================================================
// Section 14: Perft Suite
// ============================================================

type perftTest struct {
	name    string
	fen     string
	results map[int]uint64
}

func runPerftSuite() {
	tests := []perftTest{
		{
			name: "Startpos",
			fen:  "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
			results: map[int]uint64{
				1: 20,
				2: 400,
				3: 8902,
				4: 197281,
				5: 4865609,
			},
		},
		{
			name: "Kiwipete",
			fen:  "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
			results: map[int]uint64{
				1: 48,
				2: 2039,
				3: 97862,
				4: 4085603,
				5: 193690690,
			},
		},
		{
			name: "Position 3",
			fen:  "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			results: map[int]uint64{
				1: 14,
				2: 191,
				3: 2812,
				4: 43238,
				5: 674624,
				6: 11030083,
			},
		},
		{
			name: "Position 4",
			fen:  "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
			results: map[int]uint64{
				1: 6,
				2: 264,
				3: 9467,
				4: 422333,
				5: 15833292,
			},
		},
		{
			name: "Position 5",
			fen:  "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 0 1",
			results: map[int]uint64{
				1: 44,
				2: 1486,
				3: 62379,
				4: 2103487,
				5: 89941194,
			},
		},
		{
			name: "Position 6",
			fen:  "r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 1",
			results: map[int]uint64{
				1: 46,
				2: 2079,
				3: 89890,
				4: 3894594,
				5: 164075551,
			},
		},
	}

	allPassed := true
	for _, test := range tests {
		fmt.Printf("=== %s ===\n", test.name)
		pos := parseFEN(test.fen)

		passed := true
		for depth := 1; depth <= 5; depth++ {
			expected, ok := test.results[depth]
			if !ok {
				continue
			}

			start := time.Now()
			result := perft(&pos, depth)
			elapsed := time.Since(start)

			status := "PASS"
			if result != expected {
				status = "FAIL"
				passed = false
				allPassed = false
			}

			nps := uint64(0)
			if elapsed.Seconds() > 0 {
				nps = uint64(float64(result) / elapsed.Seconds())
			}
			fmt.Printf("  depth %d: got %d, expected %d [%s] (%v, %d nps)\n",
				depth, result, expected, status, elapsed, nps)

			if result != expected {
				break
			}
		}

		if expected, ok := test.results[6]; ok {
			start := time.Now()
			result := perft(&pos, 6)
			elapsed := time.Since(start)

			status := "PASS"
			if result != expected {
				status = "FAIL"
				passed = false
				allPassed = false
			}
			fmt.Printf("  depth 6: got %d, expected %d [%s] (%v)\n",
				result, expected, status, elapsed)
		}

		if passed {
			fmt.Printf("  Result: PASS\n\n")
		} else {
			fmt.Printf("  Result: FAIL\n\n")
		}
	}

	if allPassed {
		fmt.Println("ALL TESTS PASSED!")
	} else {
		fmt.Println("SOME TESTS FAILED!")
	}
}
