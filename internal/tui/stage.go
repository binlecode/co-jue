package tui

import (
	"github.com/binlecode/ting/internal/tui/layout"
)

// ViewState represents the independent preserved state of a single workspace.
type ViewState struct {
	Rows     []row
	All      []row
	Cursor   int
	Top      int
	Query    string
	Filter   string
	FilterOn bool
	Total    int
	TotalFmt string
	Label    string
	PlName   string
	Src      source
	Feed     string
}

// stageModel manages the active and background workspaces on the main stage,
// eliminating the destructive single-slot stash mechanism.
type stageModel struct {
	bounds layout.Rect
	active Workspace
	states map[Workspace]*ViewState
}

func newStage() stageModel {
	m := stageModel{
		active: WsSearch,
		states: make(map[Workspace]*ViewState),
	}
	for _, ws := range []Workspace{WsSearch, WsFeeds, WsQueue, WsPlaylists, WsHistory} {
		m.states[ws] = &ViewState{}
	}
	return m
}

func (s *stageModel) SetBounds(r layout.Rect) {
	s.bounds = r
}

func (s *stageModel) State(ws Workspace) *ViewState {
	if s.states[ws] == nil {
		s.states[ws] = &ViewState{}
	}
	return s.states[ws]
}

func (s *stageModel) ActiveState() *ViewState {
	return s.State(s.active)
}

func (s *stageModel) Active() Workspace {
	return s.active
}

func (s *stageModel) SwitchTo(ws Workspace) {
	s.active = ws
}

// Has reports whether the workspace has any rows or query saved.
func (s *stageModel) Has(ws Workspace) bool {
	st := s.states[ws]
	return st != nil && (len(st.All) > 0 || st.Query != "")
}

// Save stores the view state into the specified workspace slot.
func (s *stageModel) Save(ws Workspace, st ViewState) {
	s.states[ws] = &st
}
