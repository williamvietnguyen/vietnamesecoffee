package engine

import (
	"strings"
)

// ============================================================
// Section 6: FEN Parsing
// ============================================================

func ParseFEN(fen string) Position {
	var pos Position
	pos.EnPassant = NoSquare

	parts := strings.Fields(fen)
	if len(parts) < 4 {
		return pos
	}

	// Piece placement
	ranks := strings.Split(parts[0], "/")
	for i, rankStr := range ranks {
		rank := 7 - i // FEN starts from rank 8
		file := 0
		for _, ch := range rankStr {
			if ch >= '1' && ch <= '8' {
				file += int(ch - '0')
			} else {
				var color int
				var piece int
				switch ch {
				case 'P':
					color, piece = White, Pawn
				case 'N':
					color, piece = White, Knight
				case 'B':
					color, piece = White, Bishop
				case 'R':
					color, piece = White, Rook
				case 'Q':
					color, piece = White, Queen
				case 'K':
					color, piece = White, King
				case 'p':
					color, piece = Black, Pawn
				case 'n':
					color, piece = Black, Knight
				case 'b':
					color, piece = Black, Bishop
				case 'r':
					color, piece = Black, Rook
				case 'q':
					color, piece = Black, Queen
				case 'k':
					color, piece = Black, King
				}
				sq := rank*8 + file
				pos.Pieces[color][piece] |= 1 << uint(sq)
				file++
			}
		}
	}

	// Side to move
	if parts[1] == "b" {
		pos.SideToMove = Black
	}

	// Castling rights
	if parts[2] != "-" {
		for _, ch := range parts[2] {
			switch ch {
			case 'K':
				pos.CastlingRights |= WhiteKingSide
			case 'Q':
				pos.CastlingRights |= WhiteQueenSide
			case 'k':
				pos.CastlingRights |= BlackKingSide
			case 'q':
				pos.CastlingRights |= BlackQueenSide
			}
		}
	}

	// En passant
	if parts[3] != "-" {
		file := int(parts[3][0] - 'a')
		rank := int(parts[3][1] - '1')
		pos.EnPassant = rank*8 + file
	}

	// Half move clock
	if len(parts) > 4 {
		for _, ch := range parts[4] {
			pos.HalfMoveClock = pos.HalfMoveClock*10 + int(ch-'0')
		}
	}

	// Full move number
	if len(parts) > 5 {
		for _, ch := range parts[5] {
			pos.FullMoveNumber = pos.FullMoveNumber*10 + int(ch-'0')
		}
	}

	pos.UpdateOccupied()

	// Compute Zobrist hash
	for c := 0; c < 2; c++ {
		for pc := 0; pc < 6; pc++ {
			bb := pos.Pieces[c][pc]
			for bb != 0 {
				sq := PopLSB(&bb)
				pos.Hash ^= ZobristPiece[c][pc][sq]
			}
		}
	}
	pos.Hash ^= ZobristCastling[pos.CastlingRights]
	if pos.EnPassant != NoSquare {
		pos.Hash ^= ZobristEP[SqFile(pos.EnPassant)]
	}
	if pos.SideToMove == Black {
		pos.Hash ^= ZobristSide
	}

	return pos
}

// ============================================================
// Section 7: Move Generation
// ============================================================

