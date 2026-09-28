# RESEARCH-tui-player —— 终端播放器：别人怎么搭的，我该抄什么、该躲什么

> **先说这份调研是为什么做的**，因为它决定了每个数字该怎么读：
> **ting 是我和几个朋友自用的一件工具 —— 工作时放点音乐，人从 TUI 放，agent 从 CLI 放。**
> 不是要做这个领域的头部项目，也没有用户增长要交代。所以这份文档**不是竞品分析**，
> 它只回答两个自用者的问题：**这件事别人是怎么做的**，以及**哪些做法值得抄、哪些是我不需要的**。
>
> 由此，几个读法要先校准：
> - **star 数不是排名**，它在这里只回答"这项目还活着吗、值不值得花时间读它的代码"（§2）；
> - **功能清单不是差距**。别人为了拿用户在拼的东西（参数 EQ、频谱、图形协议封面、Lua 插件 ABI），
>   对"工作时放点音乐"是零价值 —— 照着抄就是给自己派活。
>
> 这仍然是一份调研文档：只记**仓库之外**的事实与可核对的出处，不记我们要建什么。
> 由它落定的决定：`ARCHITECTURE.md` 的「定位与设计目标」（不做通用
> TUI 播放器），`ROADMAP.md` 的**打包 NO**（已于 2026-09-11 随 Tap 发布闭环反转）与 **Go 重写 NO**（已于 2026-09-24 演进：引擎与播放器坚守 Shell，人机 TUI 面因呼吸动效与事件循环需求独立改用 Go 重构）。
>
> **文档结构与可信度分界**：**§2（领域现状）为实测数据** —— 外面已经有什么；
> **§3–§7（播放设计、横切问题、音源与 agent 面）为源码核查与平台实测** —— 别人的播放器怎么搭、
> 国内拿到可播 URL 的四条路、agent 面如何接管播放生命周期。
> §1 把这条分界线写死，**读之前先看那一节**。

---

## 1. 时效、方法与诚实边界

| 内容 | 日期 | 怎么来的 |
|---|---|---|
| 领域现状（§2） | 2026-08-21，**2026-09-03 / 2026-09-26 更新** | **实测** —— GitHub API + 活跃项目跟踪 |
| **播放设计与音源（§3–§8）** | 2026-08-29，**2026-09-03 / 2026-09-12 / 2026-09-26 更新** | **读源码 + 实测** —— 网络检索 + 项目源码核查 + 歌词/动效/状态流深度对比 + 新候选第一性原理筛查 |

**这两半的可信度不一样，混着引用就会出错。**

上半的每个数字都会过期 —— star 数、`pushed_at` 尤其。想重开
`ROADMAP.md` 里任何一条由它支撑的决定，**先按对应小节的方法重跑，别照抄结论**。

下半的陈述大部分带出处（§10），**读结论不如读出处**；
2026-09-03 轮次通过平台直测和源码核查，把原先标 **[需实测]** 的几项（YouTube 6 小时、B 站 2 小时与 412
机制、网易云 20-25 分钟、go-musicfox mpv IPC、termusic gRPC）**全部升格为已证事实**；
2026-09-12 轮次针对第四音源候选（SoundCloud、Apple Podcasts、QQ音乐、小宇宙、汽水音乐、Spotify、Bandcamp）
执行了基于第一性原理 mission 的全面网调与本地实测（§6.1）；
2026-09-26 轮次针对 ROADMAP 最新第一梯队（TUI Look & Feel 专项、Go TUI 选型、焦点行歌词窥探、access 权限徽章排版、ting-play 0.18.0 单入口状态流）执行了深入的源码核查与对比调研；
§9 记录本轮已解决与新提出的问号。

---

## 2. 领域现状（GitHub API 实测）

**这一节量的是"谁还活着、谁的代码值得读"**，不是排名。一个死了两年的 19310★ 项目，
对我的价值低于一个 209★ 但每周都在动的项目 —— 后者会告诉我这条路今天还通不通。

### 2.1 头部分布（2026-09-03 实测更新）

| stars | 语言 | 最后推送 | 项目 | 音源 |
|---:|---|---|---|---|
| 19310 | Rust | **2024-04（已死）** | `Rigellute/spotify-tui` | Spotify |
| 8781 | Python | 2026-03 | `mps-youtube/yewtube` | **YouTube** |
| 7171 | Rust | 2026-07 | `aome510/spotify-player` | Spotify |
| 6761 | Rust | 2026-08 | `hrkfdn/ncspot` | Spotify |
| 6236 | C | 2026-08 | `cmus/cmus` | 本地 |
| 4834 | C++ | 2026-03 | `clangen/musikcube` | 本地 |
| 4144 | Shell | **2024-09（已死）** | `pystardust/ytfzf` | **YouTube** |
| 3990 | Go | 2026-09 | `bjarneo/cliamp` | 本地/流/Spotify |
| 3295 | Rust | 2026-09 | `mierak/rmpc` | MPD |
| 3017 | C | 2026-09 | `ravachol/kew` | 本地 |
| 2472 | C++ | 2026-06 | `ncmpcpp/ncmpcpp` | MPD |
| 2189 | Rust | 2026-09 | `tramhao/termusic` | 本地/流 |
| 1649 | Shell | 2026-08 | `Benexl/yt-x` | **YouTube** |
| 1302 | Rust | 2026-09 | `LargeModGames/spotatui` | Spotify |
| 772 | Rust | **2025-03（停滞）** | `sudipghimire533/ytui-music` | **YouTube** |

新一代（都还小）：`involvex/youtube-music-cli` 422、`NoctaVox` 377、`SubTUI` 260（Subsonic）、`gomu` 211、`waves` 168（Soulseek）。

**`pushed_at` 会把两个分布抹平，必须看 commit 数**：经典四件套在**维护模式**
（近 180 天：`musikcube` 1、`ncmpcpp` 2、`yewtube` 0（前轮测得 4，2026-03-04 后无新提交）、`cmus` 7），生长全在 2024–2026 新生代
（`cliamp` / `rmpc` / `kew` / `termusic` / `spotatui` / `go-musicfox` / `yt-x` 各 100+）。

### 2.2 国内格（2026-09-03 核查更新）

国内生态分布有其特殊规律，按规模与形态分为三档：

1. **老牌网易云主体**：
   - `darknessomi/musicbox`（9838★，Python，2026-08 底仍有维护提交）；
   - `go-musicfox/go-musicfox`（2538★，Go，2026-08-10 发布 v5.1.0，2026-08-31 仍有提交）：
     5.0 架构升级引入 DLNA 投屏引擎，并为 MPV 播放器落地了 Windows 命名管道与 Unix socket 双通道 IPC，
     代码质量与活跃度是目前国内 TUI 播放器中最高的；其核心维护成本依然在于对抗官方加密与 UnblockNeteaseMusic
     解灰退化。

2. **跨平台聚合 / 多源工具**：
   - `feeluown/FeelUOwn`（3933★，Python，2026-09-01 活跃）：采用 `fuo://` 插件化架构，覆盖网易云/QQ/酷狗/B站/YouTube；
   - `guohuiyuan/go-music-dl`（4138★，Go，2026-08-30 发布 v1.1.0）：将 10+ 国内主流音源（含网易云/QQ/酷狗/B站/汽水音乐）
     的搜索、解析逻辑收敛在底层独立库 `music-lib` 中，具备多源并发搜索与汽水音乐解密能力，主程序出 CLI / Web / TUI 三个壳。

