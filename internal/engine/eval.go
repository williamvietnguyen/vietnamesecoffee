package engine

// ============================================================
// Section 9: Evaluation — Tapered Eval
// ============================================================
//
// Every eval term produces two scores: middlegame (mg) and endgame (eg).
// A game phase is computed from remaining non-pawn material, and the
// final score interpolates between mg and eg based on the phase.

// Phase weights for non-pawn material
const (
	PhaseKnight = 1
	PhaseBishop = 1
	PhaseRook   = 2
	PhaseQueen  = 4
	TotalPhase  = 24 // 4*1 + 4*1 + 4*2 + 2*4
)

// Piece values (centipawns) — used by MVV-LVA move ordering in search.go
// Tuned for aggressive play: knights and bishops valued higher to encourage
// attacking piece play and sacrifices. Rooks slightly devalued to de-emphasize
// slow endgame grinding in favor of dynamic middlegame attacks.
var PieceValue = [6]int{100, 340, 350, 490, 900, 0}

// Piece values split into middlegame and endgame
// MG values match the aggressive PieceValue tuning above.
// EG values boost pawns (120, they promote!), rooks (530, they dominate open
// boards), and queens (1000). Knights/bishops stay the same.
var PieceValueMG = [6]int{100, 340, 350, 490, 900, 0}
var PieceValueEG = [6]int{120, 340, 350, 530, 1000, 0}

// Own-pawn discount: middlegame aggression concept only.
// Own pawns valued at 90cp (cheap to sacrifice for open lines), opponent pawns
// at 100cp (still respect their value when capturing).
// Endgame uses symmetric values since pawns matter equally for both sides.
const ownPawnValueMG = 90

// ------------------------------------------------------------------
// Piece-square tables — Middlegame
// ------------------------------------------------------------------
// From White's perspective, A1=index 0. For Black, mirror via sq^56.

// Pawn PST tuned for aggressive play: encourages central and kingside pawn advances
var pstPawnMG = [64]int{
	 0,  0,  0,  0,  0,  0,  0,  0,
	 5, 10, 10,-20,-20, 10, 15, 20,  // boost kingside pawns
	 5, -5,-10,  0,  0,  0, 10, 15,  // encourage f/g/h pawn advances
	 0,  0,  0, 20, 20, 15, 20, 20,
	 5,  5, 10, 30, 30, 20, 25, 25,  // aggressive central control
	10, 10, 20, 35, 35, 25, 30, 30,
	50, 50, 50, 55, 55, 50, 50, 50,
	 0,  0,  0,  0,  0,  0,  0,  0,
}

// Knight PST tuned for aggressive play: big bonuses for advanced/central knights
var pstKnightMG = [64]int{
	-50,-40,-30,-30,-30,-30,-40,-50,
	-40,-20,  0,  5,  5,  0,-20,-40,
	-30,  5, 15, 20, 20, 15,  5,-30,  // encourage early development
	-20, 10, 20, 25, 25, 20, 10,-20,  // aggressive central knights
	-20, 10, 20, 25, 25, 20, 10,-20,
	-30,  5, 15, 20, 20, 15,  5,-30,
	-40,-20,  0,  5,  5,  0,-20,-40,
	-50,-40,-30,-30,-30,-30,-40,-50,
}

// Bishop PST tuned for aggressive play: rewards long diagonals and active placement
var pstBishopMG = [64]int{
	-20,-10,-10,-10,-10,-10,-10,-20,
	-10,  5,  0,  0,  0,  0,  5,-10,
	-10, 10, 10, 10, 10, 10, 10,-10,
	-10,  5, 15, 15, 15, 15,  5,-10,  // active central bishops
	-10,  5, 10, 15, 15, 10,  5,-10,
	-10,  5, 10, 10, 10, 10,  5,-10,
	-10,  5,  5,  5,  5,  5,  5,-10,
	-20,-10,-10,-10,-10,-10,-10,-20,
}

var pstRookMG = [64]int{
	 0,  0,  0,  5,  5,  0,  0,  0,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	 5, 10, 10, 10, 10, 10, 10,  5,
	 0,  0,  0,  0,  0,  0,  0,  0,
}

