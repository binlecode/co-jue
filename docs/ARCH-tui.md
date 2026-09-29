# ARCH-tui —— 人机面 `ting` 的实现

**这份属于 `ARCH-*` 系列**，入口和全文档路由在 [`ARCHITECTURE.md`](ARCHITECTURE.md)。

## 模块功能和结构

**管什么**：**唯一的人机交互界面 `ting`** —— 一个 Go 二进制（`cmd/ting`，Bubbletea 事件循环），
套件里唯一不是 bash 的那一个。启动门与引擎发现、一个视图与七种行源、帧的高度预算与宽度、键位解析、
播放态横幅（由 `ting-play --watch` 驱动）、存储键与 3 秒撤销、URL 目标、十三套主题与亮暗判断、
偏好写回，以及焦点行的 kitty 协议封面。
🔴 **纯编排面**：不含任何站点知识，也不含播放器控制逻辑；对下只调这套 CLI 的公开动词，
与 agent 平级。

**不管什么**（边界表，走错门会得到相反的建议）：

| 事项 | 归哪 |
|---|---|
| 音源搜索、解析直链、站点的句柄与主机表 | [`ARCH-engine.md`](ARCH-engine.md) |
| 播放器生命周期、mpv 参数、`--watch` 状态流、两个持久存储 | [`ARCH-player.md`](ARCH-player.md) |
| `ting` 的 argv、TTY 门与配置写回在契约上的地位；信封、退出码、SemVer | [`ARCH-cli-contract.md`](ARCH-cli-contract.md) |
| 演进路线与待办/待决事项 | [`ROADMAP.md`](ROADMAP.md) |
| 界面设计决定（卡片化/双栏/按键表/真彩/Kitty封面） | 本文「帧」「键与输入」「主题与偏好」「封面」 |

### 一张图：架构分层与组件

```
   用户终端 TTY
        |  按键、粘贴、窗口尺寸                       ^  一帧（整帧文本，差量写出）
        v                                            |
   +------------------------------------------------------------------+
   | cmd/ting        flag 门 -> 引擎发现 -> TTY 门 -> 背景/封面探测   |
   |                 -> 接管在跑的播放器 -> tea.Program -> 退出收尾   |
   +---------------------------------+--------------------------------+
                                     v
   +------------------------------------------------------------------+
   | internal/tui    Model（Bubbletea 根模型，单向时钟与状态调度）   |
   |   navbar.go / stage.go / dock.go / inspector.go / stage_mode.go |
   |   style.go / cover.go (Kitty 两阶段渲染) / text.go (字符标尺)   |
   +----------------+-------------------------------+-----------------+
                   v                               v
   +-------------------------------+ +--------------------------------+
   | internal/tui/layout           | | internal/config  配置与写回    |
   |   三档断点 / 纯空格槽防撕裂   | |   flag > env > 用户 > 出厂     |
   +-------------------------------+ +--------------------------------+
                   |
                   v
   +-------------------------------+
   | internal/verb  唯一跑子进程处 |
   |   argv -> 信封 -> 类型化错误  |
   +---------------+---------------+
                   v
   ting-play | ting-playlist | ting-history          （bash 3.2）
                   v
   ting-engine-<site>     mpv / yt-dlp / curl        （私有）
```

## 接口与 API

一个 TTY 上的键位面（键表由 `ting --help` 陈述、由 `tests/contract.sh` 的 tmux 段证明），
对下组合动词：`ting-play --search/--info/--items/--auth -j`（引擎动词一律经 `ting-play` 转发）、
`ting-play -d -j` 与各 socket 动词、`ting-play --watch -j`、`ting-playlist`、`ting-history`。
它在契约上拥有的三样（argv、TTY 门、写回）在 `ARCH-cli-contract.md`「`ting`」。

### 调用面 —— 两道门，顺序固定

flag 门在前，TTY 门在后，**两道都退 1**，所以只能靠文案分辨：喂同一根管道两次读回两种文案，
顺序就被钉住了（`ting -f viz </dev/null` 答 mode 门，`ting -f video </dev/null` 答 TTY 门）。
每个标量旋钮值非法时点名那个键，**在 TTY 门之前** —— 一个默认 `auto` 的能力键写错了值，
最容易的失败是"屏幕安静地永远不画"，所以它必须出声。`--engine` 的名单也在 TTY 门之前，
这要求引擎发现（一次 `ting-play --engines -j`）排在 TTY 门之前。

---

## 分层：Go 里能出现什么

**一条判据：二进制里出现 mpv、yt-dlp、socket 路径或针对某个站点的行为，就是一次分层违规。**
引擎名作为不透明字符串显示、转发不算。一件只有 TUI 能做的事，要么下沉成一个动词（agent 同时得到它），
要么不做。这条判据的落点是 `internal/verb`：**全二进制只有它执行子进程**，拼 argv、解最后一行信封、
把退出码映射成三类错误（1 用法 / 2+ 外部 / 4 未生效）。键处理器只调它。