func (p *Position) GenerateMoves() []Move {
	moves := make([]Move, 0, 64)
	us := p.SideToMove
	them := us ^ 1
	ourPieces := p.Occupied[us]
	theirPieces := p.Occupied[them]
	allPieces := p.AllOccupied

	// Pawn moves
	pawns := p.Pieces[us][Pawn]
	for pawns != 0 {
		from := PopLSB(&pawns)
		fromBit := uint64(1) << uint(from)
		rank := SqRank(from)
		file := SqFile(from)

		if us == White {
			// Single push
			toSq := from + 8
			if toSq < 64 && allPieces&(1<<uint(toSq)) == 0 {
				if rank == 6 { // promotion rank
					moves = append(moves, NewMove(from, toSq, FlagQueenPromo))
					moves = append(moves, NewMove(from, toSq, FlagRookPromo))
					moves = append(moves, NewMove(from, toSq, FlagBishopPromo))
					moves = append(moves, NewMove(from, toSq, FlagKnightPromo))
				} else {
					moves = append(moves, NewMove(from, toSq, FlagQuiet))
					// Double push
					if rank == 1 {
						toSq2 := from + 16
						if allPieces&(1<<uint(toSq2)) == 0 {
							moves = append(moves, NewMove(from, toSq2, FlagDoublePawn))
						}
					}
				}
			}
			// Captures
			attacks := PawnAttacks[White][from]
			captures := attacks & theirPieces
			for captures != 0 {
				to := PopLSB(&captures)
				if rank == 6 {
					moves = append(moves, NewMove(from, to, FlagQueenPromoCapture))
					moves = append(moves, NewMove(from, to, FlagRookPromoCapture))
					moves = append(moves, NewMove(from, to, FlagBishopPromoCapture))
					moves = append(moves, NewMove(from, to, FlagKnightPromoCapture))
				} else {
					moves = append(moves, NewMove(from, to, FlagCapture))
				}
			}
			// En passant
			if p.EnPassant != NoSquare && attacks&(1<<uint(p.EnPassant)) != 0 {
				moves = append(moves, NewMove(from, p.EnPassant, FlagEPCapture))
			}
		} else { // Black
			// Single push
			toSq := from - 8
			if toSq >= 0 && allPieces&(1<<uint(toSq)) == 0 {
				if rank == 1 { // promotion rank
					moves = append(moves, NewMove(from, toSq, FlagQueenPromo))
					moves = append(moves, NewMove(from, toSq, FlagRookPromo))
					moves = append(moves, NewMove(from, toSq, FlagBishopPromo))
					moves = append(moves, NewMove(from, toSq, FlagKnightPromo))
				} else {
					moves = append(moves, NewMove(from, toSq, FlagQuiet))
					// Double push
					if rank == 6 {
						toSq2 := from - 16
						if allPieces&(1<<uint(toSq2)) == 0 {
							moves = append(moves, NewMove(from, toSq2, FlagDoublePawn))
						}
					}
				}
			}
			// Captures
			attacks := PawnAttacks[Black][from]
			captures := attacks & theirPieces
			for captures != 0 {
				to := PopLSB(&captures)
				if rank == 1 {
					moves = append(moves, NewMove(from, to, FlagQueenPromoCapture))
					moves = append(moves, NewMove(from, to, FlagRookPromoCapture))
					moves = append(moves, NewMove(from, to, FlagBishopPromoCapture))
					moves = append(moves, NewMove(from, to, FlagKnightPromoCapture))
				} else {
					moves = append(moves, NewMove(from, to, FlagCapture))
				}
			}
			// En passant
			if p.EnPassant != NoSquare && attacks&(1<<uint(p.EnPassant)) != 0 {
				moves = append(moves, NewMove(from, p.EnPassant, FlagEPCapture))
			}
		}
		_ = fromBit
		_ = file
	}

	// Knight moves
	knights := p.Pieces[us][Knight]
	for knights != 0 {
		from := PopLSB(&knights)
		attacks := KnightAttacks[from] & ^ourPieces
		for attacks != 0 {
			to := PopLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, NewMove(from, to, FlagCapture))
			} else {
				moves = append(moves, NewMove(from, to, FlagQuiet))
			}
		}
	}

	// Bishop moves
	bishops := p.Pieces[us][Bishop]
	for bishops != 0 {
		from := PopLSB(&bishops)
		attacks := BishopAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := PopLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, NewMove(from, to, FlagCapture))
			} else {
				moves = append(moves, NewMove(from, to, FlagQuiet))
			}
		}
	}

	// Rook moves
	rooks := p.Pieces[us][Rook]
	for rooks != 0 {
		from := PopLSB(&rooks)
		attacks := RookAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := PopLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, NewMove(from, to, FlagCapture))
			} else {
				moves = append(moves, NewMove(from, to, FlagQuiet))
			}
		}
	}

	// Queen moves
	queens := p.Pieces[us][Queen]
	for queens != 0 {
		from := PopLSB(&queens)
		attacks := QueenAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := PopLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, NewMove(from, to, FlagCapture))
			} else {
				moves = append(moves, NewMove(from, to, FlagQuiet))
			}
		}
	}

	// King moves
	kingBB := p.Pieces[us][King]
	if kingBB != 0 {
		from := Lsb(kingBB)
		attacks := KingAttacks[from] & ^ourPieces
		for attacks != 0 {
			to := PopLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, NewMove(from, to, FlagCapture))
			} else {
				moves = append(moves, NewMove(from, to, FlagQuiet))
			}
		}

		// Castling
		if us == White {
			if p.CastlingRights&WhiteKingSide != 0 {
				// King on e1, rook on h1, f1 and g1 must be empty, e1/f1/g1 not attacked
				if from == E1 &&
					allPieces&(1<<F1|1<<G1) == 0 &&
					!p.IsSquareAttacked(E1, them) &&
					!p.IsSquareAttacked(F1, them) &&
					!p.IsSquareAttacked(G1, them) {
					moves = append(moves, NewMove(E1, G1, FlagKingCastle))
				}
			}
			if p.CastlingRights&WhiteQueenSide != 0 {
				if from == E1 &&
					allPieces&(1<<D1|1<<C1|1<<B1) == 0 &&
					!p.IsSquareAttacked(E1, them) &&
					!p.IsSquareAttacked(D1, them) &&
					!p.IsSquareAttacked(C1, them) {
					moves = append(moves, NewMove(E1, C1, FlagQueenCastle))
				}
			}
		} else {
			if p.CastlingRights&BlackKingSide != 0 {
				if from == E8 &&
					allPieces&(1<<F8|1<<G8) == 0 &&
					!p.IsSquareAttacked(E8, them) &&
					!p.IsSquareAttacked(F8, them) &&
					!p.IsSquareAttacked(G8, them) {
					moves = append(moves, NewMove(E8, G8, FlagKingCastle))
				}
			}
			if p.CastlingRights&BlackQueenSide != 0 {
				if from == E8 &&
					allPieces&(1<<D8|1<<C8|1<<B8) == 0 &&
					!p.IsSquareAttacked(E8, them) &&
					!p.IsSquareAttacked(D8, them) &&
					!p.IsSquareAttacked(C8, them) {
					moves = append(moves, NewMove(E8, C8, FlagQueenCastle))
				}
			}
		}
	}

	return moves
}

