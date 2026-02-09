package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/bits"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ============================================================
// Section 1: Constants & Types
// ============================================================

// Square indices (A1=0, B1=1, ..., H8=63)
const (
	A1, B1, C1, D1, E1, F1, G1, H1 = 0, 1, 2, 3, 4, 5, 6, 7
	A2, B2, C2, D2, E2, F2, G2, H2 = 8, 9, 10, 11, 12, 13, 14, 15
	A3, B3, C3, D3, E3, F3, G3, H3 = 16, 17, 18, 19, 20, 21, 22, 23
	A4, B4, C4, D4, E4, F4, G4, H4 = 24, 25, 26, 27, 28, 29, 30, 31
	A5, B5, C5, D5, E5, F5, G5, H5 = 32, 33, 34, 35, 36, 37, 38, 39
	A6, B6, C6, D6, E6, F6, G6, H6 = 40, 41, 42, 43, 44, 45, 46, 47
	A7, B7, C7, D7, E7, F7, G7, H7 = 48, 49, 50, 51, 52, 53, 54, 55
	A8, B8, C8, D8, E8, F8, G8, H8 = 56, 57, 58, 59, 60, 61, 62, 63
	NoSquare                        = -1
)

// Piece types
const (
	Pawn   = 0
	Knight = 1
	Bishop = 2
	Rook   = 3
	Queen  = 4
	King   = 5
)

// Colors
const (
	White = 0
	Black = 1
)

// Castling flags
const (
	WhiteKingSide  uint8 = 1
	WhiteQueenSide uint8 = 2
	BlackKingSide  uint8 = 4
	BlackQueenSide uint8 = 8
)

// Move flags (4 bits)
const (
	FlagQuiet      = 0
	FlagDoublePawn = 1
	FlagKingCastle = 2
	FlagQueenCastle = 3
	FlagCapture    = 4
	FlagEPCapture  = 5
	// Promotions: 8-11 quiet, 12-15 capture
	FlagKnightPromo        = 8
	FlagBishopPromo        = 9
	FlagRookPromo          = 10
	FlagQueenPromo         = 11
	FlagKnightPromoCapture = 12
	FlagBishopPromoCapture = 13
	FlagRookPromoCapture   = 14
	FlagQueenPromoCapture  = 15
)

// Move is a uint32 encoding: from(6) | to(6) | flags(4)
type Move uint32

func newMove(from, to, flags int) Move {
	return Move(from | (to << 6) | (flags << 12))
}

func (m Move) From() int  { return int(m) & 0x3F }
func (m Move) To() int    { return int(m>>6) & 0x3F }
func (m Move) Flags() int { return int(m>>12) & 0xF }

func (m Move) IsCapture() bool    { return m.Flags()&FlagCapture != 0 }
func (m Move) IsPromotion() bool  { return m.Flags()&0x8 != 0 }
func (m Move) IsEP() bool         { return m.Flags() == FlagEPCapture }
func (m Move) IsCastle() bool     { return m.Flags() == FlagKingCastle || m.Flags() == FlagQueenCastle }

func (m Move) PromoPiece() int {
	switch m.Flags() & 0x3 {
	case 0:
		return Knight
	case 1:
		return Bishop
	case 2:
		return Rook
	case 3:
		return Queen
	}
	return 0
}

// Position holds the full board state
type Position struct {
	Pieces         [2][6]uint64
	Occupied       [2]uint64
	AllOccupied    uint64
	Hash           uint64
	SideToMove     int
	CastlingRights uint8
	EnPassant      int // square index or NoSquare
	HalfMoveClock  int
	FullMoveNumber int
}

func (p *Position) updateOccupied() {
	p.Occupied[White] = p.Pieces[White][Pawn] | p.Pieces[White][Knight] |
		p.Pieces[White][Bishop] | p.Pieces[White][Rook] |
		p.Pieces[White][Queen] | p.Pieces[White][King]
	p.Occupied[Black] = p.Pieces[Black][Pawn] | p.Pieces[Black][Knight] |
		p.Pieces[Black][Bishop] | p.Pieces[Black][Rook] |
		p.Pieces[Black][Queen] | p.Pieces[Black][King]
	p.AllOccupied = p.Occupied[White] | p.Occupied[Black]
}

// ============================================================
// Section 2: Bitboard Utilities
// ============================================================

func popcount(b uint64) int    { return bits.OnesCount64(b) }
func lsb(b uint64) int         { return bits.TrailingZeros64(b) }

func popLSB(b *uint64) int {
	sq := lsb(*b)
	*b &= *b - 1
	return sq
}

func sqFile(sq int) int { return sq & 7 }
func sqRank(sq int) int { return sq >> 3 }

// File masks
var fileMask [8]uint64
var rankMask [8]uint64

func initMasks() {
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			fileMask[f] |= 1 << uint(r*8+f)
			rankMask[r] |= 1 << uint(r*8+f)
		}
	}
}

const (
	FileA uint64 = 0x0101010101010101
	FileB uint64 = 0x0202020202020202
	FileG uint64 = 0x4040404040404040
	FileH uint64 = 0x8080808080808080
)

// ============================================================
// Section 3: Attack Tables & Init
// ============================================================

var knightAttacks [64]uint64
var kingAttacks [64]uint64
var pawnAttacks [2][64]uint64