### 人机界面横切规范（UI 与 CLI 双界面的四条硬判据）

这四条是约束 TUI 与 CLI 行为一致性的基础原则：

1. **一个功能必带 agent 面**：人有按键，agent 就要有动词加一个 `-j` 信封。套件定位是 agent 优先（[`ARCHITECTURE.md`](ARCHITECTURE.md)「定位与设计目标」），一个只有键位的功能只做了一半。严禁先给 TUI 加个键、“agent 面回头再说”；
2. **按下前可预期 —— 分清“根本做不了”与“答案可能是没有”**：
   - 结构上做不了的键不许占一格（如单引擎下的 `e`、未装存储时的 `a`/`b`/`d`），按前即可判定，必须在界面提示块上门控排除；
   - `c`/`i` 是提问键，问的是行内是否还有分 P 或详情，“没有”是合法答案。提问键遵循三条硬约束：提示进帧不偷下一个按键、否定答案带回基础元数据、单槽缓存不买第二次。严禁由 UI 启发式猜测（[`ARCHITECTURE.md`](ARCHITECTURE.md)「站点知识的边界」）；
3. **只为一个答案停下来**：界面阻塞等键只能是因为它真的需要一个答案（如输入歌单名）。绝不做“你确定吗”类弹窗（挡不住误操作反而被练成连招），阻挡误操作必须由 `z` 撤销兜底。一条非交互消息是瞬态帧的内容，由唯一的渲染器画出，下一次按键即清理；
4. **付了的代价不丢**：用户明确触发的网络往返，取回的数据落屏并不因行源暂存而丢弃（会话内内存缓存）。

判据在移植里实际拒掉过三样 shell 版持有的东西，各自换成了不认站点的做法：

- **粘贴一个 URL** —— shell 版有一张主机表、按站点的 URL 模式判断"容器还是单曲"、还从 stderr 里
  找关键词。这里只问 `ting-play`（它本来就按主机把 URL 交给对应引擎）：先 `--items`，退 1（不是容器）
  或只有一项时落到 `--info`，一行结果替换搜索列表。判断只看退出码与信封，一个字的文案都不读。
  先 `--items` 是量出来的：同一个 YouTube 歌单，`--items` 秒回，`--info` 超过 80 秒没答。
  什么算 URL 同样不认站点：一段 `http(s)://`，或一个以 `www.` 开头的词；`lofi.mix/2024` 这种形如
  主机/路径的文字是查询词。URL 打开的那一行后面没有查询，`o`、`e` 与翻出更多都不拿 URL 去搜；`e` 只换下一次搜索用的引擎，
  而状态行显示的是这一行自己的引擎，所以它用一条提示说出换到了哪里。
- **引擎色块** —— shell 版按 `yt`/`bili`/`ne` 查一张品牌色表。这里不给引擎上色：状态行里的引擎名
  与其余条目一样是暗色文字。名字本身已经说明来源；按名字哈希出的颜色是任意的，品牌色表又要认站点。
- **存储行的封面** —— shell 版能从 url 推一张 YouTube 缩略图。这里只画信封里带着 `thumbnail` 的行。

## 启动与退出

**引擎发现只问一次，TUI 不持有源清单。** 去哪里找、同名谁赢是 `ting-play` 的规矩，每个引擎接受什么
是引擎自己的 `flags[]`。`c` 只在焦点行的引擎有 `--items` 时给，`i` 只在有 `--info` 时给 ——
不去无句柄地调一次再从 stderr 分辨，那句文案不在契约里。零个引擎是致命的启动错误。

**接管：屏幕开机时音箱里可能已经在响。** 播放是脱离终端的，所以启动时问一次 `--status`，
按核心自己的 0/1/多规则：恰好一个就接管到横幅上；两个及以上只出一条提示、横幅留空 ——
这边不比核心猜得更狠，下一次 Enter 是用户自己的选择。

**`q` 退出并停掉横幅上的播放器**（本会话起的或接管来的）。退出收尾在一个地方：关掉 `--watch`、删封面、丢掉撤销副本、写回偏好、停播放器；
停不掉就在屏幕之外打出停掉它的那条命令 —— 按 `q` 时那一行是唯一留得下来的证据。

**升级会把工具从正在跑的会话底下抽走。** 包管理器新版一落地就删旧目录，会话握着的
`ting-play` 路径随之失效，而 `q` 发的停播是唯一不许悄悄失败的调用。`verb` 在每次调用前 stat 一次
路径，文件不在了就按名字到 PATH 上重找（包管理器在那里为当前版本留着稳定的名字）；
文件还在就绝不重找，所以一个普通会话从头到尾说话的对象不变。

