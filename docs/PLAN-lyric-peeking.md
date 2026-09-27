# PLAN — 焦点行歌词窥探：details 空间内联在播歌词（Go TUI 之后）

> **Status**: 草案 · 排在 [`PLAN-go-tui.md`](PLAN-go-tui.md) 之后，不在 bash TUI 上实现  
> **Priority**: 第一梯队 · Go TUI 落地后的第一个人机面特性  
> **Target Branch**: main  
> **Roadmap 关联**: [`docs/ROADMAP.md`](ROADMAP.md)「第一梯队」——焦点行歌词窥探  
> **前置**: `PLAN-go-tui.md` 全部完成——Go `ting` 已接过名字，`ting-play --watch -j` 与 `ting-play --engines -j` 的 `flags[]` 已发布  
> **Governing Docs**: [`docs/ARCH-tui.md`](ARCH-tui.md)「重排与分页」（届时为 Go 版重写后的对应章节）、[`docs/ARCH-engine.md`](ARCH-engine.md)「字幕（`--transcript`）」、[`docs/ARCH-cli-contract.md`](ARCH-cli-contract.md)「数据契约」、[`docs/ROADMAP.md`](ROADMAP.md)「横切规范 —— 每个新功能都要过的判据」  
> **Verification**: `go test ./...`、`tests/contract.sh --offline`、tmux 驱动段（Go 版）  
> **Scope Boundary**: 仅当光标停在正在播放的那一行、且歌词就绪时，在它下方的 details 空间里显示当前一句。置换 Description，details 段高度不变。数据只来自两个已发布的动词：`ting-play --transcript -j --segments`（歌词）与 `ting-play --watch -j`（播放位置）。不加 CLI 选项、信封字段或依赖，不开独立歌词视图。

---

## 为什么放到 Go 之后

旧稿是为 bash TUI 设计的：1 Hz 整数秒时钟、平行数组、落盘 TSV 缓存、后台子 shell 轮询。
那一整套是在**绕开** bash 3.2 的限制，而这些限制正是 `PLAN-go-tui.md` 要删掉的负债。
在 bash 上做完再移植，等于把这份绕路写两遍。放到 Go 之后，同一个特性变成：
一个 goroutine 调一次动词、内存里一个切片、一个由 `--watch` 驱动的亚秒游标。

它也是 Go TUI 的第一块试金石：歌词与播放位置对齐，正是 1 Hz 做不好、而切分之后应该免费得到的东西。

---

## 为什么是窥探而不是歌词视图

- **不开独立歌词视图**：全屏歌词或双栏会破坏单视图原地重绘，`ROADMAP.md` 已记录
  「卡片化 NO」「双栏中轴竖线 NO」。换了语言，这些 NO 的理由（行预算、单栏）不变。
- **details 空间本来就在**：光标下方的 details 段是变高 chrome，先量后画，
  不与结果行争固定高度。
- **聚焦即来**：光标在别的行，details 显示检索元数据与简介；停到在播那一行，就换成歌词。
  平时看不到，看过去就在。

---

## 排版契约：行预算守恒

屏幕高度是量出来的，chrome 每多一行，结果就少一行。所以歌词**置换** Description，不追加：

- **普通 / 非在播行 / 无歌词**：Meta 一行 + Description 最多两行。
- **在播 且 歌词就绪**：Meta 一行 + Media 解码行 + 当前歌词一行。

```
  > 选中的在播曲目行
    周杰伦 · 04:32 · 12,345,678 views · 1824020871             <- Meta
    flac · 44100Hz · 920kbps · stereo                         <- Media
    ♪  这故事，留给后世，来谱写一曲传世之作                   <- Lyric
```

details 段的行数与置换前相同，上方结果行数不变。

### 视觉

- **行首标识**：`♪ `，ASCII 模式回退为 `> `。
- **色彩层级**：Meta / Media 用弱化色，歌词正文用当前主题的强调色。
- **换句过渡**：这是 bash 版做不到、Go 版应该做的一件事——新一句出现时有一次短暂的
  亮度过渡，而不是硬切。时长与曲线在实现时对着真实终端试（按
  「先渲染候选再动手」的惯例，先把候选做成可对比的帧再定）。`--color never` 与 mono 主题下没有过渡。
- **前奏与长间奏**：未到第一句、或当前句结束后下一句还远时，显示弱化的 `♪  · · ·`。
  间奏判定用 segment 的 `start + duration` 与下一句 `start` 的间距，不再靠猜测阈值。
- **宽度**：歌词含 CJK、假名、全半角标点，按显示宽度测量截断，超长补 `…`，绝不折行。

---

## 数据流