func initAttacks() {
	initMasks()

	// Knight attacks
	knightDeltas := [][2]int{{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2}, {1, -2}, {1, 2}, {2, -1}, {2, 1}}
	for sq := 0; sq < 64; sq++ {
		r, f := sqRank(sq), sqFile(sq)
		for _, d := range knightDeltas {
			nr, nf := r+d[0], f+d[1]
			if nr >= 0 && nr < 8 && nf >= 0 && nf < 8 {
				knightAttacks[sq] |= 1 << uint(nr*8+nf)
			}
		}
	}

	// King attacks
	kingDeltas := [][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	for sq := 0; sq < 64; sq++ {
		r, f := sqRank(sq), sqFile(sq)
		for _, d := range kingDeltas {
			nr, nf := r+d[0], f+d[1]
			if nr >= 0 && nr < 8 && nf >= 0 && nf < 8 {
				kingAttacks[sq] |= 1 << uint(nr*8+nf)
			}
		}
	}

	// Pawn attacks
	for sq := 0; sq < 64; sq++ {
		r, f := sqRank(sq), sqFile(sq)
		// White pawns attack up-left and up-right
		if r < 7 {
			if f > 0 {
				pawnAttacks[White][sq] |= 1 << uint((r+1)*8+(f-1))
			}
			if f < 7 {
				pawnAttacks[White][sq] |= 1 << uint((r+1)*8+(f+1))
			}
		}
		// Black pawns attack down-left and down-right
		if r > 0 {
			if f > 0 {
				pawnAttacks[Black][sq] |= 1 << uint((r-1)*8+(f-1))
			}
			if f < 7 {
				pawnAttacks[Black][sq] |= 1 << uint((r-1)*8+(f+1))
			}
		}
	}
}

// Zobrist hashing
var zobristPiece [2][6][64]uint64
var zobristSide uint64
var zobristCastling [16]uint64
var zobristEP [8]uint64

func initZobrist() {
	rng := rand.New(rand.NewSource(1070372))
	for c := 0; c < 2; c++ {
		for pc := 0; pc < 6; pc++ {
			for sq := 0; sq < 64; sq++ {
				zobristPiece[c][pc][sq] = rng.Uint64()
			}
		}
	}
	zobristSide = rng.Uint64()
	for i := 0; i < 16; i++ {
		zobristCastling[i] = rng.Uint64()
	}
	for i := 0; i < 8; i++ {
		zobristEP[i] = rng.Uint64()
	}
}

// ============================================================
// Section 4: Sliding Piece Attacks
// ============================================================

func bishopAttacks(sq int, occ uint64) uint64 {
	var attacks uint64
	directions := [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	for _, d := range directions {
		r, f := sqRank(sq)+d[0], sqFile(sq)+d[1]
		for r >= 0 && r < 8 && f >= 0 && f < 8 {
			bit := uint64(1) << uint(r*8+f)
			attacks |= bit
			if occ&bit != 0 {
				break
			}
			r += d[0]
			f += d[1]
		}
	}
	return attacks
}

func rookAttacks(sq int, occ uint64) uint64 {
	var attacks uint64
	directions := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, d := range directions {
		r, f := sqRank(sq)+d[0], sqFile(sq)+d[1]
		for r >= 0 && r < 8 && f >= 0 && f < 8 {
			bit := uint64(1) << uint(r*8+f)
			attacks |= bit
			if occ&bit != 0 {
				break
			}
			r += d[0]
			f += d[1]
		}
	}
	return attacks
}

func queenAttacks(sq int, occ uint64) uint64 {
	return bishopAttacks(sq, occ) | rookAttacks(sq, occ)
}

// ============================================================
// Section 5: Position Helpers
// ============================================================

func (p *Position) isSquareAttacked(sq int, byColor int) bool {
	// Pawn attacks: is sq attacked by a pawn of byColor?
	// If a White pawn on X attacks sq, then sq is in pawnAttacks[White][X].
	// Equivalently, X is in pawnAttacks[Black][sq] (reverse direction).
	if pawnAttacks[byColor^1][sq]&p.Pieces[byColor][Pawn] != 0 {
		return true
	}
	if knightAttacks[sq]&p.Pieces[byColor][Knight] != 0 {
		return true
	}
	if kingAttacks[sq]&p.Pieces[byColor][King] != 0 {
		return true
	}
	if bishopAttacks(sq, p.AllOccupied)&(p.Pieces[byColor][Bishop]|p.Pieces[byColor][Queen]) != 0 {
		return true
	}
	if rookAttacks(sq, p.AllOccupied)&(p.Pieces[byColor][Rook]|p.Pieces[byColor][Queen]) != 0 {
		return true
	}
	return false
}

func (p *Position) inCheck(color int) bool {
	kingBB := p.Pieces[color][King]
	if kingBB == 0 {
		return false
	}
	kingSq := lsb(kingBB)
	return p.isSquareAttacked(kingSq, color^1)
}

func (p *Position) pieceAt(sq int) (color int, piece int, found bool) {
	bit := uint64(1) << uint(sq)
	for c := 0; c < 2; c++ {
		if p.Occupied[c]&bit == 0 {
			continue
		}
		for pc := 0; pc < 6; pc++ {
			if p.Pieces[c][pc]&bit != 0 {
				return c, pc, true
			}
		}
	}
	return 0, 0, false
}

// ============================================================
// Section 6: FEN Parsing
// ============================================================

func parseFEN(fen string) Position {
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

	pos.updateOccupied()

	// Compute Zobrist hash
	for c := 0; c < 2; c++ {
		for pc := 0; pc < 6; pc++ {
			bb := pos.Pieces[c][pc]
			for bb != 0 {
				sq := popLSB(&bb)
				pos.Hash ^= zobristPiece[c][pc][sq]
			}
		}
	}
	pos.Hash ^= zobristCastling[pos.CastlingRights]
	if pos.EnPassant != NoSquare {
		pos.Hash ^= zobristEP[sqFile(pos.EnPassant)]
	}
	if pos.SideToMove == Black {
		pos.Hash ^= zobristSide
	}

	return pos
}

// ============================================================
// Section 7: Move Generation
// ============================================================

func (p *Position) generateMoves() []Move {
	moves := make([]Move, 0, 64)
	us := p.SideToMove
	them := us ^ 1
	ourPieces := p.Occupied[us]
	theirPieces := p.Occupied[them]
	allPieces := p.AllOccupied

	// Pawn moves
	pawns := p.Pieces[us][Pawn]
	for pawns != 0 {
		from := popLSB(&pawns)
		fromBit := uint64(1) << uint(from)
		rank := sqRank(from)
		file := sqFile(from)

		if us == White {
			// Single push
			toSq := from + 8
			if toSq < 64 && allPieces&(1<<uint(toSq)) == 0 {
				if rank == 6 { // promotion rank
					moves = append(moves, newMove(from, toSq, FlagQueenPromo))
					moves = append(moves, newMove(from, toSq, FlagRookPromo))
					moves = append(moves, newMove(from, toSq, FlagBishopPromo))
					moves = append(moves, newMove(from, toSq, FlagKnightPromo))
				} else {
					moves = append(moves, newMove(from, toSq, FlagQuiet))
					// Double push
					if rank == 1 {
						toSq2 := from + 16
						if allPieces&(1<<uint(toSq2)) == 0 {
							moves = append(moves, newMove(from, toSq2, FlagDoublePawn))
						}
					}
				}
			}
			// Captures
			attacks := pawnAttacks[White][from]
			captures := attacks & theirPieces
			for captures != 0 {
				to := popLSB(&captures)
				if rank == 6 {
					moves = append(moves, newMove(from, to, FlagQueenPromoCapture))
					moves = append(moves, newMove(from, to, FlagRookPromoCapture))
					moves = append(moves, newMove(from, to, FlagBishopPromoCapture))
					moves = append(moves, newMove(from, to, FlagKnightPromoCapture))
				} else {
					moves = append(moves, newMove(from, to, FlagCapture))
				}
			}
			// En passant
			if p.EnPassant != NoSquare && attacks&(1<<uint(p.EnPassant)) != 0 {
				moves = append(moves, newMove(from, p.EnPassant, FlagEPCapture))
			}
		} else { // Black
			// Single push
			toSq := from - 8
			if toSq >= 0 && allPieces&(1<<uint(toSq)) == 0 {
				if rank == 1 { // promotion rank
					moves = append(moves, newMove(from, toSq, FlagQueenPromo))
					moves = append(moves, newMove(from, toSq, FlagRookPromo))
					moves = append(moves, newMove(from, toSq, FlagBishopPromo))
					moves = append(moves, newMove(from, toSq, FlagKnightPromo))
				} else {
					moves = append(moves, newMove(from, toSq, FlagQuiet))
					// Double push
					if rank == 6 {
						toSq2 := from - 16
						if allPieces&(1<<uint(toSq2)) == 0 {
							moves = append(moves, newMove(from, toSq2, FlagDoublePawn))
						}
					}
				}
			}
			// Captures
			attacks := pawnAttacks[Black][from]
			captures := attacks & theirPieces
			for captures != 0 {
				to := popLSB(&captures)
				if rank == 1 {
					moves = append(moves, newMove(from, to, FlagQueenPromoCapture))
					moves = append(moves, newMove(from, to, FlagRookPromoCapture))
					moves = append(moves, newMove(from, to, FlagBishopPromoCapture))
					moves = append(moves, newMove(from, to, FlagKnightPromoCapture))
				} else {
					moves = append(moves, newMove(from, to, FlagCapture))
				}
			}
			// En passant
			if p.EnPassant != NoSquare && attacks&(1<<uint(p.EnPassant)) != 0 {
				moves = append(moves, newMove(from, p.EnPassant, FlagEPCapture))
			}
		}
		_ = fromBit
		_ = file
	}

	// Knight moves
	knights := p.Pieces[us][Knight]
	for knights != 0 {
		from := popLSB(&knights)
		attacks := knightAttacks[from] & ^ourPieces
		for attacks != 0 {
			to := popLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, newMove(from, to, FlagCapture))
			} else {
				moves = append(moves, newMove(from, to, FlagQuiet))
			}
		}
	}

	// Bishop moves
	bishops := p.Pieces[us][Bishop]
	for bishops != 0 {
		from := popLSB(&bishops)
		attacks := bishopAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := popLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, newMove(from, to, FlagCapture))
			} else {
				moves = append(moves, newMove(from, to, FlagQuiet))
			}
		}
	}

	// Rook moves
	rooks := p.Pieces[us][Rook]
	for rooks != 0 {
		from := popLSB(&rooks)
		attacks := rookAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := popLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, newMove(from, to, FlagCapture))
			} else {
				moves = append(moves, newMove(from, to, FlagQuiet))
			}
		}
	}

	// Queen moves
	queens := p.Pieces[us][Queen]
	for queens != 0 {
		from := popLSB(&queens)
		attacks := queenAttacks(from, allPieces) & ^ourPieces
		for attacks != 0 {
			to := popLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, newMove(from, to, FlagCapture))
			} else {
				moves = append(moves, newMove(from, to, FlagQuiet))
			}
		}
	}

	// King moves
	kingBB := p.Pieces[us][King]
	if kingBB != 0 {
		from := lsb(kingBB)
		attacks := kingAttacks[from] & ^ourPieces
		for attacks != 0 {
			to := popLSB(&attacks)
			if theirPieces&(1<<uint(to)) != 0 {
				moves = append(moves, newMove(from, to, FlagCapture))
			} else {
				moves = append(moves, newMove(from, to, FlagQuiet))
			}
		}

		// Castling
		if us == White {
			if p.CastlingRights&WhiteKingSide != 0 {
				// King on e1, rook on h1, f1 and g1 must be empty, e1/f1/g1 not attacked
				if from == E1 &&
					allPieces&(1<<F1|1<<G1) == 0 &&
					!p.isSquareAttacked(E1, them) &&
					!p.isSquareAttacked(F1, them) &&
					!p.isSquareAttacked(G1, them) {
					moves = append(moves, newMove(E1, G1, FlagKingCastle))
				}
			}
			if p.CastlingRights&WhiteQueenSide != 0 {
				if from == E1 &&
					allPieces&(1<<D1|1<<C1|1<<B1) == 0 &&
					!p.isSquareAttacked(E1, them) &&
					!p.isSquareAttacked(D1, them) &&
					!p.isSquareAttacked(C1, them) {
					moves = append(moves, newMove(E1, C1, FlagQueenCastle))
				}
			}
		} else {
			if p.CastlingRights&BlackKingSide != 0 {
				if from == E8 &&
					allPieces&(1<<F8|1<<G8) == 0 &&
					!p.isSquareAttacked(E8, them) &&
					!p.isSquareAttacked(F8, them) &&
					!p.isSquareAttacked(G8, them) {
					moves = append(moves, newMove(E8, G8, FlagKingCastle))
				}
			}
			if p.CastlingRights&BlackQueenSide != 0 {
				if from == E8 &&
					allPieces&(1<<D8|1<<C8|1<<B8) == 0 &&
					!p.isSquareAttacked(E8, them) &&
					!p.isSquareAttacked(D8, them) &&
					!p.isSquareAttacked(C8, them) {
					moves = append(moves, newMove(E8, C8, FlagQueenCastle))
				}
			}
		}
	}

	return moves
}