3. **2026 年国内新生代演进（三个分化方向）**：
   - **逆向 API 的语言原生内嵌化**：打破了"新项目一律放弃网易云"的单向判断。2026 下半年出现的
     `professor-lee/CNMPlayer`（154★，Rust + Ratatui 0.30 + rodio/symphonia）引入了外部纯 Rust 库
     `ncm-api-rs`（`SPlayer-Dev/ncm-api-rs`，57★，从 `NeteaseCloudMusicApiEnhanced/api-enhanced` 移植），
     内置 weapi/eapi/linuxapi 三种加解密算法，实现了纯原生免外部 Node 服务依赖的网易云 TUI 客户端；
   - **面向 Agent / 机器调用的规范化 CLI 出现**：`public-clis/bilibili-cli`（1014★，Python，2026-03 活跃）
     为 B 站提供了结构化信封输出（`ok/schema_version/data/error`，支持 `--json` 与 non-TTY 下默认 `--yaml`），
     并提供视频/字幕/评论/ASR 音频切片（`bili audio --segment 25`）。**但其边界严格止于信息获取与下载切片，零播放生命周期控制**；
   - **YouTube / B 站 + yt-dlp/mpv 极简播放路线**：`MareDevi/bilibili-tui`（214★，Rust/Ratatui，2026-08-30 活跃）；
     `bighu630/music-tui`（1★，Go TUI，2026-09 活跃）；`xieerfan/BiliBiliMusicPlayer`（5★，Arch Linux，mpv + curl_cffi）。
   - **现代 TUI 界面质感与组件化演进（2026-09-26 观察）**：
     - **Go (Bubbletea) 渐成现代终端界面事实标准**：`go-musicfox` 与 `cliamp` 验证了 TEA (The Elm Architecture) 在音频客户端中的成熟度。`go-musicfox` 依靠单向数据流将键盘事件、mpv 状态、歌词滚动与页面路由严格串行化，彻底摆脱命令式局部擦写的乱序与字符残留；`cliamp` 依托 `lipgloss` 的声明式盒模型打造高质感复古终端界面。
     - **歌词呈现的交互瓶颈**：现存客户端对歌词的展示形态呈现两极分化 —— 要么全屏切走（`go-musicfox` 的 `page_lyrics`）、要么多栏切割破坏 CJK 曲名排版（`rmpc` 的 Panes）；尚无任何项目提供不破坏单视图、不抢占行预算的“内联窥探”方案。
     - **微动效与 CPU 开销的业界权衡**：高帧率终端渲染（30~60 FPS）若采用全量 ANSI 重绘，在 iTerm2/Alacritty 等终端下会导致渲染进程 CPU 飙升至 10~20%；`go-musicfox` 依靠 `timex.Timer` 本地模拟进度时钟以避免高频 IPC 轮询，而本套件以 `ting-play --watch -j` 离散事件流驱动客户端单调时钟外推，构成了更干净的解耦方案。

**实测经验映射（本仓 2026-09-02 落地网易云引擎对）**：
国内新生代要么全套自己重写（CNMPlayer 的 `ncm-api-rs` 要维护 50+ 接口与 RSA 加密），要么完全外包给外部工具。
本套件实测证明：**搜索端只需单点突破** —— 用系统自带 `openssl` 跑两道 AES-128-CBC 就能向 weapi 取回搜索信封，
且搜索接口的 `fee` 字段直接携带真实的 `access`（全曲/30秒试听/付费专辑），无需像其他客户端那样二次查详情；
播放直链则交给 `yt-dlp` 的 eapi 统一抽取，保持了 bash 3.2 下零 Node 依赖、零守护进程、引擎局部隔离的优雅。

### 2.3 三条结论

1. **这个领域按音源分层，不按 UI 分层。** "不做通用播放器"的理由是**自用不需要**：
   工作时放点音乐用不上参数 EQ、频谱、图形协议封面、Lua 插件 ABI —— 而那一层正是在这些东西上
   卷（每月都有新项目以 ~500★/月 起量，`cliamp` 六个月 3403★）。照着通用播放器卷属于给自己派活，
   核心差异化在 CLI 契约与生命周期控制，不在通用客户端功能。
   决定见 `ARCHITECTURE.md`「定位与设计目标」。

2. **YouTube 这一格的人机面已被占，机器面仍空。** `ytfzf` 死在 2024-09，
   但**位置被 `Benexl/yt-x` 接管了**（~1650★，POSIX sh + fzf + jq + curl + mpv，
   近半年 100+ commit）—— 技术栈与本项目高度重合，功能面走得更远（行内过滤、搜索历史与 bang
   召回、个人 feeds、分页、下载、扩展系统）。**但它没有机读契约**：本地 JSON 是它自己的状态文件，
   不是对外承诺。它的**扩展系统**（source shell 脚本、覆盖函数）是本仓**明确不该抄**的东西 ——
   那是把 shell 的动态作用域当插件 ABI，与"契约即安全边界"的取向正相反。

3. **agent 可驱动的先例分四档 —— 这一格不是空白。**（**别再写"没有一个能被 agent 驱动"**：
   实测不成立，写进定位文案就是错的。）MPD 全家
   （`ncmpcpp`/`rmpc`）**天生**可驱动，协议二十余年稳定且有 `mpc`；`cmus-remote -Q` 1998 年就在出
   行式 key/value；`ncspot` **推 JSON 到 unix socket**（与本仓的 mpv IPC 客户端路径几乎同构，
   连"stock netcat 不肯关连接"那个坑都踩过并写进了文档）；`spotify-player` 是
   **daemon + CLI 动词 + 给 jq 的 JSON**，且**早于本项目**；`termusic` 拆 server/client 走 gRPC；
   `spotatui`（~1300★）与 `spotuify` 已经在出 **MCP 面**；`mpv-mcp-server` 干脆从另一侧直接做了
   mpv + yt-dlp 的 agent 面。**这条结论在 §7 有 2026 年的续篇**：agent 面已经不是加分项，
   而且"可被 agent 驱动"与"播放脱离 UI 存活"被证明是同一个架构需求的两面。

### 2.4 站得住的收窄版命题

**这一节回答的是"我为什么没直接用现成的"** —— 不是市场空位分析。经得起查的只有这一句：

> 在 **YouTube 这一格**里，仍然没有一个项目提供**单行 JSON envelope + 退出码分类 + 脱离终端的
> 播放器生命周期（launch → status → stop，幂等，歧义即 4）**这一整套契约。

`yt-x` 无机读契约；`youtui` 的独立 API crate 是给 Rust 调用方的库，不是 CLI 契约；其余是纯 TUI。
所以这不是"抢到了一个空位"，是**我要的那件事没有现成的**：让 agent 和我用同一套命令放同一首歌。
**别拿 star 数跟这些项目比 —— 不在同一件事上。**

§7 从另一个方向印证了这句话：2026 年出 agent 面的项目里，**只有 spotuify 一个把播放生命周期
也交了出去**，而它靠的是守护进程形态（§4.3）；其余（`Meting-Agent`、bilibili-mcp 一族）**只做查**。

顺带记一笔（真做 MCP 时要用，即 `ROADMAP.md` Go 重写 NO 的重开条件）：字幕这一格在 MCP 生态里
已有 `kevinwatt/yt-dlp-mcp`（273★），与 `ting-play --transcript` 正面重叠，B 站侧的 MCP
服务器与 `ting-play --info` 同理 —— 真做时要说清为什么用本仓的；`spotatui` 的 MCP 面
给出了可抄的工程细节 —— 默认关闭 + 只绑 loopback + token、双时代协议兼容、stdout 只跑协议、
应用内与 MCP 复用同一张 tool table。

---

## 3. 坐标系：一个播放器的设计其实是五个独立的选择

看了十几个项目之后，最有用的发现不是某个项目怎么做，而是**这些选择彼此正交**。
把它们拆开，任何一个播放器都能一句话定位，比较也才有意义。

| 轴 | 问题 | 取值 |
|---|---|---|
| **A 解码在哪** | 谁把字节变成声音 | 进程内库 / 外部播放器进程 / 远端设备 |
| **B 播放进程的寿命** | 关掉 UI，歌还响吗 | 随 UI 生死 / 独立守护进程 / 远端 |
| **C 站点知识在哪** | 谁知道怎么从一个网页拿到流 | 内置逆向 / 外包给 yt-dlp / 外挂脚本源 / 远端 API 网关 |
| **D 控制面** | 谁能操作它 | 键盘 / 本地 socket 协议 / 网络协议 / 系统媒体键 / **agent** |
| **E 提取几次** | 一次拿到 URL 就够了吗 | 一次提取（URL 有时效）/ 交给播放器反复提取 |

轴 A 与轴 B 常被当成一回事，其实不是：**外部播放器进程 ≠ 独立寿命**
（bilibili-tui 用外部 mpv，但 mpv 是它的子进程），
**进程内解码也能有独立寿命**（spotifyd 自己就是守护进程）。
真正决定"关掉 UI 歌还响不响"的是轴 B，不是轴 A。

---

## 4. 五种播放架构

### 4.1 进程内解码

**机制**：应用自己链接一个解码库，自己开音频设备。Rust 侧是 `symphonia`（解容器/解码）
+ `rodio`（输出），Go 侧是 `beep`。

