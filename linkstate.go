package main

type LinkState uint8

const (
	StateWalk LinkState = iota
	StateFall
	StateSwim
	StatePipe
)

func (s LinkState) String() string {
	switch s {
	case StateWalk:
		return "walk"
	case StateFall:
		return "fall"
	case StateSwim:
		return "swim"
	case StatePipe:
		return "pipe"
	default:
		panic("bad LinkState")
	}
}
