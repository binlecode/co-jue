package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// strs is the chrome in one language. Help text and errors stay English; this is only what
// the frame prints.
type strs struct {
	Select, Page, Play, Mode, Quality, Search, Sort, Filter, Stop, Quit, LangKey, ThemeKey     string
	Keys, Pause, Vol, Jump, RowNum, UResults, UPage, ListMode                                  string
	SortRelevance, SortViews, SortDur, ModeAudio, ModeVideo, ModeFast                          string
	AuthIn, AuthAnon, AuthBlocked, Seek, Playing, Paused, Starting                             string
	PromptSearch, NewSearch, EngineKey, EngineAct, Searching, Refetch, ResultsN                string
	PlayFailed, SearchAct, MoreAct, SortAct                                                    string
	NoMatch, NoMatchNew, NoMatchFilt, NoMatchSrc, FilterHint                                   string
	QAdd, QSkip, QAct, Loop, LoopAct, LoopOff, LoopSeq, LoopOne, LoopNext, QNone, QEnd, QAdded string
	Failed, AdoptAct, AdoptMany                                                                string

	UItems, UChap, Likes, Total, URLAct, ContainerAct, BackSearch                             string
	PLAdd, PLOpen, PLAct, PLPromptAdd, PLPromptOpen, PLPromptNew, PLAdded, PLEmpty, PLNone    string
	PLItem, PLItems, PLAbsent, PLSearchOnly, PLListOnly, PLRmKey, PLRemoved, PLDelKey         string
	PLDeleted, PLRenameKey, PLRenamePrompt, PLRenamed                                         string
	UndoKey, UndoAct, UndoHint, UndoDone, UndoNone, UndoStale                                 string
	HistKey, HistAct, HistLabel, HistEmpty, HistAbsent, PartsKey, PartsAct, PartsOne, InfoAct string
	RelatedKey, RelatedAct, FeedAct, RelatedNoCap, NoResults, NoResultsFor                    string
	ChapKey, ChapNone, QKey, QLabel, QPlayNow, QRmKey, QMvKey, QClearKey, QOne, QElsewhere    string
	QPlayingRow, QRemoved, QCleared, QStale                                                   string
	PrefAct, PrefPinned, PrefFailed                                                           string
	BusyHeld, NextSearch                                                                      string
}

