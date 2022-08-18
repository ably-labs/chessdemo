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
		name   string
		colour chess.Color
		input  string
		want   string
	}{
		{"e4", chess.White, "e4\n", "e4"},
		{"Bad move then e4", chess.White, "zz\ne4\n", "e4"},
		{"impossible move then e4", chess.White, "exd5\ne4\n", "e4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &app{}
			a.game = chess.NewGame()
			a.colour = tt.colour
			move := a.moveFromReader(context.Background(), bufio.NewReader(strings.NewReader(tt.input)))
			assert.Equal(t, tt.want, move)
		})

	}
}
