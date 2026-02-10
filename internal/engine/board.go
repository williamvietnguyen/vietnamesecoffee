package engine

import (
	"math/bits"
	"math/rand"
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

func NewMove(from, to, flags int) Move {
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

func (p *Position) UpdateOccupied() {
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

func Popcount(b uint64) int    { return bits.OnesCount64(b) }
func Lsb(b uint64) int         { return bits.TrailingZeros64(b) }

func PopLSB(b *uint64) int {
	sq := Lsb(*b)
	*b &= *b - 1
	return sq
}

func SqFile(sq int) int { return sq & 7 }
func SqRank(sq int) int { return sq >> 3 }

// File masks
var FileMask [8]uint64
var RankMask [8]uint64

func initMasks() {
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			FileMask[f] |= 1 << uint(r*8+f)
			RankMask[r] |= 1 << uint(r*8+f)
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

var KnightAttacks [64]uint64
var KingAttacks [64]uint64
var PawnAttacks [2][64]uint64

// Magic bitboard structures
type sqMagic struct {
	mask  uint64
	magic uint64
	shift uint
	table []uint64
}

var bishopMagics [64]sqMagic
var rookMagics [64]sqMagic

// Hardcoded magic numbers from dragontoothmg (LERF A1=0)
var bishopMagicNumbers = [64]uint64{
	0x0002020202020200, 0x0002020202020000, 0x0004010202000000, 0x0004040080000000,
	0x0001104000000000, 0x0000821040000000, 0x0000410410400000, 0x0000104104104000,
	0x0000040404040400, 0x0000020202020200, 0x0000040102020000, 0x0000040400800000,
	0x0000011040000000, 0x0000008210400000, 0x0000004104104000, 0x0000002082082000,
	0x0004000808080800, 0x0002000404040400, 0x0001000202020200, 0x0000800802004000,
	0x0000800400A00000, 0x0000200100884000, 0x0000400082082000, 0x0000200041041000,
	0x0002080010101000, 0x0001040008080800, 0x0000208004010400, 0x0000404004010200,
	0x0000840000802000, 0x0000404002011000, 0x0000808001041000, 0x0000404000820800,
	0x0001041000202000, 0x0000820800101000, 0x0000104400080800, 0x0000020080080080,
	0x0000404040040100, 0x0000808100020100, 0x0001010100020800, 0x0000808080010400,
	0x0000820820004000, 0x0000410410002000, 0x0000082088001000, 0x0000002011000800,
	0x0000080100400400, 0x0001010101000200, 0x0002020202000400, 0x0001010101000200,
	0x0000410410400000, 0x0000208208200000, 0x0000002084100000, 0x0000000020880000,
	0x0000001002020000, 0x0000040408020000, 0x0004040404040000, 0x0002020202020000,
	0x0000104104104000, 0x0000002082082000, 0x0000000020841000, 0x0000000000208800,
	0x0000000010020200, 0x0000000404080200, 0x0000040404040400, 0x0002020202020200,
}

var bishopMagicShifts = [64]uint{
	58, 59, 59, 59, 59, 59, 59, 58,
	59, 59, 59, 59, 59, 59, 59, 59,
	59, 59, 57, 57, 57, 57, 59, 59,
	59, 59, 57, 55, 55, 57, 59, 59,
	59, 59, 57, 55, 55, 57, 59, 59,
	59, 59, 57, 57, 57, 57, 59, 59,
	59, 59, 59, 59, 59, 59, 59, 59,
	58, 59, 59, 59, 59, 59, 59, 58,
}

var rookMagicNumbers = [64]uint64{
	0x0080001020400080, 0x0040001000200040, 0x0080081000200080, 0x0080040800100080,
	0x0080020400080080, 0x0080010200040080, 0x0080008001000200, 0x0080002040800100,
	0x0000800020400080, 0x0000400020005000, 0x0000801000200080, 0x0000800800100080,
	0x0000800400080080, 0x0000800200040080, 0x0000800100020080, 0x0000800040800100,
	0x0000208000400080, 0x0000404000201000, 0x0000808010002000, 0x0000808008001000,
	0x0000808004000800, 0x0000808002000400, 0x0000010100020004, 0x0000020000408104,
	0x0000208080004000, 0x0000200040005000, 0x0000100080200080, 0x0000080080100080,
	0x0000040080080080, 0x0000020080040080, 0x0000010080800200, 0x0000800080004100,
	0x0000204000800080, 0x0000200040401000, 0x0000100080802000, 0x0000080080801000,
	0x0000040080800800, 0x0000020080800400, 0x0000020001010004, 0x0000800040800100,
	0x0000204000808000, 0x0000200040008080, 0x0000100020008080, 0x0000080010008080,
	0x0000040008008080, 0x0000020004008080, 0x0000010002008080, 0x0000004081020004,
	0x0000204000800080, 0x0000200040008080, 0x0000100020008080, 0x0000080010008080,
	0x0000040008008080, 0x0000020004008080, 0x0000800100020080, 0x0000800041000080,
	0x00FFFCDDFCED714A, 0x007FFCDDFCED714A, 0x003FFFCDFFD88096, 0x0000040810002101,
	0x0001000204080011, 0x0001000204000801, 0x0001000082000401, 0x0001FFFAABFAD1A2,
}

var rookMagicShifts = [64]uint{
	52, 53, 53, 53, 53, 53, 53, 52,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 54, 54, 54, 54, 53,
	53, 54, 54, 53, 53, 53, 53, 53,
}

func InitAttacks() {
	initMasks()

	// Knight attacks
	knightDeltas := [][2]int{{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2}, {1, -2}, {1, 2}, {2, -1}, {2, 1}}
	for sq := 0; sq < 64; sq++ {
		r, f := SqRank(sq), SqFile(sq)
		for _, d := range knightDeltas {
			nr, nf := r+d[0], f+d[1]
			if nr >= 0 && nr < 8 && nf >= 0 && nf < 8 {
				KnightAttacks[sq] |= 1 << uint(nr*8+nf)
			}
		}
	}

	// King attacks
	kingDeltas := [][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	for sq := 0; sq < 64; sq++ {
		r, f := SqRank(sq), SqFile(sq)
		for _, d := range kingDeltas {
			nr, nf := r+d[0], f+d[1]
			if nr >= 0 && nr < 8 && nf >= 0 && nf < 8 {
				KingAttacks[sq] |= 1 << uint(nr*8+nf)
			}
		}
	}

	// Pawn attacks
	for sq := 0; sq < 64; sq++ {
		r, f := SqRank(sq), SqFile(sq)
		// White pawns attack up-left and up-right
		if r < 7 {
			if f > 0 {
				PawnAttacks[White][sq] |= 1 << uint((r+1)*8+(f-1))
			}
			if f < 7 {
				PawnAttacks[White][sq] |= 1 << uint((r+1)*8+(f+1))
			}
		}
		// Black pawns attack down-left and down-right
		if r > 0 {
			if f > 0 {
				PawnAttacks[Black][sq] |= 1 << uint((r-1)*8+(f-1))
			}
			if f < 7 {
				PawnAttacks[Black][sq] |= 1 << uint((r-1)*8+(f+1))
			}
		}
	}

	initMagics()
}

// Zobrist hashing
var ZobristPiece [2][6][64]uint64
var ZobristSide uint64
var ZobristCastling [16]uint64
var ZobristEP [8]uint64

func InitZobrist() {
	rng := rand.New(rand.NewSource(1070372))
	for c := 0; c < 2; c++ {
		for pc := 0; pc < 6; pc++ {
			for sq := 0; sq < 64; sq++ {
				ZobristPiece[c][pc][sq] = rng.Uint64()
			}
		}
	}
	ZobristSide = rng.Uint64()
	for i := 0; i < 16; i++ {
		ZobristCastling[i] = rng.Uint64()
	}
	for i := 0; i < 8; i++ {
		ZobristEP[i] = rng.Uint64()
	}
}

// ============================================================
// Section 4: Sliding Piece Attacks
// ============================================================

func bishopAttacksSlow(sq int, occ uint64) uint64 {
	var attacks uint64
	directions := [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	for _, d := range directions {
		r, f := SqRank(sq)+d[0], SqFile(sq)+d[1]
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

func rookAttacksSlow(sq int, occ uint64) uint64 {
	var attacks uint64
	directions := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, d := range directions {
		r, f := SqRank(sq)+d[0], SqFile(sq)+d[1]
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

// bishopRelevantOccupancy returns the mask of squares whose occupancy
// affects bishop attacks from sq, excluding edge squares.
func bishopRelevantOccupancy(sq int) uint64 {
	var mask uint64
	r, f := SqRank(sq), SqFile(sq)
	for dr, df := 1, 1; r+dr < 7 && f+df < 7; dr, df = dr+1, df+1 {
		mask |= 1 << uint((r+dr)*8+(f+df))
	}
	for dr, df := 1, -1; r+dr < 7 && f+df > 0; dr, df = dr+1, df-1 {
		mask |= 1 << uint((r+dr)*8+(f+df))
	}
	for dr, df := -1, 1; r+dr > 0 && f+df < 7; dr, df = dr-1, df+1 {
		mask |= 1 << uint((r+dr)*8+(f+df))
	}
	for dr, df := -1, -1; r+dr > 0 && f+df > 0; dr, df = dr-1, df-1 {
		mask |= 1 << uint((r+dr)*8+(f+df))
	}
	return mask
}

// rookRelevantOccupancy returns the mask of squares whose occupancy
// affects rook attacks from sq, excluding edge squares.
func rookRelevantOccupancy(sq int) uint64 {
	var mask uint64
	r, f := SqRank(sq), SqFile(sq)
	for i := r + 1; i < 7; i++ {
		mask |= 1 << uint(i*8+f)
	}
	for i := r - 1; i > 0; i-- {
		mask |= 1 << uint(i*8+f)
	}
	for i := f + 1; i < 7; i++ {
		mask |= 1 << uint(r*8+i)
	}
	for i := f - 1; i > 0; i-- {
		mask |= 1 << uint(r*8+i)
	}
	return mask
}

func initMagics() {
	for sq := 0; sq < 64; sq++ {
		// Bishop
		bm := &bishopMagics[sq]
		bm.mask = bishopRelevantOccupancy(sq)
		bm.magic = bishopMagicNumbers[sq]
		bm.shift = bishopMagicShifts[sq]
		bm.table = make([]uint64, 1<<(64-bm.shift))

		// Carry-Rippler enumeration of all subsets of mask
		occ := uint64(0)
		for {
			idx := (occ * bm.magic) >> bm.shift
			bm.table[idx] = bishopAttacksSlow(sq, occ)
			occ = (occ - bm.mask) & bm.mask
			if occ == 0 {
				break
			}
		}

		// Rook
		rm := &rookMagics[sq]
		rm.mask = rookRelevantOccupancy(sq)
		rm.magic = rookMagicNumbers[sq]
		rm.shift = rookMagicShifts[sq]
		rm.table = make([]uint64, 1<<(64-rm.shift))

		occ = 0
		for {
			idx := (occ * rm.magic) >> rm.shift
			rm.table[idx] = rookAttacksSlow(sq, occ)
			occ = (occ - rm.mask) & rm.mask
			if occ == 0 {
				break
			}
		}
	}
}

// BishopAttacks returns bishop attack bitboard using magic bitboards.
func BishopAttacks(sq int, occ uint64) uint64 {
	m := &bishopMagics[sq]
	return m.table[((occ&m.mask)*m.magic)>>m.shift]
}

// RookAttacks returns rook attack bitboard using magic bitboards.
func RookAttacks(sq int, occ uint64) uint64 {
	m := &rookMagics[sq]
	return m.table[((occ&m.mask)*m.magic)>>m.shift]
}

func QueenAttacks(sq int, occ uint64) uint64 {
	return BishopAttacks(sq, occ) | RookAttacks(sq, occ)
}

// ============================================================
// Section 5: Position Helpers
// ============================================================

func (p *Position) IsSquareAttacked(sq int, byColor int) bool {
	// Pawn attacks: is sq attacked by a pawn of byColor?
	// If a White pawn on X attacks sq, then sq is in PawnAttacks[White][X].
	// Equivalently, X is in PawnAttacks[Black][sq] (reverse direction).
	if PawnAttacks[byColor^1][sq]&p.Pieces[byColor][Pawn] != 0 {
		return true
	}
	if KnightAttacks[sq]&p.Pieces[byColor][Knight] != 0 {
		return true
	}
	if KingAttacks[sq]&p.Pieces[byColor][King] != 0 {
		return true
	}
	if BishopAttacks(sq, p.AllOccupied)&(p.Pieces[byColor][Bishop]|p.Pieces[byColor][Queen]) != 0 {
		return true
	}
	if RookAttacks(sq, p.AllOccupied)&(p.Pieces[byColor][Rook]|p.Pieces[byColor][Queen]) != 0 {
		return true
	}
	return false
}

func (p *Position) InCheck(color int) bool {
	kingBB := p.Pieces[color][King]
	if kingBB == 0 {
		return false
	}
	kingSq := Lsb(kingBB)
	return p.IsSquareAttacked(kingSq, color^1)
}

func (p *Position) PieceAt(sq int) (color int, piece int, found bool) {
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