// generateLegalMoves returns only legal moves
func (p *Position) generateLegalMoves() []Move {
	pseudoLegal := p.generateMoves()
	legal := make([]Move, 0, len(pseudoLegal))
	for _, m := range pseudoLegal {
		newPos := p.makeMove(m)
		if !newPos.inCheck(p.SideToMove) {
			legal = append(legal, m)
		}
	}
	return legal
}

// ============================================================
// Section 8: Make Move
// ============================================================

func (p *Position) makeMove(m Move) Position {
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
	newPos.Hash ^= zobristPiece[us][movingPiece][from]
	newPos.Hash ^= zobristPiece[us][movingPiece][to]

	// Remove captured piece (if any, non-EP)
	if flags&FlagCapture != 0 && flags != FlagEPCapture {
		for pc := 0; pc < 6; pc++ {
			if newPos.Pieces[them][pc]&toBit != 0 {
				newPos.Pieces[them][pc] ^= toBit
				newPos.Hash ^= zobristPiece[them][pc][to]
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
		newPos.Hash ^= zobristPiece[us][Pawn][to]
		newPos.Hash ^= zobristPiece[us][promoPiece][to]
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
		newPos.Hash ^= zobristPiece[them][Pawn][capturedSq]
	}

	// Handle castling - move the rook
	if flags == FlagKingCastle {
		if us == White {
			newPos.Pieces[White][Rook] ^= (1 << H1) | (1 << F1)
			newPos.Hash ^= zobristPiece[White][Rook][H1]
			newPos.Hash ^= zobristPiece[White][Rook][F1]
		} else {
			newPos.Pieces[Black][Rook] ^= (1 << H8) | (1 << F8)
			newPos.Hash ^= zobristPiece[Black][Rook][H8]
			newPos.Hash ^= zobristPiece[Black][Rook][F8]
		}
	}
	if flags == FlagQueenCastle {
		if us == White {
			newPos.Pieces[White][Rook] ^= (1 << A1) | (1 << D1)
			newPos.Hash ^= zobristPiece[White][Rook][A1]
			newPos.Hash ^= zobristPiece[White][Rook][D1]
		} else {
			newPos.Pieces[Black][Rook] ^= (1 << A8) | (1 << D8)
			newPos.Hash ^= zobristPiece[Black][Rook][A8]
			newPos.Hash ^= zobristPiece[Black][Rook][D8]
		}
	}

	// Hash: XOR out old EP file if set
	if p.EnPassant != NoSquare {
		newPos.Hash ^= zobristEP[sqFile(p.EnPassant)]
	}

	// Update en passant square
	if flags == FlagDoublePawn {
		if us == White {
			newPos.EnPassant = from + 8
		} else {
			newPos.EnPassant = from - 8
		}
		// Hash: XOR in new EP file
		newPos.Hash ^= zobristEP[sqFile(newPos.EnPassant)]
	} else {
		newPos.EnPassant = NoSquare
	}

	// Hash: XOR out old castling rights
	newPos.Hash ^= zobristCastling[p.CastlingRights]

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
	newPos.Hash ^= zobristCastling[newPos.CastlingRights]

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
	newPos.Hash ^= zobristSide

	newPos.updateOccupied()
	return newPos
}

// ============================================================
// Section 9: Evaluation
// ============================================================

// Piece values (centipawns)
var pieceValue = [6]int{100, 320, 330, 500, 900, 0}

// Piece-square tables (from White's perspective, A1=index 0)
// For Black, mirror vertically via sq^56.

var pstPawn = [64]int{
	 0,  0,  0,  0,  0,  0,  0,  0,
	 5, 10, 10,-20,-20, 10, 10,  5,
	 5, -5,-10,  0,  0,-10, -5,  5,
	 0,  0,  0, 20, 20,  0,  0,  0,
	 5,  5, 10, 25, 25, 10,  5,  5,
	10, 10, 20, 30, 30, 20, 10, 10,
	50, 50, 50, 50, 50, 50, 50, 50,
	 0,  0,  0,  0,  0,  0,  0,  0,
}

var pstKnight = [64]int{
	-50,-40,-30,-30,-30,-30,-40,-50,
	-40,-20,  0,  5,  5,  0,-20,-40,
	-30,  0, 10, 15, 15, 10,  0,-30,
	-30,  5, 15, 20, 20, 15,  5,-30,
	-30,  0, 15, 20, 20, 15,  0,-30,
	-30,  5, 10, 15, 15, 10,  5,-30,
	-40,-20,  0,  0,  0,  0,-20,-40,
	-50,-40,-30,-30,-30,-30,-40,-50,
}

var pstBishop = [64]int{
	-20,-10,-10,-10,-10,-10,-10,-20,
	-10,  5,  0,  0,  0,  0,  5,-10,
	-10, 10, 10, 10, 10, 10, 10,-10,
	-10,  0, 10, 10, 10, 10,  0,-10,
	-10,  5,  5, 10, 10,  5,  5,-10,
	-10,  0,  5, 10, 10,  5,  0,-10,
	-10,  0,  0,  0,  0,  0,  0,-10,
	-20,-10,-10,-10,-10,-10,-10,-20,
}

var pstRook = [64]int{
	 0,  0,  0,  5,  5,  0,  0,  0,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	-5,  0,  0,  0,  0,  0,  0, -5,
	 5, 10, 10, 10, 10, 10, 10,  5,
	 0,  0,  0,  0,  0,  0,  0,  0,
}

var pstQueen = [64]int{
	-20,-10,-10, -5, -5,-10,-10,-20,
	-10,  0,  5,  0,  0,  0,  0,-10,
	-10,  5,  5,  5,  5,  5,  0,-10,
	  0,  0,  5,  5,  5,  5,  0, -5,
	 -5,  0,  5,  5,  5,  5,  0, -5,
	-10,  0,  5,  5,  5,  5,  0,-10,
	-10,  0,  0,  0,  0,  0,  0,-10,
	-20,-10,-10, -5, -5,-10,-10,-20,
}

var pstKing = [64]int{
	 20, 30, 10,  0,  0, 10, 30, 20,
	 20, 20,  0,  0,  0,  0, 20, 20,
	-10,-20,-20,-20,-20,-20,-20,-10,
	-20,-30,-30,-40,-40,-30,-30,-20,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
	-30,-40,-40,-50,-50,-40,-40,-30,
}

var pst = [6]*[64]int{&pstPawn, &pstKnight, &pstBishop, &pstRook, &pstQueen, &pstKing}

// evaluate returns the score in centipawns relative to the side to move.
func evaluate(pos *Position) int {
	score := 0
	for pc := 0; pc < 6; pc++ {
		// White pieces
		bb := pos.Pieces[White][pc]
		for bb != 0 {
			sq := popLSB(&bb)
			score += pieceValue[pc] + pst[pc][sq]
		}
		// Black pieces
		bb = pos.Pieces[Black][pc]
		for bb != 0 {
			sq := popLSB(&bb)
			score -= pieceValue[pc] + pst[pc][sq^56]
		}
	}
	if pos.SideToMove == Black {
		score = -score
	}
	return score
}

// ============================================================
// Section 10: Search
// ============================================================

const SearchInfinity = 30000
const MateScore = 29000

type SearchInfo struct {
	nodes    uint64
	stopTime time.Time
	stopped  int32
	bestMove Move
	history  []uint64
}

func (info *SearchInfo) isStopped() bool {
	return atomic.LoadInt32(&info.stopped) != 0
}

func (info *SearchInfo) setStop() {
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
	key   uint64
	move  Move
	score int16
	depth int8
	flag  uint8
}

const DefaultTTSize = 64 // MB

var tt []TTEntry
var ttMask uint64

func initTT(sizeMB int) {
	entries := (sizeMB * 1024 * 1024) / 16
	size := uint64(1)
	for size*2 <= uint64(entries) {
		size *= 2
	}
	tt = make([]TTEntry, size)
	ttMask = size - 1
}

func probeTT(key uint64) (TTEntry, bool) {
	entry := tt[key&ttMask]
	if entry.key == key {
		return entry, true
	}
	return entry, false
}

func storeTT(key uint64, move Move, score int16, depth int8, flag uint8) {
	tt[key&ttMask] = TTEntry{key: key, move: move, score: score, depth: depth, flag: flag}
}

// scoreMove assigns a sort score for move ordering (higher = search first).
func scoreMove(pos *Position, m Move, ttMove Move) int {
	if m == ttMove && ttMove != 0 {
		return 30000
	}
	score := 0
	if m.IsCapture() {
		var victimVal int
		if m.IsEP() {
			victimVal = pieceValue[Pawn]
		} else {
			_, vic, _ := pos.pieceAt(m.To())
			victimVal = pieceValue[vic]
		}
		_, attacker, _ := pos.pieceAt(m.From())
		attackerVal := pieceValue[attacker]
		score += 10000 + victimVal*10 - attackerVal
	}
	if m.IsPromotion() {
		score += 9000 + pieceValue[m.PromoPiece()]
	}
	return score
}

// pickMove does incremental selection sort: swaps the best-scored move into position i.
func pickMove(moves []Move, scores []int, i int) {
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

// quiescence searches captures and promotions to avoid the horizon effect.
func quiescence(pos *Position, alpha, beta int, info *SearchInfo) int {
	info.nodes++

	eval := evaluate(pos)
	if eval >= beta {
		return beta
	}
	if eval > alpha {
		alpha = eval
	}

	moves := pos.generateLegalMoves()
	// Filter to captures and promotions only
	tactical := moves[:0]
	for _, m := range moves {
		if m.IsCapture() || m.IsPromotion() {
			tactical = append(tactical, m)
		}
	}

	scores := make([]int, len(tactical))
	for i, m := range tactical {
		scores[i] = scoreMove(pos, m, 0)
	}

	for i := 0; i < len(tactical); i++ {
		if info.isStopped() {
			return 0
		}
		pickMove(tactical, scores, i)
		newPos := pos.makeMove(tactical[i])
		score := -quiescence(&newPos, -beta, -alpha, info)
		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}
	return alpha
}

// negamax is the core alpha-beta search.
func negamax(pos *Position, depth, ply, alpha, beta int, info *SearchInfo) int {
	if info.nodes&2047 == 0 && info.nodes > 0 {
		if time.Now().After(info.stopTime) {
			info.setStop()
			return 0
		}
	}

	// Repetition detection
	if ply > 0 {
		for _, h := range info.history {
			if h == pos.Hash {
				return 0
			}
		}
	}

	// Fifty-move rule
	if ply > 0 && pos.HalfMoveClock >= 100 {
		return 0
	}

	if depth <= 0 {
		return quiescence(pos, alpha, beta, info)
	}

	info.nodes++
	origAlpha := alpha

	// TT probe
	var ttMove Move
	entry, hit := probeTT(pos.Hash)
	if hit {
		ttMove = entry.move
		if int(entry.depth) >= depth {
			ttScore := int(entry.score)
			// Adjust mate scores from distance-from-root to distance-from-node
			if ttScore > MateScore-100 {
				ttScore -= ply
			} else if ttScore < -MateScore+100 {
				ttScore += ply
			}
			switch entry.flag {
			case TTExact:
				if ply == 0 {
					info.bestMove = entry.move
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
	inCheck := pos.inCheck(pos.SideToMove)
	if inCheck {
		depth++
	}

	// Null move pruning
	if ply > 0 && depth >= 3 && !inCheck {
		// Check for non-pawn material
		us := pos.SideToMove
		hasNonPawnMaterial := pos.Pieces[us][Knight]|pos.Pieces[us][Bishop]|
			pos.Pieces[us][Rook]|pos.Pieces[us][Queen] != 0
		if hasNonPawnMaterial {
			// Make null move: flip side, clear EP, update hash
			nullPos := *pos
			nullPos.SideToMove ^= 1
			nullPos.Hash ^= zobristSide
			if nullPos.EnPassant != NoSquare {
				nullPos.Hash ^= zobristEP[sqFile(nullPos.EnPassant)]
				nullPos.EnPassant = NoSquare
			}
			score := -negamax(&nullPos, depth-1-2, ply+1, -beta, -beta+1, info)
			if info.isStopped() {
				return 0
			}
			if score >= beta {
				return beta
			}
		}
	}

	moves := pos.generateLegalMoves()
	if len(moves) == 0 {
		if inCheck {
			return -MateScore + ply // checkmate
		}
		return 0 // stalemate
	}

	scores := make([]int, len(moves))
	for i, m := range moves {
		scores[i] = scoreMove(pos, m, ttMove)
	}

	bestScore := -SearchInfinity
	var bestMoveAtNode Move
	for i := 0; i < len(moves); i++ {
		if info.isStopped() {
			return 0
		}
		pickMove(moves, scores, i)
		newPos := pos.makeMove(moves[i])
		info.history = append(info.history, pos.Hash)
		score := -negamax(&newPos, depth-1, ply+1, -beta, -alpha, info)
		info.history = info.history[:len(info.history)-1]
		if score > bestScore {
			bestScore = score
			bestMoveAtNode = moves[i]
			if score > alpha {
				alpha = score
				if ply == 0 {
					info.bestMove = moves[i]
				}
			}
		}
		if score >= beta {
			break
		}
	}

	// Store in TT
	if !info.isStopped() {
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
		storeTT(pos.Hash, bestMoveAtNode, storeScore, int8(depth), flag)
	}

	return bestScore
}

// search runs iterative deepening. The caller sets up info (stopTime, history).
// Returns the best move found.
func search(pos *Position, maxDepth int, info *SearchInfo) Move {
	if maxDepth <= 0 {
		maxDepth = 64
	}

	var bestMove Move
	for depth := 1; depth <= maxDepth; depth++ {
		info.nodes = 0
		startTime := time.Now()
		score := negamax(pos, depth, 0, -SearchInfinity, SearchInfinity, info)
		if info.isStopped() {
			break
		}
		bestMove = info.bestMove
		elapsed := time.Since(startTime)
		elapsedMs := elapsed.Milliseconds()
		if elapsedMs == 0 {
			elapsedMs = 1
		}
		nps := info.nodes * 1000 / uint64(elapsedMs)

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
			depth, scoreStr, info.nodes, nps, elapsed.Milliseconds())
	}

	return bestMove
}

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

// ============================================================
// Section 16: Lichess Bot Integration
// ============================================================

const (
	lichessBaseURL       = "https://lichess.org"
	maxConcurrentGames   = 4
	moveOverheadMs       = 300
	eventStreamRetryWait = 5 * time.Second
)

// --- NDJSON types ---

type lichessEvent struct {
	Type      string           `json:"type"`
	Challenge *lichessChallenge `json:"challenge,omitempty"`
	Game      *lichessGameRef  `json:"game,omitempty"`
}

type lichessChallenge struct {
	ID          string              `json:"id"`
	Challenger  lichessPlayer       `json:"challenger"`
	Variant     lichessVariant      `json:"variant"`
	TimeControl lichessTimeControl  `json:"timeControl"`
	Rated       bool                `json:"rated"`
	Speed       string              `json:"speed"`
}

type lichessPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type lichessVariant struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type lichessTimeControl struct {
	Type      string `json:"type"`
	Limit     int    `json:"limit"`
	Increment int    `json:"increment"`
	Show      string `json:"show"`
}

type lichessGameRef struct {
	ID     string `json:"id"`
	GameID string `json:"gameId"`
}

type lichessGameFull struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	White      lichessGamePlayer `json:"white"`
	Black      lichessGamePlayer `json:"black"`
	Clock      *lichessClock     `json:"clock"`
	InitialFen string            `json:"initialFen"`
	State      lichessGameState  `json:"state"`
}

type lichessGamePlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type lichessClock struct {
	Initial   int `json:"initial"`
	Increment int `json:"increment"`
}

type lichessGameState struct {
	Type   string `json:"type"`
	Moves  string `json:"moves"`
	Wtime  int    `json:"wtime"`
	Btime  int    `json:"btime"`
	Winc   int    `json:"winc"`
	Binc   int    `json:"binc"`
	Status string `json:"status"`
}

type lichessChatLine struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Text     string `json:"text"`
	Room     string `json:"room"`
}

// --- Bot struct ---

type lichessBot struct {
	token      string
	botID      string
	httpClient *http.Client
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	activeGames map[string]context.CancelFunc
}

// --- HTTP helpers ---

func (bot *lichessBot) doRequest(method, path, body, contentType string) (*http.Response, error) {
	for {
		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}
		reqURL := lichessBaseURL + path
		req, err := http.NewRequestWithContext(bot.ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+bot.token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := bot.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			log.Println("[lichess] rate limited, waiting 60s")
			select {
			case <-time.After(60 * time.Second):
				continue
			case <-bot.ctx.Done():
				return nil, bot.ctx.Err()
			}
		}
		return resp, nil
	}
}

func streamNDJSON(resp *http.Response, handler func([]byte) bool) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue // keepalive
		}
		if !handler(line) {
			return
		}
	}
}

