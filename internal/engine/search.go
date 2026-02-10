package engine

import (
	"fmt"
	"sync/atomic"
	"time"
)

// ============================================================
// Section 10: Search
// ============================================================

const SearchInfinity = 30000
const MateScore = 29000
const MaxPly = 128

// Contempt factor: treats draws as slightly losing, forcing the engine to play
// for a win rather than settle for repetitions or simplifications. A value of
// 40cp means the engine would rather be 40cp worse than accept a draw.
const Contempt = 80

type SearchInfo struct {
	Nodes    uint64
	StopTime time.Time
	stopped  int32
	BestMove Move
	History  []uint64
	Killers  [MaxPly][2]Move // killer moves: quiet moves that caused beta cutoffs
}

func (info *SearchInfo) IsStopped() bool {
	return atomic.LoadInt32(&info.stopped) != 0
}

func (info *SearchInfo) SetStop() {
	atomic.StoreInt32(&info.stopped, 1)
}

// Transposition table
const (
	TTNone  uint8 = 0
	TTExact uint8 = 1
	TTAlpha uint8 = 2 // upper bound (failed low)
	TTBeta  uint8 = 3 // lower bound (failed high)
)

type TTEntry struct {
	Key   uint64
	Move  Move
	Score int16
	Depth int8
	Flag  uint8
}

const DefaultTTSize = 256 // MB

var TT []TTEntry
var TTMask uint64

func InitTT(sizeMB int) {
	entries := (sizeMB * 1024 * 1024) / 16
	size := uint64(1)
	for size*2 <= uint64(entries) {
		size *= 2
	}
	TT = make([]TTEntry, size)
	TTMask = size - 1
}

func ProbeTT(key uint64) (TTEntry, bool) {
	entry := TT[key&TTMask]
	if entry.Key == key {
		return entry, true
	}
	return entry, false
}

func StoreTT(key uint64, move Move, score int16, depth int8, flag uint8) {
	TT[key&TTMask] = TTEntry{Key: key, Move: move, Score: score, Depth: depth, Flag: flag}
}

// ScoreMove assigns a sort score for move ordering (higher = search first).
func ScoreMove(pos *Position, m Move, ttMove Move, killers [2]Move) int {
	if m == ttMove && ttMove != 0 {
		return 30000
	}
	score := 0
	if m.IsCapture() {
		var victimVal int
		if m.IsEP() {
			victimVal = PieceValue[Pawn]
		} else {
			_, vic, _ := pos.PieceAt(m.To())
			victimVal = PieceValue[vic]
		}
		_, attacker, _ := pos.PieceAt(m.From())
		attackerVal := PieceValue[attacker]
		score += 10000 + victimVal*10 - attackerVal
	}
	if m.IsPromotion() {
		score += 9000 + PieceValue[m.PromoPiece()]
	}
	// Bonus for checking moves — explore forcing lines first (aggressive/combinational style)
	if score < 10000 { // don't bother for moves already scored high (TT, good captures)
		newPos := pos.MakeMove(m)
		if newPos.InCheck(newPos.SideToMove) {
			score += 5000
		}
	}
	// Killer move bonus (quiet moves that caused cutoffs at this ply)
	if score == 0 {
		if m == killers[0] {
			score = 4000
		} else if m == killers[1] {
			score = 3900
		}
	}
	return score
}

// PickMove does incremental selection sort: swaps the best-scored move into position i.
func PickMove(moves []Move, scores []int, i int) {
	best := i
	for j := i + 1; j < len(moves); j++ {
		if scores[j] > scores[best] {
			best = j
		}
	}
	if best != i {
		moves[i], moves[best] = moves[best], moves[i]
		scores[i], scores[best] = scores[best], scores[i]
	}
}

