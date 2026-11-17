package domain

// Target locates an action in frame space, the space screenshots and their
// annotations use. Region limits a screenshot to that rectangle.
type Target struct {
	X      int
	Y      int
	Region *Region
}

// Region is a rectangle in the frame coordinate space.
type Region struct {
	X      int
	Y      int
	Width  int
	Height int
}
