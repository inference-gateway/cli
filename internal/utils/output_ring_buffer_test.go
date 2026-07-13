package utils

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestNewOutputRingBuffer(t *testing.T) {
	rb := NewOutputRingBuffer(1024)

	if rb == nil {
		t.Fatal("NewOutputRingBuffer returned nil")
	}

	if rb.MaxSize() != 1024 {
		t.Errorf("Expected maxSize=1024, got %d", rb.MaxSize())
	}

	if rb.Size() != 0 {
		t.Errorf("Expected size=0, got %d", rb.Size())
	}

	if rb.TotalWritten() != 0 {
		t.Errorf("Expected totalWritten=0, got %d", rb.TotalWritten())
	}
}

func TestWrite(t *testing.T) {
	tests := []struct {
		name         string
		maxSize      int
		writes       []string
		wantSize     int
		wantTotal    int64
		wantContents string
	}{
		{
			name:         "simple write",
			maxSize:      100,
			writes:       []string{"Hello, World!"},
			wantSize:     13,
			wantTotal:    13,
			wantContents: "Hello, World!",
		},
		{
			name:         "multiple writes",
			maxSize:      100,
			writes:       []string{"Hello", " ", "World", "!"},
			wantSize:     12,
			wantTotal:    12,
			wantContents: "Hello World!",
		},
		{
			name:         "wraparound",
			maxSize:      10,
			writes:       []string{"0123456789ABCDE"},
			wantSize:     10,
			wantTotal:    15,
			wantContents: "56789ABCDE",
		},
		{
			name:         "multiple wraparounds",
			maxSize:      5,
			writes:       []string{"12345", "67890", "ABCDE"},
			wantSize:     5,
			wantTotal:    15,
			wantContents: "ABCDE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewOutputRingBuffer(tt.maxSize)

			for _, w := range tt.writes {
				n, err := rb.Write([]byte(w))
				if err != nil {
					t.Fatalf("Write failed: %v", err)
				}
				if n != len(w) {
					t.Errorf("Expected to write %d bytes, wrote %d", len(w), n)
				}
			}

			if rb.Size() != tt.wantSize {
				t.Errorf("Expected size=%d, got %d", tt.wantSize, rb.Size())
			}

			if rb.TotalWritten() != tt.wantTotal {
				t.Errorf("Expected totalWritten=%d, got %d", tt.wantTotal, rb.TotalWritten())
			}

			if rb.String() != tt.wantContents {
				t.Errorf("Expected buffer contents %q, got %q", tt.wantContents, rb.String())
			}
		})
	}
}

func TestReadFrom_NoWrap(t *testing.T) {
	rb := NewOutputRingBuffer(100)

	_, _ = rb.Write([]byte("Hello World"))

	tests := []struct {
		name     string
		offset   int64
		expected string
	}{
		{"From beginning", 0, "Hello World"},
		{"From middle", 6, "World"},
		{"From end", 11, ""},
		{"Beyond end", 20, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, newOffset := rb.ReadFrom(tt.offset)

			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}

			if newOffset != rb.TotalWritten() {
				t.Errorf("Expected newOffset=%d, got %d", rb.TotalWritten(), newOffset)
			}
		})
	}
}

func TestReadFrom_WithWrap(t *testing.T) {
	rb := NewOutputRingBuffer(10)

	_, _ = rb.Write([]byte("0123456789"))
	_, _ = rb.Write([]byte("ABCDEFGHIJ"))
	_, _ = rb.Write([]byte("KLMNO"))

	t.Logf("Buffer state: %s", rb.Stats())
	t.Logf("Buffer contents: %q", rb.String())

	tests := []struct {
		name   string
		offset int64
		minLen int
	}{
		{"Old offset (overwritten)", 0, 10},
		{"Oldest available", 15, 10},
		{"From middle", 20, 5},
		{"Latest", 25, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, newOffset := rb.ReadFrom(tt.offset)

			if len(result) < tt.minLen {
				t.Errorf("Expected at least %d chars, got %d (%q)", tt.minLen, len(result), result)
			}

			if newOffset != rb.TotalWritten() {
				t.Errorf("Expected newOffset=%d, got %d", rb.TotalWritten(), newOffset)
			}
		})
	}
}

func TestReadFrom_Incremental(t *testing.T) {
	rb := NewOutputRingBuffer(100)

	_, _ = rb.Write([]byte("Line 1\n"))
	offset := int64(0)

	result1, offset := rb.ReadFrom(offset)
	if result1 != "Line 1\n" {
		t.Errorf("Expected 'Line 1\\n', got %q", result1)
	}

	result2, offset := rb.ReadFrom(offset)
	if result2 != "" {
		t.Errorf("Expected empty string, got %q", result2)
	}

	_, _ = rb.Write([]byte("Line 2\n"))

	result3, _ := rb.ReadFrom(offset)
	if result3 != "Line 2\n" {
		t.Errorf("Expected 'Line 2\\n', got %q", result3)
	}
}