**第一帧等第一次搜索回来。** 之前只画一行 `searching "…"`。标题行上的 `query='` 因此是一个真的
就绪标记 —— 套件与 `drive.sh` 都等它，等到了就按 Enter；早画的标题行会让那一下落在空列表上。
第一次搜索**失败**（非零退出）没有东西可操纵，以 `search failed (<reason>): <advice>` 结束会话；
**没有结果**（退 0、count 0）不是失败，是一个空列表加提示 —— 修它的 `n`、`e`、`b` 都要菜单在。

## 多任务平级工作区（Stage & Navbar）

五大顶级工作区各自持有独立生命周期的 `ViewState`（光标、过滤词、行源、已拉取列表），切换工作区不销毁原浏览状态：

- **五大顶级工作区（Top-level Workspaces）**：
  - `WsSearch`（`w1`）：全局关键词搜索流；
  - `WsFeeds`（`w2`）：算法推荐首页与订阅流（`--feed home`）；
  - `WsQueue`（`w3`）：在播播放器待播队列（带 `x` 移除、`>` 置顶、`p/P` 重排）；
  - `WsPlaylists`（`w4`）：本地歌单库（`b`）与云端外部歌单（`B`）。歌单库本身是一级行源（`srcPlaylists`）：
    每个歌单一行占满主舞台，`Enter` 进入该歌单曲目（`srcPlaylist`），`d`/`D` 删单，`R` 改名，`Esc`/`b` 返回搜索结果。
    彻底废除旧时代底栏 `askOpen` 弹框选择器（避免搜索结果残留与大面积空白）；
  - `WsHistory`（`w5`）：本地收听历史日志（`h`）。
- **单键平权与既有契约兼容**：
  - `b`：在歌单工作区与搜索工作区之间平级双向切换；
  - `h`：在历史工作区与搜索工作区之间平级双向切换；
  - `u`：在队列工作区与搜索工作区之间平级双向切换；
  - `g`：单跳关联推荐（Related），再次按 `g` 原路退出并恢复原搜索列表。
- **下钻子视图的状态隔离**：
  分 P `c`、章节 `i`、单跳关联 `g` 属于特定曲目的下钻详情层（工作区映射为 `-1`）。下钻不污染顶级工作区状态槽，退出（`backToSearch`）时直接从 `WsSearch` 槽只读恢复。
- **队列焦点激活再核验（Revalidation on Focus）**：
  切回队列工作区时，若离焦期间收到了 `--watch` 推进事件，后台异步拉取 `ting-play --queue-show -j` 刷新槽位；编辑按下标加 `--expect-url`，对不上时重读报错；在播行不能移出。
- **存储行混源路由**：每一行自带 `engine`，播放、入队、存歌单按行路由；状态行报引擎组合与条数。

## 帧：上带、行、下带

一帧分为三段：**上带**（标题行与右贴状态段、提示行、横幅、进度条、一条空行）、**行**（光标槽 + 序号 + 标题 + 右贴时长轨 + 最右列滚动条）、**下带**（空行、焦点行 details、空行、键位块、过滤/提示输入行）。Chrome 收在两端，中间全是内容。

**高度预算链（`layout()`）**：
先定上带，再定键位块能不能留（列表至少还要四五行），再钳光标、为焦点行测 details（封面闸门在此决定），最后剩下的才是可用行数；不够时先丢行间空行，再丢键位块上的空行，再丢过滤提示行。
**宽屏双栏核心不变式**：双栏块的高度严格为 `终端行数 − 上带 − 下带`（`m.height - f.chromeH - f.footH`，`Nj` 计数在画时再让一行），绝不裸用 `layout.Compute` 的 `Stage.H`（后者假定没有底座与下带）。整帧总行数严格 $\le m.height$，多出 1 行就会触发 AltScreen 滚屏推走 Header。

- **列表模式只有一根轴**：`page` 模式取 `-p` 与余量的小者，`→` 翻页追加、`←` 砍掉；`scroll` 模式无页，动作挂在首尾行两端，`←/→` 静默；
- **状态是一串值，不是一串 `键=值`**；停在默认值上的字段（`auto` 档位、`off` 循环、0 秒边界）不占格；
- **提示是帧的内容，不是模态**：计入测量，由下一个按键清掉；撤销提示开着时说 "z 撤销"，开多久说多久；
- **权限徽章仅在受限时出现**：完整可播曲目零字符侵占，仅在遇到试听（`30s`）或 VIP 锁定时行尾追加微型标记，输入 `30s` 或 `VIP` 可直接过滤。