var strsEN = strs{
	Select: "select", Page: "page", Play: "play", Mode: "mode", Quality: "quality",
	Search: "search", Sort: "sort", Filter: "filter", Stop: "stop", Quit: "quit",
	LangKey: "language", ThemeKey: "theme", Keys: "keys", Pause: "pause", Vol: "volume",
	Jump: "jump", RowNum: "numbers", UResults: "results", UPage: "page", ListMode: "view",
	SortRelevance: "relevance", SortViews: "views", SortDur: "duration",
	ModeAudio: "audio", ModeVideo: "video", ModeFast: "fast",
	AuthIn: "signed in", AuthAnon: "anonymous", AuthBlocked: "anonymous (cookies unreadable)",
	Seek: "seek", Playing: "Playing", Paused: "Paused", Starting: "Starting",
	PromptSearch: "Search", NewSearch: "New search (Esc or empty to cancel)",
	EngineKey: "source", EngineAct: "Source", Searching: "searching", Refetch: "re-fetching",
	ResultsN: "results", PlayFailed: "Play failed", SearchAct: "Search", MoreAct: "More",
	SortAct: "Sort", NoMatch: "(no matches", NoMatchNew: "n new search", NoMatchFilt: "/ filter",
	NoMatchSrc: "e change source)", FilterHint: "filter (type to narrow, Enter plays, Esc clears)",
	QAdd: "queue", QSkip: "skip", QAct: "Queue", Loop: "loop", LoopAct: "Loop", LoopOff: "off",
	LoopSeq: "seq", LoopOne: "one", LoopNext: " (from next play)", QNone: "nothing is playing",
	QEnd: "nothing queued after this track", QAdded: "Queued", Failed: "failed",
	AdoptAct:  "Player",
	AdoptMany: "several background players are running, none adopted (ting-play --status lists them)",
	UItems:    "items", UChap: "chapters", Likes: "likes", Total: "total", URLAct: "URL",
	ContainerAct: "Container", BackSearch: "back to results",
	PLAdd: "add to list", PLOpen: "playlists", PLAct: "Playlist",
	PLPromptAdd: "Add to which (number, name, Esc cancels)", PLPromptOpen: "Open which (number, name, Esc cancels)",
	PLPromptNew: "Name the new playlist (Esc cancels)", PLAdded: "Added to", PLEmpty: "that playlist is empty",
	PLNone: "no playlists yet (press a to make one)", PLItem: "item", PLItems: "items",
	PLAbsent: "ting-playlist is not installed", PLSearchOnly: "that key needs search results (press n to search)",
	PLListOnly: "that key needs a playlist (press b to open one)", PLRmKey: "remove", PLRemoved: "Removed from",
	PLDelKey: "del-list", PLDeleted: "Deleted playlist", PLRenameKey: "rename",
	PLRenamePrompt: "New name for playlist (Esc cancels)", PLRenamed: "Renamed to",
	UndoKey: "undo", UndoAct: "Undo", UndoHint: "z to undo", UndoDone: "Undone", UndoNone: "nothing to undo",
	UndoStale: "changed elsewhere, not undone",
	HistKey:   "history", HistAct: "History", HistLabel: "newest first", HistEmpty: "nothing listened to yet",
	HistAbsent: "ting-history is not installed", PartsKey: "parts", PartsAct: "Parts",
	RelatedKey: "related", RelatedAct: "Related", FeedAct: "Feed",
	RelatedNoCap: "this engine does not support related recommendations",
	NoResults:    "no results", NoResultsFor: "no results for",
	PartsOne: "this video has only one part", InfoAct: "Info", ChapKey: "chapters", ChapNone: "this item has no chapters",
	QKey: "queue", QLabel: "waiting", QPlayNow: "play now", QRmKey: "remove", QMvKey: "move", QClearKey: "clear",
	QOne: "the queue holds only the track that is playing", QElsewhere: "go back to the results first, then open the queue",
	QPlayingRow: "this one is playing — s stops it, > skips it", QRemoved: "Removed from the queue",
	QCleared: "Cleared the queue", QStale: "the queue moved — reopening it",
	BusyHeld: "busy — press it again when this finishes", NextSearch: "for the next search",
	PrefAct: "Config", PrefPinned: "is fixed by the environment — not written to your config",
	PrefFailed: "could not be written — this session's preferences were not saved",
}