var pstQueenMG = [64]int{
	-20,-10,-10, -5, -5,-10,-10,-20,
	-10,  0,  5,  0,  0,  0,  0,-10,
	-10,  5,  5,  5,  5,  5,  0,-10,
	  0,  0,  5,  5,  5,  5,  0, -5,
	 -5,  0,  5,  5,  5,  5,  0, -5,
	-10,  0,  5,  5,  5,  5,  0,-10,
	-10,  0,  0,  0,  0,  0,  0,-10,
	-20,-10,-10, -5, -5,-10,-10,-20,
}

var pstKingMG = [64]int{
	 20, 30, 10,  0,  0, 10, 30, 20,
	 20, 20,  0,  0,  0,  0, 20, 20,
	-10,-20,-20,-20,-20,-20,-20,-10,
	-20,-30,-30,-40,-40,-30,-30,-20,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
}

var pstMG = [6]*[64]int{&pstPawnMG, &pstKnightMG, &pstBishopMG, &pstRookMG, &pstQueenMG, &pstKingMG}

// ------------------------------------------------------------------
// Piece-square tables — Endgame
// ------------------------------------------------------------------

// Endgame pawn: advanced pawns are much more valuable (about to promote)
var pstPawnEG = [64]int{
	 0,  0,  0,  0,  0,  0,  0,  0,
	 5,  5,  5,  5,  5,  5,  5,  5,
	10, 10, 10, 10, 10, 10, 10, 10,
	20, 20, 20, 20, 20, 20, 20, 20,
	35, 35, 35, 35, 35, 35, 35, 35,
	60, 60, 60, 60, 60, 60, 60, 60,
	90, 90, 90, 90, 90, 90, 90, 90,
	 0,  0,  0,  0,  0,  0,  0,  0,
}

// Endgame knight: central control still good, less extreme edge penalty
var pstKnightEG = [64]int{
	-40,-30,-20,-20,-20,-20,-30,-40,
	-30,-10,  0,  5,  5,  0,-10,-30,
	-20,  0, 10, 15, 15, 10,  0,-20,
	-15,  5, 15, 20, 20, 15,  5,-15,
	-15,  5, 15, 20, 20, 15,  5,-15,
	-20,  0, 10, 15, 15, 10,  0,-20,
	-30,-10,  0,  5,  5,  0,-10,-30,
	-40,-30,-20,-20,-20,-20,-30,-40,
}

// Endgame bishop: centralization rewarded
var pstBishopEG = [64]int{
	-20,-15,-10,-10,-10,-10,-15,-20,
	-10,  0,  0,  5,  5,  0,  0,-10,
	-10,  0, 10, 10, 10, 10,  0,-10,
	-10,  5, 10, 15, 15, 10,  5,-10,
	-10,  5, 10, 15, 15, 10,  5,-10,
	-10,  0, 10, 10, 10, 10,  0,-10,
	-10,  0,  0,  5,  5,  0,  0,-10,
	-20,-15,-10,-10,-10,-10,-15,-20,
}

// Endgame rook: reward active centralized rooks
var pstRookEG = [64]int{
	 0,  0,  0,  0,  0,  0,  0,  0,
	 0,  0,  0,  0,  0,  0,  0,  0,
	 0,  0,  0,  0,  0,  0,  0,  0,
	 5,  5,  5,  5,  5,  5,  5,  5,
	 5,  5,  5,  5,  5,  5,  5,  5,
	 5,  5,  5,  5,  5,  5,  5,  5,
	10, 10, 10, 10, 10, 10, 10, 10,
	 5,  5,  5,  5,  5,  5,  5,  5,
}

// Endgame queen: centralization
var pstQueenEG = [64]int{
	-20,-10,-10, -5, -5,-10,-10,-20,
	-10,  0,  0,  0,  0,  0,  0,-10,
	-10,  0,  5,  5,  5,  5,  0,-10,
	 -5,  0,  5, 10, 10,  5,  0, -5,
	 -5,  0,  5, 10, 10,  5,  0, -5,
	-10,  0,  5,  5,  5,  5,  0,-10,
	-10,  0,  0,  0,  0,  0,  0,-10,
	-20,-10,-10, -5, -5,-10,-10,-20,
}