**宽度规则与空气槽隔离（`layout` & `text.go`）**：
- **宽度标尺**：go-runewidth，East-Asian Ambiguous 算一格，`TING_AMBIG_WIDE=1` 才算两格。标题先剥离不可信图形符号（emoji、变体选择符等），每个字形都有确定宽度；
- **三档响应式断点**：宽屏（≥96，双栏 Stage + Inspector，工作区融入顶栏）、标准（85~95，单栏平铺展开）、紧凑（<85，单栏折叠，检查器与封面自动收起）；
- **纯空格物理空气槽防撕裂**：双栏不用 `|` 实心竖线，统一用 2 格纯空格（`GutterWidth = 2`）隔离。不同终端对 CJK/EAW 字宽解释不一时，实心竖线极易锯齿折断；纯空格配合 `newWidth` 标尺逐行对齐，彻底杜绝折行撕裂。

## 界面各视图实机 ASCII 帧（Real TUI View Frames）

所有帧均由真实运行的 `ting` 实例通过渲染模型录制生成（`TING_ASCII=1` 纯 ASCII 模式），经由 `clean_capture.py` 严格清洗、由 `assert_pane.py` 度量对齐后录入，无任何手绘漂移：

### 1. 宽屏旗舰模式（130×26 视窗实拍：双列自适应工作台，顶栏工作区集成）

<!-- pane:wide-130 -->

```
[ 听 ]  [w1 搜索*]  w2 推荐   w3 待播   w4 歌单   w5 历史   query='lofi hip hop'             yt | 14 结果 |  | 已登录 |  | 质量
> 播放中: Best of lofi hip hop 2021 [beats to relax/study to]                                [00:07/06:10:57] | cpu 32% ram 102M
---------------------------------------------------------------------------------------------------------------------------------

> Best of lofi hip hop 2021 [beats to relax/study to]                                6:10:58 |  当前播放与同步歌词
  lofi hip hop radio beats to relax/study to                                           --:-- |  Best of lofi hip hop 2021 [beat...
  Ｎｉｇｈｔ Ｄｒｉｖｅ ~ lofi hip hop mix ~ beats to chill / drive to              24:37:03 |  yt
  90's Chill Lofi Study Music Lofi Rain Chillhop Beats Lofi Rain Playlist           11:53:45 |  139k opus  |  []
  1 A.M Study Session [lofi hip hop]                                                 1:01:14 |  ------------------------------
  90's Chill Lofi Chill Music Lofi Rain Hip Hop Beats Lofi Rain Playlist             1:43:55 |    Rain falling on the roof
  remember when lofi hip-hop was chill like this.                                    1:00:21 |  > Neon lights blur in mist
  Work Lofi - R&B That Sparks a Mood [rnb , lofi hiphop]                             3:23:04 |    Coffee aroma in the room
  Chill Lofi Mix [chill lo-fi hip hop beats]                                         1:44:52 |
  Chill Study Beats 4 • jazz & lofi hiphop Mix [2017]                                2:01:15 |
  remember when lofi hip-hop was smooth like this.                                   1:02:02 |
  Upbeat Lofi Mix Beats to Boost Your Energy & Focus                                 4:27:53 |
  lofi hip hop mix beats to relax/study to (Part 1)                                  2:50:41 |
  𝐏𝐥ａｙｌｉｓｔ Tokyo Lo-fi Hiphop Chill Beats for Study & Relax                    3:00:08 |





  Up/Dn 选择   Enter 播放   / 过滤   z 撤销   q 退出   ? 键位
```

<!-- /pane:wide-130 -->

### 2. 标准工作台与在播底座（100×26 视窗实拍）

<!-- pane:main-dock-100 -->

```
[ 听 ]  query='lofi hip hop'                                   yt | 14 结果 |  | 已登录 |  | 质量
> 播放中: Best of lofi hip hop 2021 [beats to relax/study to]  [00:07/06:10:57] | cpu 32% ram 102M
---------------------------------------------------------------------------------------------------

> Best of lofi hip hop 2021 [beats to relax/study to]                                      6:10:58 |
  lofi hip hop radio beats to relax/study to                                                 --:-- |
  Ｎｉｇｈｔ Ｄｒｉｖｅ ~ lofi hip hop mix ~ beats to chill / drive to                    24:37:03 |
  90's Chill Lofi Study Music Lofi Rain Chillhop Beats Lofi Rain Playlist                 11:53:45 |
  1 A.M Study Session [lofi hip hop]                                                       1:01:14 |
  90's Chill Lofi Chill Music Lofi Rain Hip Hop Beats Lofi Rain Playlist                   1:43:55 |
  remember when lofi hip-hop was chill like this.                                          1:00:21 |
  Work Lofi - R&B That Sparks a Mood [rnb , lofi hiphop]                                   3:23:04 |
  Chill Lofi Mix [chill lo-fi hip hop beats]                                               1:44:52 |
  Chill Study Beats 4 • jazz & lofi hiphop Mix [2017]                                      2:01:15 |
  remember when lofi hip-hop was smooth like this.                                         1:02:02 |
  Upbeat Lofi Mix Beats to Boost Your Energy & Focus                                       4:27:53 |
  lofi hip hop mix beats to relax/study to (Part 1)                                        2:50:41 |
  𝐏𝐥ａｙｌｉｓｔ Tokyo Lo-fi Hiphop Chill Beats for Study & Relax                          3:00:08 |

  Lofi Girl | 6:10:58 | 57,696,650 views | n61ULEU7CO0
  opus 139 kbps 48 kHz stereo
  Listen on Spotify, Apple music and more https://fanlink.tv/BestofLofi2021 The new Lofi Girl
  compilation “Best of 2021” is out now ...

  Up/Dn 选择   Enter 播放   / 过滤   z 撤销   q 退出   ? 键位
```

