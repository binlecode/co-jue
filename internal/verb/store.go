package verb

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Item is one entry of every list-shaped envelope: a playlist's --show, the log's --ls, a
// container's --items and a player's --queue-show. They are one record on purpose, so one
// reader serves all four.
type Item struct {
	Index       *int     `json:"index"` // --queue-show only: what the queue verbs take
	Engine      string   `json:"engine"`
	ID          *string  `json:"id"`
	URL         string   `json:"url"`
	Title       *string  `json:"title"`
	Duration    *float64 `json:"duration"`
	Description string   `json:"description"`
	Thumbnail   string   `json:"thumbnail"`
}

// ItemList is any of those envelopes.
type ItemList struct {
	Status  string `json:"status"`
	Engine  string `json:"engine"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Count   int    `json:"count"`
	Total   *int   `json:"total"`
	HasMore bool   `json:"has_more"`
	Pos     int    `json:"pos"`
	Len     int    `json:"len"`
	Items   []Item `json:"items"`
}

// Written is a store or queue write's envelope: what the undo offer needs, and for the
// undo verbs, what was put back.
type Written struct {
	Undo *struct {
		Deadline int64 `json:"deadline"`
	} `json:"undo"`
	Queue *struct {
		Pos int `json:"pos"`
		Len int `json:"len"`
	} `json:"queue"`
	Undone string `json:"undone"`
	Name   string `json:"name"`
	From   string `json:"from"`
	Index  *int   `json:"index"`
}

// Deadline is when the write's undo offer closes (epoch seconds); 0 when there is none.
func (w *Written) Deadline() int64 {
	if w == nil || w.Undo == nil {
		return 0
	}
	return w.Undo.Deadline
}

func owner(argv []string, pid int) []string {
	if pid > 0 {
		argv = append(argv, "--owner", strconv.Itoa(pid))
	}
	return argv
}

// ── the playlist store ──────────────────────────────────────────────────────────────────

// Playlist is one --ls row.
type Playlist struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Suite) PlaylistLs(ctx context.Context) ([]Playlist, error) {
	var env struct {
		Playlists []Playlist `json:"playlists"`
	}
	if err := run(ctx, []string{s.TPlaylist, "--ls", "-j"}, &env); err != nil {
		return nil, err
	}
	return env.Playlists, nil
}

func (s *Suite) PlaylistShow(ctx context.Context, name string) (*ItemList, error) {
	var l ItemList
	if err := run(ctx, []string{s.TPlaylist, "--show", name, "-j"}, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Suite) PlaylistAdd(ctx context.Context, name string, items []QueueItem, pid int) (*Written, error) {
	in, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var w Written
	return &w, runIn(ctx, owner([]string{s.TPlaylist, "--add", name, "-j"}, pid), in, &w)
}

func (s *Suite) PlaylistRm(ctx context.Context, name string, index, pid int) (*Written, error) {
	var w Written
	return &w, run(ctx, owner([]string{s.TPlaylist, "--rm", name, "--index", strconv.Itoa(index), "-j"}, pid), &w)
}

func (s *Suite) PlaylistDel(ctx context.Context, name string, pid int) (*Written, error) {
	var w Written
	return &w, run(ctx, owner([]string{s.TPlaylist, "--del", name, "-j"}, pid), &w)
}

func (s *Suite) PlaylistRename(ctx context.Context, from, to string, pid int) (*Written, error) {
	var w Written
	return &w, run(ctx, owner([]string{s.TPlaylist, "--rename", from, to, "-j"}, pid), &w)
}

// PlaylistUndo puts back this process's last write, or with discard drops the copy.
func (s *Suite) PlaylistUndo(ctx context.Context, pid int, discard bool) (*Written, error) {
	argv := []string{s.TPlaylist, "--undo", "--owner", strconv.Itoa(pid), "-j"}
	if discard {
		argv = append(argv, "--discard")
	}
	var w Written
	return &w, run(ctx, argv, &w)
}

// ── the listening log ───────────────────────────────────────────────────────────────────

func (s *Suite) HistoryLs(ctx context.Context, n int) (*ItemList, error) {
	var l ItemList
	if err := run(ctx, []string{s.THistory, "--ls", "-n", strconv.Itoa(n), "-j"}, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// ── engine verbs on a handle ────────────────────────────────────────────────────────────

func handleArgv(tplay, verb, engine, handle string) []string {
	argv := []string{tplay, verb, "-j"}
	if engine != "" {
		// Without --engine, a URL picks its own engine, as a play does.
		argv = append(argv, "--engine", engine)
	}
	return append(argv, "--", handle)
}

// Items expands a container (a playlist, album, channel, or a video's parts).
func (s *Suite) Items(ctx context.Context, engine, handle string) (*ItemList, error) {
	var l ItemList
	if err := run(ctx, handleArgv(s.TPlay, "--items", engine, handle), &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Chapter is one --info chapter.
type Chapter struct {
	Start float64  `json:"start_time"`
	End   *float64 `json:"end_time"`
	Title string   `json:"title"`
}

// Info is the --info envelope: a search row plus chapters.
type Info struct {
	Result
	Engine     string    `json:"engine"`
	Uploader   string    `json:"uploader"`
	LikeCount  *int64    `json:"like_count"`
	UploadDate string    `json:"upload_date"`
	Chapters   []Chapter `json:"chapters"`
}

func (s *Suite) Info(ctx context.Context, engine, handle string) (*Info, error) {
	var i Info
	if err := run(ctx, handleArgv(s.TPlay, "--info", engine, handle), &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// ── the queue ───────────────────────────────────────────────────────────────────────────

func (s *Suite) QueueShow(ctx context.Context, id string) (*ItemList, error) {
	var l ItemList
	if err := run(ctx, []string{s.TPlay, "--queue-show", "--id", id, "-j"}, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// QueueRm drops the waiting track at index, which must still hold url.
func (s *Suite) QueueRm(ctx context.Context, id string, index int, url string, pid int) (*Written, error) {
	var w Written
	return &w, run(ctx, owner([]string{s.TPlay, "--queue-rm", strconv.Itoa(index), "--expect-url", url,
		"--id", id, "-j"}, pid), &w)
}

func (s *Suite) QueueMv(ctx context.Context, id string, index, to int, url string) (*Written, error) {
	var w Written
	return &w, run(ctx, []string{s.TPlay, "--queue-mv", strconv.Itoa(index), "--to", strconv.Itoa(to),
		"--expect-url", url, "--id", id, "-j"}, &w)
}

func (s *Suite) QueueJump(ctx context.Context, id string, index int, url string) (*Written, error) {
	var w Written
	return &w, run(ctx, []string{s.TPlay, "--queue-jump", strconv.Itoa(index), "--expect-url", url,
		"--id", id, "-j"}, &w)
}

func (s *Suite) QueueClear(ctx context.Context, id string, pid int) (*Written, error) {
	var w Written
	return &w, run(ctx, owner([]string{s.TPlay, "--queue-clear", "--id", id, "-j"}, pid), &w)
}

// QueueUndo puts back this process's last queue write, or with discard drops the copy.
func (s *Suite) QueueUndo(ctx context.Context, pid int, discard bool) (*Written, error) {
	argv := []string{s.TPlay, "--undo", "--owner", strconv.Itoa(pid), "-j"}
	if discard {
		argv = append(argv, "--discard")
	}
	var w Written
	return &w, run(ctx, argv, &w)
}

// SeekTo moves the playhead to an absolute second.
func (s *Suite) SeekTo(ctx context.Context, id string, sec int) error {
	return run(ctx, []string{s.TPlay, "--seek-to", fmt.Sprint(sec), "--id", id, "-j"}, nil)
}
