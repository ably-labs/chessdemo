package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ably/ably-go/ably"
	"github.com/notnil/chess"
	"github.com/notnil/chess/image"
	"github.com/notnil/chess/uci"
	"github.com/pkg/browser"
	"io"
	"log"
	"os"
	"os/signal"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const resign = "resign"

type app struct {
	game             *chess.Game
	colour           chess.Color
	userID           string
	engine           string
	mover            mover
	engMoveTime      time.Duration
	isSpectator      bool
	drawSVG          bool
	oLock            sync.RWMutex
	opponent         string
	waitForOpponent  chan struct{}
	onOpponentArrive sync.Once
	iHaveEntered     chan struct{}
	onMyEntry        sync.Once

	gameID string
	moveNo int
	client *ably.Realtime
	ch     *ably.RealtimeChannel
	nShow  int
}

type msg struct {
	Move      string `json:"move,omitempty"`
	Algebriac string `json:"algebriac,omitempty"`
	Resigned  bool   `json:"resigned,omitempty"`
	MoveNum   int    `json:"move_num"`
	Colour    int    `json:"colour"`
	NextFEN   string `json:"next_FEN"`
	Result    string `json:"result"`
}

var (
	uciNotation       = chess.UCINotation{}
	algebraicNotation = chess.AlgebraicNotation{}
)