// GenerateLegalMoves returns only legal moves
func (p *Position) GenerateLegalMoves() []Move {
	pseudoLegal := p.GenerateMoves()
	legal := make([]Move, 0, len(pseudoLegal))
	for _, m := range pseudoLegal {
		newPos := p.MakeMove(m)
		if !newPos.InCheck(p.SideToMove) {
			legal = append(legal, m)
		}
	}
	return legal
}

// ============================================================
// Section 8: Make Move
// ============================================================

func (p *Position) MakeMove(m Move) Position {
	newPos := *p // copy

	us := p.SideToMove
	them := us ^ 1
	from := m.From()
	to := m.To()
	flags := m.Flags()
	fromBit := uint64(1) << uint(from)
	toBit := uint64(1) << uint(to)

	// Find moving piece
	movingPiece := -1
	for pc := 0; pc < 6; pc++ {
		if newPos.Pieces[us][pc]&fromBit != 0 {
			movingPiece = pc
			break
		}
	}

	// Hash: move the piece from -> to
	newPos.Hash ^= ZobristPiece[us][movingPiece][from]
	newPos.Hash ^= ZobristPiece[us][movingPiece][to]

	// Remove captured piece (if any, non-EP)
	if flags&FlagCapture != 0 && flags != FlagEPCapture {
		for pc := 0; pc < 6; pc++ {
			if newPos.Pieces[them][pc]&toBit != 0 {
				newPos.Pieces[them][pc] ^= toBit
				newPos.Hash ^= ZobristPiece[them][pc][to]
				break
			}
		}
	}

	// Move the piece
	newPos.Pieces[us][movingPiece] ^= fromBit | toBit

	// Handle promotions
	if m.IsPromotion() {
		promoPiece := m.PromoPiece()
		// Remove pawn from destination, add promoted piece
		newPos.Pieces[us][Pawn] ^= toBit
		newPos.Pieces[us][promoPiece] |= toBit
		// Hash: undo pawn placement at to, add promoted piece
		newPos.Hash ^= ZobristPiece[us][Pawn][to]
		newPos.Hash ^= ZobristPiece[us][promoPiece][to]
	}

	// Handle en passant capture
	if flags == FlagEPCapture {
		var capturedSq int
		if us == White {
			capturedSq = to - 8
		} else {
			capturedSq = to + 8
		}
		newPos.Pieces[them][Pawn] ^= 1 << uint(capturedSq)
		newPos.Hash ^= ZobristPiece[them][Pawn][capturedSq]
	}

	// Handle castling - move the rook
	if flags == FlagKingCastle {
		if us == White {
			newPos.Pieces[White][Rook] ^= (1 << H1) | (1 << F1)
			newPos.Hash ^= ZobristPiece[White][Rook][H1]
			newPos.Hash ^= ZobristPiece[White][Rook][F1]
		} else {
			newPos.Pieces[Black][Rook] ^= (1 << H8) | (1 << F8)
			newPos.Hash ^= ZobristPiece[Black][Rook][H8]
			newPos.Hash ^= ZobristPiece[Black][Rook][F8]
		}
	}
	if flags == FlagQueenCastle {
		if us == White {
			newPos.Pieces[White][Rook] ^= (1 << A1) | (1 << D1)
			newPos.Hash ^= ZobristPiece[White][Rook][A1]
			newPos.Hash ^= ZobristPiece[White][Rook][D1]
		} else {
			newPos.Pieces[Black][Rook] ^= (1 << A8) | (1 << D8)
			newPos.Hash ^= ZobristPiece[Black][Rook][A8]
			newPos.Hash ^= ZobristPiece[Black][Rook][D8]
		}
	}

	// Hash: XOR out old EP file if set
	if p.EnPassant != NoSquare {
		newPos.Hash ^= ZobristEP[SqFile(p.EnPassant)]
	}

	// Update en passant square
	if flags == FlagDoublePawn {
		if us == White {
			newPos.EnPassant = from + 8
		} else {
			newPos.EnPassant = from - 8
		}
		// Hash: XOR in new EP file
		newPos.Hash ^= ZobristEP[SqFile(newPos.EnPassant)]
	} else {
		newPos.EnPassant = NoSquare
	}

	// Hash: XOR out old castling rights
	newPos.Hash ^= ZobristCastling[p.CastlingRights]

	// Update castling rights
	// If king moves, lose both castling rights for that side
	if movingPiece == King {
		if us == White {
			newPos.CastlingRights &^= WhiteKingSide | WhiteQueenSide
		} else {
			newPos.CastlingRights &^= BlackKingSide | BlackQueenSide
		}
	}
	// If rook moves from its starting square, lose that castling right
	if movingPiece == Rook {
		switch from {
		case A1:
			newPos.CastlingRights &^= WhiteQueenSide
		case H1:
			newPos.CastlingRights &^= WhiteKingSide
		case A8:
			newPos.CastlingRights &^= BlackQueenSide
		case H8:
			newPos.CastlingRights &^= BlackKingSide
		}
	}
	// If rook is captured on its starting square
	switch to {
	case A1:
		newPos.CastlingRights &^= WhiteQueenSide
	case H1:
		newPos.CastlingRights &^= WhiteKingSide
	case A8:
		newPos.CastlingRights &^= BlackQueenSide
	case H8:
		newPos.CastlingRights &^= BlackKingSide
	}

	// Hash: XOR in new castling rights
	newPos.Hash ^= ZobristCastling[newPos.CastlingRights]

	// Update half move clock
	if movingPiece == Pawn || m.IsCapture() {
		newPos.HalfMoveClock = 0
	} else {
		newPos.HalfMoveClock++
	}

	// Update full move number
	if us == Black {
		newPos.FullMoveNumber++
	}

	// Switch side
	newPos.SideToMove = them
	newPos.Hash ^= ZobristSide

	newPos.UpdateOccupied()
	return newPos
}