// Quiescence searches captures and promotions to avoid the horizon effect.
func Quiescence(pos *Position, alpha, beta int, info *SearchInfo) int {
	info.Nodes++

	eval := Evaluate(pos)
	if eval >= beta {
		return beta
	}
	if eval > alpha {
		alpha = eval
	}

	moves := pos.GenerateLegalMoves()
	// Filter to captures and promotions only
	tactical := moves[:0]
	for _, m := range moves {
		if m.IsCapture() || m.IsPromotion() {
			tactical = append(tactical, m)
		}
	}

	scores := make([]int, len(tactical))
	for i, m := range tactical {
		scores[i] = ScoreMove(pos, m, 0, [2]Move{})
	}

	for i := 0; i < len(tactical); i++ {
		if info.IsStopped() {
			return 0
		}
		PickMove(tactical, scores, i)
		newPos := pos.MakeMove(tactical[i])
		score := -Quiescence(&newPos, -beta, -alpha, info)
		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}
	return alpha
}

// Negamax is the core alpha-beta search.
func Negamax(pos *Position, depth, ply, alpha, beta int, info *SearchInfo) int {
	if info.Nodes&2047 == 0 && info.Nodes > 0 {
		if time.Now().After(info.StopTime) {
			info.SetStop()
			return 0
		}
	}

	// Repetition detection (contempt: treat draws as losing)
	if ply > 0 {
		for _, h := range info.History {
			if h == pos.Hash {
				return -Contempt
			}
		}
	}

	// Fifty-move rule (contempt: treat draws as losing)
	if ply > 0 && pos.HalfMoveClock >= 100 {
		return -Contempt
	}

	if depth <= 0 {
		return Quiescence(pos, alpha, beta, info)
	}

	info.Nodes++
	origAlpha := alpha

	// TT probe
	var ttMove Move
	entry, hit := ProbeTT(pos.Hash)
	if hit {
		ttMove = entry.Move
		if int(entry.Depth) >= depth {
			ttScore := int(entry.Score)
			// Adjust mate scores from distance-from-root to distance-from-node
			if ttScore > MateScore-100 {
				ttScore -= ply
			} else if ttScore < -MateScore+100 {
				ttScore += ply
			}
			switch entry.Flag {
			case TTExact:
				if ply == 0 {
					info.BestMove = entry.Move
				}
				return ttScore
			case TTAlpha:
				if ttScore <= alpha {
					return alpha
				}
			case TTBeta:
				if ttScore >= beta {
					return ttScore
				}
			}
		}
	}

	// Check extension
	inCheck := pos.InCheck(pos.SideToMove)
	if inCheck {
		depth++
	}

	// Null move pruning (disabled when we have 3+ attackers on enemy king —
	// NMP would prune away sacrifice lines in attacking positions)
	if ply > 0 && depth >= 3 && !inCheck {
		us := pos.SideToMove
		hasNonPawnMaterial := pos.Pieces[us][Knight]|pos.Pieces[us][Bishop]|
			pos.Pieces[us][Rook]|pos.Pieces[us][Queen] != 0
		if hasNonPawnMaterial {
			// Count attackers on enemy king to decide if we're in an attack
			them := us ^ 1
			skipNMP := false
			enemyKingBB := pos.Pieces[them][King]
			if enemyKingBB != 0 {
				ek := Lsb(enemyKingBB)
				kz := KingAttacks[ek] | (1 << uint(ek))
				nmpAttackers := 0
				for pc := Knight; pc <= Queen; pc++ {
					bb := pos.Pieces[us][pc]
					for bb != 0 {
						sq := PopLSB(&bb)
						var att uint64
						switch pc {
						case Knight:
							att = KnightAttacks[sq]
						case Bishop:
							att = BishopAttacks(sq, pos.AllOccupied)
						case Rook:
							att = RookAttacks(sq, pos.AllOccupied)
						case Queen:
							att = QueenAttacks(sq, pos.AllOccupied)
						}
						if att&kz != 0 {
							nmpAttackers++
						}
					}
				}
				if nmpAttackers >= 3 {
					skipNMP = true // don't prune — we're attacking!
				}
			}

			if !skipNMP {
				// Make null move: flip side, clear EP, update hash
				nullPos := *pos
				nullPos.SideToMove ^= 1
				nullPos.Hash ^= ZobristSide
				if nullPos.EnPassant != NoSquare {
					nullPos.Hash ^= ZobristEP[SqFile(nullPos.EnPassant)]
					nullPos.EnPassant = NoSquare
				}
				score := -Negamax(&nullPos, depth-1-2, ply+1, -beta, -beta+1, info)
				if info.IsStopped() {
					return 0
				}
				if score >= beta {
					return beta
				}
			}
		}
	}

	moves := pos.GenerateLegalMoves()
	if len(moves) == 0 {
		if inCheck {
			return -MateScore + ply // checkmate
		}
		return 0 // stalemate
	}

	var plyKillers [2]Move
	if ply < MaxPly {
		plyKillers = info.Killers[ply]
	}
	scores := make([]int, len(moves))
	for i, m := range moves {
		scores[i] = ScoreMove(pos, m, ttMove, plyKillers)
	}

	bestScore := -SearchInfinity
	var bestMoveAtNode Move
	for i := 0; i < len(moves); i++ {
		if info.IsStopped() {
			return 0
		}
		PickMove(moves, scores, i)
		m := moves[i]
		newPos := pos.MakeMove(m)

		// Sacrifice extension: if we gave up material (moving piece worth more
		// than captured piece) and we have 2+ attackers on the enemy king, extend
		// search by 1 ply so the engine can see through the sacrifice.
		ext := 0
		if m.IsCapture() {
			us := pos.SideToMove
			// Find moving piece
			fromBit := uint64(1) << uint(m.From())
			movingPc := Pawn
			for pc := 0; pc < 6; pc++ {
				if pos.Pieces[us][pc]&fromBit != 0 {
					movingPc = pc
					break
				}
			}
			// Find captured piece
			them := us ^ 1
			toBit := uint64(1) << uint(m.To())
			capturedPc := Pawn
			if m.IsEP() {
				capturedPc = Pawn
			} else {
				for pc := 0; pc < 6; pc++ {
					if pos.Pieces[them][pc]&toBit != 0 {
						capturedPc = pc
						break
					}
				}
			}
			// Is this a sacrifice? (giving up more valuable piece)
			if PieceValue[movingPc] > PieceValue[capturedPc]+50 {
				// Count attackers near enemy king in the new position
				enemyKingBB := newPos.Pieces[them][King]
				if enemyKingBB != 0 {
					enemyKingSq := Lsb(enemyKingBB)
					kingZone := KingAttacks[enemyKingSq] | (1 << uint(enemyKingSq))
					attackers := 0
					for pc := Knight; pc <= Queen; pc++ {
						bb := newPos.Pieces[us][pc]
						for bb != 0 {
							sq := PopLSB(&bb)
							var att uint64
							switch pc {
							case Knight:
								att = KnightAttacks[sq]
							case Bishop:
								att = BishopAttacks(sq, newPos.AllOccupied)
							case Rook:
								att = RookAttacks(sq, newPos.AllOccupied)
							case Queen:
								att = QueenAttacks(sq, newPos.AllOccupied)
							}
							if att&kingZone != 0 {
								attackers++
							}
						}
					}
					if attackers >= 2 {
						ext = 1
					}
				}
			}
		}

		info.History = append(info.History, pos.Hash)

		var score int
		if i == 0 {
			// PV move: full depth, full window
			score = -Negamax(&newPos, depth-1+ext, ply+1, -beta, -alpha, info)
		} else {
			// LMR: reduce depth for late quiet moves
			reduction := 0
			if i >= 3 && depth >= 3 && !inCheck && !m.IsCapture() && !m.IsPromotion() &&
				m != plyKillers[0] && m != plyKillers[1] && m != ttMove {
				reduction = 1
				if i >= 6 {
					reduction = 2
				}
			}

			// Search with null window (PVS) and possible reduction (LMR)
			score = -Negamax(&newPos, depth-1-reduction+ext, ply+1, -alpha-1, -alpha, info)

			// If reduced search found something interesting, re-search at full depth
			if reduction > 0 && score > alpha {
				score = -Negamax(&newPos, depth-1+ext, ply+1, -alpha-1, -alpha, info)
			}

			// If null window failed high, re-search with full window
			if score > alpha && score < beta {
				score = -Negamax(&newPos, depth-1+ext, ply+1, -beta, -alpha, info)
			}
		}

		info.History = info.History[:len(info.History)-1]
		if score > bestScore {
			bestScore = score
			bestMoveAtNode = moves[i]
			if score > alpha {
				alpha = score
				if ply == 0 {
					info.BestMove = moves[i]
				}
			}
		}
		if score >= beta {
			// Store killer move (quiet moves only — captures already ordered by MVV-LVA)
			if !m.IsCapture() && !m.IsPromotion() && ply < MaxPly {
				if m != info.Killers[ply][0] {
					info.Killers[ply][1] = info.Killers[ply][0]
					info.Killers[ply][0] = m
				}
			}
			break
		}
	}

	// Store in TT
	if !info.IsStopped() {
		var flag uint8
		if bestScore <= origAlpha {
			flag = TTAlpha
		} else if bestScore >= beta {
			flag = TTBeta
		} else {
			flag = TTExact
		}
		// Adjust mate scores for storage (distance-from-root)
		storeScore := int16(bestScore)
		if bestScore > MateScore-100 {
			storeScore = int16(bestScore + ply)
		} else if bestScore < -MateScore+100 {
			storeScore = int16(bestScore - ply)
		}
		StoreTT(pos.Hash, bestMoveAtNode, storeScore, int8(depth), flag)
	}

	return bestScore
}