// --- API methods ---

func (bot *lichessBot) getAccount() (string, error) {
	resp, err := bot.doRequest("GET", "/api/account", "", "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var acct struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(data, &acct); err != nil {
		return "", err
	}
	bot.botID = strings.ToLower(acct.ID)
	return acct.Username, nil
}

func (bot *lichessBot) acceptChallenge(id string) {
	resp, err := bot.doRequest("POST", "/api/challenge/"+id+"/accept", "", "")
	if err != nil {
		log.Printf("[lichess] accept challenge %s error: %v\n", id, err)
		return
	}
	resp.Body.Close()
	log.Printf("[lichess] accepted challenge %s\n", id)
}

func (bot *lichessBot) declineChallenge(id, reason string) {
	form := url.Values{"reason": {reason}}.Encode()
	resp, err := bot.doRequest("POST", "/api/challenge/"+id+"/decline", form, "application/x-www-form-urlencoded")
	if err != nil {
		log.Printf("[lichess] decline challenge %s error: %v\n", id, err)
		return
	}
	resp.Body.Close()
	log.Printf("[lichess] declined challenge %s (reason: %s)\n", id, reason)
}

func (bot *lichessBot) postMove(gameID, move string) {
	resp, err := bot.doRequest("POST", "/api/bot/game/"+gameID+"/move/"+move, "", "")
	if err != nil {
		log.Printf("[game %s] post move %s error: %v\n", gameID, move, err)
		return
	}
	resp.Body.Close()
}

