package main

import (
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Game represents the Go board and game state
type Game struct {
	Board         [][]int   `json:"board"`
	Size          int       `json:"size"`
	CurrentPlayer int       `json:"currentPlayer"`
	Stats         GameStats `json:"stats"`
	mu            sync.Mutex
}

// GameStats tracks various game statistics
type GameStats struct {
	BlackMoves    int     `json:"blackMoves"`
	WhiteMoves    int     `json:"whiteMoves"`
	BlackCaptures int     `json:"blackCaptures"`
	WhiteCaptures int     `json:"whiteCaptures"`
	GameStartTime string  `json:"gameStartTime"`
	MoveTimes     []int64 `json:"moveTimes"` // Time taken for each move in milliseconds
}

// Move represents a player's move
type Move struct {
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Difficulty string `json:"difficulty"`
}

// GameConfig represents the configuration for a new game
type GameConfig struct {
	Size int `json:"size"`
}

var game *Game

func main() {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Initialize game with 19x19 board (standard size)
	game = newGame(19)

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	// Game API endpoints
	http.HandleFunc("/api/state", handleGameState)
	http.HandleFunc("/api/move", handleMove)
	http.HandleFunc("/api/reset", handleReset)
	http.HandleFunc("/api/ai-move", handleAIMove)

	log.Println("Server starting on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func newGame(size int) *Game {
	board := make([][]int, size)
	for i := range board {
		board[i] = make([]int, size)
	}
	return &Game{
		Board:         board,
		Size:          size,
		CurrentPlayer: 1,
		Stats: GameStats{
			GameStartTime: time.Now().Format(time.RFC3339),
			MoveTimes:     make([]int64, 0),
		},
	}
}

func handleGameState(w http.ResponseWriter, r *http.Request) {
	game.mu.Lock()
	defer game.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game)
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var move Move
	if err := json.NewDecoder(r.Body).Decode(&move); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	// Validate move
	if move.X < 0 || move.X >= game.Size || move.Y < 0 || move.Y >= game.Size {
		http.Error(w, "Invalid move position", http.StatusBadRequest)
		return
	}

	if game.Board[move.Y][move.X] != 0 {
		http.Error(w, "Position already occupied", http.StatusBadRequest)
		return
	}

	// Place stone and update stats
	game.Board[move.Y][move.X] = game.CurrentPlayer
	if game.CurrentPlayer == 1 {
		game.Stats.BlackMoves++
	} else {
		game.Stats.WhiteMoves++
	}

	// Record move time
	game.Stats.MoveTimes = append(game.Stats.MoveTimes, time.Now().UnixMilli())

	// Switch players (1 for black, 2 for white)
	game.CurrentPlayer = 3 - game.CurrentPlayer

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game)
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config GameConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		// If no config provided, use default size
		config.Size = 19
	}

	// Validate board size
	validSizes := map[int]bool{9: true, 13: true, 19: true}
	if !validSizes[config.Size] {
		config.Size = 19 // Default to 19x19 if invalid size
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	game = newGame(config.Size)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game)
}

// findValidMoves returns a list of valid moves on the board
func (g *Game) findValidMoves() []Move {
	var validMoves []Move
	for y := 0; y < g.Size; y++ {
		for x := 0; x < g.Size; x++ {
			if g.Board[y][x] == 0 {
				validMoves = append(validMoves, Move{X: x, Y: y})
			}
		}
	}
	return validMoves
}

// evaluateMove scores a potential move based on various factors
func (g *Game) evaluateMove(x, y int, player int) float64 {
	score := 0.0

	// Check for adjacent friendly stones (connection)
	for _, d := range []struct{ dx, dy int }{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		newX, newY := x+d.dx, y+d.dy
		if newX >= 0 && newX < g.Size && newY >= 0 && newY < g.Size {
			if g.Board[newY][newX] == player {
				score += 1.0
			}
		}
	}

	// Prefer moves closer to the center (for opening/middle game)
	centerX, centerY := g.Size/2, g.Size/2
	distanceFromCenter := math.Sqrt(float64((x-centerX)*(x-centerX) + (y-centerY)*(y-centerY)))
	score += 5.0 / (distanceFromCenter + 1)

	// Prefer moves that are close to opponent stones (for interaction)
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			newX, newY := x+dx, y+dy
			if newX >= 0 && newX < g.Size && newY >= 0 && newY < g.Size {
				if g.Board[newY][newX] == 3-player { // opponent's stone
					distance := math.Sqrt(float64(dx*dx + dy*dy))
					score += 2.0 / (distance + 1)
				}
			}
		}
	}

	return score
}

func (g *Game) findBestMove(difficulty string) Move {
	validMoves := g.findValidMoves()
	if len(validMoves) == 0 {
		return Move{X: -1, Y: -1}
	}

	switch difficulty {
	case "easy":
		// Random move
		return validMoves[rand.Intn(len(validMoves))]

	case "medium":
		// Semi-random with some strategy
		if len(validMoves) > 3 {
			// Take the top 3 moves
			var topMoves []Move
			moveScores := make(map[Move]float64)

			for _, move := range validMoves {
				score := g.evaluateMove(move.X, move.Y, 2)
				moveScores[move] = score
			}

			// Sort moves by score
			sort.Slice(validMoves, func(i, j int) bool {
				return moveScores[validMoves[i]] > moveScores[validMoves[j]]
			})

			topMoves = validMoves[:3]
			return topMoves[rand.Intn(len(topMoves))]
		}
		return validMoves[rand.Intn(len(validMoves))]

	case "hard":
		// Always choose the best move
		var bestMove Move
		bestScore := -1.0

		for _, move := range validMoves {
			score := g.evaluateMove(move.X, move.Y, 2)
			if score > bestScore {
				bestScore = score
				bestMove = move
			}
		}
		return bestMove

	default:
		return validMoves[rand.Intn(len(validMoves))]
	}
}

func handleAIMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var moveRequest Move
	if err := json.NewDecoder(r.Body).Decode(&moveRequest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	// Ensure it's the AI's turn (player 2)
	if game.CurrentPlayer != 2 {
		http.Error(w, "Not AI's turn", http.StatusBadRequest)
		return
	}

	// Find best move based on difficulty
	move := game.findBestMove(moveRequest.Difficulty)

	// Make the move and update stats
	game.Board[move.Y][move.X] = game.CurrentPlayer
	game.Stats.WhiteMoves++
	game.Stats.MoveTimes = append(game.Stats.MoveTimes, time.Now().UnixMilli())

	game.CurrentPlayer = 3 - game.CurrentPlayer

	// Log the move for debugging
	log.Printf("AI made move at x:%d, y:%d with difficulty %s", move.X, move.Y, moveRequest.Difficulty)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(game); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
