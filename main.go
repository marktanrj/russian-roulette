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

// getPlayerID returns a suitable identifier for the player
func getPlayerID(sender *telebot.User) string {
	if sender.Username != "" {
		return sender.Username
	}
	if sender.FirstName != "" {
		return sender.FirstName
	}
	return fmt.Sprintf("player%d", sender.ID)
}

type Game struct {
	Players         []string
	Bullet          int
	CurrentPos      int
	PullCount       int
	IsActive        bool
	Skips           map[string]int // Track remaining skips for each player
	HasPulledOnTurn bool           // Track if current player has pulled at least once on their turn
}

var (
	games = make(map[int64]*Game)
	mutex sync.Mutex
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

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

	bot.Handle("/create", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		if game, exists := games[m.Chat.ID]; exists && game.IsActive {
			bot.Send(m.Chat, "A game is already in progress!")
			return
		}

		playerID := getPlayerID(m.Sender)
		log.Printf("New game started by player: %s", playerID)

		games[m.Chat.ID] = &Game{
			Players:         []string{playerID},
			Bullet:          rand.Intn(6),
			CurrentPos:      0,
			PullCount:       0,
			IsActive:        true,
			Skips:           map[string]int{playerID: 2},
			HasPulledOnTurn: false,
		}

		bot.Send(m.Chat, fmt.Sprintf("🎮 @%s started a game of Russian Roulette!\nUse /join to join the game.\nUse /start when all players have joined.", m.Sender.Username))
	})

	bot.Handle("/join", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /create to create a new game.")
			return
		}

		playerID := getPlayerID(m.Sender)
		log.Printf("Player trying to join: %s", playerID)

		for _, player := range game.Players {
			if player == playerID {
				bot.Send(m.Chat, "You're already in the game!")
				return
			}
		}

		game.Players = append(game.Players, playerID)
		game.Skips[playerID] = 2
		bot.Send(m.Chat, fmt.Sprintf("@%s joined the game! Current players: %v", m.Sender.Username, game.Players))
	})

	bot.Handle("/start", func(m *telebot.Message) {
		mutex.Lock()
		game, exists := games[m.Chat.ID]
		mutex.Unlock()

		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /create to create a new game.")
			return
		}

		if len(game.Players) < 2 {
			bot.Send(m.Chat, "Need at least 2 players to start!")
			return
		}

		bot.Send(m.Chat, "🎲 Game starting! Use /pull to take your turn (you can pull multiple times), /skip to skip your turn (max 2 skips per player), or /pass after pulling at least once.")
		bot.Send(m.Chat, fmt.Sprintf("First up: @%s", game.Players[0]))
	})

	bot.Handle("/skip", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /create to create a new game.")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		if getPlayerID(m.Sender) != currentPlayer {
			bot.Send(m.Chat, fmt.Sprintf("It's not your turn! Waiting for @%s to play.", currentPlayer))
			return
		}

		if game.HasPulledOnTurn {
			bot.Send(m.Chat, "You've already pulled the trigger this turn! Use /pass to end your turn.")
			return
		}

		if game.Skips[currentPlayer] <= 0 {
			bot.Send(m.Chat, "You have no skips remaining! You must /pull!")
			return
		}

		game.Skips[currentPlayer]--
		game.CurrentPos++
		game.HasPulledOnTurn = false
		nextPlayer := game.Players[game.CurrentPos%len(game.Players)]

		skipsLeft := game.Skips[currentPlayer]
		bot.Send(m.Chat, fmt.Sprintf("@%s skipped their turn! (%d skip(s) remaining)\nNext up: @%s",
			currentPlayer, skipsLeft, nextPlayer))
	})

	bot.Handle("/pass", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /create to create a new game.")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		if getPlayerID(m.Sender) != currentPlayer {
			bot.Send(m.Chat, fmt.Sprintf("It's not your turn! Waiting for @%s to play.", currentPlayer))
			return
		}

		if !game.HasPulledOnTurn {
			bot.Send(m.Chat, "You must pull the trigger at least once before passing!")
			return
		}

		game.CurrentPos++
		game.HasPulledOnTurn = false
		nextPlayer := game.Players[game.CurrentPos%len(game.Players)]
		bot.Send(m.Chat, fmt.Sprintf("@%s passed their turn.\nNext up: @%s", currentPlayer, nextPlayer))
	})

	bot.Handle("/pull", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game! Use /create to create a new game.")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		if getPlayerID(m.Sender) != currentPlayer {
			bot.Send(m.Chat, fmt.Sprintf("It's not your turn! Waiting for @%s to pull the trigger.", currentPlayer))
			return
		}

		if game.PullCount == game.Bullet {
			bot.Send(m.Chat, fmt.Sprintf("💥 BANG! @%s is dead! Game Over!", m.Sender.Username))
			delete(games, m.Chat.ID)
			return
		}

		remainingChambers := 6 - game.PullCount - 1
		if remainingChambers <= 0 {
			bot.Send(m.Chat, fmt.Sprintf("💥 BANG! @%s is dead! Game Over!", m.Sender.Username))
			delete(games, m.Chat.ID)
			return
		}

		oddsPercentage := (1.0 / float64(remainingChambers)) * 100

		game.HasPulledOnTurn = true
		game.PullCount++

		survivalMsg := fmt.Sprintf("*click* @%s survives!\nChambers left: %d\nChance of next shot being fatal: %.1f%%\nSkips remaining: %d\nUse /pull to try again or /pass to end your turn",
			getPlayerID(m.Sender),
			remainingChambers,
			oddsPercentage,
			game.Skips[currentPlayer])
		bot.Send(m.Chat, survivalMsg)
	})

	bot.Handle("/stop", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		if game, exists := games[m.Chat.ID]; exists && game.IsActive {
			delete(games, m.Chat.ID)
			bot.Send(m.Chat, "Game stopped.")
		} else {
			bot.Send(m.Chat, "No active game to stop!")
		}
	})

	bot.Handle("/help", func(m *telebot.Message) {
		helpText := `Game commands:
/create - Start a new game
/join - Join the current game
/start - Start the game after players have joined
/stop - Stop the current game
/status - Show current game status

Options during game:
	/pull - Pull the trigger (can be used multiple times on your turn)
	/pass - End your turn (only after pulling at least once)
	/skip - Skip your turn (max 2 skips per player)

/help - Show this help message`
		bot.Send(m.Chat, helpText)
	})

	bot.Handle("/status", func(m *telebot.Message) {
		mutex.Lock()
		defer mutex.Unlock()

		game, exists := games[m.Chat.ID]
		if !exists || !game.IsActive {
			bot.Send(m.Chat, "No active game!")
			return
		}

		currentPlayer := game.Players[game.CurrentPos%len(game.Players)]
		status := fmt.Sprintf("Current players: %v\nWaiting for: @%s\nSkips remaining: ", game.Players, currentPlayer)

		for _, player := range game.Players {
			status += fmt.Sprintf("\n@%s: %d", player, game.Skips[player])
		}

		bot.Send(m.Chat, status)
	})

	log.Println("Bot started...")
	bot.Start()
}
