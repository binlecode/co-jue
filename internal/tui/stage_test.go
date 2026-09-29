package tui

import (
	"context"
	"testing"

	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStageWorkspaceStatePreservation(t *testing.T) {
	stg := newStage()

	// Initial state: not populated
	if stg.Has(WsSearch) {
		t.Errorf("Newly created stage should not have search rows")
	}

	// Save Search state
	stg.Save(WsSearch, ViewState{
		Rows:   []row{{Title: "Search 1"}, {Title: "Search 2"}},
		All:    []row{{Title: "Search 1"}, {Title: "Search 2"}},
		Cursor: 1,
		Query:  "lofi",
		Src:    srcSearch,
	})

	// Save Queue state
	stg.Save(WsQueue, ViewState{
		Rows:   []row{{Title: "Queue 1"}},
		All:    []row{{Title: "Queue 1"}},
		Cursor: 0,
		Src:    srcQueue,
	})

	if !stg.Has(WsSearch) {
		t.Errorf("stg.Has(WsSearch) should be true after Save")
	}
	if !stg.Has(WsQueue) {
		t.Errorf("stg.Has(WsQueue) should be true after Save")
	}

	searchSt := stg.State(WsSearch)
	if searchSt.Query != "lofi" || searchSt.Cursor != 1 || len(searchSt.Rows) != 2 {
		t.Errorf("Search state corrupted: %+v", searchSt)
	}

	queueSt := stg.State(WsQueue)
	if queueSt.Cursor != 0 || len(queueSt.Rows) != 1 {
		t.Errorf("Queue state corrupted: %+v", queueSt)
	}
}

func TestModelWorkspaceSwitchNonDestructive(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "piano",
	})

	// Setup mock search rows
	m.src = srcSearch
	m.all = []row{{Title: "Track 1"}, {Title: "Track 2"}, {Title: "Track 3"}}
	m.rows = m.all
	m.cursor = 2
	m.query = "piano"

	// Save search and switch to Queue
	m.saveCurrentWorkspace()
	if !m.stage.Has(WsSearch) {
		t.Fatalf("Search workspace was not saved in stageModel")
	}

	// Switch to Queue workspace with dummy queue rows
	m.switchWorkspace(WsQueue)
	m.src = srcQueue
	m.all = []row{{Title: "Queued A"}, {Title: "Queued B"}}
	m.rows = m.all
	m.cursor = 1
	m.saveCurrentWorkspace()

	// Switch back to Search workspace
	m.switchWorkspace(WsSearch)
	if m.src != srcSearch {
		t.Errorf("Expected srcSearch after switching back, got %v", m.src)
	}
	if m.query != "piano" {
		t.Errorf("Expected query 'piano', got %q", m.query)
	}
	if len(m.rows) != 3 {
		t.Errorf("Expected 3 rows in search, got %d", len(m.rows))
	}
	if m.cursor != 2 {
		t.Errorf("Expected cursor at 2, got %d", m.cursor)
	}
}

func TestBackToSearchFromSubviewsPreservesSearch(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "lofi",
	})

	// Setup search rows
	m.src = srcSearch
	m.all = []row{{Title: "Search 1"}, {Title: "Search 2"}}
	m.rows = m.all
	m.cursor = 1
	m.query = "lofi"
	m.saveCurrentWorkspace()

	// Drill down into parts (srcParts)
	m.openRows(srcParts, "Parts", []row{{Title: "Part A"}, {Title: "Part B"}})
	if m.src != srcParts {
		t.Fatalf("Expected srcParts, got %v", m.src)
	}

	// Back to search
	m.backToSearch()
	if m.src != srcSearch {
		t.Fatalf("Expected srcSearch after backToSearch, got %v", m.src)
	}
	if len(m.rows) != 2 || m.rows[0].Title != "Search 1" {
		t.Fatalf("Search rows corrupted by backToSearch from parts: %+v", m.rows)
	}
	if m.cursor != 1 {
		t.Errorf("Cursor position not preserved: got %d, want 1", m.cursor)
	}
}

func TestLeaderWKeys(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "ambient",
	})
	m.rows = []row{{Title: "R1"}, {Title: "R2"}, {Title: "R3"}}
	m.all = m.rows
	m.cursor = 2

	// Press 'w': sets leader
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if m.leader != "w" {
		t.Errorf("Expected leader to be 'w', got %q", m.leader)
	}
	if m.noticeL != "Workspace:" {
		t.Errorf("Expected notice 'Workspace:', got %q", m.noticeL)
	}

	// Press '3' (Queue workspace)
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if m.leader != "" {
		t.Errorf("Expected leader to be cleared after digit, got %q", m.leader)
	}
	if m.stage.Active() != WsQueue {
		t.Errorf("Expected active workspace to be WsQueue (3), got %v", m.stage.Active())
	}
}