<!-- /pane:main-dock-100 -->

### 3. 全屏纯享舞台模式（`F` 键唤起，100×26 视窗实拍）

<!-- pane:stage-mode-100 -->

```
[Esc / F] 返回工作台

                        Best of lofi hip hop 2021 [beats to relax/study to]
                                          139k opus  |  []

                                           (暂无同步歌词)

                    ------------------------------------------------------------
                                          00:07 / 06:10:57
```

<!-- /pane:stage-mode-100 -->

### 4. 待播队列工作区视窗（`w3` / `u`，100×26 视窗实拍）

<!-- pane:queue-100 -->

```
[ 听 ]  queue='待播队列'                                                      yt | 2 项 |  | 质量
队列: 已加入队列 -> lofi hip hop radio beats to relax/study to | z 撤销
> 播放中: Best of lofi hip hop 2021 [beats to relax/study to]  [00:07/06:10:57] | cpu 32% ram 102M
---------------------------------------------------------------------------------------------------

> Best of lofi hip hop 2021 [beats to relax/study to]                                      6:10:57 |
  lofi hip hop radio beats to relax/study to                                                  0:00 |

  Lofi Girl | 6:10:57 | 57,696,650 views | n61ULEU7CO0
  opus 139 kbps 48 kHz stereo

  Up/Dn 选择   Enter 立刻播   / 过滤   z 撤销   q 退出   x 移出   pP 上移/下移   u 返回搜索列表
  ? 键位
```

<!-- /pane:queue-100 -->

### 5. 极端受限窄屏自适应模式（62×20 视窗实拍）

<!-- pane:compact-62 -->

```
[ 听 ]  query='lofi hip hop'
yt | 14 结果 |  | 已登录 |  | 质量
> 播放中: Best of lof...  [00:07/06:10:57] | cpu 1% ram 116M
-------------------------------------------------------------

> Best of lofi hip hop 2021 [beats to relax/stud...  6:10:58 #
  lofi hip hop radio beats to relax/study to           --:-- #
  Ｎｉｇｈｔ Ｄｒｉｖｅ ~ lofi hip hop mix ~ be...  24:37:03 #
  90's Chill Lofi Study Music Lofi Rain Chillho...  11:53:45 #
  1 A.M Study Session [lofi hip hop]                 1:01:14 |
  90's Chill Lofi Chill Music Lofi Rain Hip Hop ...  1:43:55 |
  remember when lofi hip-hop was chill like this.    1:00:21 |

  Lofi Girl | 6:10:58 | 57,696,650 views | n61ULEU7CO0
  opus 139 kbps 48 kHz stereo
  Listen on Spotify, Apple music and more
  https://fanlink.tv/BestofLofi2021 The new Lofi Girl

  Up/Dn 选择   Enter 播放   / 过滤   z 撤销   q 退出   ? 键位
```

<!-- /pane:compact-62 -->

## 键与输入

键表不在这里（`ting --help` 陈述，套件证明）。这里只记派发的形状与几条源码说不出来的理由。

- **两段派发**：通用段（播放控制与显示开关 —— 属于播放器或渲染，不属于某一个视图，所以在每个行源里都有效），
  列表段（移动、动作、换行源、取数、退出）。**数字只在列表段**：`/` 开着时数字是用户在打的查询，
  过滤有它自己的读取器，数字够不着 —— 过滤器因此是构造上安全的。跳行计数由任何不构造、不结束它的键清掉，
  清除点只有一个，在两段之前。`j` 有计数时结束计数并跳到**绝对**行号（`#` 印出的那个），没有时是下移。
