# chessdemo

a simple demo using ably realitime API to transmit chess moves.

## Dependencies

- go 1.19

## Installation

`go install github.com/ably/chessdemo@latest`

## Playing

```
export ABLY_KEY=xxxxxxxxxxxxxxxxx
chessdemo -name myname -game gameId
```

If you are the first player in the gameID game, then you play white.
If you are the second, you play black.
Otherwise you are a spectator.

Input can be any move in algebraic notation, or the string `resign`

## Bugs
- All players require my Abley API Key
