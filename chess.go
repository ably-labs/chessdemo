package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ably/ably-go/ably"
	"github.com/notnil/chess"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const resign = "resign"

type app struct {
	game            *chess.Game
	colour          chess.Color
	userID          string
	oLock           sync.RWMutex
	opponent        string
	waitForOpponent chan struct{}
	gameID          string
	moveNo          int
	ch              *ably.RealtimeChannel
}

type msg struct {
	Move   string `json:"move"`
	Colour int    `json:"colour"`
	FEN    string `json:"FEN"`
}

func (a *app) watchGame(ctx context.Context) {
	done := make(chan bool)
	nMove := 0
	unsub, err := a.ch.Subscribe(ctx, a.gameID, func(message *ably.Message) {
		nMove++
		m := decodeMsg(message)

		if nMove == 1 {
			fen, err := chess.FEN(m.FEN)
			if err != nil {
				log.Fatalln(err)
			}
			a.game = chess.NewGame(fen)
		}
		fmt.Println(m.Move)
		if m.Move == resign {
			a.game.Resign(chess.Color(m.Colour))
			done <- true
			return
		}

		err := a.game.MoveStr(m.Move)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Println(a.game.Position().Board().Draw())
		if a.gameIsOver() {
			done <- true
		}
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()
	<-done
	fmt.Println(a.game.Outcome())
}

func (a *app) prompt() string {
	switch a.colour {
	case chess.White:
		return fmt.Sprintf("%d: ", a.moveNo)
	case chess.Black:
		return fmt.Sprintf("%d: ... ", a.moveNo)
	}
	panic("bad colour")
}

func (a *app) readInput(r *bufio.Reader) string {
	os.Stdout.WriteString(a.prompt())
	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatalln(err)
	}
	move := strings.TrimSpace(line)
	return move
}

func handleOpponentMove(ctx context.Context, game *chess.Game, waitCh chan msg) {
	var m msg
	select {
	case <-ctx.Done():
		return
	case m = <-waitCh:
		break
	}
	if m.Move == resign {
		game.Resign(chess.Color(m.Colour))
		return
	}

	err := game.MoveStr(m.Move)
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(m.Move)
	fmt.Println(game.Position().Board().Draw())

}

func decodeMsg(am *ably.Message) msg {
	var msg msg
	m, ok := am.Data.(string)
	if !ok {
		log.Fatalf("message.Data is not a string, but a %T", am.Data)
	}
	err := json.Unmarshal([]byte(m), &msg)
	if err != nil {
		log.Fatalln(err)
	}
	return msg
}

func (a *app) playGame(ctx context.Context) {
	waitChan := make(chan msg)
	unsub, err := a.ch.Subscribe(ctx, a.gameID, func(message *ably.Message) {
		if message.ClientID != a.userID {
			waitChan <- decodeMsg(message)
		}
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()
	switch a.colour {
	case chess.White:
		fmt.Println("Waiting for an opponent to arrive.")
		<-a.waitForOpponent
		fmt.Println("Your opponent", a.opponent, "is playing black.")
	}
	if a.colour == chess.Black {
		// If we are black, our opponent moves first.
		handleOpponentMove(ctx, a.game, waitChan)
	}

	userIn := bufio.NewReader(os.Stdin)
	for !a.gameIsOver() {
		a.moveNo++
		a.handleMyMove(ctx, userIn)
		if a.gameIsOver() || ctx.Err() != nil {
			break
		}
		handleOpponentMove(ctx, a.game, waitChan)
	}
	fmt.Println(a.game)
}

func (a *app) gameIsOver() bool {
	return a.game.Outcome() != chess.NoOutcome
}

func (a *app) handleMyMove(ctx context.Context, userIn *bufio.Reader) {
	fen, err := a.game.Position().MarshalText()
	if err != nil {
		log.Fatalln(err)
	}

	var myMove string
	for ctx.Err() == nil {
		myMove = a.readInput(userIn)
		if myMove == resign {
			a.game.Resign(a.colour)
			break
		}
		err := a.game.MoveStr(myMove)
		if err == nil {
			break // the user has entered a legal move
		}
		// illegal move, print out an error, and try again
		fmt.Println(err)
	}
	if ctx.Err() != nil {
		return
	}

	fmt.Println(a.game.Position().Board().Draw())

	err = a.ch.Publish(ctx, a.gameID, msg{
		Move:   myMove,
		Colour: int(a.colour),
		FEN:    string(fen),
	})
	if err != nil {
		log.Fatalln(err)
	}
}

func (a *app) setOppent(id string) (changed bool) {
	a.oLock.Lock()
	defer a.oLock.Unlock()
	if a.opponent != "" {
		return false
	}
	a.opponent = id
	return true
}

func (a *app) Oppent() string {
	a.oLock.RLock()
	defer a.oLock.RUnlock()
	return a.opponent
}

func main() {
	userName := flag.String("name", "", "your name")
	game := flag.String("game", "game1", "game name")
	flag.Parse()
	if *userName == "" {
		log.Fatalln("You must provide a -name argument")
	}

	key := os.Getenv("ABLY_KEY")
	if key == "" {
		log.Fatalln("you must set ABLY_KEY")
	}
	ctx, cancel := context.WithCancel(context.Background())

	client, err := ably.NewRealtime(
		ably.WithKey(key),
		ably.WithClientID(*userName))
	if err != nil {
		log.Fatalln(err)
	}
	go func() {
		// TERM or KILL signal should result in a graceful shutdown.
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		s := <-sigs
		log.Println("Got signal", s, "shutting down.")
		cancel()
		client.Close()
		os.Exit(0)
	}()

	a := app{
		game:            chess.NewGame(),
		userID:          *userName,
		gameID:          "chess:" + *game,
		waitForOpponent: make(chan struct{}),
	}

	defer client.Close()

	a.ch = client.Channels.Get(a.gameID)
	//ably.ChannelWithParams("rewind", "1"))

	err = a.ch.Presence.Enter(ctx, a.userID)
	if err != nil {
		log.Fatalln(err)
	}
	iHaveEntered := make(chan struct{})
	a.ch.Presence.SubscribeAll(ctx, func(message *ably.PresenceMessage) {
		//log.Println(message)
		switch message.Action {
		case ably.PresenceActionEnter:
			if message.ClientID == a.userID {
				close(iHaveEntered)
				return
			}
			changed := a.setOppent(message.ClientID)
			if changed {
				close(a.waitForOpponent)
			}
		case ably.PresenceActionLeave:
			opponentGone := message.ClientID == a.Oppent()
			if opponentGone {
				log.Println("opponent", a.Oppent(), "has left the game")
				client.Close()
				cancel()
				os.Exit(0)
			}
		}
	})

	// We need to wait until we appear in presence.
	select {
	case <-iHaveEntered:
	case <-time.After(time.Second):
	}
	players, err := a.ch.Presence.Get(ctx)
	if err != nil {
		log.Fatalln(err)
	}
	sort.Slice(players, func(i, j int) bool {
		return players[i].Timestamp < players[j].Timestamp
	})

	switch {
	case players[0].ClientID == a.userID:
		a.colour = chess.White
		fmt.Println("you are white")
		if err != nil {
			log.Fatalln(err)
		}
		a.playGame(ctx)
	case players[1].ClientID == a.userID:
		a.setOppent(players[0].ClientID)
		a.colour = chess.Black
		fmt.Println("you are playing black against", players[0].ClientID)
		if err != nil {
			log.Fatalln(err)
		}
		a.playGame(ctx)

	default:
		a.setOppent(players[0].ClientID)
		fmt.Println("you are a spectator:", players[0].ClientID, " v ", players[1].ClientID)
		a.watchGame(ctx)
	}

}