- **一次读到的几个键是几个键，不是一段文本。** Bubbletea 把一次 read 里的多个字符交成一条消息；
  双击的 `?`、快速打的 `12j`、按住的 `j` 都会这样到达。这里把每个字符拆成一个键 —— 真的粘贴走括号粘贴，
  不走这条路。粘贴进列表：URL 打开它指向的东西，其余文本打开搜索提示并填好。
  `p` / Ctrl+V 读系统剪贴板（队列视图里 `p` 是上移）。
- **提示只有一个读取器**（启动查询、`n`、`a` 的新歌单名与选单、`B` 的选单、`R` 的新名字），
  所以 Esc、编辑与宽字符在每个提示里含义一致；启动时的提示取消即干净退出。提示只留给真要打字的问题：
  "打开哪个歌单"不是 —— `b` 把歌单库放上主舞台，列表键就是选择器（见「多任务平级工作区」）。
  `a` 的选单是提示上方一段带序号的散文：回答是一次按键而不是一次回忆；序号压过同名的数字，
  而一个真名叫 `7` 的列表照样够得着 —— 用它自己那一行的序号。存储为空时两键分岔：`b` 就地答"还没有"，
  `a` 改问新列表叫什么。
- **按键处理保持原生分支匹配，拒绝按键注册表（按键注册表 NO）**：
  键位派发直接使用 Go 原生 `switch key` 分支匹配，不抽象为全局按键映射表。键位有效性由运行上下文表达式动态门控
  （如单引擎门控排除 `e`、未装存储门控排除 `a`/`b`/`d`），表驱动增加维护间接层，且本套件无自定义键位映射需求。
  重开条件：确需支持用户自定义键位映射。

## 常驻底座与单调时钟（Player Dock & PlayerClock SSOT）

底座由 `ting-play --watch -j` 驱动并下沉常驻：每行都是完整状态，读者只留最新一条，中途接上也不缺字段。`--watch` 在独立进程组，TUI 退出或切歌时收走子进程，不影响播放器。
**统一单调时钟快照（`PlayerClock` SSOT）**：由根模型维护唯一的 250ms 单调外推引擎，向 Dock、Inspector 等子视窗只读广播同一份毫秒时间戳与状态快照（`Active`、`Playing`、`Paused`、`Buffering`、`Live`、`Pos`、`Dur`），杜绝各组件独立外推导致时间戳漂移与歌词不同步。

- **三档播放态**：`-d` 返回即 "缓冲中"（spinner），首个 `ready` 为 "播放中"，暂停另算；闲置时静默（`Active == false` 返回空），不占行高；
- **1/8 亚格进度条与章节断口**：进度条按左侧八分块（`▏▎▍▌▋▊▉█`）平滑推进，一格约 4–8 秒；在播曲目属于章节列表时，在 `chapterSpan` 起止点留白断口，标明当前章节在全曲的位置。ASCII 模式回退为 `=` 与 `-`；
- **音量连按合并防抖**：目标值从最新一次按键算起，同一时刻最多一个 `--set-volume` 在途，在途按键仅替换下一次发送值；
- **窄屏时间优先保全**：宽度受限时优先截断曲名文本，确保右侧时间进度（`01:23 / 04:15`）始终可见；
- **`r` 三态而播放器只有两态**：`off`/`one` 直发播放器 `--loop`；`seq` 不是播放器状态，而是指示下一次 Enter 组装队列，当场提示 "下次起播生效"；
- **Enter 是切换不是叠加**：先停当前播放器（接管来的也一样），再起新播放器，两首歌绝不同时进扬声器。

## 存储键与可撤销

**删除类按键不设 "确定吗" 弹窗，`z` 在 3 秒内撤回** —— 弹窗挡不住连招误触，撤销才能。

- **副本不在 TUI 里**：每次写带 `--owner <pid>`，由存储维护副本并在信封给出截止时间；TUI 仅记录四个标量：存储名、过期时间、提示文案、挂起的停播；
- **全套件单槽撤销**：写到另一个存储时先释放旧副本，`z` 永远撤销 "刚才那一下"；
- **副作用推迟至提示关闭**：`d`/`D` 删到正在播放的曲目时，停播挂起到撤销窗口关闭 —— 停掉的播放器是撤销唯一无法恢复的副作用；
- **按 URL 查索引而非光标**：`d` 提交的索引根据 URL 与出现序号从存储反查，不用光标位置，防止过滤视图下删错行；查不到即拒绝；
- **网络取数单飞排他**：存储与队列调用为本地文件锁（毫秒级同步调用）；引擎取数走异步命令且同一时刻只有一次在途。在途期间再次触发取数（`o`/`e`/`i`/`c`、翻页、搜索）直接拒绝并提示 "完成后再按一次"，不排队。

## 主题与偏好

