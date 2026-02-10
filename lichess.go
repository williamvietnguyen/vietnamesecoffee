package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ============================================================
// Section 16: Lichess Bot Integration
// ============================================================

const (
	lichessBaseURL       = "https://lichess.org"
	maxConcurrentGames   = 6
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
	mu          sync.Mutex
	activeGames map[string]context.CancelFunc
	lastGameEnd time.Time
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

func (bot *lichessBot) challengeBot(username string, clockLimit, clockIncrement int) error {
	form := fmt.Sprintf("rated=true&clock.limit=%d&clock.increment=%d&color=random&variant=standard",
		clockLimit, clockIncrement)
	resp, err := bot.doRequest("POST", "/api/challenge/"+username, form, "application/x-www-form-urlencoded")
	if err != nil {
		log.Printf("[lichess] challenge %s error: %v\n", username, err)
		return err
	}
	resp.Body.Close()
	log.Printf("[lichess] challenged %s (%d+%d)\n", username, clockLimit, clockIncrement)
	return nil
}

func (bot *lichessBot) getOnlineBots() ([]string, error) {
	resp, err := bot.doRequest("GET", "/api/bot/online?nb=50", "", "")
	if err != nil {
		return nil, err
	}
	var usernames []string
	streamNDJSON(resp, func(line []byte) bool {
		var entry struct {
			Username string `json:"username"`
		}
		if json.Unmarshal(line, &entry) == nil && strings.ToLower(entry.Username) != bot.botID {
			usernames = append(usernames, entry.Username)
		}
		return true
	})
	return usernames, nil
}

func (bot *lichessBot) seekGame() {
	bots, err := bot.getOnlineBots()
	if err != nil {
		log.Printf("[lichess] failed to get online bots: %v\n", err)
		return
	}
	if len(bots) == 0 {
		log.Println("[lichess] no online bots found")
		return
	}

	type timeControl struct{ limit, increment int }
	controls := []timeControl{
		{60, 0}, {60, 1}, {120, 1}, // bullet
		{180, 0}, {180, 2}, {300, 0}, {300, 3}, // blitz
		{600, 0}, {600, 5}, {900, 10}, // classical
	}

	opponent := bots[rand.Intn(len(bots))]
	tc := controls[rand.Intn(len(controls))]
	log.Printf("[lichess] seeking game: challenging %s (%d+%d)\n", opponent, tc.limit, tc.increment)
	bot.challengeBot(opponent, tc.limit, tc.increment)
}

func (bot *lichessBot) autoChallenge() {
	for {
		select {
		case <-bot.ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		bot.mu.Lock()
		count := len(bot.activeGames)
		lastEnd := bot.lastGameEnd
		bot.mu.Unlock()
		if count < 1 && (lastEnd.IsZero() || time.Since(lastEnd) >= time.Hour) {
			bot.seekGame()
		}
	}
}

// --- Challenge filter ---

func (bot *lichessBot) shouldAcceptChallenge(ch *lichessChallenge) (bool, string) {
	if ch.Variant.Key != "standard" {
		return false, "variant"
	}
	// Reject games with initial time < 30 seconds per side
	// Note: TimeControl.Limit is in seconds
	if ch.TimeControl.Limit < 30 {
		return false, "tooFast"
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
		bot.lastGameEnd = time.Now()
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
						bot.lastGameEnd = time.Now()
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
	go bot.autoChallenge()

	bot.runEventStream()
}
