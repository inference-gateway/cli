// Command mock-gateway serves the internal/mockgateway scenario server as a
// standalone binary for manual testing and terminal-automation harnesses.
// Go tests use the internal/mockgateway package in-process instead.
//
// The first stdout line is the readiness protocol for harnesses:
//
//	mock-gateway listening on http://127.0.0.1:<port>
//
// One line per request is logged to stderr, including the resolved scenario
// and step from the X-Mockgateway-* response headers.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/inference-gateway/cli/internal/mockgateway"
)

func main() {
	host := flag.String("host", "127.0.0.1", "host/interface to bind (use 0.0.0.0 in a container)")
	port := flag.Int("port", 0, "port to listen on (0 picks a free port)")
	scenarios := flag.String("scenarios", "", "path to a scenarios YAML file (default: built-in library)")
	flag.Parse()

	defs, err := loadScenarios(*scenarios)
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *host, *port))
	if err != nil {
		log.Fatalf("listening: %v", err)
	}

	fmt.Printf("mock-gateway listening on http://%s\n", ln.Addr())
	if err := http.Serve(ln, logRequests(mockgateway.New(defs))); err != nil {
		log.Fatalf("serving: %v", err)
	}
}

func loadScenarios(path string) (*mockgateway.ScenarioFile, error) {
	if path == "" {
		return mockgateway.Default(), nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenarios: %w", err)
	}
	return mockgateway.Load(b)
}

// logRequests prints one line per request to stderr so harnesses can follow
// which scenario and step each call resolved to.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s scenario=%q step=%s",
			r.Method, r.URL.Path,
			w.Header().Get("X-Mockgateway-Scenario"),
			w.Header().Get("X-Mockgateway-Step"))
	})
}
