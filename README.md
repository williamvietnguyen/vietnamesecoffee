# VietCoffee

A chess engine written in Go, tuned to play aggressively. Zero dependencies, stdlib only.

VietCoffee speaks the UCI protocol for use with chess GUIs, and includes a built-in Lichess Bot API client for playing live games on lichess.org.

## Building

Requires Go 1.21 or later.

```
go build -o viet_coffee ./cmd/vietnamesecoffee
```

Or run directly:

```
go run ./cmd/vietnamesecoffee
```

## Usage

### UCI mode (default)

```
./viet_coffee
```

Launches a UCI-compliant engine on stdin/stdout. Connect it to any UCI-compatible GUI (Arena, CuteChess, BanksiaGUI, etc.).

Supported UCI commands:

| Command | Description |
|---|---|
| `uci` | Print engine ID and `uciok` |
| `isready` | Respond `readyok` |
| `ucinewgame` | Reset position and clear transposition table |
| `position startpos [moves ...]` | Set position from starting position with optional move list |
| `position fen <fen> [moves ...]` | Set position from FEN with optional move list |
| `go depth <n>` | Search to a fixed depth |
| `go movetime <ms>` | Search for a fixed time in milliseconds |
| `go wtime <ms> btime <ms> [winc <ms>] [binc <ms>] [movestogo <n>]` | Search with clock management |
| `go infinite` | Search until `stop` |
| `go perft <n>` | Run perft (move path enumeration) to depth n with divide output |
| `stop` | Halt the current search and print best move |
| `d` | Print an ASCII board diagram (debug) |
| `quit` | Exit |

### Lichess Bot mode

```
LICHESS_TOKEN=lip_xxxx ./viet_coffee --lichess
```