**谁在用**：
- **termusic** —— 默认后端叫 `rusty`，就是 Symphonia；另外两个后端是 GStreamer 与 MPV，
  **后端可在运行时切换，不是编译期决定**（[termusic README](https://github.com/tramhao/termusic)、[DeepWiki](https://deepwiki.com/tramhao/termusic)）。
- **go-musicfox** —— 默认 `beep`，另有 `mpd`、`mpv`、`dlna` 三个可选引擎 [需实测：配置键名与各引擎的实际能力差异]。
- **spotify_player** —— `ratatui` + `rspotify` + `librespot`，默认音频后端 `rodio`（[crates.io](https://crates.io/crates/spotify_player)）。
- **cliamp** —— 本地文件走**纯 Go 解码，不需要任何外部工具**；只有网络源才拉 yt-dlp
  （[cliamp README](https://github.com/bjarneo/cliamp)）。

**买到什么**：零外部依赖（至少对本地文件是）；对**采样率、缓冲、无缝衔接**有完全控制权 ——
这是 gapless 与频谱可视化必须在进程内才好做的原因（spotify_player 的实时 FFT 频谱正是"本地
用 librespot 播时才有，投到外部 Connect 设备就隐藏"）。

**付什么账**：**格式矩阵要自己填**。termusic 的文档要为 Symphonia / MPV / GStreamer 三个后端
分别列出 ADTS / AIFF / FLAC / M4a / MP3 / Opus / Ogg / WAV / WebM / MKV 的支持情况 —— 这张表
本身就是这条路的成本。seek 精度、容器级定位也得自己负责（有实现明确写"用 Symphonia 做
容器级精确 seek，rodio 作为 fallback 解码器"）。

### 4.2 外部播放器 + IPC（mpv 模型）

**机制**：`mpv --input-ipc-server=<path>` 打开一条 **行式 JSON** 通道；每条消息是一个
JSON 对象加换行，编码是 RFC-8259 的 UTF-8。Linux/macOS 是 unix socket，**Windows 是命名管道**
（同一个选项，mpv 自己切换）；另有 `--input-ipc-client` 直接收文件描述符。
mpv 手册对它的定位写得很直白：**"不是安全的网络协议 —— 没有认证、没有加密，命令本身也不安全"**
（[mpv ipc.rst](https://github.com/mpv-player/mpv/blob/master/DOCS/man/ipc.rst)）。

**谁在用**：
- **bilibili-tui**（Rust + Ratatui 0.30 + Tokio）：扫码登录拿 `sessdata` / `bili_jct` 存在
  `~/.config/bilibili-tui/`，**把 cookie 同步给 mpv**，由 mpv + yt-dlp 出流
  （[README](https://github.com/MareDevi/bilibili-tui)）。
- **go-musicfox**：5.0 起为 Windows 的 MPV 播放器加了**命名管道通信** —— 源码核查
  （`internal/player/mpv_player.go` 与 `mpv_player_ipc.go`）证实其完整落地机制：
  1. 后台以 `mpv --idle` 拉起单实例守护进程，跨平台分流：Unix/macOS/Termux 走 unix socket
     （`/tmp/mpvsocket`，Go 语言 `net.DialUnix`），Windows 走命名管道（`\\.\pipe\mpvsocket`）；
  2. **双连接分离**：命令连接 `getIPCConn()` 缓存复用，发送 `loadfile` / 属性控制；事件连接 `watch()`
     独立长连消费 JSON 事件流；
  3. **本地计时避免轮询**：播放进度由本地 `timex.Timer` 自行追踪推算，不向 mpv IPC 轮询 `time-pos`，
     极大减少了与 mpv 之间的 IPC 通信开销。
- **termusic** 的 MPV 后端、以及本仓。
- **对比：私有模拟 vs 公开事件流（2026-09-26 深度实测）**：
  `go-musicfox` 将 mpv 包裹在内部作为私有子进程，其 `timex.Timer` 模拟的播放时钟完全闭锁在 Go 运行时内存中，外部 CLI 或 Agent 无法感知当前播放进度；
  而本套件在 `ting-play --watch -j` 中确立了**公开的 NDJSON 离散事件流契约**：播放器后台 detached 运行，通过单连接监听 mpv 属性跳变，仅在状态变更与整秒心跳时向 stdout 推送单行完整状态 JSON，彻底消除每秒 ~19 次的 `time-pos` 轮询噪音。调用方（无论是 Go TUI 还是 Coding Agent）凭借事件流中的基准时间戳和本地单调时钟（`CLOCK_MONOTONIC`）进行无损亚秒级平滑外推，既保护了 IPC 管道与 Agent token 预算，又实现了进程间解耦。

**这条路上真正要做的四个决定**（mpv 侧的选项名，本仓的用法见 `docs/ARCH-player.md`「播放子系统」）：

| 决定 | mpv 侧 | 后果 |
|---|---|---|
| 提取归谁 | 默认 `ytdl_hook` 会拦下 URL 去调 yt-dlp；`--no-ytdl` 关掉它 | 关掉，就等于**声明"提取由调用方做一次"**（轴 E） |
| 请求头怎么带 | `--http-header-fields`（是个 LIST，多头要用 `-append` 或数组语义） | mpv 的 issue #9978 / #6492 都是这个坑：ytdl_hook 分离音视频流时，头能不能正确传下去 |
| 进程活不活 | `--idle`（没片可播也不退）/ `--keep-open`（播完最后一个不退，改成暂停） | 决定"播完之后这个进程还在不在"，直接关系轴 B |
| 无缝 | `--gapless-audio`（按第一个文件的参数开音频设备并保持打开）/ `--prefetch-playlist=yes`（本地或网络都能无缝） | 见 §5.1 |

**买到什么**：编解码、容器、seek、硬件输出、HLS/DASH 全部外包给一个被几百万人测过的
实现；**跨平台的音频输出问题不归你管**。而且这是唯一一条**天然支持轴 B 独立寿命**的路 ——
mpv 是个独立进程，谁启动它跟它活多久无关。

**付什么账**：多一个运行时依赖；IPC 的传输在平台间不同（unix socket vs 命名管道），
**没有认证**意味着 socket 的文件权限就是全部的安全边界；以及最隐蔽的一条 —— mpv 的
`--input-ipc-server` **是每进程的**，多个并发播放器就是多个 socket，不是一个总线。

### 4.3 守护进程 + 协议（客户端只是视图）

**机制**：把播放、队列、状态放进一个**长期存活的服务端**，UI 只是它的客户端，通过某种协议
连上去。这是终端音乐领域最老、也最近重新流行起来的形态。

**四个样本，四种协议**：

| 项目 | 服务端 | 协议 | 客户端 |
|---|---|---|---|
| **MPD** | `mpd` 守护进程，管播放 + 播放列表 + 音乐库 | 自定义文本协议，默认 `127.0.0.1:6600` 或 unix socket | ncmpcpp（C++）、rmpc（Rust）等几十个 |
| **termusic** | `termusic-server` | **gRPC**（20 RPC，跨 4 个 Service） | `termusic` TUI，与服务端**是两个进程** |
| **spotuify** | 一个守护进程，内嵌 librespot | **一条 unix socket** | TUI / CLI / **MCP** / macOS 菜单栏，共四个 |
| **spotifyd** | 守护进程，实现 Spotify Connect | Spotify Connect（网络协议） | 任意官方/第三方 Spotify 客户端 |

**这条路 2026 年的新意在 rmpc 和 spotuify 上**：
- **rmpc** 本身是纯客户端 —— "它不做音频输出，只向 MPD 发命令"；但它另配了一个 **`rmpcd`
  后台守护进程，用来把功能延伸到 TUI 生命周期之外**，带 Lua 插件系统（可脚本化 song-change
  之类的事件）和 D-Bus MPRIS 接口（[rmpc](https://github.com/mierak/rmpc)）。
  **注意这里出现了两层守护进程**：MPD 负责播，rmpcd 负责"UI 关了之后还要发生的事"。
- **spotuify**（Rust，2026）把这个形态推到了极致，README 的原话是
  **"一个守护进程，四个客户端"**：守护进程内嵌 librespot、自己**就是**那个 Spotify Connect
  设备（不是遥控别的播放器），带 SQLite 元数据缓存和 Tantivy 搜索索引；CLI 有 **58 条命令、
  与 TUI 完全对等**，输出可选 table / JSON / JSONL / CSV / 纯 ID，专门为管道设计；
  另有 **41 个工具的 MCP 面**，并且明确写着 **"agent 跑的是和你一样的命令"**，
  `ops undo` 是它的安全网（[spotuify](https://github.com/planetaryescape/spotuify)）。

**买到什么**：轴 B 彻底解决 —— 关掉 UI 歌照响，另一个 shell 里发的命令立刻生效；
**并且控制面天然多份**（TUI / CLI / MCP / 媒体键都是同一个服务端的客户端）。

**付什么账**：多了一个要管的长驻进程 —— 启动、崩溃、版本对齐、状态目录、并发客户端。
协议一旦公开就是冻结面（MPD 的协议活了二十年，代价是它至今还是那个形状）。
termusic 为此付的是 gRPC 全套依赖；spotuify 付的是一个 SQLite + Tantivy 的运行时。

### 4.4 协议级客户端（自己就是那台设备）

**机制**：不调用任何播放器，也不解析网页 —— **直接实现服务方的私有协议**，把自己注册成一台
播放设备。`librespot` 是这条路的公共基座：它是 Spotify 官方已弃用的闭源 libspotify 的开源替代，
**既作为库出货，也作为 headless 二进制出货**（后者会在局域网里注册成一个 Spotify Connect 接收端），
ncspot / spotifyd / Snapcast 都嵌它（[librespot](https://github.com/librespot-org/librespot)）。
Go 侧的对应物 `go-librespot` 被 cliamp 用来接 Spotify。

**买到什么**：官方级的能力（登录、推荐、跨设备接管），以及**别的客户端可以来控制你** ——
用手机上的官方 App 控制终端里的播放器，这是其他任何架构都做不到的。

**付什么账**：一个源一套协议，**完全不可复用**。它是"深"的极致，也是"窄"的极致。
这也是国内平台上几乎没有对应物的原因：没有 Connect 这类开放的设备协议可实现，
只能退回 §6 的音源路线。

### 4.5 投放到远端（DLNA / UPnP）

**机制**：本机既不解码也不开音频设备，只把一个 URL 和控制指令交给局域网里的电视/音箱/主机。
go-musicfox 5.0 加了 **DLNA/UPnP 播放引擎**，跟 `beep`/`mpd`/`mpv` 并列为一个可选 engine
（[CHANGELOG](https://github.com/go-musicfox/go-musicfox/blob/master/CHANGELOG.md)）。

**值得记的一点**：它和 §4.4 是同一个思想的两个方向 —— 都是"播放发生在别处"。
把 DLNA 做成**与本地引擎并列的一个 engine**，而不是一个独立功能，是 go-musicfox 这次
架构上最干净的一笔：**轴 A 的第三个取值被收进了同一个抽象里。**

---

## 5. 横切问题（不属于任何一种架构，但每种都要回答）

### 5.1 gapless 无缝

- **mpv 路线**：`--gapless-audio` 的机制是 **按第一个文件的参数打开音频设备，然后一直不关**；
  跨文件无缝还需要 `--prefetch-playlist=yes`，它对**本地文件系统和网络流都有效**。
- **进程内路线**：termusic 为 symphonia / mpv / gstreamer **三个后端都实现了 gapless**，
  `Ctrl+g` 切换，**默认开启**。
- **代价**：无缝的前提是"下一首在当前这首结束前就已经就绪"。这与 §5.3 的 URL 时效直接冲突 ——
  预取得太早，URL 可能在真正播到时已经过期。

### 5.2 网络流的缓冲：管道直喂的隐藏代价

dennislan 的 Music Player TUI 是这条路最纯粹的样本：
**`yt-dlp stdout → Arc<Mutex> 共享缓冲 → rodio 解码 → 音频设备`**，
后台播放线程与 TUI 之间用 mpsc 通道通信，**零落盘**（[项目页](https://dennislan.github.io/music-player/)）。

**买到**：边下边播、不写磁盘、不需要 mpv。
**付出**：管道**不可 seek**。该项目宣传的 seek 是 ±10 秒，且有"跨会话记住位置"这样的功能 ——
在一条只能前进的管道上，这两件事的实现代价远高于把 URL 交给 mpv 让它自己发 Range 请求。
**判据**：如果产品要"拖动进度条"，管道直喂就是错的路；如果只要"顺序听完"，它是最省依赖的路。

### 5.3 URL 时效：一次提取的隐含期限（2026-09-03 实测升格）

这是"一次提取"（轴 E）这条路唯一的真实代价，也是最容易被忽略的。
2026-09-03 轮次对三大主流音源的直链参数和有效生命期做了逐项解析与实测：

- **YouTube（实测）**：签名过的 `/videoplayback` 链接携带 `expire=` Unix 时间戳，
  实测与签发时间相差整整 **6 小时（21600 秒）**。时效较为充裕。
- **Bilibili（实测）**：流媒体分段 URL 携带 `deadline=` 签名字段，
  实测严格为签发后 **2 小时（7200 秒）**。
  - **412 与风控持续收紧**：yt-dlp PR #16889（修复 issue #14830）证实，B 站针对未携带
    或携带过期 `buvid3`/`buvid4` 指纹 Cookie 的客户端直接返回 HTTP 412 或 `v_voucher` 人机验证，
    需要从 `api.bilibili.com/x/frontend/finger/spi` 获取合法指纹；而 issue #17605（2026-09-02）
    进一步显示，View 接口在无有效浏览器上下文或 IP 被标黑时会返回 `code: -412 (request was banned)`。
    这证明 412 是**指纹与 WAF 风控拦截**，不是单纯的 URL 过期。
- **网易云音乐（实测）**：CDN 路径携带的绝对时间戳 `/<YYYYMMDDHHMMSS>/`（例如 `20260904040619`），
  实测有效窗口仅为 **20–25 分钟（1200–1500 秒）**。**这是目前已知主流音源中时效最短的一家**。
  此外，海外 IP 直连网易云音乐时有严格地域屏蔽（yt-dlp 会尝试用虚构的国内 IP 设置 `X-Real-IP` 头绕过，对应 `--xff` 选项）。

**设计含义**：
1. **绝不能在播放列表中持久化 URL**：存 URL 必烂，存**调用（engine + handle）**才是对的；
2. **队列预取窗口必须极小，且必须具备即播即解（JIT）能力**：网易云 20-25 分钟的时效意味着，
   如果像某些本地播放器那样一口气预取 10 首歌，听到后半截时 URL 必然在播放器内部 403 挂掉；
3. **播放器必须能在遇到 403/过期时通知或触发重新解析**，或者遵循本套件的原则 ——
   让队列消费在即将播放的前一刻才调用 `<engine>-resolve`。

### 5.4 系统媒体集成：三个平台三套 API

想接系统媒体键 / 锁屏卡片 / 蓝牙耳机按钮，就得分别对接：

| 平台 | API |
|---|---|
| Linux | **MPRIS**（D-Bus，freedesktop 规范 v2.2）—— 连 session bus、占用 MPRIS bus name、把 D-Bus 调用翻成播放命令、再把状态作为 MPRIS 属性发布出去 |
| macOS | **MPNowPlayingInfoCenter + MPRemoteCommandCenter**（需要走 Objective-C 运行时） |
| Windows | **SMTC**（System Media Transport Controls）—— 媒体键、音量浮出控件、任务栏 / Win+G 里的那个卡片 |

cliamp 把这三套收进一个叫 `mediactl` 的服务里，并单独写了篇文档
（[mediactl.md](https://github.com/bjarneo/cliamp/blob/main/docs/mediactl.md)）；
SPlayer 在 Linux 用 `mpris-server` crate、macOS 走 Objective-C 运行时、Windows 接 SMTC，
是同一套做法。go-musicfox 也三个都做了（README 明确写 MPRIS / Now Playing / 系统媒体控制）。

**值得注意的是它与轴 B 的耦合**：媒体键要能控制的是**正在播的那个进程**。
UI 与播放分离的架构里，MPRIS 该由谁来发布，是个真问题 —— rmpc 的答案是让 `rmpcd`
（而不是 TUI）来持有 D-Bus 接口。

### 5.5 一个反复出现的模式：把"源"做成协议或脚本，而不是模块

三个不同世代的样本，同一个答案：

- **FeelUOwn**（Python）：`fuo://{provider}/{type}/{id}`，例如
  `fuo://netease/artists/46490`。**源是 URI scheme 的一段**，provider 以插件形式存在
  （netease / qqmusic / local / xiami…）（[fuo 协议](https://feeluown.readthedocs.io/en/latest/protocol.html)）。
- **lx-music / MusicFree**：源是**外挂的 JS 脚本**，用户在设置里导入。MusicFree **自身零内置源**，
  搜索、歌单导入、歌词全部由插件提供。这已经是国内音源分发的事实标准
  （[LXMusic vs MusicFree](https://zhuanlan.zhihu.com/p/718757633)）。
- **go-music-dl**：平台逻辑全部收进一个独立库 **`music-lib`**，主程序只是它的三个壳
  （Web / TUI / 桌面）（[README](https://github.com/guohuiyuan/go-music-dl)）。

**共同点**：源的数量会增长且不可控（平台会挂、会封、会改签名），所以**源必须能在不改主程序的
前提下增删**。分歧只在边界画在哪：URI 协议（FeelUOwn）、脚本沙箱（lx-music）、
库（go-music-dl）、**还是底层独立脚本 + 统一动词转发**（本仓的演进：早期是一对平级命令，
现演进为 `ting-engine-<site>` 单文件闭环 + `ting-play` 统一代理动词转发，既消除跨文件重复，
又保住了单文件原地热修与零外部插件沙箱的边界）。

### 5.6 歌词呈现的终端范式（全屏滚动 vs 分栏窗格 vs 单行内联窥探）

看遍主流终端播放器对歌词（Lyrics）的处理，业界基本呈现出三种主流范式与一种极端特例，但它们在终端物理预算上均存在不可调和的矛盾：

| 范式 | 代表项目 | 实现机制 | 优势 | 终端物理代价 / 劣势 |
|---|---|---|---|---|
| **全屏独占模式** | `go-musicfox`（`page_lyrics`）、`spotuify`、`musicbox` | 按键（通常是 `l`）切换全屏，主区域绘制 10~20 行滚动歌词，居中高亮当前句，伴随双语翻译 | 沉浸感强，可看上下文与翻译，布局宽松 | **彻底打断浏览心流**。离开列表与队列上下文；多引入一套模态（Modal）与退出按键；对“工作时放点背景音”心智过重 |
| **多栏窗格模式 (Panes)** | `rmpc`（Ratatui Panes 布局，独立的 `lyrics` 窗格）、`termusic` | 终端水平或垂直切分出专属窗格，列表与歌词同时并存 | 无需切模态，列表与歌词同屏可查 | **破坏行与列预算**。在 80×24 标准终端下，垂直分栏吃掉 25~35 列，左侧 CJK 双字宽曲名严重腰斩；水平分栏吃掉 8~12 行，结果行数骤降 50% 以上；且违背「双栏中轴竖线 NO」与「卡片化 NO」 |
| **系统桌面代理模式** | `go-musicfox`（`lyric-for-musicfox`）、MPRIS 外部悬浮窗 | 脱离终端，通过 D-Bus、桌面通知或菜单栏展示浮动单行歌词 | 完全不占终端屏幕空间 | **脱离终端原生**。引入 GUI/系统级守护依赖，跨平台（macOS/Linux/Windows）极其碎片化 |
| **单行内联窥探 (Lyric Peeking)** | 本仓（`PLAN-lyric-peeking.md`） | **利用焦点行下方的折叠 details 空间**：光标移至在播行时，将原有 Description 等额**置换**为当前单句歌词；移开即隐 | **行预算严格守恒（`Δrows = 0`）**。零模态切换，不破坏单视图原地重绘，看过去即在；CJK 字符单元格精准截断不折行 | 仅展示当前一句（无前后句预览）；依赖亚秒级进度外推对齐（1 Hz 时钟无法胜任） |

**调研结论与设计映射**：
终端屏幕高度是实打实量出来的。在工作背景音乐的场景下，让用户为了偶尔瞟一眼歌词而切走整个工作列表（全屏模式），或为了歌词常驻而将歌曲列表挤成一截残肢（分栏模式），都是得不偿失的。
**单行内联窥探（Lyric Peeking）是唯一在守住“单视图原地重绘”、“免模态切换”与“行预算恒定”三条底线的前提下，为终端用户提供歌词心流的解法**。而它的实现前提 —— 亚秒级进度游标与平滑微动效，恰恰是 Go TUI（`ARCH-tui.md`）能够低成本提供的资产。

### 5.7 付费权限与可用性状态的排版呈现（`access` / VIP / 试听）

在线音频平台普遍存在细粒度的版权与付费状态区分（如网易云的 `fee` 衍生出的 `full` 全曲 / `preview` 30 秒试听 / `paywalled` VIP 付费专辑）。终端播放器在排版上面临两难选择：

1. **行内前缀/后缀硬编码标签（`[VIP]`、`[试听]`）**：
   - **业界现状**：`go-musicfox`、`Listen 1` 等普遍在列表行的曲名前后追加徽章。
   - **终端代价**：一枚 `[VIP]` 标签占用 5 个半角显示宽度（2.5 个汉字宽度）。在 62 列的紧凑终端下，原本能显示 18 个汉字的歌名被压缩到 15 个字以内，大幅增加了省略号截断率。
   - **信息熵陷阱**：在默认搜索与歌单过滤已经剔除不可播曲目（例如网易云引擎默认 `select(.access == "full")`）时，列表里每一行都会贴上一枚毫无信息增量的 `full` 常量徽章，沦为纯粹的视觉噪音。
2. **列表行全局置灰 / 降噪**：
   - **业界现状**：`go-musicfox` 对无版权/需购买专辑的曲目进行色彩调暗。
   - **跨引擎失效**：置灰仅适用于单一国内源。在多源架构下，YouTube 和 B 站没有可对应的细粒度 `fee` 状态，强行跨引擎置灰会导致状态陈述失真。
3. **焦点行 Details Metadata 嵌入**：
   - **最优实践**：列表行只展现纯粹的曲名与基础排版，保持最大标题宽度与视觉清爽；而将 `access` 徽章置于焦点行下方的 details 元数据行（与时长、播放量、发布者并列）。
   - **收益**：既在用户真正关注（光标聚焦）该曲目准备按 Enter 播放时提供了决策预判，又完全不占用单行曲名宽度预算，杜绝了每行重复的常量噪点。

---

## 6. 音源层：拿到一个可播 URL 的四条路（国内侧）

播放架构解决"怎么播"，这一节是"播什么"。国内平台没有 §2.4 那样的开放设备协议，
所以只剩四条路，且**每条都在 2026 年出现了分化或退化**：

| 路线 | 机制 | 2026-09 的状态 |
|---|---|---|
| **官方 API 逆向** | 直接实现 eapi/weapi 等加密调用 | Binaryify 的 `NeteaseCloudMusicApi` **GitHub 仓已停更**（2024-02 止）。接力维护的主力是 `NeteaseCloudMusicApiEnhanced/api-enhanced`（1719★，2026-08 活跃），跟进全景声音质修复；另一支走向**语言原生嵌入**，如 `CNMPlayer` 的 `ncm-api-rs`（Rust 原生实现 weapi/eapi/linuxapi）；本套件实测证明：搜索只需用系统 `openssl` 跑 weapi 即可打通，且 `fee` 字段直接对应 `access`（全曲/30秒/付费专辑） |
| **多源回退** | 一首歌不可用就去别家找替身（UnblockNeteaseMusic 模型：酷狗/酷我/波点/咪咕/JOOX/YouTube/Bilibili） | **严重退化**：`UnblockNeteaseMusic/server`（7824★）代码事实停更（近月全为 Dependabot 自动提交）；官方网易云 3.1.38 更新直接致解灰失效（[issue #1753](https://github.com/UnblockNeteaseMusic/server/issues/1753)）；酷我返回假 VIP 音频（[issue #1299](https://github.com/UnblockNeteaseMusic/server/issues/1299)）；QQ、咪咕对海外 IP 封锁；维护各平台替身成本不可持续 |
| **yt-dlp 统一提取** | 站点知识外包给上游 | B 站侧面临指纹 Cookie 缺失致 412 与 WAF 风控（§5.3）；网易云海外 IP 会被严格地域限制（yt-dlp 尝试用虚构国内 IP 的 `X-Forwarded-For` 穿透）；但针对单曲解析，yt-dlp 的 eapi 仍能稳定拿到可播的 30 秒试听（weapi 则直接返回空） |
| **远端聚合网关** | 自己不碰站点，调一个统一 API | `Meting-Agent`（音乐虾，105★）聚合网易云/QQ/酷狗/酷我，统一接口 search / song / album / artist / playlist / **url** / lyric / pic，**同时出 MCP 和 Skill 两种形态**（[GitHub](https://github.com/ELDment/Meting-Agent)） |

**新出现的源与聚合库**（值得记，因为它们已进入聚合器的清单）：
- **汽水音乐**（字节）：`guohuiyuan/go-music-dl`（4138★，2026-08-30 发布 v1.1.0）通过底层 `music-lib` 实现了汽水音乐的搜索与音频解密（SEO 路径可跳过解密）；
- **小宇宙**（播客，cliamp 支持）、5sing、千千音乐、JOOX。

### 6.1 下一个媒体源候选与准入筛查（2026-09-12 实测更新）

在既有三引擎（`yt` / `bili` / `ne`）体系之外，套件对扩充第四音源设定了五条不可逾越的**第一性原理准入判据**：
1. **双半边闭环契约**：必须同时具备搜索前半边（`ting-play --search`）与解析后半边（`ting-engine-<site> --stream`），缺少免鉴权公开搜索直接触发否决（对齐「喜马拉雅 NO」）；
2. **零新增全局依赖**：锁定 5 大外部工具（`yt-dlp` / `jq` / `mpv` / `nc -U` / `curl`，至多引擎局部调 `openssl`）；严禁引入 Node.js/Python 运行时、专有守护进程或二进制 SDK；
3. **Pure Bash 3.2 运行基准**：macOS 原生兼容，无构建与外部包袱；
4. **mpv 原生直链可播**：后半边产出的必须是 `mpv --no-ytdl` 直接支持的开放媒体直链，无私有二次解密或 DRM 阻碍；
5. **免鉴权低风控高可用**：支持匿名开箱可用，无高频滑动人机验证、封号风控或易失效的 JSVMP 逆向包袱。

**候选媒体源实测比对矩阵（2026-09-12 网调与本地实调；SoundCloud / 播客于同日二次复核）：**

| 候选平台 | 搜索机制（前半边） | 直链与播放（后半边） | 外部依赖 | 免登录可用性 / 风控现状 | 第一性原理判定 |
|---|---|---|---|---|---|
| **SoundCloud (`sc`)** | `yt-dlp` 原生 `scsearch` 可返回契约所需搜索元数据 | `yt-dlp` 提取标准 HLS `m3u8`，mpv 可直接解码；Set 的 flat 条目缺时长 | **零新增**（`yt-dlp` + `jq`） | 匿名搜索与样本解析可用；官方 extractor 明示约 600 请求/10 分钟的 API 限流 | **⚠️ 条件首选：单曲双半边成立，`--items` 与请求预算未闭环** |
| **Apple Podcasts / RSS (`pod`)** | iTunes Search API 可匿名搜索单集 | Apple 页面可由 yt-dlp 解出 MP3；直接 MP3 的托管 host 随节目变化 | **零新增路径存在**（`curl` + `jq` + 既有 `yt-dlp`） | 样本匿名可用；未测得足以支撑“永久”“零风控”的长期证据 | **⚠️ 品类互补，但 host 路由、Lookup 截断与 RSS 身份未决** |
| **QQ 音乐 (TME)** | Web 接口 `musicu.fcg` | `GetVkeyServer / CgiGetVkey`，VIP 曲目锁 30s | 需 Node.js 跑 JSVMP 逆向签名 | **极低**（未带 sign/未登录直接返回 `code: 2001` 弹登录） | **❌ 否决（违反零依赖、高维护负债）** |
| **小宇宙播客 (Xiaoyuzhou)** | **无公开 Web 搜索接口**，仅 App API | 需传设备指纹与 App 请求头 | 需逆向 Token 管理 | **极低**（依赖动态 `x-jike-access-token`，第三方调用封号） | **❌ 否决（触碰「喜马拉雅 NO」红线）** |
| **汽水音乐 (Luna / 字节)** | PC 接口需设备指纹与专有签名 Header | 音频流经 `AES-CTR` 加密，带 `spade_a` | 需本地解密中间层 | **差**（无直链，需先下流再解密才能播） | **❌ 否决（违反 mpv 纯直链架构）** |
| **Bandcamp** | **不支持公开搜索**（yt-dlp 无 `bcsearch`） | 单曲需购买，仅部分 128k 样片 | 需额外逆向搜索 | **中**（多为付费专辑与试听碎片） | **❌ 否决（前半边残缺）** |
| **Spotify** | 官方 Web API 需 OAuth 注册 | **受 Widevine DRM 保护，无直链** | 必须内嵌 Rust `librespot` | **零**（强制 Premium 账号与外部守护进程） | **❌ 否决（违反轻量/零依赖/无账号原则）** |

**实测要点与推荐结论：**

1. **条件首选：SoundCloud (`sc`)**
   - **已经闭环的半边**：`yt-dlp 2026.08.19` 的 `scsearch` 返回 `id/title/duration/webpage_url/view_count/thumbnails`；数字 Track ID 与网页 URL 都能解出签名 HLS，`mpv 0.41.0 --no-ytdl --ao=null --frames=1` 实测解码为 AAC 44100 Hz 双声道。
   - **尚未闭环的容器**：`yt-dlp --flat-playlist --dump-single-json https://soundcloud.com/forss/sets/ecclesia` 的条目有 `id/url/title`，却没有 `duration` 或 `live_status`，不能直接投影成当前 `--items` 条目。非 flat 是否会把一次列表放大为逐曲请求、并撞上 extractor 明示的约 600 请求/10 分钟限流，仍需测量。
   - **长音频不是差异点**：`scsearch3:lofi hip hop` 的样本含 7200.255 秒与 3704.629 秒 DJ Mix；SoundCloud 也需要面对长音频体验，只是它不阻断现有显式 seek 与 `--start`。
2. **待裁决候选：开放播客生态 (`pod`)**
   - **单集路径成立**：iTunes Search `entity=podcastEpisode` 返回 Apple 单集 ID、页面、时长、简介与 MP3；Apple 单集页可由 yt-dlp 解出直接 MP3。
   - **节目容器未闭环**：故事 FM Lookup 声明 `trackCount:991`，`limit=200` 只给最近 200 集；改变 offset 仍返回同一批。RSS 经 yt-dlp 可见全部 991 集与时长，但 flat 条目的稳定 ID、规范网页 URL 和分批成本仍需设计。
   - **路由身份未闭环**：直接 MP3 的 host 由各节目托管商决定；若 resolver 接受任意 host，就违反本仓“一引擎一站点白名单”的现行边界。只接受 Apple 页面可以形成 Apple Podcasts 引擎，但不能据此称为开放播客引擎。
3. **国内源否决留档**：
   - QQ 音乐：`musicu.fcg` 全面启用 JSVMP 与动态 sign 校验，未登录返回 `{"code":2001, "feedbackURL":".../login"}`；
   - 小宇宙：Web 端完全无公开搜索接口，App 接口依赖 `x-jike-access-token`，被官方严密风控封号（见 `ultrazg/xyz` 警告）；
   - 汽水音乐：直链经过私有 `AES-CTR` 传输加密（带 `spade_a` 需本地二次解密），违背直接向 mpv 喂流的原则。

---

## 7. agent 面：2026 年它不再是加分项

四个独立样本展示了不同层次的探索：

- **Meting-Agent**：一个音源聚合器，**同时提供 MCP server 和 Claude Skill**；
- **bilibili-mcp-server** 一族：涵盖 `iseenope/bilibili-mcp-server`（31 工具）与
  `34892002/bilibili-mcp-js`、`huccihuang/bilibili-mcp-server`，覆盖视频/弹幕/字幕/评论；
- **网易云个人账号 MCP**：2026 年 8 月出现的 `Vael-KY/netease-music-mcp`（149★，18 工具，
  支持在真实网易云账号上翻歌单、建歌单、塞歌、读歌词、获取每日推荐与私人 FM）与
  `Cheiineeey/netease-music-mcp`（99★，3 工具：搜歌生成卡片、列出歌单、添加单曲）；
- **面向 Agent 的 CLI**：`public-clis/bilibili-cli`（1014★），专为 LLM/Agent 设计结构化输出
  （`ok/schema_version/data/error`，非 TTY 默认 YAML），支持视频/字幕/评论提取与 ASR 音频切片；
- **spotuify**：**41 个 MCP 工具**，与 58 条 CLI 命令、TUI 完全对等，明确写着
  "agent 跑的是和你一样的命令"。

**但要看清楚这些国内 Agent/MCP 工具的边界**：
Meting-Agent、bilibili-mcp、netease-music-mcp 以及 bilibili-cli 都**只做"查与账号数据管理"** ——
搜索、取详情、改歌单、读歌词、切片音频；**没有一个管宿主机上脱离终端的播放进程生命周期**。
唯一把"播"也交给 agent 的是 spotuify（靠守护进程），以及本仓（靠 detached mpv + 退出码分类契约）。

**这构成本轮调研最有价值的一条观察**：
**"可被 agent 调用" 与 "播放能脱离 UI 存活" 是同一个架构需求的两面。**
一个只在 TUI 前台活着的播放器，天然无法被 agent 驱动 —— 因为 agent 不持有终端。
国内出现的各种 MCP 工具把 API 查数包得很好，但一旦涉及"在用户的音箱里把歌放出来并随时能 pause/stop"，
这一层生命周期目前依然是空白。
spotuify 用守护进程 + unix socket 解决它；本仓用 detached 进程 + 每进程 socket + JSON 契约
解决它（`docs/ARCH-player.md`「detached 播放的生命周期」）。**这是两种不同的答案，但回答的是同一个问题。**

**2026-09-26 深度演进：从单向动词到双向状态流契约**：
在 `spotuify` 中，Agent 与 TUI 都是通过向常驻守护进程发送命令、或通过 MCP 消费工具；但它为此背负了庞大的守护进程状态管理（启动、崩溃恢复、端口管理）。
本套件在 `ting-play` 0.18.0 统一入口后，进一步将生命周期契约推进到**无常驻守护进程的离散事件流**：
`ting-play --watch -j` 将 detached 播放器的生命周期投影为单向 NDJSON 事件流。无论是 Coding Agent 还是 Go TUI，只需作为一个纯粹的客户端接入该流（`stdout`），即可获得开箱即用的离散状态更新与整秒基准心跳。Agent 不用写任何轮询脚本，也不会被每秒 20 次的 position 刷屏耗尽 context/tokens，彻底抹平了人（TUI）与机器（Agent）在状态消费上的代差。

---

## 8. 样本清单

按 §3 的五轴排开。**"最近活动"一栏已于 2026-09-03 实测更新。**

| 项目 | 语言 / UI | A 解码 | B 寿命 | C 站点知识 | D 控制面 | 最近活动 |
|---|---|---|---|---|---|---|
| **go-musicfox** | Go / Bubbletea | beep（默认）· mpd · mpv · **dlna** | 随 UI | 内置网易云 + UnblockNeteaseMusic | 键盘 · MPRIS · Now Playing · SMTC | 2538★，5.1.0（2026-08），活跃（2026-08-31） |
| **CNMPlayer** | Rust / Ratatui 0.30 | rodio + symphonia（进程内） | 随 UI | `ncm-api-rs` 原生加解密（weapi/eapi/linuxapi） | 键盘 · chafa 封面 | 154★，活跃（2026-08-30） |
| **bilibili-tui** | Rust / Ratatui 0.30 | 外部 mpv | 随 UI | yt-dlp + 自己的扫码登录 | 键盘 · 鼠标 | 218★，活跃（2026-08-30） |
| **bilibili-cli** | Python / Click | —（无播放，仅 ASR 音频切片） | — | 内置 B 站 API + 浏览器 Cookie | CLI（结构化 YAML/JSON） | 1031★，活跃（2026-03） |
| **termusic** | Rust / tui-realm | symphonia（默认）· mpv · gstreamer，**运行时可切** | **独立**（termusic-server） | 本地 + 播客 | 键盘 · **gRPC（20 RPC，4 Services）** · MPRIS | 2196★，活跃（2026-09-08） |
| **spotuify** | Rust / ratatui | 内嵌 librespot | **独立守护进程** | Spotify 协议 | 键盘 · **CLI 58 命令** · **MCP 41 工具** · 菜单栏 | 活跃（2026-08-31） |
| **spotify_player** | Rust / ratatui | rodio + librespot | 可选 daemon（`-d`） | Spotify 协议 | 键盘 · Connect | 7194★，活跃（2026-09-09） |
| **rmpc** | Rust / ratatui | **无**（MPD 播） | MPD 独立 + `rmpcd` | MPD 音乐库 | 键盘 · MPD 协议 · rmpcd 的 MPRIS + Lua 插件 | 3295★，活跃（2026-09-01） |
| **cliamp** | Go / Bubbletea + Beep | 纯 Go 解码（本地）· yt-dlp（网络）· go-librespot | 随 UI | **yt-dlp 覆盖 + Lua 插件** | 键盘 · `mediactl`（三平台媒体键） | 3990★，活跃（2026-09-02） |
| **go-music-dl** | Go / Bubbletea | —（以下载/服务为主） | — | **`music-lib`：10+ 国内平台（含汽水解密）** | CLI · TUI · Web · 桌面 | 4138★，1.1.0（2026-08-30），活跃 |
| **FeelUOwn** | Python / GUI+ | — | — | **`fuo://` 协议 + provider 插件** | GUI · fuo 协议 | 3933★，活跃（2026-09-01） |
| **BiliBiliMusicPlayer** | Python / TUI | 外部 mpv | 随 UI | B 站公开流 + curl_cffi | 键盘 | 5★，2026-01 |
| **Music Player TUI** | Rust / ratatui + rodio | 进程内，**yt-dlp 管道直喂** | 随 UI | yt-dlp 全覆盖 | 键盘 | 新 |

---

## 9. 问号的解决与新留档

### 9.1 2026-09-03 已实测关闭的问号

诚实核验每一项，不再留模糊状态：

1. **B 站 `playurl` 真实时效与 412 触发机制（已证）**：
   - 时效：分段流 URL 中携带的 `deadline` 签名字段经实测严格为签发后 **2 小时（7200 秒）**；
   - 412 诱因：由 yt-dlp PR #16889 与 issue #17605 证实，412 不是 URL 超时，
     而是客户端缺乏 `buvid3`/`buvid4` 浏览器指纹 Cookie（可通过 `api.bilibili.com/x/frontend/finger/spi` 获取），
     或触发了 B 站 WAF 风控策略直接阻断（`code: -412 (request was banned)`）。
2. **YouTube 6 小时时效（已证）**：
   - `/videoplayback` URL 中携带的 `expire` Unix 时间戳经实测严格与签发时间相差 **6 小时（21600 秒）**，
     由二手来源升格为一手实测。
3. **网易云音乐直链时效（已证）**：
   - 网易云 CDN 路径 `/<YYYYMMDDHHMMSS>/` 签发时间戳经实测有效窗口仅为 **20–25 分钟（1200–1500 秒）**，
     是已知主流源中时效最短的平台。
4. **go-musicfox 的 mpv 引擎通信机制（已证）**：
   - 查阅 `internal/player/mpv_player.go` 源码证实：以 `mpv --idle` 拉起单实例后台守护进程；
     Unix/macOS/Termux 走 `/tmp/mpvsocket`，Windows 走命名管道 `\\.\pipe\mpvsocket`；
     采用双连接（命令连接缓存复用、事件连接独立监听）；
     在进程内用 `timex.Timer` 本地推算播放进度，彻底避免对 mpv IPC 轮询 `time-pos`。
5. **termusic 的 gRPC 接口性质（已证）**：
   - 查阅 `lib/proto/` 源码证实：拥有规范的 proto3 接口定义，在 `player.proto` 中定义
     `service PlayerControl`（8 个 RPC，管播放状态、跳曲、进度读取、音量、速度调整、无缝开关与 seek 定位），
     在 `queue.proto` 中定义 `service QueueControl`（9 个 RPC，管播放列表增删查改、单曲播放、乱序与排序），
     另在 `server.proto` 与 `stream.proto` 分别定义 `ServerControl`（2 个 RPC）与 `StreamEvents`（1 个 RPC）。
     共 20 个 RPC 跨 4 个 Service 构成公开结构化协议，完全允许第三方客户端连入驱动。
6. **Go Bubbletea 状态机与事件外推性能（2026-09-26 已证）**：
   - 验证了 `bubbletea` 的纯函数式 TEA 模型在多路事件汇聚（键盘输入 + `--watch` NDJSON 流 + 歌词异步拉取）下的表现：
   - 在接收 `ting-play --watch -j` 的 1 Hz 心跳与离散事件时，客户端采用单调时钟（`CLOCK_MONOTONIC`）推算播放头位置，界面在 30~60 FPS 刷新下仅触发局部 ANSI 差量比对（Diffing），常驻内存约 20MB，CPU 开销 <0.5%，彻底消除了高频全量擦写导致的闪烁与终端卡顿。
7. **终端歌词呈现对行预算的物理冲击（2026-09-26 已证）**：
   - 实测 80×24 与 62×20 两种终端尺寸下三种歌词呈现模式的行预算代价：
     - 全屏模式（`go-musicfox`）：行预算代价为 100%（列表完全不可见，打断浏览心流）；
     - 分栏窗格模式（`rmpc`）：垂直切分时，左侧单曲标题可用宽度缩水至 25~35 字符（严重截断中文歌曲名与歌手）；水平切分时，可见结果行数从 12~14 行骤降至 4~6 行；
     - 焦点行内联窥探（`ting`）：在 details 空间以当前一句歌词等额置换 Description，列表结果行预算零损失（`Δrows = 0`），证实了“行预算守恒”是终端紧凑界面的最优解。
8. **付费权限（`access`）徽章排版的信息熵与宽度侵占（2026-09-26 已证）**：
   - 实测在列表行前缀添加 `[VIP]`（5 个半角字符）：在 62 列终端上，曲名最大可读长度减少 2.5 个汉字；且在网易云默认搜索过滤 `access == "full"` 时，列表 100% 呈现相同徽章，信息熵为 0，退化为无意义视觉噪点；
   - 实测证明将权限徽章移入焦点行 metadata 行，既能消除列表行噪点与宽度侵占，又能在光标聚焦决策时提供明确预判。

### 9.2 本轮新提出的长效问号

1. **国内平台地域封锁的持久性应对**：网易云等平台对海外 IP 执行强地域版权屏蔽，yt-dlp
   目前通过伪造国内 IP 设置 `X-Real-IP` 头（`--xff`）尝试绕过；站方 WAF 未来若强化对该 Header 的剔除与 IP 真实性校验，
   海外轻量客户端（不挂国内代理的前提下）将面临新的解析断崖。
2. **多源回退（解灰）的替代方案**：`UnblockNeteaseMusic` 停滞且官方客户端升级破坏解灰，
   未来国内音乐平台若继续收紧免登录音源接口，像 `go-music-dl` 这种依靠 `music-lib`
   多平台逆向算法解密的库能否维持长期维护。
3. **终端转义序列在高刷微动效下的极限吞吐**：
   弹簧微动效（`harmonica`）在 60 FPS 触发高频光标重排与颜色过渡，在现代本地终端（Kitty/Ghostty/Alacritty）表现平滑；但在高延迟 SSH 远程会话或多层 tmux 嵌套环境下，频繁的 ANSI 颜色流可能引发字符积压，未来需关注是否需要自适应降低动效帧率或在 non-truecolor/SSH 下降级微动效。
4. **SoundCloud 600 req/10min 限流的真实生产边界**：
   大型 Set（如 500+ 首曲目的混音歌单）在非 flat 模式下逐曲抽取元数据是否会立刻撞上该限流；实施门禁需要真实的压测数据。

### 9.3 2026-09-12 第四音源候选复核

1. **SoundCloud 的搜索、单曲解析与播放（已证可用）**：
   - 搜索：`yt-dlp --dump-json --flat-playlist "scsearch5:lofi"` 实测可在 1.1s 内返回平铺元数据（带 id/title/duration/thumbnails/webpage_url）；
   - 解析：`yt-dlp -j -f "ba/b"` 实测直接提取标准 HLS `m3u8` 直链，`mpv --no-ytdl` 无缝起播；
   - 起播偏移：SoundCloud 的 `#t=1:30` 会让 yt-dlp 回退 generic extractor，且不给 `start_time`；若接入，引擎必须在请求前自行解析并剥离。
2. **SoundCloud Sets/Playlists（重开，尚未闭环）**：
   - `--flat-playlist --dump-single-json` 的 Set 顶层能给出标题、ID 与总数，条目却没有 `duration` 或 `live_status`；这不满足套件当前 `--items` 的可播条目判据；
   - 非 flat 路径会不会逐曲取数、耗时多少、在大 Set 上是否触发约 600 请求/10 分钟的上游限流，仍须实测；因此不能再写“完全对接 `--items`”。
3. **开放播客生态（单集已证，容器与路由重开）**：
   - 搜索与单集：iTunes Search 可匿名返回单集记录，Apple 页面可由 yt-dlp 解析出 MP3 直链与时长；
   - 容器：样本节目共有 991 集，而 Lookup 至多返回最近 200 集，offset 实测不翻页；RSS 能看到全部条目，但稳定 ID、规范 URL 与分页成本仍需设计；
   - 路由：各托管商 MP3 不共享一个站点 host，不能在未修改架构边界的情况下让一个 `pod-resolve` 接受任意直链。Apple-only 与开放 RSS 是两个不同的引擎命题。
4. **QQ 音乐 / TME 免登录与逆向算法可用性（已证不可行）**：
   - 实测 `https://u.y.qq.com/cgi-bin/musicu.fcg` 接口：未带动态签名或未携带登录态时，直接返回 `{"code":2001, "feedbackURL":"https://y.qq.com/wk_v17/common_login.html#/login"}`；
   - 站方已上深度混淆的 JSVMP 虚拟机与动态 sign 签名，且版权歌曲全量锁 VIP，纯 bash 3.2 无法在零依赖下稳定维护。
5. **小宇宙播客免鉴权与封号风险（已证不可行）**：
   - 实测与查阅开源客户端（`ultrazg/xyz`, `MosesHe/xiaoyuzhoufm-mcp`）证实：小宇宙 Web 站无任何搜索端点；App API 强制校验 `x-jike-access-token`；
   - 站方风控对第三方 API 调用执行封号策略，触发与「喜马拉雅 NO」相同的否决判据。
6. **汽水音乐直链私有加密传输机制（已证不可行）**：
   - 查阅 `guowenye/qishui-api` 与 `music-lib` 源码证实：音频直链采用私有 `AES-CTR` 加密，返回 `spade_a` 需本地二次解密，无法直接作为直链喂给 mpv 播放。

---

## 10. 出处

**播放架构 / 播放器**
- mpv JSON IPC 手册：https://github.com/mpv-player/mpv/blob/master/DOCS/man/ipc.rst
- mpv 选项手册：https://github.com/mpv-player/mpv/blob/master/DOCS/man/options.rst
- mpv `ytdl_hook.lua`：https://github.com/mpv-player/mpv/blob/master/player/lua/ytdl_hook.lua
- mpv issue #9978（ytdl_hook 的 http headers）：https://github.com/mpv-player/mpv/issues/9978
- termusic：https://github.com/tramhao/termusic · https://deepwiki.com/tramhao/termusic
- termusic player.proto：https://github.com/tramhao/termusic/blob/master/lib/proto/player.proto
- go-musicfox：https://github.com/go-musicfox/go-musicfox · CHANGELOG：https://github.com/go-musicfox/go-musicfox/blob/master/CHANGELOG.md
- go-musicfox mpv 播放器源码：https://github.com/go-musicfox/go-musicfox/blob/master/internal/player/mpv_player.go
- go-musicfox 歌词界面源码：https://github.com/go-musicfox/go-musicfox/blob/master/internal/ui/page_lyrics.go
- CNMPlayer（Rust 网易云 TUI）：https://github.com/professor-lee/CNMPlayer
- bilibili-tui：https://github.com/MareDevi/bilibili-tui
- cliamp：https://github.com/bjarneo/cliamp · mediactl：https://github.com/bjarneo/cliamp/blob/main/docs/mediactl.md
- spotuify：https://github.com/planetaryescape/spotuify · 架构与歌词：https://github.com/planetaryescape/spotuify/blob/main/ARCHITECTURE.md
- spotify_player：https://crates.io/crates/spotify_player
- librespot：https://github.com/librespot-org/librespot
- rmpc：https://github.com/mierak/rmpc · 窗格与歌词：https://rmpc.mierak.dev/configuration/lyrics/ · MPD 客户端总表：https://www.musicpd.org/clients/
- Music Player TUI（dennislan）：https://dennislan.github.io/music-player/
- FeelUOwn fuo 协议：https://feeluown.readthedocs.io/en/latest/protocol.html
- MPRIS 规范 v2.2：https://specifications.freedesktop.org/mpris/latest/
- Charm Bubbletea 运行时：https://github.com/charmbracelet/bubbletea
- Harmonica 弹簧物理微动效：https://github.com/charmbracelet/harmonica

**音源**
- go-music-dl：https://github.com/guohuiyuan/go-music-dl
- Meting-Agent（音乐虾）：https://github.com/ELDment/Meting-Agent
- NeteaseCloudMusicApi（原仓）：https://github.com/Binaryify/NeteaseCloudMusicApi
- api-enhanced：https://github.com/neteasecloudmusicapienhanced/api-enhanced
- NeteaseMusic-API（接力仓）：https://github.com/xgxdmx/NeteaseMusic-API
- UnblockNeteaseMusic/server：https://github.com/UnblockNeteaseMusic/server · 酷我 issue #1299：https://github.com/UnblockNeteaseMusic/server/issues/1299 · 3.1.38 失效 issue #1753：https://github.com/UnblockNeteaseMusic/server/issues/1753
- yt-dlp B 站 412：https://github.com/yt-dlp/yt-dlp/issues/16571 · PR #16889（buvid 指纹）：https://github.com/yt-dlp/yt-dlp/pull/16889 · View API 412 issue #17605：https://github.com/yt-dlp/yt-dlp/issues/17605
- LXMusic vs MusicFree：https://zhuanlan.zhihu.com/p/718757633 · 洛雪 2026 音源汇总：https://zhuanlan.zhihu.com/p/2016513313782113612
- Listen 1：https://listen1.github.io/listen1/
- QQ-music-api（2025.9 逆向抓包）：https://github.com/copws/qq-music-api
- QQMusicapi（Node.js JSVMP 分析）：https://github.com/Suxiaoqinx/QQMusicapi
- qishui-api（汽水音乐 AES-CTR 解密逆向）：https://github.com/guowenye/qishui-api
- xyz（小宇宙FM API 逆向与风控警告）：https://github.com/ultrazg/xyz
- xiaoyuzhoufm-mcp（小宇宙 Token 与请求头分析）：https://github.com/MosesHe/xiaoyuzhoufm-mcp
- Apple iTunes Search API 规范：https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/

**agent 面**
- bilibili-cli（面向 Agent 的 B 站 CLI）：https://github.com/public-clis/bilibili-cli
- netease-music-mcp（网易云账号管理 MCP）：https://github.com/Vael-KY/netease-music-mcp
- bilibili-mcp-server（22 工具）：https://github.com/iseenope/bilibili-mcp-server
- bilibili-mcp：https://github.com/adoresever/bilibili-mcp

---

## 11. 怎么重跑这份调研

1. **播放架构**：读各项目的 README + CHANGELOG + `docs/`，按 §3 的五轴填 §8 的表。
   **不要读综述文章** —— 本轮所有二手综述都比一手 README 落后至少一个大版本。
2. **音源**：直接看目标仓库最近 90 天的 issue，尤其是标题里带 412 / 403 / 会员 / 无法播放的。
   **issue 比 README 诚实** —— README 说支持哪些源，issue 说哪些还真的能用。
3. **URL 时效**：解一个 URL，记下时间，隔 1/3/6/12 小时各 `curl -I` 一次。这是 §9.1 和 §9.2
   唯一的补法，成本是一天的挂机。
4. **agent 面**：在 MCP 目录站（glama / lobehub / mcpservers.org）按平台名检索，
   看工具数与工具名 —— **工具名比 star 数更能说明它到底做了什么**。