func (a *app) watchGame(ctx context.Context) {
	a.ch = a.client.Channels.Get("[?rewind=1]" + a.gameID)

	done := make(chan bool)
	nMove := 0
	unsub, err := a.ch.Subscribe(ctx, a.gameID, func(message *ably.Message) {
		nMove++
		m := decodeMsg(message)
		moved := false
		if nMove == 1 {
			fen, err := chess.FEN(m.NextFEN)
			if err != nil {
				log.Fatalln(err)
			}
			a.game = chess.NewGame(fen)
			moved = true
		}
		if m.Resigned {
			a.game.Resign(chess.Color(m.Colour))
			done <- true
			return
		}
		if !moved {
			move, err := uciNotation.Decode(a.game.Position(), m.Move)
			if err != nil {
				log.Fatalln(err)
			}

			err = a.game.Move(move)

			if err != nil {
				log.Fatalln(err)
			}

		}
		a.moveNo = m.MoveNum
		a.colour = chess.Color(m.Colour)

		fmt.Println(a.prompt(), m.Algebriac)
		fmt.Println(a.game.Position().Board().Draw())
		if a.drawSVG {
			a.showSVG()
		}
		if a.gameIsOver(ctx) {
			done <- true
		}
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer unsub()
	<-done
	fmt.Println(a.game, a.game.Method())
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

func (r *readerInput) readInput() string {
	os.Stdout.WriteString(r.a.prompt())
	line, err := r.userIn.ReadString('\n')
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
	if m.Resigned {
		game.Resign(chess.Color(m.Colour))
		return
	}

	move, err := uciNotation.Decode(game.Position(), m.Move)
	if err != nil {
		log.Fatalln(err)
	}

	err = game.Move(move)
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(m.Algebriac)
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
	if a.engine != "" {
		a.mover = a.startEngine()
	} else {
		a.mover = a.newReaderInput(os.Stdin)
	}
	defer a.mover.close()
	defer unsub()

	// Print initial position.
	fmt.Println(a.game.Position().Board().Draw())

	switch a.colour {
	case chess.White:
		fmt.Println("Waiting for an opponent to arrive.")
		<-a.waitForOpponent
		fmt.Println("Your opponent", a.Opponent(), "is playing black.")
		time.Sleep(time.Second)
	case chess.Black:
		// If we are black, our opponent moves first.
		handleOpponentMove(ctx, a.game, waitChan)
	}

	for !a.gameIsOver(ctx) {
		a.moveNo++
		a.handleMyMove(ctx)
		if a.gameIsOver(ctx) {
			break
		}

		handleOpponentMove(ctx, a.game, waitChan)
	}
	fmt.Println(a.game, a.game.Method())
}

func (a *app) gameIsOver(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	return a.game.Outcome() != chess.NoOutcome
}

func (a *app) showSVG() error {
	f, err := os.Create(path.Join(os.TempDir(), "chess.svg"))
	if err != nil {
		return err
	}
	defer f.Close()
	err = image.SVG(f, a.game.Position().Board())
	if err != nil {
		return err
	}
	log.Println("Drawing board to", f.Name())

	err = f.Close()
	if err != nil {
		return err
	}
	a.nShow++
	if a.nShow > 1 {
		return nil
	}
	return browser.OpenFile(f.Name())
}

type mover interface {
	choose(ctx context.Context) (*chess.Move, bool)
	close()
}

type readerInput struct {
	userIn *bufio.Reader
	a      *app
}

func (a *app) newReaderInput(r io.Reader) *readerInput {
	return &readerInput{
		userIn: bufio.NewReader(r),
		a:      a,
	}
}

func (r *readerInput) choose(ctx context.Context) (move *chess.Move, resigned bool) {
	for ctx.Err() == nil {
		myMove := r.readInput()
		if myMove == resign {
			r.a.game.Resign(r.a.colour)
			return nil, true
		}
		if myMove == "show" {
			err := r.a.showSVG()
			if err != nil {
				log.Println(err)
				continue
			}
			continue
		}
		var err error
		move, err = algebraicNotation.Decode(r.a.game.Position(), myMove)
		if err != nil {
			fmt.Println("Can not decode move", err)
			continue
		}
		g2 := r.a.game.Clone()
		err = g2.Move(move)
		if err != nil {
			fmt.Println("illegal move", err)
			continue
		}
		break
	}
	return move, false
}

func (r *readerInput) close() {}

type engineInput struct {
	engine *uci.Engine
	a      *app
}

func (a *app) startEngine() *engineInput {
	eng, err := uci.New(a.engine)
	if err != nil {
		log.Fatalln(err)
	}
	// initialize uci with new game
	threadsCmd := uci.CmdSetOption{Name: "Threads", Value: strconv.Itoa(runtime.NumCPU())}
	err = eng.Run(uci.CmdUCI, uci.CmdIsReady, threadsCmd, uci.CmdUCINewGame)

	if err != nil {
		log.Fatalln(err)
	}
	return &engineInput{engine: eng, a: a}
}

func (e *engineInput) close() {
	e.engine.Close()
}

func (e *engineInput) choose(ctx context.Context) (*chess.Move, bool) {

	prevPos := e.a.game.Position()
	cmdPos := uci.CmdPosition{Position: prevPos}
	cmdGo := uci.CmdGo{MoveTime: e.a.engMoveTime}
	err := e.engine.Run(cmdPos, cmdGo)
	if err != nil {
		log.Fatalln(err)
	}
	sr := e.engine.SearchResults()

	log.Printf("depth %d, %d nodes searched in %s, %d nodes per second",
		sr.Info.Depth, sr.Info.Nodes, sr.Info.Time, sr.Info.NPS)
	return sr.BestMove, false
}

func (a *app) handleMyMove(ctx context.Context) {
	fen, err := a.game.Position().MarshalText()
	if err != nil {
		log.Fatalln(err)
	}

	myMove, resigned := a.mover.choose(ctx)
	if ctx.Err() != nil {
		return
	}

	if resigned {
		err = a.ch.Publish(ctx, a.gameID, msg{
			Resigned: resigned,
			Colour:   int(a.colour),
			MoveNum:  a.moveNo,
			NextFEN:  string(fen),
		})
		if err != nil {
			log.Fatalln(err)
		}
		return
	}
	alg := algebraicNotation.Encode(a.game.Position(), myMove)
	err = a.game.Move(myMove)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(a.game.Position().Board().Draw())
	nextFen, err := a.game.Position().MarshalText()
	if err != nil {
		log.Fatalln(err)
	}

	result := ""
	if a.game.Outcome() != chess.NoOutcome {
		result = fmt.Sprintf("%s %s", a.game.Outcome(), a.game.Method().String())
	}

	uciMove := uciNotation.Encode(a.game.Position(), myMove)

	err = a.ch.Publish(ctx, a.gameID, msg{
		Move:      uciMove,
		Algebriac: alg,
		Resigned:  resigned,
		Colour:    int(a.colour),
		MoveNum:   a.moveNo,
		NextFEN:   string(nextFen),
		Result:    result,
	})
	if err != nil {
		log.Fatalln(err)
	}
}

func (a *app) setOppent(id string) {
	a.oLock.Lock()
	a.opponent = id
	a.oLock.Unlock()
}

func (a *app) Opponent() string {
	a.oLock.RLock()
	defer a.oLock.RUnlock()
	return a.opponent
}

func (a *app) handlePresenceEvent(message *ably.PresenceMessage, cancel func()) {
	//log.Println(message)
	switch message.Action {
	case ably.PresenceActionEnter:
		if message.ClientID == a.userID {
			a.onMyEntry.Do(func() {
				close(a.iHaveEntered)
			})
			return
		}

		a.onOpponentArrive.Do(func() {
			a.setOppent(message.ClientID)
			close(a.waitForOpponent)
		})
	case ably.PresenceActionLeave:
		opponentGone := message.ClientID == a.Opponent()
		if opponentGone {
			log.Println("opponent", a.Opponent(), "has left the game")
			log.Println(a.game, a.game.Method())
			a.client.Close()
			cancel()
			os.Exit(0)
		}
	}
}

func (a *app) engineText() string {
	if a.engine == "" {
		return ""
	}
	return fmt.Sprintf(" (using %s)", a.engine)
}

func presenceStr(p *ably.PresenceMessage) string {
	return fmt.Sprintf("%s (%s)", p.ClientID, p.Data)
}

func (a *app) determineMyColour(ctx context.Context) chess.Color {
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
	case players[1].ClientID == a.userID:
		a.setOppent(players[0].ClientID)
		a.colour = chess.Black
	default:
		a.colour = chess.NoColor
		a.isSpectator = true
		a.setOppent(players[0].ClientID)
		fmt.Println("You are watching the game:", presenceStr(players[0]), " v ", presenceStr(players[1]))
	}
	return a.colour
}

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime)
	a := app{
		game:            chess.NewGame(),
		waitForOpponent: make(chan struct{}),
		iHaveEntered:    make(chan struct{}),
	}
	flag.StringVar(&a.userID, "name", "", "your name")
	flag.StringVar(&a.gameID, "game", "game1", "game name")
	flag.StringVar(&a.engine, "engine", "", "run UCI engine")
	flag.DurationVar(&a.engMoveTime, "time", 500*time.Millisecond, "how much time to allow engine to make each move")
	flag.BoolVar(&a.isSpectator, "watch", false, "watch game")
	flag.BoolVar(&a.drawSVG, "svg", false, "draw SVG of each move")
	flag.Parse()
	if a.userID == "" {
		log.Fatalln("You must provide a -name argument")
	}

	key := os.Getenv("ABLY_KEY")
	if key == "" {
		log.Fatalln("you must set ABLY_KEY")
	}
	ctx, cancel := context.WithCancel(context.Background())

	var err error
	a.client, err = ably.NewRealtime(
		ably.WithKey(key),
		ably.WithClientID(a.userID))
	if err != nil {
		log.Fatalln(err)
	}
	go func() {
		// TERM or KILL signal should result in a graceful shutdown.
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		s := <-sigs
		log.Println("Got signal", s, "shutting down.")
		fmt.Println(a.game)
		a.client.Close()
		cancel()

		// If we are still running, we are stuck in a blocking read, so force-close.
		time.Sleep(time.Second)
		os.Exit(0)
	}()

	defer a.client.Close()

	if a.isSpectator {
		a.watchGame(ctx)
		return
	}

	a.ch = a.client.Channels.Get(a.gameID)

	cancelSubscription, err := a.ch.Presence.SubscribeAll(ctx, func(message *ably.PresenceMessage) {
		a.handlePresenceEvent(message, cancel)
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer cancelSubscription()

	err = a.ch.Presence.Enter(ctx, a.userID)
	if err != nil {
		log.Fatalln(err)
	}

	// We need to wait until we appear in presence.
	select {
	case <-a.iHaveEntered:
	case <-time.After(time.Second):
	}

	colour := a.determineMyColour(ctx)
	if colour == chess.NoColor {
		a.watchGame(ctx)
		return
	}
	a.ch.Presence.Update(ctx, colour.Name())
	fmt.Println("you are " + a.colour.Name() + a.engineText())
	if a.Opponent() != "" {
		fmt.Println("playing against", a.Opponent())
	}
	a.playGame(ctx)
}
