package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/ably/ably-go/ably"
	"github.com/notnil/chess"
	"log"
	"os"
	"strings"
)

func watchGame(ch *ably.RealtimeChannel, name string) {
	ctx := context.Background()
	game := chess.NewGame()

	done := make(chan bool)
	unsub, err := ch.Subscribe(ctx, name, func(message *ably.Message) {
		m, ok := message.Data.(string)
		if !ok {
			log.Fatalln(message, "message.Data is not a string")
		}
		fmt.Println(message.Data)
		if strings.HasSuffix(m, "resigned") {
			switch m[0] {
			case 'w':
				game.Resign(chess.White)
			case 'b':
				game.Resign(chess.Black)
			}
			done <- true
			return
		}

		err := game.MoveStr(m)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Println(game.Position().Board().Draw())
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()
	<-done
	fmt.Println(game.Outcome())
}

func readInput(pfx string, r *bufio.Reader) string {
	os.Stdout.WriteString(pfx)
	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatalln(err)
	}
	move := strings.TrimSpace(line)
	return move
}

func handleOpponentMove(game *chess.Game, waitCh chan string) {
	m := <-waitCh
	if strings.HasSuffix(m, "resigned") {
		switch m[0] {
		case 'w':
			game.Resign(chess.White)
		case 'b':
			game.Resign(chess.Black)
		}
		return
	}

	err := game.MoveStr(m)
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(game.Position().Board().Draw())

}

func playGame(ch *ably.RealtimeChannel, name string, user string, colour chess.Color) {
	ctx := context.Background()
	game := chess.NewGame()
	waitChan := make(chan string)
	unsub, err := ch.Subscribe(ctx, name, func(message *ably.Message) {
		if message.ClientID == user {
			// already process my own move
			return
		}
		m, ok := message.Data.(string)
		if !ok {
			log.Fatalln(message, "message.Data is not a string")
		}
		fmt.Println(message.Data)

		waitChan <- m
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()

	if colour == chess.Black {
		handleOpponentMove(game, waitChan)
	}

	userIn := bufio.NewReader(os.Stdin)
	m := 0
	for game.Outcome() == chess.NoOutcome {
		m++
		var prompt string
		switch colour {
		case chess.White:
			prompt = fmt.Sprintf("%d: ", m)
		case chess.Black:
			prompt = fmt.Sprintf("%d: ... ", m)
		}
		var myMove string
		for {
			myMove = readInput(prompt, userIn)
			if myMove == "resign" {
				game.Resign(colour)
				myMove = colour.String() + " resigned"
				break
			}
			err := game.MoveStr(myMove)
			if err != nil {
				fmt.Println(err)
			}
			if err == nil {
				break
			}
		}
		fmt.Println(game.Position().Board().Draw())
		ch.Publish(ctx, name, myMove)
		if game.Outcome() != chess.NoOutcome {
			break
		}
		handleOpponentMove(game, waitChan)
	}
	fmt.Println(game.Outcome())
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
	ctx := context.Background()
	client, err := ably.NewRealtime(
		ably.WithKey(key),
		ably.WithClientID(*userName))
	if err != nil {
		log.Fatalln(err)
	}
	defer client.Close()

	channelName := "chess:" + *game
	channel := client.Channels.Get(channelName)
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
		defer channel.Presence.Leave(ctx, *userName)
		playGame(channel, channelName, *userName, chess.White)
	case 1:
		fmt.Println("you are playing black against", players[0].ClientID)
		err := channel.Presence.Enter(ctx, "black")
		if err != nil {
			log.Fatalln(err)
		}
		defer channel.Presence.Leave(ctx, *userName)
		playGame(channel, channelName, *userName, chess.Black)

	default:
		fmt.Println("you are a spectator")
		for _, p := range players {
			fmt.Println(p.ClientID, p.Data)
		}
		watchGame(channel, channelName)
		return
	}

}