func (bot *lichessBot) sendChat(gameID, room, text string) {
	form := url.Values{"room": {room}, "text": {text}}.Encode()
	resp, err := bot.doRequest("POST", "/api/bot/game/"+gameID+"/chat", form, "application/x-www-form-urlencoded")
	if err != nil {
		log.Printf("[game %s] send chat error: %v\n", gameID, err)
		return
	}
	resp.Body.Close()
}

func (bot *lichessBot) resign(gameID string) {
	resp, err := bot.doRequest("POST", "/api/bot/game/"+gameID+"/resign", "", "")
	if err != nil {
		log.Printf("[game %s] resign error: %v\n", gameID, err)
		return
	}
	resp.Body.Close()
}

// --- Challenge filter ---

func (bot *lichessBot) shouldAcceptChallenge(ch *lichessChallenge) (bool, string) {
	if ch.Variant.Key != "standard" {
		return false, "variant"
	}
	bot.mu.Lock()
	count := len(bot.activeGames)
	bot.mu.Unlock()
	if count >= maxConcurrentGames {
		return false, "later"
	}
	return true, ""
}

// --- Position setup + time computation ---

func buildPosition(initialFen, movesStr string) (Position, []uint64) {
	fen := initialFen
	if fen == "" || fen == "startpos" {
		fen = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
	}
	pos := parseFEN(fen)
	history := []uint64{pos.Hash}

	if movesStr == "" {
		return pos, history
	}

	moves := strings.Fields(movesStr)
	for _, uci := range moves {
		m, ok := parseUCIMove(&pos, uci)
		if !ok {
			log.Printf("[lichess] failed to parse move: %s\n", uci)
			break
		}
		pos = pos.makeMove(m)
		history = append(history, pos.Hash)
	}
	return pos, history
}

