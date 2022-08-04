# chessdemo

a simple demo using ably realitime API to transmit chess moves.

## Dependencies

- go 1.19

## Installation

`go install github.com/ably-labs/chessdemo@latest`

Alternatively, if you don't have Go installed, you can just grab 
pre-complied executables for your machine from 
[here](https://github.com/ably-labs/chessdemo/releases).

## Playing

```
export ABLY_KEY=xxxxxxxxxxxxxxxxx
chessdemo -name myname -game gameId
```

If you are the first player in the gameID game, then you play white.
If you are the second, you play black.
Otherwise you are a spectator.

Input can be any move in [algebraic notation](https://en.wikipedia.org/wiki/Algebraic_notation_(chess)), or the string `resign`

## Example Session

```
% ./chessdemo -name amnon
you are white
Waiting for an opponent to arrive.
Your opponent bill is playing black.
1: e4

 A B C D E F G H
8♜ ♞ ♝ ♛ ♚ ♝ ♞ ♜
7♟ ♟ ♟ ♟ ♟ ♟ ♟ ♟
6- - - - - - - -
5- - - - - - - -
4- - - - ♙ - - -
3- - - - - - - -
2♙ ♙ ♙ ♙ - ♙ ♙ ♙
1♖ ♘ ♗ ♕ ♔ ♗ ♘ ♖

f6

 A B C D E F G H
8♜ ♞ ♝ ♛ ♚ ♝ ♞ ♜
7♟ ♟ ♟ ♟ ♟ - ♟ ♟
6- - - - - ♟ - -
5- - - - - - - -
4- - - - ♙ - - -
3- - - - - - - -
2♙ ♙ ♙ ♙ - ♙ ♙ ♙
1♖ ♘ ♗ ♕ ♔ ♗ ♘ ♖

2: Nc3

 A B C D E F G H
8♜ ♞ ♝ ♛ ♚ ♝ ♞ ♜
7♟ ♟ ♟ ♟ ♟ - ♟ ♟
6- - - - - ♟ - -
5- - - - - - - -
4- - - - ♙ - - -
3- - ♘ - - - - -
2♙ ♙ ♙ ♙ - ♙ ♙ ♙
1♖ - ♗ ♕ ♔ ♗ ♘ ♖

g5

 A B C D E F G H
8♜ ♞ ♝ ♛ ♚ ♝ ♞ ♜
7♟ ♟ ♟ ♟ ♟ - - ♟
6- - - - - ♟ - -
5- - - - - - ♟ -
4- - - - ♙ - - -
3- - ♘ - - - - -
2♙ ♙ ♙ ♙ - ♙ ♙ ♙
1♖ - ♗ ♕ ♔ ♗ ♘ ♖

3: Qh5

 A B C D E F G H
8♜ ♞ ♝ ♛ ♚ ♝ ♞ ♜
7♟ ♟ ♟ ♟ ♟ - - ♟
6- - - - - ♟ - -
5- - - - - - ♟ ♕
4- - - - ♙ - - -
3- - ♘ - - - - -
2♙ ♙ ♙ ♙ - ♙ ♙ ♙
1♖ - ♗ - ♔ ♗ ♘ ♖


1. e4 f6 2. Nc3 g5 3. Qh5# 1-0
```


## Bugs
- All players require my Abley API Key
- No way to offer or accept a draw
- clunky user interface
