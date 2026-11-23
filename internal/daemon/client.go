package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	config "github.com/inference-gateway/cli/config"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// Client is one connection of a CLI command to the local daemon: it sends
// frames and reads the answers back by type.
type Client struct {
	conn   *agui.Conn
	frames chan []byte
	once   sync.Once
}

func (c *Client) Attach(*agui.Conn) {}

func (c *Client) Handle(_ *agui.Conn, frame []byte) {
	c.frames <- append([]byte(nil), frame...)
}

func (c *Client) Detach(*agui.Conn) {
	c.once.Do(func() { close(c.frames) })
}

// DialLocal connects a client of kind to the daemon cfg describes, over its
// TLS when it serves any.
func DialLocal(ctx context.Context, cfg *config.Config, kind string) (*Client, error) {
	dial, err := LocalDialConfig(ctx, cfg, kind)
	if err != nil {
		return nil, fmt.Errorf("%w, and start infer daemon", err)
	}
	client := &Client{frames: make(chan []byte, 256)}
	conn, err := agui.Dial(ctx, dial, client)
	if err != nil {
		return nil, fmt.Errorf("is infer daemon running? %w", err)
	}
	client.conn = conn
	return client, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Send(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.conn.Send(data)
}

// Next returns the next frame of one of the types, or the error a RUN_ERROR
// carries.
func (c *Client) Next(ctx context.Context, types ...string) (map[string]any, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case raw, ok := <-c.frames:
			if !ok {
				return nil, errors.New("the daemon hung up")
			}
			var frame map[string]any
			if json.Unmarshal(raw, &frame) != nil {
				continue
			}
			typ, _ := frame["type"].(string)
			for _, want := range types {
				if typ == want {
					return frame, nil
				}
			}
			if typ == "RUN_ERROR" {
				message, _ := frame["message"].(string)
				return nil, errors.New(message)
			}
		}
	}
}