var strsZH = strs{
	Select: "选择", Page: "翻页", Play: "播放", Mode: "模式", Quality: "质量",
	Search: "搜索", Sort: "排序", Filter: "过滤", Stop: "停止", Quit: "退出",
	LangKey: "语言", ThemeKey: "主题", Keys: "键位", Pause: "暂停", Vol: "音量",
	Jump: "跳行", RowNum: "行号", UResults: "结果", UPage: "页", ListMode: "视图",
	SortRelevance: "相关度", SortViews: "播放量", SortDur: "时长",
	ModeAudio: "音频", ModeVideo: "视频", ModeFast: "快速",
	AuthIn: "已登录", AuthAnon: "匿名", AuthBlocked: "匿名（cookie 读不到）",
	Seek: "跳转", Playing: "播放中", Paused: "已暂停", Starting: "缓冲中",
	PromptSearch: "搜索", NewSearch: "新搜索（Esc 或留空取消）",
	EngineKey: "音源", EngineAct: "音源", Searching: "搜索中", Refetch: "重新获取",
	ResultsN: "条", PlayFailed: "播放失败", SearchAct: "搜索", MoreAct: "更多",
	SortAct: "排序", NoMatch: "(无匹配", NoMatchNew: "n 重新搜索", NoMatchFilt: "/ 过滤",
	NoMatchSrc: "e 换音源)", FilterHint: "过滤（输入即筛选，Enter 播放，Esc 清除）",
	QAdd: "加入队列", QSkip: "下一首", QAct: "队列", Loop: "循环", LoopAct: "循环", LoopOff: "关",
	LoopSeq: "顺序", LoopOne: "单曲", LoopNext: "（下次起播生效）", QNone: "当前没有在播放的播放器",
	QEnd: "队列已到末尾", QAdded: "已加入队列", Failed: "操作失败",
	AdoptAct:  "播放器",
	AdoptMany: "后台有多个播放器在跑，没有接管（ting-play --status 可以看）",
	UItems:    "项", UChap: "章", Likes: "赞", Total: "总长", URLAct: "链接", ContainerAct: "合集",
	BackSearch: "返回搜索列表",
	PLAdd:      "加入列表", PLOpen: "播放列表", PLAct: "播放列表",
	PLPromptAdd: "加入哪个（序号或名称，Esc 取消）", PLPromptOpen: "打开哪个（序号或名称，Esc 取消）",
	PLPromptNew: "新建列表名称（Esc 取消）", PLAdded: "已加入", PLEmpty: "该列表是空的",
	PLNone: "还没有播放列表（按 a 建一个）", PLItem: "首", PLItems: "首",
	PLAbsent: "未安装 ting-playlist", PLSearchOnly: "该键只在搜索结果里可用（按 n 重新搜索）",
	PLListOnly: "该键只在播放列表里可用（按 b 打开一个）", PLRmKey: "移出", PLRemoved: "已移出",
	PLDelKey: "删列表", PLDeleted: "已删除列表", PLRenameKey: "重命名",
	PLRenamePrompt: "重命名列表名称（Esc 取消）", PLRenamed: "已重命名",
	UndoKey: "撤销", UndoAct: "撤销", UndoHint: "z 撤销", UndoDone: "已撤销", UndoNone: "无可撤销",
	UndoStale: "已被别处修改，未撤销",
	HistKey:   "历史", HistAct: "收听历史", HistLabel: "最近收听", HistEmpty: "还没有收听记录",
	HistAbsent: "未安装 ting-history", PartsKey: "分P", PartsAct: "分P", PartsOne: "这个视频只有一 P",
	RelatedKey: "相关", RelatedAct: "相关", FeedAct: "推荐",
	RelatedNoCap: "该引擎不支持相关推荐",
	NoResults:    "无结果", NoResultsFor: "无结果",
	InfoAct: "详情", ChapKey: "章节", ChapNone: "这个条目没有章节",
	QKey: "队列", QLabel: "待播队列", QPlayNow: "立刻播", QRmKey: "移出", QMvKey: "上移/下移", QClearKey: "清空",
	QOne: "队列里只有正在播放的这一首", QElsewhere: "先回结果列表再看队列",
	QPlayingRow: "正在播的这首不能移出，用 s 停、用 > 跳过", QRemoved: "已移出队列",
	QCleared: "已清空待播", QStale: "队列已经变了，重新读一次",
	BusyHeld: "进行中 —— 完成后再按一次", NextSearch: "用于下一次搜索",
	PrefAct: "配置", PrefPinned: "由环境变量固定，没有写入配置文件",
	PrefFailed: "配置文件写不了，这次改的偏好没有存下来",
}

// DetectLang resolves TING_LANG: en|zh wins, else a zh* locale picks Chinese, else English.
// ok is false for any other value, which the caller refuses.
func DetectLang(v string, getenv func(string) string) (string, bool) {
	switch v {
	case "en", "zh":
		return v, true
	case "":
		loc := firstSet(getenv, "LC_ALL", "LC_MESSAGES", "LANG")
		if strings.HasPrefix(loc, "zh") || strings.Contains(loc, "_zh") {
			return "zh", true
		}
		return "en", true
	}
	return "", false
}

// DetectASCII is TING_ASCII, else a locale that is not UTF-8 (or none at all).
func DetectASCII(v string, getenv func(string) string) bool {
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	loc := strings.ToLower(firstSet(getenv, "LC_ALL", "LC_CTYPE", "LANG"))
	return !strings.Contains(loc, "utf-8") && !strings.Contains(loc, "utf8")
}

