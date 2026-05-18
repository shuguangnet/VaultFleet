package connect

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"vaultfleet/pkg/protocol"
)

const (
	InitialBackoff            = time.Second
	MaxBackoff                = 5 * time.Minute
	BackoffFactor             = 2.0
	stableConnectionThreshold = 5 * time.Second
)

var ErrNotConnected = errors.New("not connected")

type MessageHandler func(msg protocol.Message)

type Client struct {
	serverURL string
	token     string
	handler   MessageHandler

	mu      sync.RWMutex
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func NewClient(serverURL, token string, handler MessageHandler) *Client {
	return &Client{
		serverURL: serverURL,
		token:     token,
		handler:   handler,
	}
}

func (c *Client) Run(ctx context.Context) {
	backoff := InitialBackoff
	for {
		if ctx.Err() != nil {
			c.Close()
			return
		}

		if err := c.connect(ctx); err != nil {
			log.Printf("agent websocket connect failed: %v", err)
			if !sleepWithContext(ctx, backoff) {
				c.Close()
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		connectedAt := time.Now()
		c.readLoop(ctx)

		if ctx.Err() != nil {
			c.Close()
			return
		}

		shortLived := time.Since(connectedAt) < stableConnectionThreshold
		if !shortLived {
			backoff = InitialBackoff
		}
		if !sleepWithContext(ctx, backoff) {
			c.Close()
			return
		}
		if shortLived {
			backoff = nextBackoff(backoff)
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	wsURL, err := agentWebSocketURL(c.serverURL, c.token)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}

	c.setConn(conn)
	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	conn := c.currentConn()
	if conn == nil {
		return
	}

	closeOnCancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closeOnCancelDone:
		}
	}()
	defer close(closeOnCancelDone)
	defer func() {
		_ = conn.Close()
		c.clearConn(conn)
	}()

	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			if ctx.Err() == nil {
				log.Printf("agent websocket read failed: %v", err)
			}
			return
		}
		if c.handler != nil {
			c.handler(msg)
		}
	}
}

func (c *Client) Send(msg protocol.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn := c.currentConn()
	if conn == nil {
		return ErrNotConnected
	}
	return conn.WriteJSON(msg)
}

func (c *Client) Close() {
	conn := c.currentConn()
	if conn != nil {
		_ = conn.Close()
	}
	c.clearConn(conn)
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return InitialBackoff
	}
	next := time.Duration(float64(current) * BackoffFactor)
	if next > MaxBackoff {
		return MaxBackoff
	}
	return next
}

func BackoffForAttempt(attempt int) time.Duration {
	backoff := InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff = nextBackoff(backoff)
	}
	return backoff
}

func agentWebSocketURL(serverURL, token string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("server URL must use http, https, ws, or wss")
	}
	if u.Host == "" {
		return "", errors.New("server URL must include a host")
	}

	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/ws/agent"
	u.RawQuery = url.Values{"token": []string{token}}.Encode()
	return u.String(), nil
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Client) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	previous := c.conn
	c.conn = conn
	c.mu.Unlock()

	if previous != nil && previous != conn {
		_ = previous.Close()
	}
}

func (c *Client) currentConn() *websocket.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *Client) clearConn(conn *websocket.Conn) {
	c.mu.Lock()
	if conn == nil || c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}