// Search runs iterative deepening. The caller sets up info (StopTime, History).
// Returns the best move found.
func Search(pos *Position, maxDepth int, info *SearchInfo) Move {
	if maxDepth <= 0 {
		maxDepth = 64
	}

	var bestMove Move
	var prevScore int
	aspirationWindow := 50

	for depth := 1; depth <= maxDepth; depth++ {
		info.Nodes = 0
		startTime := time.Now()

		var score int
		if depth <= 4 {
			score = Negamax(pos, depth, 0, -SearchInfinity, SearchInfinity, info)
		} else {
			alpha := prevScore - aspirationWindow
			beta := prevScore + aspirationWindow
			score = Negamax(pos, depth, 0, alpha, beta, info)
			// If score falls outside the window, re-search with full window
			if score <= alpha || score >= beta {
				score = Negamax(pos, depth, 0, -SearchInfinity, SearchInfinity, info)
			}
		}

		if info.IsStopped() {
			break
		}
		prevScore = score
		bestMove = info.BestMove
		elapsed := time.Since(startTime)
		elapsedMs := elapsed.Milliseconds()
		if elapsedMs == 0 {
			elapsedMs = 1
		}
		nps := info.Nodes * 1000 / uint64(elapsedMs)

		// Print UCI info line
		scoreStr := fmt.Sprintf("cp %d", score)
		if score > MateScore-100 {
			mateIn := (MateScore - score + 1) / 2
			scoreStr = fmt.Sprintf("mate %d", mateIn)
		} else if score < -MateScore+100 {
			mateIn := -(MateScore + score + 1) / 2
			scoreStr = fmt.Sprintf("mate %d", mateIn)
		}
		fmt.Printf("info depth %d score %s nodes %d nps %d time %d\n",
			depth, scoreStr, info.Nodes, nps, elapsed.Milliseconds())
	}

	return bestMove
}
