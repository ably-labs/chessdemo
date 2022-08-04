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
	"strings"
	"syscall"
)

const resign = "resign"

type msg struct {
	Move   string `json:"move"`
	Colour int    `json:"colour"`
	FEN    string `json:"FEN"`
}

func watchGame(ctx context.Context, ch *ably.RealtimeChannel, name string) {
	var game *chess.Game
	done := make(chan bool)
	nMove := 0
	unsub, err := ch.Subscribe(ctx, name, func(message *ably.Message) {
		nMove++
		m := decodeMsg(message)

		if nMove == 1 {
			fen, err := chess.FEN(m.FEN)
			if err != nil {
				log.Fatalln(err)
			}
			game = chess.NewGame(fen)
		}
		fmt.Println(m.Move)
		if m.Move == resign {
			game.Resign(chess.Color(m.Colour))
			done <- true
			return
		}

		err := game.MoveStr(m.Move)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Println(game.Position().Board().Draw())
		if game.Outcome() != chess.NoOutcome {
			done <- true
		}
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()
	<-done
	fmt.Println(game.Outcome())
}

func prompt(moveNo int, colour chess.Color) string {
	switch colour {
	case chess.White:
		return fmt.Sprintf("%d: ", moveNo)
	case chess.Black:
		return fmt.Sprintf("%d: ... ", moveNo)
	}
	panic("bad colour")
}

func readInput(moveNo int, colour chess.Color, r *bufio.Reader) string {
	os.Stdout.WriteString(prompt(moveNo, colour))
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

func playGame(ctx context.Context, ch *ably.RealtimeChannel, name string, user string, colour chess.Color) {
	game := chess.NewGame()
	waitChan := make(chan msg)
	unsub, err := ch.Subscribe(ctx, name, func(message *ably.Message) {
		if message.ClientID == user {
			// already process my own move
			return
		}

		waitChan <- decodeMsg(message)
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()

	if colour == chess.Black {
		handleOpponentMove(ctx, game, waitChan)
	}

	userIn := bufio.NewReader(os.Stdin)
	m := 0
	for game.Outcome() == chess.NoOutcome {
		m++
		fen, err := game.Position().MarshalText()
		if err != nil {
			log.Fatalln(err)
		}

		var myMove string
		for ctx.Err() == nil {
			myMove = readInput(m, colour, userIn)
			if myMove == resign {
				game.Resign(colour)
				break
			}
			err := game.MoveStr(myMove)
			if err == nil {
				break // the user has entered a legal move
			}
			// illegal move, print out an error, and try again
			fmt.Println(err)
		}
		if ctx.Err() != nil {
			return
		}

		fmt.Println(game.Position().Board().Draw())

		err = ch.Publish(ctx, name, msg{
			Move:   myMove,
			Colour: int(colour),
			FEN:    string(fen),
		})
		if err != nil {
			log.Fatalln(err)
		}
		if game.Outcome() != chess.NoOutcome {
			break
		}
		handleOpponentMove(ctx, game, waitChan)
	}
	fmt.Println(game)
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

	defer client.Close()

	channelName := "chess:" + *game
	channel := client.Channels.Get(channelName)
	//ably.ChannelWithParams("rewind", "1"))

	players, err := channel.Presence.Get(ctx)
	if err != nil {
		log.Fatalln(err)
	}
	switch len(players) {
	case 0:
		fmt.Println("you are white")
		err := channel.Presence.Enter(ctx, "white")
		if err != nil {
			log.Fatalln(err)
		}
		playGame(ctx, channel, channelName, *userName, chess.White)
	case 1:
		fmt.Println("you are playing black against", players[0].ClientID)
		err := channel.Presence.Enter(ctx, "black")
		if err != nil {
			log.Fatalln(err)
		}
		playGame(ctx, channel, channelName, *userName, chess.Black)

	default:
		fmt.Println("you are a spectator")
		for _, p := range players {
			fmt.Println(p.ClientID, p.Data)
		}
		watchGame(ctx, channel, channelName)
		return
	}

}