func computeSearchTime(wtime, btime, winc, binc, botColor int) int {
	var timeLeft, inc int
	if botColor == White {
		timeLeft = wtime
		inc = winc
	} else {
		timeLeft = btime
		inc = binc
	}

	if timeLeft <= 0 {
		return 50
	}

	movesLeft := 30
	allocatedTime := timeLeft/movesLeft + inc*70/100

	// Hard cap: 30% of remaining time
	hardCap := timeLeft * 30 / 100
	if allocatedTime > hardCap {
		allocatedTime = hardCap
	}

	// Subtract network overhead
	allocatedTime -= moveOverheadMs

	if allocatedTime < 50 {
		allocatedTime = 50
	}
	return allocatedTime
}

// --- Game goroutine ---

func (bot *lichessBot) playGame(gameID string) {
	gameCtx, gameCancel := context.WithCancel(bot.ctx)
	defer gameCancel()

	bot.mu.Lock()
	bot.activeGames[gameID] = gameCancel
	bot.mu.Unlock()

	defer func() {
		bot.mu.Lock()
		delete(bot.activeGames, gameID)
		bot.mu.Unlock()
		log.Printf("[game %s] ended\n", gameID)
	}()

	log.Printf("[game %s] connecting to game stream\n", gameID)

	resp, err := bot.doRequest("GET", "/api/bot/game/stream/"+gameID, "", "")
	if err != nil {
		log.Printf("[game %s] stream error: %v\n", gameID, err)
		return
	}

	var botColor int
	var gameFull lichessGameFull
	var gotFull bool
	var currentSearch *SearchInfo

	stateCh := make(chan lichessGameState, 8)

	// State processor goroutine
	go func() {
		for {
			select {
			case <-gameCtx.Done():
				return
			case state, ok := <-stateCh:
				if !ok {
					return
				}
				// Stop any running search
				if currentSearch != nil {
					currentSearch.setStop()
				}

				if state.Status != "started" {
					log.Printf("[game %s] game over: %s\n", gameID, state.Status)
					return
				}

				pos, history := buildPosition(gameFull.InitialFen, state.Moves)

				if pos.SideToMove != botColor {
					continue
				}

				allocatedTime := computeSearchTime(state.Wtime, state.Btime, state.Winc, state.Binc, botColor)

				info := &SearchInfo{}
				info.stopTime = time.Now().Add(time.Duration(allocatedTime) * time.Millisecond)
				if len(history) > 1 {
					info.history = make([]uint64, len(history)-1)
					copy(info.history, history[:len(history)-1])
				}
				currentSearch = info

				bestMove := search(&pos, 0, info)
				currentSearch = nil

				if bestMove == 0 {
					log.Printf("[game %s] no legal move found, resigning\n", gameID)
					bot.resign(gameID)
					return
				}
				moveStr := moveToString(bestMove)
				log.Printf("[game %s] playing %s\n", gameID, moveStr)
				bot.postMove(gameID, moveStr)
			}
		}
	}()

	streamNDJSON(resp, func(line []byte) bool {
		select {
		case <-gameCtx.Done():
			return false
		default:
		}

		// First line is gameFull, subsequent lines are gameState or chatLine
		if !gotFull {
			if err := json.Unmarshal(line, &gameFull); err != nil {
				log.Printf("[game %s] parse gameFull error: %v\n", gameID, err)
				return false
			}
			gotFull = true

			// Determine bot color
			if strings.ToLower(gameFull.Black.ID) == bot.botID {
				botColor = Black
				log.Printf("[game %s] playing as Black vs %s\n", gameID, gameFull.White.Name)
			} else {
				botColor = White
				log.Printf("[game %s] playing as White vs %s\n", gameID, gameFull.Black.Name)
			}

			bot.sendChat(gameID, "player", "Good luck! I'm VietCoffee, a chess engine.")

			// Process the initial state
			stateCh <- gameFull.State
			return true
		}

		// Determine type
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			return true
		}

		switch msg.Type {
		case "gameState":
			var state lichessGameState
			if err := json.Unmarshal(line, &state); err != nil {
				log.Printf("[game %s] parse gameState error: %v\n", gameID, err)
				return true
			}
			stateCh <- state
		case "chatLine":
			// ignore chat
		case "gameFull":
			// Reconnect scenario: re-parse
			if err := json.Unmarshal(line, &gameFull); err != nil {
				log.Printf("[game %s] re-parse gameFull error: %v\n", gameID, err)
				return true
			}
			stateCh <- gameFull.State
		}

		return true
	})

	// Stream closed, stop any running search
	if currentSearch != nil {
		currentSearch.setStop()
	}
	close(stateCh)
}

