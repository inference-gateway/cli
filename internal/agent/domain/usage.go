package domain

// TokenCount dereferences an optional token count from a usage report, reading
// zero when the producer left the figure out.
func TokenCount(optional *int64) int {
	if optional == nil {
		return 0
	}
	return int(*optional)
}