Connects to the Lichess Bot API and plays live games. See [Lichess Bot Setup](#lichess-bot-setup) below.

### Perft suite

```
./viet_coffee --perft
```

Runs a built-in suite of 6 standard perft positions (startpos, Kiwipete, positions 3-6) and verifies node counts at each depth. Also available via `--bench`.

## Engine Features

### Board representation

- **Bitboards** -- one `uint64` per piece type per color, with precomputed attack tables for knights, kings, and pawns. Sliding piece attacks (bishop, rook, queen) use **magic bitboards** for O(1) lookup-based attack generation.

### Move generation

- **Pseudo-legal + legality filter** -- generates all pseudo-legal moves (quiet, captures, double pawn pushes, en passant, promotions to all four piece types, kingside and queenside castling), then filters by making each move and checking if the king is left in check.

### Search

- **Iterative deepening with aspiration windows** -- searches depth 1, 2, 3, ... until time runs out or the max depth is reached. After depth 4, uses aspiration windows (narrow alpha-beta bounds around the previous depth's score) for faster cutoffs. Falls back to a full window if the score lands outside the aspiration bounds.
- **Negamax with alpha-beta pruning** -- standard negamax framework with fail-soft alpha-beta window.
- **Principal Variation Search (PVS)** -- the first move (expected best) is searched with a full window. All subsequent moves are searched with a null (zero) window — if any scores above alpha, it's re-searched with the full window. Reduces node count significantly in well-ordered trees.
- **Late Move Reductions (LMR)** -- quiet moves searched after the first 3 are reduced by 1 ply (2 plies after the 6th move). If a reduced search returns a score above alpha, the move is re-searched at full depth. Combined with PVS, this lets the engine reach 3-5 plies deeper in the same time.
- **Transposition table** -- 256 MB hash table (power-of-two sized, lockless indexing). Stores best move, score, depth, and bound type (exact, upper, lower). Used for both move ordering and search cutoffs. Mate scores are adjusted for distance from root on store/probe.
- **Null move pruning** -- if the side to move has non-pawn material and is not in check, tries passing the turn (R=2 reduction) to get a beta cutoff without searching all moves. **Disabled when attacking** — when 3+ pieces are attacking the enemy king zone, NMP is skipped so the engine doesn't prune away sacrifice lines.
- **Check extensions** -- extends search depth by 1 when the side to move is in check, avoiding horizon-effect blunders.
- **Sacrifice extensions** -- when a move sacrifices material (moving piece worth more than captured piece) and the sacrificing side has 2+ pieces attacking the enemy king zone, extends search by 1 ply. This lets the engine "see through" piece sacrifices to find checkmates and winning attacks, enabling Tal-like combinational play.
- **Quiescence search** -- at depth 0, continues searching captures and promotions until a quiet position is reached, preventing the engine from stopping at a position mid-exchange.
- **Move ordering** -- TT move first, then MVV-LVA (most valuable victim, least valuable attacker) for captures, then promotion bonus, then checking moves, then killer moves. Quiet moves that give check get a +5000 bonus so the engine explores forcing/combinational lines before passive moves. Uses incremental selection sort for lazy move ordering.
- **Killer moves** -- two killer move slots per ply. When a quiet move (non-capture, non-promotion) causes a beta cutoff, it's stored as a killer. Killers from the same ply are tried before other quiet moves, improving move ordering in non-tactical positions.
- **Contempt factor** -- draws from repetition or the fifty-move rule are scored at -80 cp instead of 0. The engine treats a draw as being nearly a pawn down, forcing it to take big risks rather than accept a draw. Stalemate (a forced draw) is unaffected.
- **Repetition detection** -- tracks Zobrist hash history across the game and within the search tree. Returns contempt-penalized score on repetition.
- **Fifty-move rule** -- returns contempt-penalized score when the half-move clock reaches 100.
- **Atomic stop** -- search can be halted instantly from another goroutine via an atomic flag. Time checks occur every 2048 nodes.
- **Concurrent search** -- the UCI `go` command launches search in a goroutine; `stop` halts it immediately.

### Evaluation

VietCoffee is tuned for **aggressive, attacking play** and uses **tapered evaluation** to smoothly interpolate between middlegame and endgame scoring.

**Tapered eval:** Every eval term produces separate middlegame (mg) and endgame (eg) scores. A game phase is computed from remaining non-pawn material (knight=1, bishop=1, rook=2, queen=4, max=24). The final score interpolates: `score = (mg * (24 - phase) + eg * phase) / 24`. In the opening/middlegame, the mg piece-square tables and attack bonuses dominate; as pieces come off the board, the eg values take over — the king centralizes, passed pawns become critical, and rook activity scales up.

**Material values (tuned for attacking style):**

|  | Middlegame | Endgame | Notes |
|---|---|---|---|
| Pawn (opponent's) | 100 cp | 120 cp | Pawns worth more in endgame (they promote!) |
| Pawn (own) | 90 cp | 120 cp | Own pawns cheap to sacrifice in mg for open lines |
| Knight | 340 cp | 340 cp | Boosted +20 over typical 320 |
| Bishop | 350 cp | 350 cp | Boosted +20 over typical 330 |
| Rook | 490 cp | 530 cp | Slightly devalued in mg; rooks dominate open endgame boards |
| Queen | 900 cp | 1000 cp | Queens worth more in endgame |

Knights and bishops are overvalued to encourage piece activity and tactical sacrifices. Rooks are slightly undervalued in the middlegame to de-emphasize slow endgame grinding. Own pawns are deliberately undervalued in the middlegame (90 cp vs 100 cp for opponent's) so the engine will happily sacrifice pawns to open files, create passed pawns, or accelerate attacks. The asymmetric pawn value is a middlegame-only concept — in the endgame, pawns are valued equally for both sides.

**Piece-square tables (separate mg and eg):**

*Middlegame PSTs* — aggressive tuning encourages:
- Forward pawn advances (especially kingside pawns)
- Central and advanced knight placement
- Active bishop diagonals
- Early piece development
- King safety in the corner (behind a pawn shield)

*Endgame PSTs* — the key difference from middlegame:
- **King centralizes** — big bonuses for center squares instead of hiding in corners. This is the single biggest improvement from tapered eval.
- **Passed/advanced pawns** — uniformly rewards pawn advancement toward promotion
- **Active centralized rooks** — rooks rewarded for central and 7th rank placement
- **Knights/bishops** — centralization still rewarded, less extreme edge penalties

**Positional evaluation:**
- Bishop pair: +50 mg / +70 eg (bishop pair is stronger in endgames with open diagonals)
- Rook on open file: +20 mg / +25 eg (+25 mg extra if file is near enemy king!)
- Rook on semi-open file: +10 mg / +12 eg (+15 mg extra if file is near enemy king!)
- Connected rooks: +20 cp when two rooks see each other on a rank or file (+25 mg extra if doubled on a file near enemy king!)
- Passed pawns: +20 to +125 mg / +30 to +184 eg based on advancement (passed pawns are ~1.5x more valuable in endgames — they're about to promote!)
- Doubled pawns: -5 mg / -8 eg per extra pawn (reduced penalty in mg — structure matters less than activity; harsher in eg)
- Isolated pawns: -8 mg / -12 eg (reduced mg penalty; harsher in eg where structure matters)
- Bishop x-ray to enemy king: +25 mg (bishop diagonal aimed at king zone) — middlegame-only
- Queen diagonal x-ray to enemy king: +30 mg — middlegame-only
- Queen rank/file x-ray to enemy king: +20 mg — middlegame-only

**King attack evaluation (middlegame-only, non-linear scaling — enables piece sacrifices):**

All king attack terms contribute to the middlegame score only. In the endgame there aren't enough pieces to mount a king attack, so these bonuses naturally fade out through the tapered interpolation.

- **Wide king danger zone** — uses a 2-ring zone around the enemy king (not just adjacent squares). Inner ring pieces count as full attackers. Outer ring pieces get proximity bonuses (knights +5, bishops/rooks +8, queens +12) — they're one move from attacking.
- **King tropism** — all pieces (knights, bishops, rooks, queens) get a bonus for being physically close to the enemy king (Manhattan distance). Knights and queens get 3cp per step closer, bishops and rooks get 2cp. Gravitates every piece toward the enemy king like a swarm.
- **Non-linear attacker scaling (doubled!)** — the more pieces aimed at the king, the disproportionately larger the reward. Weights are doubled from typical engines — the engine will sacrifice anything for a king attack:
  - 1 attacker: +10 cp (probing)
  - 2 attackers: +80 cp (real pressure, worth a pawn)
  - 3 attackers: +240 cp (worth sacrificing a piece!)
  - 4 attackers: +540 cp (worth sacrificing a rook and more!)
  - 5+ attackers: +900 cp or more (worth sacrificing a queen!)
- Penalty for weak enemy king pawn shield (missing defenders)
- **Uncastled king bonus** — +30 mg if the opponent still has castling rights (king likely in center). Encourages the engine to attack before the opponent reaches safety. Middlegame-only.
- **King safety imbalance** — when one side's king attack is stronger than the other's, the advantage grows superlinearly (quadratic scaling, divisor 200). A 100 cp attack edge yields +50 cp extra; a 200 cp edge yields +200 cp extra (worth a piece sacrifice); a 300 cp edge yields +450 cp extra. Encourages pressing king safety advantages aggressively. Middlegame-only.
- **Initiative/tempo bonus** — +15 cp flat bonus for having the move. The engine always prefers maintaining the initiative over passive defense.

**Pawn storm evaluation (middlegame-only):**
- Large bonuses for advancing pawns on files near enemy king
- Encourages direct pawn attacks on the enemy king position
- Scales with pawn advancement (up to +80 cp for a 7th rank pawn storm!)
- **Opposite-side castling boost** — when kings are castled on opposite sides (one king on files a-c, the other on files f-h), pawn storm bonuses are multiplied by 3/2 (50% boost). Opposite-side castling positions are inherently sharp — both sides race to attack, and pawn storms are the primary weapon.

All evaluation is relative to the side to move.

### Zobrist hashing

- Incrementally updated 64-bit hash covering piece placement, side to move, castling rights, and en passant file. Used for the transposition table and repetition detection.

### Perft

- Full perft implementation with divide (per-move subtree counts). Used for correctness testing against known node counts.

## Lichess Bot Setup

### Prerequisites

- A Lichess account upgraded to a bot account
- A Lichess API token with `bot:play` scope
- A Raspberry Pi, Nvidia Jetson, Ubuntu server, or any Linux machine with Go installed

### Step 1: Create a Lichess Bot account

1. Create a new Lichess account at https://lichess.org/signup (or use an existing one that has **never played a game** from the website).
2. Generate an API token at https://lichess.org/account/oauth/token/create. Enable the **"Play games with the bot API"** (`bot:play`) scope.
3. Upgrade the account to a bot account (this is irreversible and the account can never play on the website again):
   ```
   curl -X POST https://lichess.org/api/bot/account/upgrade \
     -H "Authorization: Bearer lip_xxxx"
   ```

### Step 2: Install Go on your machine

**Raspberry Pi (ARM) / Jetson Nano (ARM64):**

```bash
# For Raspberry Pi 4/5 (arm64) or Jetson Nano (arm64)
wget https://go.dev/dl/go1.23.6.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.23.6.linux-arm64.tar.gz

# For older Raspberry Pi (armv6l)
wget https://go.dev/dl/go1.23.6.linux-armhf.tar.gz
sudo tar -C /usr/local -xzf go1.23.6.linux-armhf.tar.gz

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**Ubuntu (x86_64):**

```bash
wget https://go.dev/dl/go1.23.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.6.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

Verify: `go version`

### Step 3: Build the engine

```bash
git clone https://github.com/williamvietnguyen/vietnamesecoffee.git
cd vietnamesecoffee
go build -o viet_coffee ./cmd/vietnamesecoffee
```

Run the perft suite to confirm the build is correct:

```bash
./viet_coffee --perft
```

All 6 positions should report `PASS`.

### Step 4: Run the bot

```bash
export LICHESS_TOKEN=lip_xxxx
./viet_coffee --lichess
```

You should see:

```
[lichess] logged in as: YourBotName
[lichess] connecting to event stream
```

The bot is now online. Send it a challenge from another Lichess account and it will accept and play.

### Step 5: Run as a systemd service (recommended)

Create `/etc/systemd/system/viet-coffee.service`:

```ini
[Unit]
Description=VietCoffee Lichess Bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=pi
Environment=LICHESS_TOKEN=lip_xxxx
WorkingDirectory=/home/pi/vietnamesecoffee
ExecStart=/home/pi/vietnamesecoffee/viet_coffee --lichess
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Adjust `User`, `WorkingDirectory`, `ExecStart`, and the token for your setup, then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable viet-coffee
sudo systemctl start viet-coffee
sudo journalctl -u viet-coffee -f   # watch logs
```

The bot will start on boot and restart automatically if it crashes.

### Lichess Bot behavior

- **Accepts** standard chess challenges (rated and casual, bullet/blitz/rapid/classical, human or bot opponents)
- **Declines** non-standard variants (Chess960, Crazyhouse, etc.) with reason `"variant"`
- **Declines** ultra-bullet and correspondence with reason `"timeControl"`
- **Declines** challenges when already playing 6 concurrent games with reason `"later"`
- **Concurrent games** -- handles up to 6 games simultaneously, each in its own goroutine
- **Time management** -- allocates `timeLeft / 30 + 70% of increment` per move, capped at 30% of remaining time, minus a 300ms network latency buffer. Minimum think time is 50ms.
- **Auto-reconnect** -- if the Lichess event stream drops, reconnects after 5 seconds
- **Rate limit handling** -- on HTTP 429, waits 60 seconds before retrying (per Lichess API docs)
- **Graceful shutdown** -- catches SIGINT/SIGTERM, cancels all active games, waits up to 10 seconds for them to finish
- **Chat greeting** -- sends "Good luck! I'm VietCoffee, a chess engine." at the start of each game
- **Shared transposition table** -- all concurrent games share the 256 MB global TT
- **Logging** -- all bot activity logs to stderr with `[lichess]` or `[game XXXXX]` prefixes

## Releasing a new version

Binaries are cross-compiled locally and published as GitHub releases. No CI required.

### 1. Build

```bash
./build.sh v1.0.1
```

This produces statically linked binaries in `dist/`:
- `dist/viet_coffee-linux-amd64` (Ubuntu, Debian, cloud servers)
- `dist/viet_coffee-linux-arm64` (Raspberry Pi 4/5, Jetson Nano)

### 2. Create the release

```bash
gh release create v1.0.1 \
  dist/viet_coffee-linux-amd64 \
  dist/viet_coffee-linux-arm64 \
  --title "VietCoffee v1.0.1" \
  --notes "Release notes here"
```

### 3. Deploy to your server

```bash
curl -L -o viet_coffee https://github.com/williamvietnguyen/vietnamesecoffee/releases/download/v1.0.1/viet_coffee-linux-amd64
chmod +x viet_coffee
sudo systemctl restart viet-coffee
```

Replace the version tag and architecture as needed.

## Architecture

No external dependencies — stdlib only. The code is organized into a standard Go `cmd/` + `internal/` layout:

```
vietnamesecoffee/
├── cmd/vietnamesecoffee/
│   └── main.go              # Entry point
├── internal/
│   ├── engine/
│   │   ├── board.go          # Constants, types, Move encoding, bitboard utilities, attack tables, Zobrist hashing, sliding piece attacks, position helpers
│   │   ├── movegen.go        # FEN parsing, pseudo-legal + legal move generation, make move
│   │   ├── eval.go           # Tapered eval: mg/eg material values, mg/eg piece-square tables, game phase, evaluation (king attack, rooks, x-rays, pawns, pawn storm)
│   │   ├── search.go         # Transposition table, move ordering, quiescence, negamax (PVS, LMR, NMP, killer moves, aspiration windows), iterative deepening
│   │   └── perft.go          # Perft, divide, UCI move parsing, perft test suite
│   ├── uci/
│   │   └── uci.go            # UCI protocol loop, board display
│   └── lichess/
│       └── lichess.go        # Lichess Bot API client
├── build.sh
├── go.mod
└── README.md
```