// Endgame king: CENTRALIZE! This is the single biggest improvement from
// tapered eval. The king should move to the center in endgames.
var pstKingEG = [64]int{
	-30,-20,-10, -5, -5,-10,-20,-30,
	-15, -5,  5, 10, 10,  5, -5,-15,
	-10,  5, 15, 20, 20, 15,  5,-10,
	 -5, 10, 20, 25, 25, 20, 10, -5,
	 -5, 10, 20, 25, 25, 20, 10, -5,
	-10,  5, 15, 20, 20, 15,  5,-10,
	-15, -5,  5, 10, 10,  5, -5,-15,
	-30,-20,-10, -5, -5,-10,-20,-30,
}

var pstEG = [6]*[64]int{&pstPawnEG, &pstKnightEG, &pstBishopEG, &pstRookEG, &pstQueenEG, &pstKingEG}

// ------------------------------------------------------------------
// Game phase computation
// ------------------------------------------------------------------

// gamePhase returns a value from 0 (all pieces on board = pure middlegame)
// to TotalPhase (only kings+pawns = pure endgame).
func gamePhase(pos *Position) int {
	phase := TotalPhase
	phase -= Popcount(pos.Pieces[White][Knight]|pos.Pieces[Black][Knight]) * PhaseKnight
	phase -= Popcount(pos.Pieces[White][Bishop]|pos.Pieces[Black][Bishop]) * PhaseBishop
	phase -= Popcount(pos.Pieces[White][Rook]|pos.Pieces[Black][Rook]) * PhaseRook
	phase -= Popcount(pos.Pieces[White][Queen]|pos.Pieces[Black][Queen]) * PhaseQueen
	if phase < 0 {
		phase = 0
	}
	return phase
}

// ------------------------------------------------------------------
// Main evaluation function
// ------------------------------------------------------------------

