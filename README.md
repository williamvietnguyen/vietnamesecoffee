# VietCoffee

A chess engine written in Go. Single file, zero dependencies, stdlib only.

VietCoffee speaks the UCI protocol for use with chess GUIs, and includes a built-in Lichess Bot API client for playing live games on lichess.org.

## Building

Requires Go 1.21 or later.

```
go build -o viet_coffee viet_coffee.go
```

Or run directly:

```
go run viet_coffee.go
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

- **Bitboards** -- one `uint64` per piece type per color, with precomputed attack tables for knights, kings, and pawns. Sliding piece attacks (bishop, rook, queen) use loop-based ray generation.

### Move generation

- **Pseudo-legal + legality filter** -- generates all pseudo-legal moves (quiet, captures, double pawn pushes, en passant, promotions to all four piece types, kingside and queenside castling), then filters by making each move and checking if the king is left in check.

### Search

- **Iterative deepening** -- searches depth 1, 2, 3, ... until time runs out or the max depth is reached. The best move from the last fully completed depth is used.
- **Negamax with alpha-beta pruning** -- standard negamax framework with fail-soft alpha-beta window.
- **Transposition table** -- 64 MB hash table (power-of-two sized, lockless indexing). Stores best move, score, depth, and bound type (exact, upper, lower). Used for both move ordering and search cutoffs. Mate scores are adjusted for distance from root on store/probe.
- **Null move pruning** -- if the side to move has non-pawn material and is not in check, tries passing the turn (R=2 reduction) to get a beta cutoff without searching all moves.
- **Check extensions** -- extends search depth by 1 when the side to move is in check, avoiding horizon-effect blunders.
- **Quiescence search** -- at depth 0, continues searching captures and promotions until a quiet position is reached, preventing the engine from stopping at a position mid-exchange.
- **Move ordering** -- TT move first, then MVV-LVA (most valuable victim, least valuable attacker) for captures, then promotion bonus. Uses incremental selection sort for lazy move ordering.
- **Contempt factor** -- draws from repetition or the fifty-move rule are scored at -40 cp instead of 0. The engine would rather be slightly worse than accept a draw, forcing it to avoid repetitions, refuse simplifications, and play for a win. Stalemate (a forced draw) is unaffected.
- **Repetition detection** -- tracks Zobrist hash history across the game and within the search tree. Returns contempt-penalized score on repetition.
- **Fifty-move rule** -- returns contempt-penalized score when the half-move clock reaches 100.
- **Atomic stop** -- search can be halted instantly from another goroutine via an atomic flag. Time checks occur every 2048 nodes.
- **Concurrent search** -- the UCI `go` command launches search in a goroutine; `stop` halts it immediately.

### Evaluation

VietCoffee is tuned for **aggressive, attacking play**.

**Material values (tuned for attacking style):**
- Pawn: 100 cp
- Knight: 340 cp (boosted +20 over typical 320)
- Bishop: 350 cp (boosted +20 over typical 330)
- Rook: 490 cp (reduced -10 from typical 500)
- Queen: 900 cp

Knights and bishops are overvalued to encourage piece activity and tactical sacrifices. Rooks are slightly undervalued to de-emphasize slow endgame grinding.

**Piece-square tables:** Aggressive tuning encourages:
- Forward pawn advances (especially kingside pawns)
- Central and advanced knight placement
- Active bishop diagonals
- Early piece development

**Positional evaluation:**
- Bishop pair: +50 cp
- Rook on open file: +20 cp (+45 cp if file is near enemy king!)
- Rook on semi-open file: +10 cp (+25 cp if file is near enemy king!)
- Passed pawns: +20 to +125 cp based on advancement (boosted for aggressive pawn pushes)
- Doubled pawns: -5 cp each (reduced penalty - structure matters less than activity)
- Isolated pawns: -8 cp (reduced penalty)

**King attack evaluation:**
- Bonus for pieces attacking squares near enemy king
- Bonus for knights close to enemy king (proximity bonus)
- Bonus scales with number of attackers (2+ attackers triggers extra bonuses)
- Penalty for weak enemy king pawn shield (missing defenders)

**Pawn storm evaluation:**
- Large bonuses for advancing pawns on files near enemy king
- Encourages direct pawn attacks on the enemy king position
- Scales with pawn advancement (up to +80 cp for a 7th rank pawn storm!)

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
git clone https://github.com/YOUR_USERNAME/vietnamesecoffee.git
cd vietnamesecoffee
go build -o viet_coffee viet_coffee.go
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

- **Accepts** standard chess challenges (rated and casual, any time control, human or bot opponents)
- **Declines** non-standard variants (Chess960, Crazyhouse, etc.) with reason `"variant"`
- **Declines** challenges when already playing 4 concurrent games with reason `"later"`
- **Concurrent games** -- handles up to 4 games simultaneously, each in its own goroutine
- **Time management** -- allocates `timeLeft / 30 + 70% of increment` per move, capped at 30% of remaining time, minus a 300ms network latency buffer. Minimum think time is 50ms.
- **Auto-reconnect** -- if the Lichess event stream drops, reconnects after 5 seconds
- **Rate limit handling** -- on HTTP 429, waits 60 seconds before retrying (per Lichess API docs)
- **Graceful shutdown** -- catches SIGINT/SIGTERM, cancels all active games, waits up to 10 seconds for them to finish
- **Chat greeting** -- sends "Good luck! I'm VietCoffee, a chess engine." at the start of each game
- **Shared transposition table** -- all concurrent games share the 64 MB global TT
- **Logging** -- all bot activity logs to stderr with `[lichess]` or `[game XXXXX]` prefixes

## Architecture

Everything lives in a single file: `viet_coffee.go`. No external dependencies. The code is organized in sections:

| Section | Contents |
|---|---|
| 1 | Constants, types, Move encoding |
| 2 | Bitboard utilities |
| 3 | Attack tables, Zobrist hashing |
| 4 | Sliding piece attack generation |
| 5 | Position helpers (attack detection, check, piece lookup) |
| 6 | FEN parsing |
| 7 | Move generation (pseudo-legal) |
| 8 | Make move |
| 9 | Evaluation (material + piece-square tables) |
| 10 | Search (iterative deepening, negamax, quiescence, TT, null move pruning) |
| 11 | Perft and divide |
| 12 | UCI helpers (move parsing, board display) |
| 13 | UCI loop |
| 14 | Perft test suite |
| 15 | Main entry point |
| 16 | Lichess Bot API client |