// firstSet is ${A:-${B:-${C:-}}}.
func firstSet(getenv func(string) string, keys ...string) string {
	for _, k := range keys {
		if v := getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// glyphs is the chrome's inventory. Every one is text-presentation (no emoji), so a width
// table can be trusted with them; TING_ASCII swaps the whole set.
type glyphs struct {
	Note, Live, Sep, Caret, Play, Pause, AV, AH, Enter, Tab, Cursor, Thumb, Track string
	Ell, Arrow, Dash, GE, LE, TimeL, TimeR, CPU, RAM, Fill, Rest                  string
	Lyric, LyricInter                                                             string
	Spin, Part                                                                    []string
}

var glyphsUTF = glyphs{
	Note: "♫ ", Live: "● LIVE", Sep: "·", Caret: "❯", Play: "▶", Pause: "❚❚", AV: "↑↓", AH: "←→",
	Enter: "⏎", Tab: "⇥", Cursor: "▎", Thumb: "█", Track: "│", Ell: "…", Arrow: "→", Dash: "—",
	GE: "≥", LE: "≤", TimeL: "【", TimeR: "】", CPU: "▣", RAM: "▤", Fill: "█", Rest: "─",
	Lyric: "♪ ", LyricInter: "♪  · · ·",
	Spin: []string{"▘", "▝", "▗", "▖"},
	Part: []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"},
}

var glyphsASCII = glyphs{
	Note: "", Live: "LIVE", Sep: "|", Caret: ">", Play: ">", Pause: "||", AV: "Up/Dn", AH: "Lt/Rt",
	Enter: "Enter", Tab: "Tab", Cursor: ">", Thumb: "#", Track: "|", Ell: "...", Arrow: "->",
	Dash: "-", GE: ">=", LE: "<=", TimeL: "[", TimeR: "]", CPU: "cpu", RAM: "ram", Fill: "=",
	Rest: "-", Lyric: "> ", LyricInter: ">  ...", Spin: []string{"|", "/", "-", "\\"},
}

// brand is the header wordmark, which is a language string too.
func brand(lang string, ascii bool) string {
	switch {
	case lang == "zh" && ascii:
		return "[ 听 ]"
	case lang == "zh":
		return "【 听 】"
	}
	return "ting"
}

// width measures display cells with ONE rule for the whole frame: East-Asian Ambiguous is one
// cell unless TING_AMBIG_WIDE=1 says the terminal draws it as two. The locale says nothing
// here (a zh locale does not make a terminal ambiguous-wide), so the condition is explicit
// rather than go-runewidth's locale guess.
type width struct{ c *runewidth.Condition }

func newWidth(ambigWide bool) width {
	c := runewidth.NewCondition()
	c.EastAsianWidth = ambigWide
	c.StrictEmojiNeutral = true
	return width{c}
}

func (w width) of(s string) int { return w.c.StringWidth(s) }

// trunc cuts s to max cells, ending in the ellipsis when anything was cut; a budget too small
// for the ellipsis cuts without one.
func (w width) trunc(s string, max int, ell string) string {
	if max <= 0 {
		return ""
	}
	if w.of(s) <= max {
		return s
	}
	if w.of(ell) >= max {
		ell = ""
	}
	return w.c.Truncate(s, max, ell)
}

// pad right-fills s with spaces to n cells.
func (w width) pad(s string, n int) string {
	if d := n - w.of(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// wrap breaks text at spaces into lines of at most max cells, splitting a word that is wider
// than a line by itself; at most maxLines lines (0 = no cap).
func (w width) wrap(text string, max, maxLines int) []string {
	var out []string
	line, cur := "", 0
	emit := func() bool {
		out = append(out, line)
		line, cur = "", 0
		return maxLines > 0 && len(out) >= maxLines
	}
	for _, word := range strings.Fields(text) {
		ww := w.of(word)
		if ww > max {
			if line != "" && emit() {
				return out
			}
			for _, r := range word {
				rw := w.c.RuneWidth(r)
				if cur+rw > max {
					if emit() {
						return out
					}
				}
				line += string(r)
				cur += rw
			}
			continue
		}
		need := ww
		if line != "" {
			need++
		}
		if cur+need > max {
			if emit() {
				return out
			}
			line, cur = word, ww
			continue
		}
		if line == "" {
			line, cur = word, ww
		} else {
			line, cur = line+" "+word, cur+ww+1
		}
	}
	if line != "" && (maxLines == 0 || len(out) < maxLines) {
		out = append(out, line)
	}
	return out
}