// Evaluate returns the score in centipawns relative to the side to move.
func Evaluate(pos *Position) int {
	mg, eg := 0, 0

	// Asymmetric pawn values: own pawns worth 90cp in mg (cheap to sacrifice),
	// opponent's pawns worth 100cp. Endgame uses symmetric values.
	whitePawnMG, blackPawnMG := PieceValueMG[Pawn], PieceValueMG[Pawn]
	if pos.SideToMove == White {
		whitePawnMG = ownPawnValueMG
	} else {
		blackPawnMG = ownPawnValueMG
	}

	// Material + piece-square tables — Pawns (asymmetric mg values)
	bb := pos.Pieces[White][Pawn]
	for bb != 0 {
		sq := PopLSB(&bb)
		mg += whitePawnMG + pstMG[Pawn][sq]
		eg += PieceValueEG[Pawn] + pstEG[Pawn][sq]
	}
	bb = pos.Pieces[Black][Pawn]
	for bb != 0 {
		sq := PopLSB(&bb)
		mg -= blackPawnMG + pstMG[Pawn][sq^56]
		eg -= PieceValueEG[Pawn] + pstEG[Pawn][sq^56]
	}

	// Non-pawn pieces
	for pc := 1; pc < 6; pc++ {
		bb = pos.Pieces[White][pc]
		for bb != 0 {
			sq := PopLSB(&bb)
			mg += PieceValueMG[pc] + pstMG[pc][sq]
			eg += PieceValueEG[pc] + pstEG[pc][sq]
		}
		bb = pos.Pieces[Black][pc]
		for bb != 0 {
			sq := PopLSB(&bb)
			mg -= PieceValueMG[pc] + pstMG[pc][sq^56]
			eg -= PieceValueEG[pc] + pstEG[pc][sq^56]
		}
	}

	// Bishop pair bonus (stronger in endgames)
	if Popcount(pos.Pieces[White][Bishop]) >= 2 {
		mg += 50
		eg += 70
	}
	if Popcount(pos.Pieces[Black][Bishop]) >= 2 {
		mg -= 50
		eg -= 70
	}

	// Rook on open/semi-open file bonus
	wRookMG, wRookEG := evaluateRooks(pos, White)
	bRookMG, bRookEG := evaluateRooks(pos, Black)
	mg += wRookMG - bRookMG
	eg += wRookEG - bRookEG

	// Bishop/queen x-ray to enemy king (middlegame-only concept)
	mg += evaluateXrays(pos, White, Black)
	mg -= evaluateXrays(pos, Black, White)

	// Pawn structure (slightly harsher penalties in endgame)
	wPawnMG, wPawnEG := evaluatePawns(pos, White)
	bPawnMG, bPawnEG := evaluatePawns(pos, Black)
	mg += wPawnMG - bPawnMG
	eg += wPawnEG - bPawnEG

	// King attack evaluation (middlegame-only — no pieces to attack with in endgame)
	whiteKingAttack := evaluateKingAttack(pos, White, Black)
	blackKingAttack := evaluateKingAttack(pos, Black, White)
	mg += whiteKingAttack - blackKingAttack

	// King safety imbalance: when one side's attack is stronger,
	// the advantage grows superlinearly — encourages sacrificing
	// material to press a king safety advantage. (Middlegame-only.)
	imbalance := whiteKingAttack - blackKingAttack
	if imbalance > 0 {
		mg += imbalance * imbalance / 200
	} else if imbalance < 0 {
		mg -= imbalance * imbalance / 200
	}

	// Pawn storm evaluation (middlegame-only)
	mg += evaluatePawnStorm(pos, White, Black)
	mg -= evaluatePawnStorm(pos, Black, White)

	// Uncastled king bonus (middlegame-only): if the opponent still has
	// castling rights, their king is likely in the center — attack before
	// they castle!
	if pos.CastlingRights&(BlackKingSide|BlackQueenSide) != 0 {
		mg += 30 // Black hasn't castled, bonus for White
	}
	if pos.CastlingRights&(WhiteKingSide|WhiteQueenSide) != 0 {
		mg -= 30 // White hasn't castled, bonus for Black
	}

	// Interpolate between middlegame and endgame scores
	phase := gamePhase(pos)
	// phase=0 means pure mg, phase=TotalPhase means pure eg
	score := (mg*(TotalPhase-phase) + eg*phase) / TotalPhase

	if pos.SideToMove == Black {
		score = -score
	}

	// Initiative/tempo bonus: reward having the move. The engine always
	// wants to keep attacking rather than making passive defensive moves.
	score += 15

	return score
}