```
  Enter / 换曲事件 (--watch)
          |
          v
  +--------------------------+     ting-play --engines -j 的 flags[]
  | 该引擎有 --transcript ?  |---- 否 ----> 状态 none，details 照旧
  +------------+-------------+
               | 是
               v
  +--------------------------+
  | goroutine: 一次调用       |     ting-play --transcript -j --segments -- URL
  | 解 segments 进内存切片    |
  +------------+-------------+
               |  消息进 Bubbletea 更新循环
               v
  +--------------------------+     ting-play --watch -j 的位置事件
  | 游标：按位置在 segments  |<--------------------------------
  | 上二分定位当前句         |
  +------------+-------------+
               v
  +--------------------------+
  | details 渲染：焦点在播   |
  | 且就绪 -> 画歌词行       |
  +--------------------------+
```

- **触发**：只在播放目标变化时（起播，或 `--watch` 报出换曲）拉一次。同一时刻只保留一个在途请求，
  新曲目到来时丢弃旧请求的结果（按 URL 比对，不按到达顺序）。
- **能力门控**：`ting-play --engines -j` 里该引擎的 `flags[]` 有没有 `--transcript`。今天 `yt` 与 `ne` 有 `--transcript`，`bili` 没有，
  但 TUI 不写死这张表——一个仓外引擎加了 `--transcript` 就自动得到歌词。
- **缓存**：只在内存里，按 URL 存，本会话有效。光标移出再移回、或同一首再播，不重新拉取。
  不落盘，也就没有 `$TMPDIR` 下的文件需要清理。
- **定位**：`--watch` 推来的位置是浮点秒；按 `start` 二分找当前句。seek 与快退是同一次二分，
  不需要旧稿里的双向步进。`--watch` 只发离散事件与约 1 秒一次的心跳，所以两次事件之间
  **必须**按单调时钟外推（Go TUI 的进度显示本来就这样做），换句时刻才不受心跳间隔影响
  （外推只用于显示；暂停时停止外推）。

---

## 边缘场景

| 工况 | 条件 | 表现 |
|---|---|---|
| 纯音乐 | 网易云引擎按站点哨兵 `纯音乐，请欣赏` 报 `no_subtitles_available` | 状态 none，照常显示 Description，不显示错误 |
| 无字幕引擎 / 失败 / 超时 | `bili`，或调用非零退出 | 同上 |
| 自动字幕轨（`yt`） | 信封 `is_auto` 为真 | 照常显示（是否加弱化标记见「未决」） |
| 前奏 / 长间奏 | 位置不在任何 segment 内 | `♪  · · ·` |
| seek | `[` / `]` 或 `--seek-to` | 下一个位置事件到达即重新二分，下一帧对齐 |
| 暂停 | `--watch` 报暂停 | 停在当前句，停止外推 |
| 超长文本 / resize | 宽度超过可用列 | 按显示宽度截断补 `…`；resize 后重新测量 |

---

## 横切规范核验

1. **一个功能必带 agent 面**：两半都是已发布的动词。agent 调 `--transcript -j --segments` 拿带时间戳的
   segments，读 `ting-play --watch -j` 拿播放位置，就能自己完成同样的对齐。窥探只是这两份契约
   在人机面的呈现，不需要新增任何东西。（旧稿说 `ting-play --status -j` 发布了播放秒数 `pos`——
   不成立：那个 `pos` 是队列下标。播放位置的 agent 面由 `--watch` 第一次提供。）
2. **按下前可预期**：无新键位，无模态；有词则显，无词则隐。
3. **只为一个答案停下来**：拉取在 goroutine 里，更新循环不等待。
4. **付了的代价不丢**：本会话内按 URL 缓存。

---

## 实施顺序

1. 在 Go 的 `verb` 包里加 `--transcript -j --segments` 的调用与 segments 解析；能力门控读 `--engines` 的 `flags[]`。
2. 模型里加歌词状态（none / loading / ready）与按 URL 的缓存；换曲事件触发拉取。
3. 游标：位置事件 + 二分 + 外推；暂停与 seek。
4. details 渲染置换与行预算；先把过渡候选渲染成帧供选择，再定实现。
5. tmux 驱动段加用例：真实 `ne` 搜索、真实起播、轮询歌词行出现在帧上（不 sleep）；
   纯音乐与 `bili` 两条退避路径各一例。
6. 更新 Go 版 `ARCH-tui.md` 的 details 章节，删除本文件。

---

## 未决

- 自动字幕轨（`is_auto`）是否加一个弱化标记，还是与人工歌词一视同仁。
- 换句过渡的时长与曲线（按上面的惯例，先渲染候选再定）。
- 多语言字幕的引擎（`yt` 的 `--sub-lang`）：TUI 用引擎默认语言，还是跟随 chrome 语言。
