package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// IPCClient speaks mpv's JSON IPC over one connection. mpv interleaves asynchronous events
// with command replies on the same stream, so a reply is matched by request_id and every
// event read on the way is queued for WaitForPlaybackSuccess rather than dropped.
type IPCClient struct {
	conn          net.Conn
	reader        *bufio.Reader
	mu            sync.Mutex // one reader and one writer at a time over the whole connection
	seq           int64
	eventQueue    []map[string]any
	superseded    bool
	activeEntryID int64
	loadedEntryID int64
}

type IPCResponse struct {
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

// commandTimeout bounds one reply: mpv answers a property read in microseconds, so a reply
// this late means the player is wedged and the caller should hear so, not hang.
const commandTimeout = 5 * time.Second

func newIPCClient(c net.Conn) *IPCClient {
	return &IPCClient{conn: c, reader: bufio.NewReader(c)}
}

func (c *IPCClient) Close() error { return c.conn.Close() }

// ClearEvents drops any asynchronous events buffered so far.
func (c *IPCClient) ClearEvents() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventQueue = nil
}

// Superseded reports whether the last WaitForPlaybackSuccess call completed because
// another play replaced this one rather than because our entry loaded.
func (c *IPCClient) Superseded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.superseded
}

// LoadedEntryID returns the playlist entry id that finished loading in the last
// successful WaitForPlaybackSuccess call (or 0 if superseded or none).
func (c *IPCClient) LoadedEntryID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadedEntryID
}

// NextEvent pops a queued asynchronous event or reads the next one from mpv.
func (c *IPCClient) NextEvent(deadline time.Time) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.eventQueue) > 0 {
		ev := c.eventQueue[0]
		c.eventQueue = c.eventQueue[1:]
		return ev, nil
	}
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		if json.Unmarshal(line, &raw) != nil {
			continue
		}
		if _, ok := raw["event"].(string); ok {
			return raw, nil
		}
	}
}

// readMessage reads one line; an event is queued and reported as nil.
func (c *IPCClient) readMessage() (map[string]any, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if json.Unmarshal(line, &raw) != nil {
		return nil, nil
	}
	if _, ok := raw["event"].(string); ok {
		c.eventQueue = append(c.eventQueue, raw)
		return nil, nil
	}
	return raw, nil
}

func (c *IPCClient) Command(args ...any) (*IPCResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	reqID := c.seq
	payload, err := json.Marshal(map[string]any{"command": args, "request_id": reqID})
	if err != nil {
		return nil, err // NaN and the like: an empty line would only earn a timeout
	}
	c.conn.SetDeadline(time.Now().Add(commandTimeout))
	defer c.conn.SetDeadline(time.Time{})
	if _, err := c.conn.Write(append(payload, '\n')); err != nil {
		return nil, err
	}
	for {
		raw, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if id, ok := raw["request_id"].(float64); ok && int64(id) == reqID {
			resp := &IPCResponse{Error: fmt.Sprint(raw["error"])}
			resp.Data, _ = json.Marshal(raw["data"])
			return resp, nil
		}
	}
}

// Get reads one property into v; an unavailable property (time-pos while loading) is
// reported as ok=false, not as an error.
func (c *IPCClient) Get(name string, v any) (bool, error) {
	r, err := c.Command("get_property", name)
	if err != nil {
		return false, err
	}
	if r.Error != "success" {
		return false, nil
	}
	return json.Unmarshal(r.Data, v) == nil, nil
}

// WaitForPlaybackSuccess waits for the entry loadfile answered with to come up. mpv's order is
// start-file{id} then file-loaded (which carries no id, so only one seen AFTER our
// start-file is ours); a failed load is end-file{id, reason:error}. Everything queued before
// our start-file belongs to an earlier entry and is discarded. Being replaced by a newer play
// is not a failure of this one.
func (c *IPCClient) WaitForPlaybackSuccess(entryID int64, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	deadline := time.Now().Add(timeout)
	defer c.conn.SetReadDeadline(time.Time{}) // a later Command must not inherit this deadline

	sawStart := false
	c.superseded = false
	c.activeEntryID = 0
	c.loadedEntryID = 0
	for {
		for i := 0; i < len(c.eventQueue); i++ {
			ev := c.eventQueue[i]
			name, _ := ev["event"].(string)
			evID, _ := ev["playlist_entry_id"].(float64)
			ours := entryID < 0 || int64(evID) == entryID
			switch {
			case name == "start-file" && !sawStart && entryID > 0 && int64(evID) > entryID:
				// Ids only grow. A newer entry starting before ours ever did means a concurrent
				// play replaced ours while it was still queued, and mpv drops such an entry
				// without a start-file or end-file of its own.
				c.eventQueue = c.eventQueue[i:]
				c.superseded = true
				return nil
			case name == "start-file" && ours:
				sawStart, entryID = true, int64(evID)
				c.activeEntryID = int64(evID)
				c.eventQueue = c.eventQueue[i+1:]
				i = -1
			case name == "file-loaded" && sawStart:
				c.eventQueue = c.eventQueue[i+1:]
				c.superseded = false
				c.loadedEntryID = c.activeEntryID
				return nil
			case name == "end-file" && int64(evID) == entryID:
				reason, _ := ev["reason"].(string)
				c.eventQueue = c.eventQueue[i+1:]
				i = -1
				switch reason {
				case "stop":
					c.superseded = true
					return nil // a newer play replaced this one: not a failure of this call
				case "redirect":
					// The URL expanded into new entries (a playlist): the next start-file is ours.
					sawStart, entryID = false, -1
					continue
				}
				if fe, ok := ev["file_error"].(string); ok {
					return fmt.Errorf("playback failed: %s", fe)
				}
				return fmt.Errorf("playback ended before it started (reason: %s)", reason)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("playback not ready after %s", timeout)
		}
		c.conn.SetReadDeadline(deadline)
		if _, err := c.readMessage(); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return fmt.Errorf("playback not ready after %s", timeout)
			}
			return fmt.Errorf("player went away while loading: %v", err)
		}
	}
}