func TestRecent(t *testing.T) {
	tests := []struct {
		name     string
		bufSize  int
		write    string
		maxBytes int
		expected string
	}{
		{"Last 5 bytes", 100, "Hello World", 5, "World"},
		{"Last 11 bytes", 100, "Hello World", 11, "Hello World"},
		{"More than available", 100, "Hello World", 50, "Hello World"},
		{"Zero bytes", 100, "Hello World", 0, ""},
		{"Negative bytes", 100, "Hello World", -5, ""},
		{"With wrap: last 5 bytes", 10, "0123456789ABCDE", 5, "ABCDE"},
		{"With wrap: last 10 bytes", 10, "0123456789ABCDE", 10, "56789ABCDE"},
		{"With wrap: more than buffer", 10, "0123456789ABCDE", 20, "56789ABCDE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewOutputRingBuffer(tt.bufSize)
			_, _ = rb.Write([]byte(tt.write))

			result := rb.Recent(tt.maxBytes)

			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestClear(t *testing.T) {
	rb := NewOutputRingBuffer(100)

	_, _ = rb.Write([]byte("Hello World"))
	rb.Clear()

	if rb.Size() != 0 {
		t.Errorf("Expected size=0 after clear, got %d", rb.Size())
	}

	if rb.TotalWritten() != 0 {
		t.Errorf("Expected totalWritten=0 after clear, got %d", rb.TotalWritten())
	}

	if rb.String() != "" {
		t.Errorf("Expected empty buffer after clear, got %q", rb.String())
	}
}

func TestConcurrentWrites(t *testing.T) {
	rb := NewOutputRingBuffer(1000)

	var wg sync.WaitGroup
	numWriters := 10
	writesPerWriter := 100

	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < writesPerWriter; j++ {
				data := fmt.Sprintf("Writer %d: Line %d\n", id, j)
				_, _ = rb.Write([]byte(data))
			}
		}(i)
	}

	wg.Wait()

	expectedWrites := int64(numWriters * writesPerWriter)
	lines := strings.Split(rb.String(), "\n")

	if rb.TotalWritten() < expectedWrites {
		t.Errorf("Some writes were lost: expected >=%d total bytes written", expectedWrites)
	}

	t.Logf("Concurrent test completed: %d writers, %d writes each, %d total bytes written",
		numWriters, writesPerWriter, rb.TotalWritten())
	t.Logf("Buffer contains %d lines", len(lines)-1)
}

func TestConcurrentReads(t *testing.T) {
	rb := NewOutputRingBuffer(1000)

	for i := 0; i < 100; i++ {
		_, _ = fmt.Fprintf(rb, "Line %d\n", i)
	}

	var wg sync.WaitGroup
	numReaders := 10

	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()

			offset := int64(0)
			for j := 0; j < 10; j++ {
				_, offset = rb.ReadFrom(offset)
			}
		}(i)
	}

	wg.Wait()

	t.Log("Concurrent reads completed without crashes")
}

func TestConcurrentReadWrite(t *testing.T) {
	rb := NewOutputRingBuffer(1000)

	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
				_, _ = fmt.Fprintf(rb, "Line %d\n", i)
			}
		}
	}()

	numReaders := 5
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()

			offset := int64(0)
			for j := 0; j < 100; j++ {
				_, offset = rb.ReadFrom(offset)
			}
		}(i)
	}

	close(done)
	wg.Wait()

	t.Log("Concurrent read/write completed without crashes")
}

func TestIOWriterInterface(t *testing.T) {
	rb := NewOutputRingBuffer(100)

	n, err := rb.Write([]byte("Hello"))

	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != 5 {
		t.Errorf("Expected to write 5 bytes, wrote %d", n)
	}
}

func TestStats(t *testing.T) {
	rb := NewOutputRingBuffer(100)

	_, _ = rb.Write([]byte("Hello World"))

	stats := rb.Stats()

	if stats == "" {
		t.Error("Stats returned empty string")
	}

	if !strings.Contains(stats, "size=") {
		t.Error("Stats missing size information")
	}

	t.Logf("Stats output: %s", stats)
}

func BenchmarkWrite_Small(b *testing.B) {
	rb := NewOutputRingBuffer(1024 * 1024)
	data := []byte("Hello, World!")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = rb.Write(data)
	}
}

func BenchmarkWrite_Large(b *testing.B) {
	rb := NewOutputRingBuffer(1024 * 1024)
	data := make([]byte, 1024)

	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = rb.Write(data)
	}
}

func BenchmarkReadFrom(b *testing.B) {
	rb := NewOutputRingBuffer(1024 * 1024)

	for i := 0; i < 1000; i++ {
		_, _ = fmt.Fprintf(rb, "Line %d\n", i)
	}

	b.ResetTimer()

	offset := int64(0)
	for i := 0; i < b.N; i++ {
		_, offset = rb.ReadFrom(offset)
		if offset >= rb.TotalWritten() {
			offset = 0
		}
	}
}

func BenchmarkRecent(b *testing.B) {
	rb := NewOutputRingBuffer(1024 * 1024)

	for i := 0; i < 1000; i++ {
		_, _ = fmt.Fprintf(rb, "Line %d\n", i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rb.Recent(1024)
	}
}
