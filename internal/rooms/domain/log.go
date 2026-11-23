package domain

// Log is a room's append-only post log. Posts are written in order and read
// back whole, and nothing between them is ever rewritten.
type Log interface {
	Append(room Room, post Post) error
	Read(room Room) ([]Post, error)
}
