package domain

// Target locates an action on screen, in the frame coordinate space (the
// same space screenshots and their annotations use).
type Target struct {
	X int
	Y int
	// Region, on a screenshot action, captures only this rectangle.
	Region *Region
}

// Region is a rectangle in the frame coordinate space.
type Region struct {
	X      int
	Y      int
	Width  int
	Height int
}
