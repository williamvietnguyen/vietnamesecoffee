package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ============================================================
// Section 12: UCI Helpers
// ============================================================

// parseUCIMove converts a UCI move string (e.g. "e2e4", "e7e8q") into a Move
// by matching against the legal moves in the current position.
func parseUCIMove(pos *Position, uci string) (Move, bool) {
	if len(uci) < 4 {
		return 0, false
	}
	fromFile := int(uci[0] - 'a')
	fromRank := int(uci[1] - '1')
	toFile := int(uci[2] - 'a')
	toRank := int(uci[3] - '1')
	from := fromRank*8 + fromFile
	to := toRank*8 + toFile

	var promoChar byte
	if len(uci) == 5 {
		promoChar = uci[4]
	}

	moves := pos.generateLegalMoves()
	for _, m := range moves {
		if m.From() != from || m.To() != to {
			continue
		}
		if m.IsPromotion() {
			pc := "nbrq"[m.Flags()&0x3]
			if promoChar != pc {
				continue
			}
		}
		return m, true
	}
	return 0, false
}

// printBoard prints an ASCII representation of the board (for the "d" debug command).
func printBoard(pos *Position) {
	pieceChars := [2][6]byte{
		{'P', 'N', 'B', 'R', 'Q', 'K'},
		{'p', 'n', 'b', 'r', 'q', 'k'},
	}
	fmt.Println()
	for rank := 7; rank >= 0; rank-- {
		fmt.Printf("  %d ", rank+1)
		for file := 0; file < 8; file++ {
			sq := rank*8 + file
			c, pc, found := pos.pieceAt(sq)
			if found {
				fmt.Printf(" %c", pieceChars[c][pc])
			} else {
				fmt.Print(" .")
			}
		}
		fmt.Println()
	}
	fmt.Println("     a b c d e f g h")
	fmt.Println()
	if pos.SideToMove == White {
		fmt.Println("  Side: White")
	} else {
		fmt.Println("  Side: Black")
	}
	castling := ""
	if pos.CastlingRights&WhiteKingSide != 0 {
		castling += "K"
	}
	if pos.CastlingRights&WhiteQueenSide != 0 {
		castling += "Q"
	}
	if pos.CastlingRights&BlackKingSide != 0 {
		castling += "k"
	}
	if pos.CastlingRights&BlackQueenSide != 0 {
		castling += "q"
	}
	if castling == "" {
		castling = "-"
	}
	fmt.Printf("  Castling: %s\n", castling)
	if pos.EnPassant != NoSquare {
		fmt.Printf("  En passant: %c%c\n", 'a'+rune(sqFile(pos.EnPassant)), '1'+rune(sqRank(pos.EnPassant)))
	} else {
		fmt.Println("  En passant: -")
	}
	fmt.Println()
}

// ============================================================
// Section 13: UCI Loop
// ============================================================

