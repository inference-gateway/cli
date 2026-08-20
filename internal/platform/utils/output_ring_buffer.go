package utils

import (
	"fmt"
	"sync"
)

// OutputRingBuffer is a thread-safe circular buffer that implements io.Writer.
// It provides bounded memory usage by overwriting oldest data when the buffer fills.
type OutputRingBuffer struct {
	buffer       []byte
	maxSize      int
	writePos     int
	totalWritten int64
	mutex        sync.RWMutex
}

// NewOutputRingBuffer creates a new ring buffer with the specified maximum size in bytes.
func NewOutputRingBuffer(maxSize int) *OutputRingBuffer {
	return &OutputRingBuffer{
		buffer:  make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

// Write implements io.Writer interface.
// Writes data to the buffer, wrapping around when full.
func (rb *OutputRingBuffer) Write(p []byte) (n int, err error) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	n = len(p)
	rb.totalWritten += int64(n)

	if len(rb.buffer) < rb.maxSize {
		spaceLeft := rb.maxSize - len(rb.buffer)

		if n <= spaceLeft {
			rb.buffer = append(rb.buffer, p...)
			rb.writePos = len(rb.buffer)
		} else {
			rb.buffer = append(rb.buffer, p[:spaceLeft]...)
			remaining := p[spaceLeft:]

			copy(rb.buffer, remaining)
			rb.writePos = len(remaining)
		}

		return n, nil
	}

	for len(p) > 0 {
		toCopy := len(p)
		spaceToEnd := rb.maxSize - rb.writePos

		if toCopy > spaceToEnd {
			toCopy = spaceToEnd
		}

		copy(rb.buffer[rb.writePos:rb.writePos+toCopy], p[:toCopy])
		rb.writePos += toCopy
		p = p[toCopy:]

		if rb.writePos >= rb.maxSize {
			rb.writePos = 0
		}
	}

	return n, nil
}

// ReadFrom returns all data from the specified offset onwards.
// The offset is absolute (based on totalWritten), not relative to buffer position.
// Returns the data as a string and the new offset to use for the next read.
func (rb *OutputRingBuffer) ReadFrom(offset int64) (string, int64) {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	if offset >= rb.totalWritten {
		return "", rb.totalWritten
	}

	availableBytes := rb.totalWritten - offset

	if len(rb.buffer) < rb.maxSize {
		startPos := int(offset)
		if startPos < 0 {
			startPos = 0
		}
		if startPos >= len(rb.buffer) {
			return "", rb.totalWritten
		}
		return string(rb.buffer[startPos:]), rb.totalWritten
	}

	oldestAvailableOffset := rb.totalWritten - int64(rb.maxSize)

	if offset < oldestAvailableOffset {
		offset = oldestAvailableOffset
		availableBytes = int64(rb.maxSize)
	}

	startPosInBuffer := int(offset % int64(rb.maxSize))
	bytesToRead := int(availableBytes)

	if bytesToRead > rb.maxSize {
		bytesToRead = rb.maxSize
	}

	var result []byte

	if startPosInBuffer+bytesToRead <= rb.maxSize {
		result = make([]byte, bytesToRead)
		copy(result, rb.buffer[startPosInBuffer:startPosInBuffer+bytesToRead])
	} else {
		firstPart := rb.maxSize - startPosInBuffer
		secondPart := bytesToRead - firstPart

		result = make([]byte, bytesToRead)
		copy(result, rb.buffer[startPosInBuffer:])
		copy(result[firstPart:], rb.buffer[:secondPart])
	}

	return string(result), rb.totalWritten
}

// Recent returns the most recent N bytes from the buffer.
// If maxBytes is larger than the buffer or total written, returns all available data.
func (rb *OutputRingBuffer) Recent(maxBytes int) string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	availableBytes := int(rb.totalWritten)
	if availableBytes > len(rb.buffer) {
		availableBytes = len(rb.buffer)
	}

	if maxBytes > availableBytes {
		maxBytes = availableBytes
	}

	if maxBytes <= 0 {
		return ""
	}

	if len(rb.buffer) < rb.maxSize {
		startPos := len(rb.buffer) - maxBytes
		return string(rb.buffer[startPos:])
	}

	endPos := rb.writePos
	if endPos == 0 {
		endPos = rb.maxSize
	}

	startPos := endPos - maxBytes

	if startPos >= 0 {
		return string(rb.buffer[startPos:endPos])
	}

	firstPart := rb.buffer[rb.maxSize+startPos:]
	secondPart := rb.buffer[:endPos]

	return string(firstPart) + string(secondPart)
}

// TotalWritten returns the total number of bytes written to the buffer.
func (rb *OutputRingBuffer) TotalWritten() int64 {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.totalWritten
}

// Size returns the current size of the buffer in bytes.
func (rb *OutputRingBuffer) Size() int {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return len(rb.buffer)
}

// MaxSize returns the maximum size of the buffer in bytes.
func (rb *OutputRingBuffer) MaxSize() int {
	return rb.maxSize
}

// String returns the entire current buffer contents as a string.
// This respects the circular nature and returns data in the correct order.
func (rb *OutputRingBuffer) String() string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	if len(rb.buffer) < rb.maxSize {
		return string(rb.buffer)
	}

	return string(rb.buffer[rb.writePos:]) + string(rb.buffer[:rb.writePos])
}

// Clear resets the buffer to empty state.
func (rb *OutputRingBuffer) Clear() {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	rb.buffer = make([]byte, 0, rb.maxSize)
	rb.writePos = 0
	rb.totalWritten = 0
}

// Stats returns statistics about the buffer.
func (rb *OutputRingBuffer) Stats() string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	return fmt.Sprintf("OutputRingBuffer: size=%d/%d, written=%d, writePos=%d",
		len(rb.buffer), rb.maxSize, rb.totalWritten, rb.writePos)
}
