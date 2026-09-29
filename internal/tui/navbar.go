package tui

import (
	"strings"

	"github.com/binlecode/ting/internal/tui/layout"
)

// Workspace defines an independent top-level tab.
type Workspace int

const (
	WsSearch Workspace = iota
	WsFeeds
	WsQueue
	WsPlaylists
	WsHistory
)

func (w Workspace) String() string {
	switch w {
	case WsSearch:
		return "Search"
	case WsFeeds:
		return "Feeds"
	case WsQueue:
		return "Queue"
	case WsPlaylists:
		return "Playlists"
	case WsHistory:
		return "History"
	default:
		return "Unknown"
	}
}

func (w Workspace) Label(s strs) string {
	switch w {
	case WsSearch:
		return s.WsSearchTitle
	case WsFeeds:
		return s.WsFeedsTitle
	case WsQueue:
		return s.WsQueueTitle
	case WsPlaylists:
		return s.WsPlaylistsTitle
	case WsHistory:
		return s.WsHistoryTitle
	default:
		return "Unknown"
	}
}

// navbarModel manages the multi-workspace navigation sidebar or top tab bar.
type navbarModel struct {
	bounds        layout.Rect
	active        Workspace
	supportsFeeds bool
}

func newNavbar() navbarModel {
	return navbarModel{
		active: WsSearch,
	}
}

func (n *navbarModel) SetBounds(r layout.Rect) {
	n.bounds = r
}

func (n *navbarModel) SetSupportsFeeds(ok bool) {
	n.supportsFeeds = ok
}

func (n *navbarModel) SwitchTo(ws Workspace) {
	n.active = ws
}

func (n *navbarModel) Next() {
	n.active = (n.active + 1) % 5
	if n.active == WsFeeds && !n.supportsFeeds {
		n.active = (n.active + 1) % 5
	}
}

func (n *navbarModel) Prev() {
	if n.active == 0 {
		n.active = 4
	} else {
		n.active--
	}
	if n.active == WsFeeds && !n.supportsFeeds {
		if n.active == 0 {
			n.active = 4
		} else {
			n.active--
		}
	}
}

// View renders the navbar into its allocated bounding box.
func (n *navbarModel) View(p palette, s strs, g glyphs, w width) string {
	if n.bounds.Empty() {
		return ""
	}

	items := []struct {
		ws  Workspace
		key string
	}{
		{WsSearch, "w1"},
		{WsFeeds, "w2"},
		{WsQueue, "w3"},
		{WsPlaylists, "w4"},
		{WsHistory, "w5"},
	}

	// Vertical sidebar mode (Wide mode, W >= 16 and H >= 5)
	if n.bounds.W >= 16 && n.bounds.H >= 5 {
		var lines []string
		if n.bounds.H >= 8 {
			lines = append(lines, "  "+p.Bold+s.NavTitle+p.Reset, "")
		} else if n.bounds.H >= 6 {
			lines = append(lines, "  "+p.Bold+s.NavTitle+p.Reset)
		}

		for _, item := range items {
			if len(lines) >= n.bounds.H {
				break
			}
			prefix := "  "
			style := p.Secondary
			if item.ws == n.active {
				prefix = "> "
				style = p.Bold + p.Accent
			} else if item.ws == WsFeeds && !n.supportsFeeds {
				style = p.Muted
			}
			label := prefix + item.key + " " + item.ws.Label(s)
			lines = append(lines, style+w.pad(label, n.bounds.W)+p.Reset)
		}

		return strings.Join(lines, "\n")
	}

	// Horizontal top tab mode (Standard mode, H <= 2)
	var tabs []string
	for _, item := range items {
		label := item.ws.Label(s)
		tab := "[" + item.key + " " + label + "]"
		if item.ws == n.active {
			tab = p.Bold + p.Accent + "[" + item.key + " " + label + "*]" + p.Reset
		} else if item.ws == WsFeeds && !n.supportsFeeds {
			tab = p.Muted + "[" + item.key + " " + label + "]" + p.Reset
		}
		tabs = append(tabs, tab)
	}

	return strings.Join(tabs, "  ")
}