**调色板是 (主题, 背景, truecolor) 的纯函数**（`style.go`），`t` 可实时重算。Truecolor 下在播行底色由主题标志色按比例混入背景（暗底 30%、亮底 18%），状态色按对比度地板推开；无 truecolor 时退回 ANSI-16。颜色关闭（`--color never`、`NO_COLOR`）时 `t` 静默。

**灰阶视觉层级与按键胶囊**：
- **Foreground**（焦点 / 语义主体）：加粗焦点标题（`p.Bold`）、发声歌词（`p.Accent`）、常规标题；
- **Secondary**（决策元数据）：右轨时长、行序号、状态项、时间进度、Details 解码信息。TrueColor 下对比度 ≥4.5:1（WCAG AA）；
- **Muted**（底噪 / 结构线）：分割点（`·`）、未播进度槽底、系统负载、滚动条。TrueColor 下对比度 ≥3.0:1；
- **Keycap**（按键胶囊弱化）：TrueColor 下收敛至微弱底色（暗底 10%、亮底 8%），去除粗体（`mono` 保留），消除大量高亮块喧宾夺主。

**亮还是暗**：`TING_BG` 给了就用；`auto` 先读 `COLORFGBG`，再在 tmux 之外问终端（OSC 11），
最后落在暗。查询跟着一个光标位置报告发出、按 `select()` 等待（termenv），不答 OSC 11 的终端当场只答
那一个报告，不会有字节留给按键读取器。

**十一个键改的设置写回用户配置**（机制、键表与硬约束在 `ARCH-cli-contract.md`「配置面」「写回」）。
TUI 这一侧的三件事：置脏点都在成功路径之后（换源、换排序取数失败时屏上是旧值，文件里也必须是旧值）；
**写在最后一次改动一秒之后**，连按 `t`/`l` 只付一次重写，而会话还开着时文件里已经是那个选择；
环境变量钉住的键不写，并且只说一次。写回逐行搬运、不用配置库（它们在解码与编码之间丢注释）。

没有同步重绘开关：Bubbletea 每帧只把变了的行在一次 write 里写出，同步重绘（DCS 1q/2q）要防的撕裂由此不再出现。

**真彩色探针仅检查 `COLORTERM`（terminfo 探测 NO）。** 真彩检测只认 `COLORTERM` 环境变量（`truecolor` / `24bit`），
不向 terminfo 探针查询 `RGB` 或 `Tc` 能力位。主流复用器（tmux）与现代终端均可靠透传 `COLORTERM`；
而宿主自带的旧版 ncurses 对能力位探测存在假阴性，查能力位反遭误伤。重开条件：宿主环境更新至原生支持真彩能力位，或出现支持真彩却不设 `COLORTERM` 的主流终端。

## 同步歌词与舞台模式（Lyrics & Stage Mode）

- **侧栏 Inspector 歌词流**：宽屏（≥96）下右侧常驻垂直歌词：
  - *当前焦点句*：主题强调色加粗（`p.Bold + p.Accent`），行首带 `> `；
  - *已唱完句*：暗色弱化（`p.Muted`）；
  - *未唱句*：次要灰阶呈现（`p.Secondary`）；
  - *间奏跳动*：保留上一句文本，下方追加弱化指示符（`> ... (间奏)`）；
  - *优雅退避*：无歌词或加载中显式提示状态，不留白屏。
- **全屏舞台模式（`F` 键）**：按 `F` 唤起全屏居中大字卡拉 OK；按 `Esc`/`F`/`q` 原路退回工作台，保留所有浏览进度与光标；仅保留发声控制键（`Space`、`-`/`=`、`[`/`]`、`r`），拦截其余键防误触。
- **单栏内联窥探（Lyric Peeking）**：窄终端（<85 列）下，当前焦点句等额置换 Details 中的 Description 空间（`Δrows = 0`），坚守单栏行预算。
- **纯公开契约**：只消费底层公开动词 `ting-play --transcript -j --segments` 与 `--watch`，内存按 URL 缓存，零临时文件。

## 封面 —— Kitty 协议两阶段渲染与防残影

能力是可选的：`TING_IMAGE=off` 或终端不支持，一个字节都不发。

**图片绝不走 mpv 的 VO（图片不走 mpv 的 VO NO）。**
终端封面显示严禁使用 mpv 视频输出驱动（VO）。mpv VO 语义为独占全屏终端而非局部嵌入，会清屏并夺取输入控制；且未压缩重传带宽开销巨大。封面由 TUI 自身异步拉取、纯 Go 解码（JPEG/PNG/WebP）、等比缩放后直发 Kitty 协议。重开条件：mpv 原生支持不接管终端屏幕与输入的局部嵌入。