// --- Event stream ---

func (bot *lichessBot) runEventStream() {
	for {
		select {
		case <-bot.ctx.Done():
			return
		default:
		}

		log.Println("[lichess] connecting to event stream")
		resp, err := bot.doRequest("GET", "/api/stream/event", "", "")
		if err != nil {
			log.Printf("[lichess] event stream error: %v\n", err)
			select {
			case <-time.After(eventStreamRetryWait):
				continue
			case <-bot.ctx.Done():
				return
			}
		}

		streamNDJSON(resp, func(line []byte) bool {
			select {
			case <-bot.ctx.Done():
				return false
			default:
			}

			var event lichessEvent
			if err := json.Unmarshal(line, &event); err != nil {
				log.Printf("[lichess] parse event error: %v\n", err)
				return true
			}

			switch event.Type {
			case "challenge":
				if event.Challenge != nil {
					ok, reason := bot.shouldAcceptChallenge(event.Challenge)
					if ok {
						bot.acceptChallenge(event.Challenge.ID)
					} else {
						bot.declineChallenge(event.Challenge.ID, reason)
					}
				}
			case "gameStart":
				if event.Game != nil {
					gameID := event.Game.GameID
					if gameID == "" {
						gameID = event.Game.ID
					}
					bot.mu.Lock()
					_, active := bot.activeGames[gameID]
					bot.mu.Unlock()
					if !active {
						go bot.playGame(gameID)
					}
				}
			case "gameFinish":
				if event.Game != nil {
					gameID := event.Game.GameID
					if gameID == "" {
						gameID = event.Game.ID
					}
					bot.mu.Lock()
					if cancel, ok := bot.activeGames[gameID]; ok {
						cancel()
						delete(bot.activeGames, gameID)
					}
					bot.mu.Unlock()
				}
			case "challengeCanceled":
				if event.Challenge != nil {
					log.Printf("[lichess] challenge %s canceled\n", event.Challenge.ID)
				}
			case "challengeDeclined":
				if event.Challenge != nil {
					log.Printf("[lichess] challenge %s declined\n", event.Challenge.ID)
				}
			}

			return true
		})

		log.Println("[lichess] event stream disconnected, reconnecting...")
		select {
		case <-time.After(eventStreamRetryWait):
		case <-bot.ctx.Done():
			return
		}
	}
}

// --- Shutdown ---

func (bot *lichessBot) waitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("[lichess] received signal %v, shutting down\n", sig)
	case <-bot.ctx.Done():
		return
	}

	bot.cancel()

	// Wait up to 10s for active games
	deadline := time.After(10 * time.Second)
	for {
		bot.mu.Lock()
		count := len(bot.activeGames)
		bot.mu.Unlock()
		if count == 0 {
			break
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-deadline:
			log.Printf("[lichess] shutdown timeout, %d games still active\n", count)
			return
		}
	}
	log.Println("[lichess] clean shutdown complete")
}

// --- Entry point ---

func lichessMain() {
	token := os.Getenv("LICHESS_TOKEN")
	if token == "" {
		log.Fatal("[lichess] LICHESS_TOKEN environment variable is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	bot := &lichessBot{
		token:       token,
		httpClient:  &http.Client{Timeout: 0},
		ctx:         ctx,
		cancel:      cancel,
		activeGames: make(map[string]context.CancelFunc),
	}

	username, err := bot.getAccount()
	if err != nil {
		log.Fatalf("[lichess] failed to get account: %v\n", err)
	}
	log.Printf("[lichess] logged in as: %s\n", username)

	go bot.waitForShutdown()

	bot.runEventStream()
}
