package main

import (
	"bufio"
	"context"
	"github.com/notnil/chess"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func Test_app_moveFromReader(t *testing.T) {
	tests := []struct {
		name         string
		colour       chess.Color
		input        string
		want         string
		wantResigned bool
	}{
		{"e4", chess.White, "e4\n", "e2e4", false},
		{"Bad move then e4", chess.White, "zz\ne4\n", "e2e4", false},
		{"impossible move then e4", chess.White, "exd5\ne4\n", "e2e4", false},
		{"resign", chess.White, "resign\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &app{}
			ri := a.newReaderInput(bufio.NewReader(strings.NewReader(tt.input)))
			a.game = chess.NewGame()
			a.colour = tt.colour
			move, resigned := ri.choose(context.Background())
			assert.Equal(t, tt.wantResigned, resigned)
			if !resigned {
				assert.Equal(t, tt.want, move.String())
			}
		})

	}
}