**两阶段安全直通（Two-Pass Rendering）与防残影**：
- **Phase 1（文本层留白隔离）**：`inspectorModel.View()` 仅在有缩略图且封面开启时预留 7 行纯空格。多栏布局拼接如果混入 Base64 转义序列会被分列截断导致终端严重乱码；
- **Phase 2（绝对坐标打点）**：双栏文本拼接完成后，通过 `CoverBox()` 获取起始绝对行列号，发射 Kitty 放置指令（`kittyPut`）。指令挂在双栏块前并用 `ESC 7`/`ESC 8` 包裹保存/恢复光标，不干扰渲染器跟踪；
- **物理显存残影擦除**：窗口缩至单栏紧凑模式（<85 列）或进入全屏舞台模式（`F`）时，强制发送 `kittyDel(false)` 物理擦除图层，杜绝幽灵残影。

**封面闸门规则**：
- 宽屏双栏：封面独占右侧 Inspector 顶部 7 行，不挤占曲目列表空间；
- 单栏紧凑：闸门严格要求列表剩余 $\ge 4$ 行且文字宽度 $\ge 50$ 列才开启 7 行 details 盒子；达不到闸门直接不画，绝不画小号封面打乱行高。
- **探测与网络防御**：启动前通过 DA1 与 Kitty 查询探针探测支持情况；像素尺寸通过 `CSI 16 t` 查询实测（Ghostty 报 16×36，不能盲猜 8×16）。取图 HTTP 客户端仅提供 X25519/P-256 曲线（避开部分 CDN 对后量子 ClientHello 的超时），单张图片 5 秒超时上限。

**为什么是 details 右栏**：一行文本放不下一张图；details 本来就是"要多少占多少、太矮就整块丢掉"的
变高 chrome，把图挂在它身上是延用一条既有规则。**闸门问列表还剩几行（4）、文字还剩几列（50），
不问终端多大**；闸门开着时 details 恒为盒子的 7 行，**有图无图都计** —— 否则光标每跨过一条没有封面的行
列表就重排一次。过不了闸门就一张也不画，不画小一号的。

- **探测在界面接管终端之前**：`auto` 在 tmux 下关（没有可移植的穿透），已知支持的终端直接开，
  其余发一次 kitty 查询，后面紧跟一个 DA1 —— 人人都答 DA1，所以不支持的终端当场答完，不留字节。
  **格子像素是问出来的**（`CSI 16 t`；实测 Ghostty 报 16×36，按猜的 8×16 转码画出来不到目标的四分之一），
  问不出来才用 8×16。
- **取图与解码在按键循环之外，同时只有一张在飞**。服务器拒绝（4xx）或解不开的图记下，本会话不再要；
  网络层的失手（超时、连不上、5xx、429）只记 30 秒，之后焦点再落到那一行时重取 —— 否则 CDN 挂一次，
  那一行整个会话都没有封面。
  YouTube 的 `hq720.jpg` 实际以 webp 返回，所以解码带 `x/image/webp`；按盒子等比缩放（不拉伸，
  方形专辑封面仍是方形），编码成 PNG 交给终端。
- **取图的 HTTP 客户端只提供 X25519 与 P-256。** Go 默认附带的后量子 X25519MLKEM768 让 ClientHello 变大，
  一个引擎的封面 CDN 对它永远不回握手（实测每次超时；换成经典曲线后同一张图 1.7 秒到，与 curl 相同）。
  那个 CDN 另外还会间歇性地挂住连接（curl 与 Go 一样），所以取图有 5 秒上限，这一次拿不到，这一行就先没有封面。
- **发射骑在行下面那一行上**：画的帧放一次放置（`a=T`，只给 `r` 由终端算列数，`C=1` 不动光标，
  `q=2` 不回话），不画的帧放一次按 id 删除（`d=i`；`d=a` 会连复用器里别人的图一起删，退出时用 `d=I`
  连数据一并释放）。渲染器只重写变了的行，所以两者都恰好在封面变化的那一帧发出一次 ——
  而删除是必须的：擦掉文字从来擦不掉图。`#` 这类重画整帧却不改封面的键不会重传它。
- 加载中，盒子中间一行画 `loading pic...` 与 spinner。

## 测试

`go test ./...` 驱动这个 checkout 的真实命令与真实网络（带网络的在 `-short` 下跳过）：翻页两端、过滤、
跳行、排序、歌单往返与撤销、三个引擎的封面解码。`tests/contract.sh` 的 tmux 段与 `tests/drive.sh`
在开头把 `cmd/ting` 构建到 `shell/.ting-go`（点开头，任何 `shell/*` 的 glob 都碰不到），然后驱动它；
断言针对行为与帧结构，封面的"发没发"由 `pipe-pane` 读写出去的字节证明 —— tmux 不转发图形转义，
`capture-pane` 看不到它们。