func uciLoop() {
	pos := parseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var gameHistory []uint64
	var currentInfo *SearchInfo
	var searchDone chan struct{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		tokens := strings.Fields(line)
		cmd := tokens[0]

		switch cmd {
		case "uci":
			fmt.Println("id name VietCoffee")
			fmt.Println("id author William Nguyen")
			fmt.Println("uciok")

		case "isready":
			fmt.Println("readyok")

		case "ucinewgame":
			pos = parseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
			gameHistory = nil
			initTT(DefaultTTSize)

		case "position":
			if len(tokens) < 2 {
				continue
			}
			movesIdx := -1
			if tokens[1] == "startpos" {
				pos = parseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
				// Find "moves" token
				for i := 2; i < len(tokens); i++ {
					if tokens[i] == "moves" {
						movesIdx = i + 1
						break
					}
				}
			} else if tokens[1] == "fen" {
				// Collect FEN parts (up to 6 fields, stop at "moves")
				fenParts := []string{}
				idx := 2
				for idx < len(tokens) && tokens[idx] != "moves" {
					fenParts = append(fenParts, tokens[idx])
					idx++
				}
				if len(fenParts) > 0 {
					pos = parseFEN(strings.Join(fenParts, " "))
				}
				if idx < len(tokens) && tokens[idx] == "moves" {
					movesIdx = idx + 1
				}
			}
			// Build game history and apply moves
			gameHistory = []uint64{pos.Hash}
			if movesIdx >= 0 {
				for i := movesIdx; i < len(tokens); i++ {
					m, ok := parseUCIMove(&pos, tokens[i])
					if ok {
						pos = pos.makeMove(m)
						gameHistory = append(gameHistory, pos.Hash)
					}
				}
			}

		case "go":
			if len(tokens) >= 3 && tokens[1] == "perft" {
				depth, err := strconv.Atoi(tokens[2])
				if err == nil && depth > 0 {
					divide(&pos, depth)
				}
			} else {
				fixedDepth := 0
				timeLeft := 0
				inc := 0
				movesToGo := 0
				moveTime := 0
				isInfinite := false

				for i := 1; i < len(tokens); i++ {
					switch tokens[i] {
					case "depth":
						if i+1 < len(tokens) {
							d, err := strconv.Atoi(tokens[i+1])
							if err == nil {
								fixedDepth = d
							}
							i++
						}
					case "movetime":
						if i+1 < len(tokens) {
							mt, err := strconv.Atoi(tokens[i+1])
							if err == nil {
								moveTime = mt
							}
							i++
						}
					case "wtime":
						if i+1 < len(tokens) {
							wt, err := strconv.Atoi(tokens[i+1])
							if err == nil && pos.SideToMove == White {
								timeLeft = wt
							}
							i++
						}
					case "btime":
						if i+1 < len(tokens) {
							bt, err := strconv.Atoi(tokens[i+1])
							if err == nil && pos.SideToMove == Black {
								timeLeft = bt
							}
							i++
						}
					case "winc":
						if i+1 < len(tokens) {
							wi, err := strconv.Atoi(tokens[i+1])
							if err == nil && pos.SideToMove == White {
								inc = wi
							}
							i++
						}
					case "binc":
						if i+1 < len(tokens) {
							bi, err := strconv.Atoi(tokens[i+1])
							if err == nil && pos.SideToMove == Black {
								inc = bi
							}
							i++
						}
					case "movestogo":
						if i+1 < len(tokens) {
							mtg, err := strconv.Atoi(tokens[i+1])
							if err == nil {
								movesToGo = mtg
							}
							i++
						}
					case "infinite":
						isInfinite = true
					}
				}

				// Compute allocated time
				var allocatedTime int
				if fixedDepth > 0 || isInfinite {
					allocatedTime = 1 << 30
				} else if moveTime > 0 {
					allocatedTime = moveTime
				} else if timeLeft > 0 {
					// Estimate moves left
					moveNumber := pos.FullMoveNumber
					movesLeft := 40 - moveNumber
					if movesLeft < 20 {
						movesLeft = 20
					}
					if movesToGo > 0 && movesToGo < movesLeft {
						movesLeft = movesToGo
					}
					allocatedTime = timeLeft/movesLeft + inc*70/100
					// Hard cap: 30% of remaining time
					hardCap := timeLeft * 30 / 100
					if allocatedTime > hardCap {
						allocatedTime = hardCap
					}
					// Safety buffer: never exceed timeLeft - 50ms
					safeMax := timeLeft - 50
					if safeMax < 1 {
						safeMax = 1
					}
					if allocatedTime > safeMax {
						allocatedTime = safeMax
					}
					if allocatedTime < 1 {
						allocatedTime = 1
					}
				} else {
					allocatedTime = 5000 // fallback
				}

				info := &SearchInfo{}
				info.stopTime = time.Now().Add(time.Duration(allocatedTime) * time.Millisecond)
				// Copy game history (all positions before the current one) for repetition detection
				if len(gameHistory) > 1 {
					info.history = make([]uint64, len(gameHistory)-1)
					copy(info.history, gameHistory[:len(gameHistory)-1])
				}
				currentInfo = info
				searchDone = make(chan struct{})
				searchPos := pos
				go func() {
					bestMove := search(&searchPos, fixedDepth, info)
					if bestMove == 0 {
						fmt.Println("bestmove 0000")
					} else {
						fmt.Printf("bestmove %s\n", moveToString(bestMove))
					}
					close(searchDone)
				}()
			}

		case "stop":
			if currentInfo != nil {
				currentInfo.setStop()
				if searchDone != nil {
					<-searchDone
				}
				currentInfo = nil
			}

		case "d":
			printBoard(&pos)

		case "quit":
			if currentInfo != nil {
				currentInfo.setStop()
				if searchDone != nil {
					<-searchDone
				}
			}
			return
		}
	}
	// stdin closed: wait for any running search to finish
	if searchDone != nil {
		<-searchDone
	}
}