func TestCtrlNCtrlPWorkspaceCycling(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "synthwave",
	})

	if m.stage.Active() != WsSearch {
		t.Fatalf("Initial workspace should be WsSearch, got %v", m.stage.Active())
	}

	// Press Ctrl+N: next workspace (skips WsFeeds if not supported, goes to WsQueue)
	m.updateList(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.stage.Active() != WsQueue {
		t.Errorf("Expected WsQueue after Ctrl+N, got %v", m.stage.Active())
	}

	// Press Ctrl+P: previous workspace (back to WsSearch)
	m.updateList(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.stage.Active() != WsSearch {
		t.Errorf("Expected WsSearch after Ctrl+P, got %v", m.stage.Active())
	}
}

func TestQueueRevalidateMsgInBackground(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "lofi",
	})
	m.src = srcSearch
	m.all = []row{{Title: "Search 1"}}
	m.rows = m.all

	m.playerID = "test-player-1"

	// Simulate queueRevalidateMsg arriving with mismatched playerID (should be ignored)
	titleOld := "Stale Song"
	m.update(queueRevalidateMsg{
		playerID: "stale-player-0",
		list:     &verb.ItemList{Pos: 0, Len: 1, Items: []verb.Item{{Title: &titleOld, URL: "https://example.com/old", Engine: "yt"}}},
	})
	if m.stage.Has(WsQueue) {
		t.Fatalf("Mismatched playerID should be ignored, but WsQueue was saved")
	}

	// Simulate queueRevalidateMsg arriving with matching playerID while in srcSearch
	title := "Queued Song"
	qList := &verb.ItemList{
		Pos:   0,
		Len:   1,
		Items: []verb.Item{{Title: &title, URL: "https://example.com/1", Engine: "yt"}},
	}
	m.update(queueRevalidateMsg{playerID: "test-player-1", list: qList})

	// Search view should remain untouched
	if m.src != srcSearch {
		t.Fatalf("Expected active view to remain srcSearch, got %v", m.src)
	}
	if len(m.rows) != 1 || m.rows[0].Title != "Search 1" {
		t.Fatalf("Search rows corrupted: %+v", m.rows)
	}

	// Queue workspace in stage must have been updated
	if !m.stage.Has(WsQueue) {
		t.Fatalf("Expected WsQueue to be saved in stage")
	}
	qs := m.stage.State(WsQueue)
	if len(qs.Rows) != 1 || qs.Rows[0].Title != "Queued Song" {
		t.Fatalf("Expected queued song in stage, got %+v", qs.Rows)
	}
}

func TestSearchFeedsSearchQueueCycle(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt", Flags: []string{"--feed"}}},
		Query:   "piano",
	})

	// 1. Initial Search state
	m.src = srcSearch
	m.all = []row{{Title: "Piano 1"}, {Title: "Piano 2"}, {Title: "Piano 3"}}
	m.rows = m.all
	m.cursor = 1
	m.query = "piano"
	m.feed = ""
	m.saveCurrentWorkspace()

	// 2. Switch to Feeds (w2) triggers feed load; simulate feed arrival via feedDone
	m.switchWorkspace(WsFeeds)
	m.feedDone(feedMsg{
		res: &verb.SearchResult{
			Engine: "yt",
			Query:  "feed:home",
			Results: []verb.Result{
				{Title: "Feed Rec 1", ID: "f1", URL: "https://youtube.com/watch?v=f1"},
				{Title: "Feed Rec 2", ID: "f2", URL: "https://youtube.com/watch?v=f2"},
			},
		},
	})

	if m.stage.Active() != WsFeeds {
		t.Fatalf("Expected WsFeeds active, got %v", m.stage.Active())
	}
	if m.feed != "home" {
		t.Fatalf("Expected m.feed='home', got %q", m.feed)
	}

	// 3. Switch back to Search (w1)
	m.switchWorkspace(WsSearch)
	if m.stage.Active() != WsSearch {
		t.Fatalf("Expected WsSearch active after w1, got %v", m.stage.Active())
	}
	if m.feed != "" {
		t.Fatalf("Expected m.feed to be cleared on Search, got %q", m.feed)
	}
	if !m.searched() {
		t.Fatalf("searched() should be true after restoring Search workspace")
	}
	if len(m.rows) != 3 || m.rows[0].Title != "Piano 1" {
		t.Fatalf("Search rows corrupted after returning from feeds: %+v", m.rows)
	}
	if m.cursor != 1 {
		t.Fatalf("Expected cursor restored to 1, got %d", m.cursor)
	}

	// 4. Switch to Queue (w3)
	m.switchWorkspace(WsQueue)
	m.src = srcQueue
	m.all = []row{{Title: "Queued Track"}}
	m.rows = m.all
	m.cursor = 0
	m.saveCurrentWorkspace()

	if m.stage.Active() != WsQueue {
		t.Fatalf("Expected WsQueue active, got %v", m.stage.Active())
	}

	// 5. Return to Search again (w1)
	m.switchWorkspace(WsSearch)
	if m.stage.Active() != WsSearch {
		t.Fatalf("Expected WsSearch active after returning from Queue, got %v", m.stage.Active())
	}
	if len(m.rows) != 3 || m.rows[0].Title != "Piano 1" {
		t.Fatalf("Search rows corrupted after returning from Queue: %+v", m.rows)
	}
}

func TestFeedsUnsupportedEngineGuard(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	// Engine without --feed capability
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "bili", Flags: []string{"--search"}}},
		Query:   "rock",
	})
	m.src = srcSearch
	m.all = []row{{Title: "Rock 1"}}
	m.rows = m.all
	m.saveCurrentWorkspace()

	// Try to switch to WsFeeds
	cmd := m.switchWorkspace(WsFeeds)
	if cmd != nil {
		t.Errorf("Expected nil command when engine lacks feeds")
	}
	if m.stage.Active() != WsSearch {
		t.Errorf("Stage active workspace should remain WsSearch, got %v", m.stage.Active())
	}
	if m.navbar.active != WsSearch {
		t.Errorf("Navbar active workspace should remain WsSearch, got %v", m.navbar.active)
	}
	if m.noticeL != m.s.FeedAct+":" {
		t.Errorf("Expected FeedAct notice, got %q", m.noticeL)
	}
}
