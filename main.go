package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/tucnak/telebot"
)

// Game represents a single game of Russian Roulette
// getPlayerID returns a suitable identifier for the player
func getPlayerID(sender *telebot.User) string {
	if sender.Username != "" {
		return sender.Username
	}
	// If no username, use FirstName
	if sender.FirstName != "" {
		return sender.FirstName
	}
	// If neither exists, use the ID
	return fmt.Sprintf("player%d", sender.ID)
}

type Game struct {
	Players    []string
	Bullet     int
	CurrentPos int
	IsActive   bool
}

var (
	// Store active games by chat ID
	games = make(map[int64]*Game)
	mutex sync.Mutex
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	// Get bot token from environment variable
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is not set")
	}

	bot, err := telebot.NewBot(telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	if err != nil {
		log.Fatal(err)
	}

	// Command to start a new game
	bot.Handle("/startroulette", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		if game, exists := games[m.Chat.ID]; exists && game.IsActive {
			bot.Send(m.Chat, "A game is already in progress!")
			return
		}

		// Get player identifier (username or first name)
		playerID := getPlayerID(m.Sender)
		log.Printf("New game started by player: %s", playerID)

		// Initialize new game
		games[m.Chat.ID] = &Game{
			Players:    []string{playerID},
			Bullet:     rand.Intn(6), // Random position for bullet (0-5)
			CurrentPos: 0,
			IsActive:   true,
		}

		bot.Send(m.Chat, fmt.Sprintf("🎮 @%s started a game of Russian Roulette!\nUse /join to join the game.\nUse /startgame when all players have joined.", m.Sender.Username))
	})

	// Command to join an existing game
	bot.Handle("/join", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /startroulette to start a new game.")
			return
		}

		playerID := getPlayerID(m.Sender)
		log.Printf("Player trying to join: %s", playerID)

		// Check if player is already in the game
		for _, player := range game.Players {
			if player == playerID {
				bot.Send(m.Chat, "You're already in the game!")
				return
			}
		}

		game.Players = append(game.Players, playerID)
		bot.Send(m.Chat, fmt.Sprintf("@%s joined the game! Current players: %v", m.Sender.Username, game.Players))
	})

	// Command to start the game after all players have joined
	bot.Handle("/startgame", func(m *telebot.Message) {
		mutex.Lock()
		game, exists := games[m.Chat.ID]
		mutex.Unlock()

		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /startroulette to start a new game.")
			return
		}

		if len(game.Players) < 2 {
			bot.Send(m.Chat, "Need at least 2 players to start!")
			return
		}

		bot.Send(m.Chat, "🎲 Game starting! Use /pull to take your turn.")
		bot.Send(m.Chat, fmt.Sprintf("First up: @%s", game.Players[0]))
	})

	// Command to pull the trigger
	bot.Handle("/pull", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /startroulette to start a new game.")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		if m.Sender.Username != currentPlayer {
			bot.Send(m.Chat, fmt.Sprintf("It's not your turn! Waiting for @%s to pull the trigger.", currentPlayer))
			return
		}

		// Check if bullet is in current position
		if game.CurrentPos == game.Bullet {
			bot.Send(m.Chat, fmt.Sprintf("💥 BANG! @%s is dead! Game Over!", m.Sender.Username))
			games[m.Chat.ID] = nil
			return
		}

		// Calculate remaining chambers and odds
		remainingChambers := 6 - (game.CurrentPos + 1)
		oddsPercentage := (1.0 / float64(remainingChambers)) * 100

		survivalMsg := fmt.Sprintf("*click* @%s survives!\nChambers left: %d\nChance of next shot being fatal: %.1f%%",
			getPlayerID(m.Sender),
			remainingChambers,
			oddsPercentage)
		bot.Send(m.Chat, survivalMsg)

		game.CurrentPos++
		nextPlayer := game.Players[game.CurrentPos%len(game.Players)]
		bot.Send(m.Chat, fmt.Sprintf("Next up: @%s", nextPlayer))
	})

	// Command to stop the current game
	bot.Handle("/stopgame", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		if game, exists := games[m.Chat.ID]; exists && game.IsActive {
			games[m.Chat.ID] = nil
			bot.Send(m.Chat, "Game stopped.")
		} else {
			bot.Send(m.Chat, "No active game to stop!")
		}
	})

	// Command to show help
	bot.Handle("/help", func(m *telebot.Message) {
		helpText := `Available commands:
/startroulette - Start a new game
/join - Join the current game
/startgame - Start the game after players have joined
/pull - Pull the trigger on your turn
/status - Show current game status
/stopgame - Stop the current game
/help - Show this help message`
		bot.Send(m.Chat, helpText)
	})

	// Command to show game status
	bot.Handle("/status", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game!")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		status := fmt.Sprintf("Current players: %v\nWaiting for: @%s", game.Players, currentPlayer)
		bot.Send(m.Chat, status)
	})

	log.Println("Bot started...")
	bot.Start()
}