// evaluateKingAttack returns a middlegame bonus for having pieces close to
// the enemy king.
func evaluateKingAttack(pos *Position, us, them int) int {
	enemyKingBB := pos.Pieces[them][King]
	if enemyKingBB == 0 {
		return 0
	}
	enemyKing := Lsb(enemyKingBB)
	bonus := 0

	// King zone: 2 rings around the enemy king (wider danger zone).
	// Inner ring = directly adjacent squares. Outer ring = squares adjacent
	// to the inner ring. Pieces in the outer ring are "almost" attacking.
	innerZone := KingAttacks[enemyKing] | (1 << uint(enemyKing))
	outerZone := uint64(0)
	temp := innerZone
	for temp != 0 {
		sq := PopLSB(&temp)
		outerZone |= KingAttacks[sq]
	}
	outerZone &^= innerZone // outer ring only (exclude inner)

	// Count attacking pieces near the enemy king
	attackers := 0

	// Knights attacking king zone + tropism
	knights := pos.Pieces[us][Knight]
	for knights != 0 {
		sq := PopLSB(&knights)
		distance := Abs(SqRank(sq)-SqRank(enemyKing)) + Abs(SqFile(sq)-SqFile(enemyKing))
		bonus += (8 - distance) * 3 // tropism: knights gravitate toward enemy king
		att := KnightAttacks[sq]
		if att&innerZone != 0 {
			attackers++
		} else if att&outerZone != 0 {
			attackers++ // outer ring still counts (piece is closing in)
			bonus += 5  // small extra bonus for being in striking distance
		}
	}

	// Bishops attacking king zone + tropism
	bishops := pos.Pieces[us][Bishop]
	for bishops != 0 {
		sq := PopLSB(&bishops)
		distance := Abs(SqRank(sq)-SqRank(enemyKing)) + Abs(SqFile(sq)-SqFile(enemyKing))
		bonus += (8 - distance) * 2 // tropism: bishops closer to enemy king
		att := BishopAttacks(sq, pos.AllOccupied)
		if att&innerZone != 0 {
			attackers++
		} else if att&outerZone != 0 {
			bonus += 8 // bishop aimed near king, one move from attacking
		}
	}

	// Rooks attacking king zone + tropism
	rooks := pos.Pieces[us][Rook]
	for rooks != 0 {
		sq := PopLSB(&rooks)
		distance := Abs(SqRank(sq)-SqRank(enemyKing)) + Abs(SqFile(sq)-SqFile(enemyKing))
		bonus += (8 - distance) * 2 // tropism: rooks closer to enemy king
		att := RookAttacks(sq, pos.AllOccupied)
		if att&innerZone != 0 {
			attackers++
		} else if att&outerZone != 0 {
			bonus += 8 // rook aimed near king
		}
	}

	// Queens attacking king zone + tropism
	queens := pos.Pieces[us][Queen]
	for queens != 0 {
		sq := PopLSB(&queens)
		distance := Abs(SqRank(sq)-SqRank(enemyKing)) + Abs(SqFile(sq)-SqFile(enemyKing))
		bonus += (8 - distance) * 3 // tropism: queen gravitates toward enemy king
		att := QueenAttacks(sq, pos.AllOccupied)
		if att&innerZone != 0 {
			attackers++
		} else if att&outerZone != 0 {
			bonus += 12 // queen lurking near king
		}
	}

	// Non-linear king attack scaling: the more attackers, the disproportionately
	// larger the bonus. This is what makes the engine sacrifice pieces — getting
	// a 3rd or 4th attacker on the king is worth more than a whole piece.
	//
	//   1 attacker:   10 cp (minor annoyance)
	//   2 attackers:  80 cp (real pressure)
	//   3 attackers: 240 cp (worth a piece sacrifice!)
	//   4 attackers: 540 cp (worth a rook sacrifice!)
	//   5+ attackers: devastating
	kingAttackWeight := [8]int{0, 10, 80, 240, 540, 900, 1200, 1500}
	if attackers >= len(kingAttackWeight) {
		attackers = len(kingAttackWeight) - 1
	}
	bonus += kingAttackWeight[attackers]

	// Bonus for attacking a king with a weak pawn shield
	kingFile := SqFile(enemyKing)
	kingRank := SqRank(enemyKing)
	shieldPawns := 0

	if them == White && kingRank < 7 {
		for f := kingFile - 1; f <= kingFile+1; f++ {
			if f < 0 || f >= 8 {
				continue
			}
			sq := (kingRank+1)*8 + f
			if pos.Pieces[White][Pawn]&(1<<uint(sq)) != 0 {
				shieldPawns++
			}
		}
	} else if them == Black && kingRank > 0 {
		for f := kingFile - 1; f <= kingFile+1; f++ {
			if f < 0 || f >= 8 {
				continue
			}
			sq := (kingRank-1)*8 + f
			if pos.Pieces[Black][Pawn]&(1<<uint(sq)) != 0 {
				shieldPawns++
			}
		}
	}

	if shieldPawns < 2 {
		bonus += (3 - shieldPawns) * 10
	}

	return bonus
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// evaluateRooks returns (mg, eg) bonuses for rooks on open or semi-open files.
func evaluateRooks(pos *Position, color int) (int, int) {
	mgBonus, egBonus := 0, 0
	them := color ^ 1

	enemyKingBB := pos.Pieces[them][King]
	enemyKingFile := -1
	if enemyKingBB != 0 {
		enemyKingFile = SqFile(Lsb(enemyKingBB))
	}

	rooks := pos.Pieces[color][Rook]
	rooksCopy := rooks
	for rooks != 0 {
		sq := PopLSB(&rooks)
		file := SqFile(sq)

		fileBB := FileMask[file]
		ourPawns := pos.Pieces[color][Pawn] & fileBB
		theirPawns := pos.Pieces[them][Pawn] & fileBB

		if ourPawns == 0 && theirPawns == 0 {
			mgBonus += 20 // open file
			egBonus += 25 // open files slightly more valuable in endgame
			// Extra bonus if open file is near enemy king (aggressive!)
			if enemyKingFile >= 0 && Abs(file-enemyKingFile) <= 1 {
				mgBonus += 25 // rook aimed at king's vicinity!
			}
		} else if ourPawns == 0 && theirPawns != 0 {
			mgBonus += 10 // semi-open file
			egBonus += 12
			// Extra bonus if semi-open file is near enemy king
			if enemyKingFile >= 0 && Abs(file-enemyKingFile) <= 1 {
				mgBonus += 15
			}
		}
	}

	// Connected rooks: bonus when two rooks see each other on a rank or file
	// (no pieces between them). Doubled rooks are a wrecking ball.
	if Popcount(rooksCopy) >= 2 {
		rooks = rooksCopy
		r1 := PopLSB(&rooks)
		r2 := PopLSB(&rooks)
		att := RookAttacks(r1, pos.AllOccupied)
		if att&(1<<uint(r2)) != 0 {
			mgBonus += 20 // connected rooks
			egBonus += 20
			// Extra bonus if connected on a file near enemy king
			if SqFile(r1) == SqFile(r2) && enemyKingFile >= 0 && Abs(SqFile(r1)-enemyKingFile) <= 1 {
				mgBonus += 25 // doubled rooks aimed at king's file!
			}
		}
	}

	return mgBonus, egBonus
}

// evaluateXrays returns a middlegame-only bonus for bishops and queens whose
// diagonals point at the enemy king zone. Encourages aggressive piece placement
// aimed at the enemy king, like a battery on an open diagonal.
func evaluateXrays(pos *Position, us, them int) int {
	enemyKingBB := pos.Pieces[them][King]
	if enemyKingBB == 0 {
		return 0
	}
	enemyKingSq := Lsb(enemyKingBB)
	kingZone := KingAttacks[enemyKingSq] | (1 << uint(enemyKingSq))
	bonus := 0

	// Bishops: bonus if diagonal attacks hit king zone
	bishops := pos.Pieces[us][Bishop]
	for bishops != 0 {
		sq := PopLSB(&bishops)
		att := BishopAttacks(sq, pos.AllOccupied)
		if att&kingZone != 0 {
			bonus += 25 // bishop aiming at king
		}
	}

	// Queens: bonus if diagonal or file attacks hit king zone
	// (rook-like component already covered by evaluateRooks for open files,
	//  so focus on the diagonal component)
	queens := pos.Pieces[us][Queen]
	for queens != 0 {
		sq := PopLSB(&queens)
		diagAtt := BishopAttacks(sq, pos.AllOccupied)
		if diagAtt&kingZone != 0 {
			bonus += 30 // queen diagonal aimed at king
		}
		rankFileAtt := RookAttacks(sq, pos.AllOccupied)
		if rankFileAtt&kingZone != 0 {
			bonus += 20 // queen rank/file aimed at king
		}
	}

	return bonus
}

// evaluatePawns returns (mg, eg) score adjustments for pawn structure.
func evaluatePawns(pos *Position, color int) (int, int) {
	mgScore, egScore := 0, 0
	them := color ^ 1

	for file := 0; file < 8; file++ {
		fileBB := FileMask[file]
		ourPawnsOnFile := pos.Pieces[color][Pawn] & fileBB
		theirPawnsOnFile := pos.Pieces[them][Pawn] & fileBB

		count := Popcount(ourPawnsOnFile)

		// Doubled pawns penalty (reduced for aggressive play - structure matters less)
		// Slightly harsher in endgame where structure is more important.
		if count >= 2 {
			mgScore -= 5 * (count - 1)
			egScore -= 8 * (count - 1)
		}

		// Isolated pawn penalty (reduced - attacking is more important than structure)
		// Slightly harsher in endgame.
		if count > 0 {
			adjacent := uint64(0)
			if file > 0 {
				adjacent |= FileMask[file-1]
			}
			if file < 7 {
				adjacent |= FileMask[file+1]
			}
			if (pos.Pieces[color][Pawn] & adjacent) == 0 {
				mgScore -= 8  // isolated pawn mg (was -15, reduced for aggression)
				egScore -= 12 // harsher in endgame
			}
		}

		// Passed pawn bonus (worth ~1.5x more in endgame)
		if count > 0 {
			bb := ourPawnsOnFile
			var mostAdvanced int
			if color == White {
				for bb != 0 {
					sq := PopLSB(&bb)
					if bb == 0 || SqRank(sq) > SqRank(mostAdvanced) {
						mostAdvanced = sq
					}
				}
				rank := SqRank(mostAdvanced)
				blockingMask := uint64(0)
				for f := file - 1; f <= file+1; f++ {
					if f < 0 || f >= 8 {
						continue
					}
					for r := rank + 1; r < 8; r++ {
						blockingMask |= 1 << uint(r*8+f)
					}
				}
				if (theirPawnsOnFile&blockingMask) == 0 && (pos.Pieces[them][Pawn]&blockingMask) == 0 {
					// Passed pawn bonus (aggressive tuning: push pawns hard!)
					mgScore += 20 + rank*15
					egScore += 30 + rank*22 // passed pawns much more valuable in endgame
				}
			} else {
				mostAdvanced = Lsb(ourPawnsOnFile)
				for bb != 0 {
					sq := PopLSB(&bb)
					if SqRank(sq) < SqRank(mostAdvanced) {
						mostAdvanced = sq
					}
				}
				rank := SqRank(mostAdvanced)
				blockingMask := uint64(0)
				for f := file - 1; f <= file+1; f++ {
					if f < 0 || f >= 8 {
						continue
					}
					for r := 0; r < rank; r++ {
						blockingMask |= 1 << uint(r*8+f)
					}
				}
				if (theirPawnsOnFile&blockingMask) == 0 && (pos.Pieces[them][Pawn]&blockingMask) == 0 {
					// Passed pawn bonus for Black (aggressive tuning)
					mgScore += 20 + (7-rank)*15
					egScore += 30 + (7-rank)*22
				}
			}
		}
	}

	return mgScore, egScore
}

// evaluatePawnStorm returns a middlegame-only bonus for advancing pawns near
// the enemy king.
func evaluatePawnStorm(pos *Position, us, them int) int {
	enemyKingBB := pos.Pieces[them][King]
	if enemyKingBB == 0 {
		return 0
	}
	enemyKing := Lsb(enemyKingBB)
	enemyKingFile := SqFile(enemyKing)

	oppositeSide := false
	ourKingBB := pos.Pieces[us][King]
	if ourKingBB != 0 {
		ourKingFile := SqFile(Lsb(ourKingBB))
		if (ourKingFile <= 2 && enemyKingFile >= 5) || (ourKingFile >= 5 && enemyKingFile <= 2) {
			oppositeSide = true
		}
	}

	bonus := 0
	for f := enemyKingFile - 1; f <= enemyKingFile+1; f++ {
		if f < 0 || f >= 8 {
			continue
		}
		ourPawns := pos.Pieces[us][Pawn] & FileMask[f]
		for ourPawns != 0 {
			sq := PopLSB(&ourPawns)
			rank := SqRank(sq)
			// Bonus for pawns advancing toward enemy king
			if us == White && rank > 3 {
				bonus += (rank - 3) * 20 // big bonus for pawn storms!
			} else if us == Black && rank < 4 {
				bonus += (4 - rank) * 20
			}
		}
	}
	if oppositeSide {
		bonus = bonus * 3 / 2
	}
	return bonus
}
