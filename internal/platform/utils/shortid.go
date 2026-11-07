package utils

// ShortID returns the first eight characters of an id, enough to tell ids apart
// in a log line.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
