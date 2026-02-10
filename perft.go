package main

import "fmt"

// ============================================================
// Section 11: Perft & Divide
// ============================================================

func perft(pos *Position, depth int) uint64 {
	if depth == 0 {
		return 1
	}

	moves := pos.generateLegalMoves()

	if depth == 1 {
		return uint64(len(moves))
	}

	var nodes uint64
	for _, m := range moves {
		newPos := pos.makeMove(m)
		nodes += perft(&newPos, depth-1)
	}
	return nodes
}

func divide(pos *Position, depth int) uint64 {
	moves := pos.generateLegalMoves()
	var total uint64
	for _, m := range moves {
		newPos := pos.makeMove(m)
		var count uint64
		if depth-1 == 0 {
			count = 1
		} else {
			count = perft(&newPos, depth-1)
		}
		total += count
		fmt.Printf("%s: %d\n", moveToString(m), count)
	}
	fmt.Printf("\nTotal: %d\n", total)
	return total
}

func moveToString(m Move) string {
	from := m.From()
	to := m.To()
	s := fmt.Sprintf("%c%c%c%c",
		'a'+rune(sqFile(from)), '1'+rune(sqRank(from)),
		'a'+rune(sqFile(to)), '1'+rune(sqRank(to)))
	if m.IsPromotion() {
		promoChars := "nbrq"
		s += string(promoChars[m.Flags()&0x3])
	}
	return s
}
