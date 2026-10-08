# ARCHITECTURE —— co-jue (jue)

**co-jue**（CLI 二进制命令为 `jue`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的轻量端侧视听感知与播放微外设（Go 静态单二进制，纯 Go 标准库，代码规模遵循 Need-based 零冗余原则，无 cgo，无第三方依赖，收容于 `~/workspace_genai/co-ting/`）。版本遵循 SemVer 规范，单一数据源声明于根目录 `VERSION`（当前版本：`2.1.0`；演进历史与工程阶段全表见根目录 [`CHANGELOG.md`](../CHANGELOG.md)）。

---

## 1. 定位与第一性原理

### 1.1 历史假设破产与第一性原理重锚

`co-jue`（前身 `ting`）在早期 v0.x 时代试图同时满足人类交互与 Agent 调用两面需求，强行在 bash 3.2 下写了 1.3 万行脚本管理 mpv detached 进程生命周期，并在 Go 侧堆砌了 1.1 万行 Bubbletea TUI 终端排版代码。这套双向妥协导致系统规模膨胀至 2.4 万行，造成了严重的架构死锁：
- **人类面不好用**：缺乏现代播放器的歌单、推荐和系统媒体键集成；
- **Agent 面不够用**：输出 100KB JSON 撑爆上下文，缺乏章节路标与声称级逐字证据，充其量只是个黑盒“音箱开关”。

随着 Agent 前沿进入**“可抛弃客户端软件（Post-App Paradigm）”**时代：
- 人类不再需要一个专有的终端 TUI 窗口去按方向键选歌；
- **Agent 本身就是唯一的端侧播放 UX（The Agent is the UX）**；
- 人类通过自然语言或语音表达意图，由 Agent 调度检索、精读证据、呈现交互卡片或在后台伴随播放；
- **jue 的本质**：彻底剥离客户端应用属性，沉降为 **Agent 挂载在宿主机上的「视听感知外设（输入端）」与「端侧声卡驱动（输出端）」**。

### 1.2 边界判据（双判据交集过滤）

jue 仅保留满足以下两个判据交集的最小原子功能：
1. **判据 A（物理断层）**：必须是 Agent 裸调 mpv 无法解决、且会引发进程失控或 Token 灾难的断层；
2. **判据 B（认知必需）**：必须是 `co-brain` 视听认知链与知识沉淀（`co-library`）不可或缺的原子环节。

| 功能模块 | 归属与裁决 | 架构理由 |
|---|---|---|
| **章节路标 (`inspect`)** | **保留** | 提取时间轴与 Chapters（<100 Tokens），为 Agent 提供全局心智模型。 |
| **逐字证据 (`transcript`)** | **保留** | 提取纯净原语言时间戳字幕（带相交判定裁剪），为 `co-library` 沉淀声称级事实原件。 |
| **时空单帧 (`frame`)** | **保留（v2.0.0）** | 瞬态 One-shot mpv --vo=image 提取秒级视觉证据（<80KB, ~690 Tokens），为 Agent 补齐视觉研读闭环。 |
| **端侧起播 (`play`)** | **保留** | 单实例无头 mpv 延迟托管与 Flock 互斥，解流并平滑替换播放（替换整个队列）。 |
| **瞬态队列 (`queue` / `control next\|prev`)** | **保留（v1.2.0）** | Turn-based Agent 回显后即挂起，无法常驻后台接力；连续性只能下沉到播放器。仅暴露 mpv 原生内存播放列表，零持久化。 |
| **时空遥测 (`status`)** | **保留** | 毫秒级反馈播放头时间点（秒数），让 Agent 获知用户听到了哪里。 |
| **确定性控制 (`control`)** | **保留** | 毫秒级下发 pause/resume/seek/volume/stop，跳过大模型推理延迟。 |
| **事件感知 (`events`)** | **保留** | 复用 mpv 原生 IPC 广播打通推流感知（曲毕/队列放毕/耳机暂停/换章），避免轮询 Token 损耗。 |
| **TUI / GUI 终端界面** | **彻底剔除** | 负债代码，由 Agent 交互直接替代。 |
| **搜索与推荐算法** | **彻底剔除** | Agent 原生具备 `web_search` 和浏览器，无需自研爬虫中间层；唯一例外是显式 `ytsearch1:` 前缀，它只是把检索词原样转交 yt-dlp 原生检索协议，jue 自身不含任何抓取或排序逻辑。 |
| **歌单与历史数据库** | **彻底剔除** | 由 `co-brain` 记忆系统与 `co-library` 原生管理；`queue` 只是 mpv 进程内存中的瞬态播放列表，随 mpv 启停生灭，绝不落盘。 |
| **派生 AI 摘要** | **彻底剔除** | 摘要由模型生成，jue 绝不拿第三方模型总结冒充事实证据。 |

### 1.3 选型裁决：为什么死锁单一最优原语（mpv）而拒绝“多驱动抽象”

通用多媒体框架（如 GStreamer、LibVLC、Mopidy）通常抽象一层 `AudioSink` 驱动接口以支持多后端切换。但在面向 Agent 的微外设体系中，**jue 坚决放弃多驱动抽象，将执行平面死锁在 `mpv` 单一原语**。这是基于第一性原理推演的深度工程抉择：

#### 1. 候选驱动物理断层对比
- **纯 Go/Cgo 底层音频库（如 miniaudio、oto、portaudio）**：只能播放已解码的 raw PCM。Agent 传入的 URL 是 YouTube/B 站/网易云的 DASH/HLS/Opus/AAC 网络流，自建解流、解封装与时钟同步将导致代码膨胀数万行；且引入 C 音频库必须开启 cgo，直接摧毁 `CGO_ENABLED=0` 下四架构本地秒级纯静态交叉编译的优势。
- **`ffplay` / `ffmpeg`**：无 Unix Domain Socket 控制接口，不支持后台无头常驻（`--idle`），无法提供毫秒级 pause/resume/seek/volume 控制与状态遥测。
- **VLC / `cvlc`**：无头常驻开销过大（80MB~150MB，mpv 仅 20MB~30MB）；RC 接口基于旧式 Telnet 纯文本，缺乏强类型 JSON-RPC，不支持 `observe_property` 异步广播与环形背压隔离。
- **系统原生 CLI（macOS `afplay` / Linux `aplay`）**：仅支持本地非压缩音源，无网络流媒体解析与 IPC 播控能力。

#### 2. 多驱动抽象在微外设场景下的三大致命代价
1. **最大公约数陷阱（The Least Common Denominator Trap）**：一旦兼容弱驱动，对外契约必被最弱者绑架。`WaitForPlaybackSuccess` 两阶段事件确认、属性观察广播、四级退出码与 200ms 声学冻结等确定性契约将全面退化为不可靠的启发式猜测与盲目轮询；
2. **细腰代码膨胀**：驱动接口、工厂模式、配置注册与差异抹平将使核心代码成倍膨胀，且全是与功能无关的胶水，违背 Need-based 零冗余底线；
3. **测试矩阵灾难**：232 项端到端物理测试若乘以多个驱动，测试矩阵急剧膨胀，且多驱动在 macOS CoreAudio 与 Linux ALSA 底层的时序抖动将引入海量难以排查的 Flaky 偶发故障。

#### 3. 裁决结论
在全开源界，能够同时满足**「全格式网络流媒体解封装」+「低内存无头常驻 (`--idle`)」+「全双工 Unix Socket JSON-IPC」+「硬件媒体键原生直通 (`--input-media-keys=yes`)」**的原语，有且仅有 `mpv`。对于智能体微外设而言，**锁定单一最优解（The Single Best Primitive）**远比平庸的多驱动兼容更可靠、更细腰、更抗漂移。

### 1.4 实证回馈：从发散场景向 7 大第一性人机工作流收敛

在端到端实测（Dogfooding）与测试闭环中，系统推翻了早期将“基础播控参数（seek/volume）”、“状态文字打印（progress bar）”、“外部简单定时（pomodoro）”以及“输入音源类型（local wav）”拆为 10 个独立场景的膨胀假象。

依据第一性原理，微外设只承载具备**真实认知杠杆或物理断层**的核心流转，收敛确立 7 大法定人机工作流：
1. **长视频分章点播流**：`inspect` 提取目录路标 ➔ 定向分章 `play --start` 精准入耳；
2. **声称级事实核查流**：靶向切片 `transcript --range` ➔ 提取原声逐字证据，防范大模型幻觉；
3. **时空打断与倒带流**：走神/耳机暂停 ➔ `events --until paused` 捕获当前播放头 ➔ 倒带重播；
4. **媒体资产沉淀流**：高价值长音频 ➔ 提炼带 `&t=` 直达时间戳的 Permanent Notes 写入 `co-library`；
5. **专注伴听与歌单连播流**：2 行极简播放卡片、毫秒级播控（相对/绝对 seek、音量）；多首连播由 `queue add` 一次压入 mpv 内存队列自驱动接力（`control next|prev` 切歌），Agent 无需常驻；需要续添时以 `events --until queue_ended`（或单曲粒度 `track_ended`）零轮询感知；
6. **歌词与多语种精读流**：`transcript` 提取网易云 LRC 歌词分词或原声分词 ➔ 逐句语法与释义精读；
7. **关键帧研读与视觉证据链**：长视频架构演讲/PPT ➔ `inspect` 定位章节 ➔ `transcript` 提取原声字幕 ➔ `frame` 提取秒级时空单帧 ➔ Agent 调用 `read_image` 视觉解析架构拓扑与代码，输出图文并茂的证据。

系统设计与回归套件严格围绕这 7 大流转展开，严禁在文档中滋生脱离真实工作流的伪场景。

---

## 2. 系统拓扑与进程模型

```
+-----------------------------------------------------------------------------+
|                               Human (The Master)                            |
+-----------------------------------------------------------------------------+
               |                                               |
       (A) 语义编排通道 (自然语言)                      (B) 下行物理直通 / 紧急急停
       ("放点专注背景音乐", "第14分钟讲了什么")          (AirPods/键盘暂停键 0ms 逃生)
               |                                               |
               v                                               |
+-------------------------------------------------------------+ |
|            The Agent (Claude / OpenCode / co-cli)           | |
|                 * 唯一的端侧播放 UX 呈现层 *                 | |
|  - 意图识别、语义检索与候选挑选                               | |
|  - 呈现轻量 Markdown 媒体卡片与状态同步                      | |
|  - 订阅事件信标并自主续播/即时介入                            | |
|  - 提炼知识写入 co-library 30-resources/                    | |
+-------------------------------------------------------------+ |
        |                               ^                      |
        | 1. 静轨：被动事实抓取与控制     | 2. 动轨：主动事件信标   |
        |    (inspect/transcript/play)  |    (events --until)  |
        v                               |                      |
+-------------------------------------------------------------+ |
|                            jue                              | |
|          (单一纯 Go 二进制: Need-based 零冗余)              | |
|                                                             | |
|   【感知输入 (Ingest)】              【执行输出 (Daemon/IPC)】| |
|   - inspect: 章节索引 (<100 Tok)     - Flock 互斥与单实例无头 | |
|   - transcript: 逐字原话 (<500 Tok)  - UDS 客户端 (0700 目录) | |
|   - frame: 视觉单帧 (~690 Tok)       - request_id 解交错协议  | |
|   - events: 原生广播推流信标         - file-loaded 两阶段确认 | |
|   - 样式清洗与 300 条截断护盾        - 瞬态队列 (mpv 内存列表)| |
|   - ytsearch1: 剥 ytdl:// 交 yt-dlp  - 检索补 ytdl:// 交 mpv  | |
+-------------------------------------------------------------+ |
             | (yt-dlp 原语提取)               | (本地 UDS)     |
             v                                 v                v
+-----------------------------+   +---------------------------------------------+
|     yt-dlp (仅查元数据)     |   |            mpv (无头常驻子进程)              |
|  (只取字幕与章节，绝不下载) |   | --idle=yes --no-video --input-media-keys=yes|
+-----------------------------+   | (接收物理急停，并将状态变更广播至 Unix Socket) |
                                  | (内存播放列表：队列随进程生灭，自驱动接力)   |
                                  +---------------------------------------------+
```

---

## 3. 感知平面架构 (Ingestion Plane)

### 3.1 字段投影与 Token 护盾

原始 `yt-dlp` 的 dump-json 包含数十种视频轨、格式码与握手参数，体积达 90KB~150KB（消耗 20k~50k Tokens）。
- **字段投影**：在内存中仅反序列化 `id`, `title`, `duration`, `uploader`, `chapters` 5 个核心属性；
- **章节映射**：将 yt-dlp 原始的 `start_time` 正确映射为对外信封的 `start`，杜绝时间戳归零；
- **输出体量**：严格控制在 **1KB 左右（< 150 Tokens）**，为 Agent 建立轻量级时空地图。

### 3.2 声称级事实原件提取 (Transcript)

- **语言链与防机翻**：优先提取人工字幕与原语言自动字幕轨（带有 `-orig` 后缀，如 `en-orig`），拒收 YouTube 自动翻译的跨语言垃圾；
- **格式清洗**：以 `--sub-format json3` 获取底层结构化分词，剔除格式控制字符与空行，合并相邻滚动重复行；
- **相交区间裁剪 (`--range <s-e>`)**：按时间窗口区间相交（`cue.Start < end && cue.End > start`）进行过滤；
- **硬截断保护**：无论是否带 `--range`，字幕条数超过 300 条时，截取前 300 条并在信封中标记 `truncated: true`，防止一次性撑爆上下文。
- **多站点统一**：
  - **YouTube**：提取官方字幕或原声 ASR，信封标记 `is_auto`；
  - **Bilibili**：实测无公开字幕轨，如实返回 `status: "unavailable"`（退出码 4）；
  - **网易云**：内置 HTTP GET 请求获取逐行 LRC 歌词，推导起止秒数作为 cues 交付。

### 3.3 显式检索前缀与非对称分流 (Query Resolution)

语义点歌（“放首晴天”）不应迫使 Agent 先绕一轮 `web_search` 拿 URL。jue 不自研爬虫，只接受**显式**检索前缀并原样转交 yt-dlp 的原生检索协议：

- **白名单**：仅 `ytsearch1:` 与 `ytsearch:`（两者都取第一条结果）。以头部锚定正则 `^[a-z0-9]*search[a-z0-9]*:` 识别检索前缀，白名单外（如 `ytsearch5:`、`scsearch:`）一律退出码 1；只锚定头部，`youtube.com/results?search_query=…` 这类普通 URL 不会误判；
- **坚持显式，拒绝猜测**：裸关键词不会被猜成检索，仍按本地路径处理（缺失即退出码 4），非 URL 输入的退出码 1 契约保持不变；
- **非对称分流（实测确证）**：
  - mpv 不认裸 `ytsearch1:…`（当成本地相对路径），必须以 `ytdl://ytsearch1:…` 才会走内置 ytdl 钩子 → `normalizeForMPV` 补 `ytdl://`；
  - yt-dlp 拒收 `ytdl://`（`Unsupported url scheme`）→ `normalizeForYtdlp` 剥离，并在网易云 id 识别之前执行，防止前缀遮蔽域名；
- **检索外壳解包**：`yt-dlp --dump-single-json "ytsearch1:…"` 返回 `_type: "playlist"` 外壳，真实视频在 `entries[0]`。仅当输入是检索前缀时，以 `entries[0]` 的原始字节替换整个记录（`transcript` 经 `--load-info-json` 回灌时读到的是视频本身而非外壳）；`entries` 为空返回退出码 4 `unavailable`；普通播放列表 URL 不做降维；
- **规范 URL 回写**：起播成功后读取 mpv 的 `path` 属性（ytdl 钩子完成 redirect 后即为规范 watch URL）作为信封 `url`，Agent 拿到的是可直接引用的地址，而非检索词。

---

## 4. 执行平面架构 (Playback Plane)

### 4.1 目录安全与生命周期管理

- **目录隔离**：运行时 Socket 严格收容在 `$TMPDIR/jue-<uid>/`；
- **安全门禁**：启动前通过 `os.Lstat` 验证目录属主为自身 UID、权限严格为 `0700`、严禁为符号链接；
- **Flock 互斥启动**：并发拉起时通过 `$TMPDIR/jue-<uid>/jue.lock` 文件锁排队；探测现有 socket 时等待至多 1 秒验证 `idle-active`，杜绝并发竞争误删正在初始化的 socket。

### 4.2 request_id 解交错与事件缓冲

mpv 在同一条 Socket 连接中并发交错下发异步事件（`start-file`, `file-loaded`, `end-file`）与命令回执。
- **解交错**：每条指令携带唯一的单调递增 `request_id`；读取循环只将匹配该 id 的响应行作为命令回执，异步事件全部压入 `eventQueue`；
- **两阶段就绪判定 (`WaitForPlaybackSuccess`)**：
  1. `loadfile` 返回 `playlist_entry_id`；
  2. 监听事件队列，等待出现匹配该 id 的 `start-file`，并丢弃排在其前面的陈旧事件；
  3. 随后收到的第一个 `file-loaded` 即判定起播成功；
  4. 若收到匹配该 id 的 `end-file`（`reason == "error"`），立即报错返回退出码 4；若 `reason == "stop"`（被并发新 play 顶替），判定为正常交接退出。

### 4.3 瞬态内存队列 (Transient Queue)

Turn-based Agent 输出卡片后即挂起等待人类输入，无法常驻后台在曲毕时手动接力。连续播放因此下沉到 mpv：jue 只暴露 mpv 原生内存播放列表，**零持久化**，队列随 mpv 进程启停生灭。

- **统一锁 `flockAction`**：`play` 与 `queue add` 在同一把 `jue.lock` 排他锁内完成拨号/懒启动、状态探查与指令分发；锁内只发命令，锁外再用返回的连接与 `playlist_entry_id` 调 `WaitForPlaybackSuccess` 等待出声。冷启动并发多个 add 只会拉起一个 mpv，且互相看不到对方半成品的播放列表，条目零丢失；
- **空闲判定用 `playlist-pos`**：`playlist-pos == -1` 即空闲。`idle-active` 在 `loadfile` 后有跳变延迟，并发的第二个 add 会误判仍空闲，因此不作为判据；
- **`queue add` 双分支**：
  - 空闲/未运行：先 `playlist-clear`（mpv 放完后仍保留已播历史条目，不清会让 `count` 膨胀），再 `loadfile <url> append-play`、解除暂停，锁外等待出声并回写规范 URL，返回 `state:"playing", pos:0, count:1`；
  - 播放中：仅 `loadfile <url> append-play`，读 `playlist-count` 后非阻塞立返 `state:"queued", pos:count-1`，`url` 为归一化输入（待播条目在轮到之前不解析）；
- **`queue list`**：读 `playlist` / `playlist-pos`，待播条目在 mpv 中只有 `filename`，故 `title` 为 `omitempty`；`current` 仅当前项为 `true`。未运行或空闲返回空队列，且与 `status` 一样不创建运行时目录；
- **`queue clear`**：`playlist-clear` 清除除当前发声曲目外的全部条目；
- **`play` 即替换整队**：`loadfile <url> replace`；
- **`control next|prev`**：下发 `playlist-next weak` / `playlist-prev weak`。`weak` 使越界时 mpv 拒绝命令而非终止播放，Go 侧映射为退出码 4 `unavailable`（`end of playlist` / `start of playlist`）。切换成功后解除暂停（pause 属于播放器而非曲目）并 `WaitForPlaybackSuccess(-1)` 等待任意新条目出声。

### 4.4 物理逃生口（硬件直通）

mpv 启动时配置 `--input-media-keys=yes`。在 macOS 上原生接管全局媒体按键：
- AirPods 双击/按压暂停直接由 mpv 响应；
- macOS 媒体键 (F8) 由操作系统直接派发给 mpv；
- **跳过大模型**：急停操作实现 0ms 模型延迟，杜绝网络卡顿导致声音无法停止的窘境。

### 4.5 事件感知与外发平面 (Event Beacon)

为打破“只能被动轮询（Pull-only）”的局限，jue 增加了 `events` 动词，支持主动事件感知（Push-based）：
- **零第二常驻守护**：宿主机唯一常驻进程仍仅为 mpv。`jue events` 仅作为轻量监听客户端直连已有的 Unix Socket，复用 mpv 原生多客户端事件广播机制与有界环形背压隔离；
- **双执行模式**：
  - **单次阻塞模式 (`--until <EVENT> [--timeout SEC]`)**：面向一问一答式 Coding Agent（如 Claude Code / co-cli），阻塞等待目标事件触发（默认上限 600s），命中即输出单行 JSON 并以退出码 0 退出，**彻底消除自动续播轮询 Token 损耗**；
  - **流式管道模式 (`jue events [--timeout SEC]`)**：面向实时语音流智能体（如 `co-s2s`），连接首行立即派发电平快照（`snapshot`），后续持续打印单行紧凑 JSON；
- **六类确定性事件 Schema**：
  - `snapshot`：初次连接即时电平快照（复用 `status` 数据结构，防漏状态）；
  - `track_started`：媒体真实出声确认（绑定 `file-loaded`）；
  - `track_ended`：映射为 4 种状态（`eof` 正常放完续播信号、`replaced` 被并发新曲顶替、`stopped` 手动 stop 或退出、`error` 解码故障）；
  - `paused` / `resumed`：即时感知 AirPods 或键盘媒体键触控；
  - `chapter_changed`：播放头跨越章节边界时即时派发；
  - `queue_ended`：整个队列放毕。观察 `idle-active` 的 `false → true` 边沿，且最后一首以 `eof` 或 `error` 结束时才派发；被 `play` 替换、`stop` 停止属调用方自身动作，不报。`--until queue_ended` 启动时播放器已空闲或未运行立即退出码 4（否则永远等不到），监听中途播放器退出返回退出码 4 `player exited`；
- **边沿触发与初始假事件压制**：利用 `StatusOn` 预取初始状态，对 `observe_property` 注册后 mpv 立即回推的初始值做严格差分去抖，杜绝伪事件；
- **URL 归属**：`start-file` 到达时 mpv 的 `path` 已是新条目，此刻即刷新当前 URL，切到加载失败的曲目时 `track_ended` 不会挂在上一首名下。

---

## 5. 数据契约与退出码

### 5.1 统一信封格式

全命令默认输出单行紧凑 JSON，`status` 字段统一表达执行结果；事件行以 `event` 字段标识：

```json
// 正常起播（url 为 mpv 解析后的规范地址，检索词亦返回 watch URL）
{"status":"ok","state":"playing","url":"https://...","start":18}

// 队列追加：空闲时起播 / 在播时排队
{"status":"ok","action":"add","state":"playing","url":"https://www.youtube.com/watch?v=...","pos":0,"count":1}
{"status":"ok","action":"add","state":"queued","url":"ytdl://ytsearch1:七里香 周杰伦","pos":1,"count":2}

// 查看队列（待播项无 title，仅当前项带 current）
{"status":"ok","pos":0,"count":2,"items":[{"index":0,"url":"https://...","title":"晴天","current":true},{"index":1,"url":"ytdl://ytsearch1:七里香 周杰伦"}]}

// 切歌越界
{"status":"unavailable","error":"end of playlist"}

// 队列放毕 (jue events --until queue_ended)
{"event":"queue_ended"}

// 正常查询状态
{"status":"ok","state":"playing","url":"https://...","time_pos":845.2,"duration":2540.0,"volume":85}

// 单次阻塞捕获曲毕事件 (jue events --until track_ended)
{"event":"track_ended","url":"https://...","reason":"eof","duration":269.0}

// 捕获硬件触控暂停 (jue events --until paused)
{"event":"paused","time_pos":84.2}

// 无可用字幕
{"status":"unavailable","error":"no original-language subtitles for https://..."}

// 单帧视觉感知 (jue frame --at 73)
{"status":"ok","url":"https://...","at":73.0,"actual_at":null,"path":"/tmp/jue-501/scratch-frame-123/frame_73s.jpg","width":960,"height":540,"size_bytes":43490,"format":"jpg"}

// 媒体无有效视频轨
{"status":"unavailable","error":"media contains no video stream"}

// 时间戳超出媒体总时长
{"status":"unavailable","error":"timestamp out of range: requested 999.00s >= duration 213.00s"}

// 未在播放时执行控制或监听事件
{"status":"not_playing","error":"no player running"}
```

### 5.2 四级退出码

- `0`：成功（Success）；
- `1`：命令行用法错、参数格式非法、时间戳解析失败或数值溢出、不支持的检索前缀；
- `2`：外部依赖缺失（未装 mpv 或 yt-dlp）、底层网络中断、拉流超时、无可靠时长流中断；
- `4`：业务未就绪（如媒体无公开字幕/无视频轨 `unavailable`、检索无结果、`next`/`prev` 越界、时间戳超出视频总时长、播放器空闲时执行控制或 `queue clear`、媒体加载失败）。

---

## 6. 端侧视觉感知架构与原语决策 (Visual Frame Perception Architecture)

### 6.1 动静隔离：为什么坚持 One-shot `--vo=image`

在视频研读场景中，PPT 幻灯片、架构拓扑与代码屏幕是核心论据载体。jue 引入原子动词 `jue frame <url> --at <time> [--width N] [--quality N]` 打通端侧视觉感知。系统在进程模型上坚决贯彻**动静彻底隔离**：
- **执行平面（Daemon）**：后台常驻守护进程保持 `--no-video` 纯音频运行（20-30MB 极低开销）。由于无头模式下渲染管线完全旁路，无法通过 IPC 执行截图；若强开 GPU VO，macOS 会强制弹窗破坏无头契约并导致常驻内存暴增 5 倍，且切流会冲垮正在后台伴播的音频任务。
- **感知平面（Ingest）**：抽帧坚定采纳独立的**瞬态 One-shot 进程（`mpv --frames=1 --vo=image`）**，按需派生，秒级退出，常驻内存增量为 0，与后台音频伴播物理隔离、零打扰。

### 6.2 独立进程组与统一收割流水线

- **进程组隔离**：One-shot mpv 子进程派生时强制配置 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`，自成独立进程组；
- **负 PID 强杀与有界等待**：Context 超时或取消时，通过 `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` 向独立进程组广播强杀，递归收割 mpv 及其派生的 yt-dlp 后代；配置 `cmd.WaitDelay = 2 * time.Second` 杜绝后代继承管道写端导致的 EOF 永久挂死；
- **严格收尾时序与时间预算**：总时间预算严格锁定为“15s 工作预算 + 2s WaitDelay 管道等待 + 500ms 进程组消亡探测”（总计 17.5s 物理硬上限）；在 `defer` 中先向独立进程组广播 SIGKILL，并在 500ms 内以 10ms 间隔轮询探测 `syscall.Kill(-pgid, 0)`，显式收到 `ESRCH` 确认整组完全消亡后才同步删除失败的 scratch 目录；超时未确认消亡则保留现场交由后续机会式 GC 安全回收，彻底杜绝并发写删竞态。

### 6.3 双边外框约束与 Token 预算收敛

为防范竖屏视频（Shorts/Reels 1080x1920）在单一宽度缩放下高度暴增至 1706 导致 Token 预算膨胀 3 倍，系统装配 lavfi 双边等比外框约束滤镜：
```
--vf=lavfi=[scale=w='min(N,iw)':h='min(N,ih)':force_original_aspect_ratio=decrease,scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2']
```
确保常见 16:9 视频在横屏（960x540）与竖屏（540x960）下的像素与 Vision Token 预算严格对称一致（均为约 690 Tokens），单张 JPG 体积稳定在 30KB~80KB。

### 6.4 沙箱隔离与同步机会式 GC

- 运行时在 `$TMPDIR/jue-<uid>/` 下创建模式为 `0700` 的专用临时目录 `scratch-frame-*`；
- 图片文件生成后由 Go 侧显式调用 `os.Chmod(targetPath, 0600)` 确立权限；
- 每次创建 scratch 目录时就地同步执行 `syncOpportunisticGC`（门控：至多 30 次进程组存活探测、耗时上限 50ms；未过期、无 pgid 或 pgid 损坏的目录跳过且不计入配额）；仅对 `time.Since > 1h` 且包含有效 `0600` `pgid` 文件、且负 PID 探测返回 `ESRCH` 确认消亡的过期目录执行清理，存活或损坏目录安全跳过。

---

## 7. 验证与设计镜像公理 (Verification-Design Mirror Axiom)

系统设计坚决贯彻“没有自动化可执行物理断言的设计就是空头支票”原则：
1. **工作流 1:1 物理镜像**：用户手册与系统总纲所声明的 7 大核心人机工作流，在本地回归套件 `tests/test_suite.sh` 中必须拥有完全对应的端到端用例（`Block E-a ~ E-f`、`Block H` 与 `Block M` 视觉帧感知），零 Mock 驱动真实 mpv 与外部端点；
2. **两轨感知闭环覆盖**：静轨（Pull）被动事实提取与动轨（Push）主动事件信标（Block I 16 项、Block L 13 项断言）均实现物理核销，包括曲目自然放毕（`reason=="eof"`）、队列放毕（`queue_ended`）、AirPods 硬件急停捕获、初始伪事件去抖与背压安全；
3. **队列与检索契约覆盖**：瞬态队列（Block J 35 项，含 6 路并发冷启动 add 单 mpv 零丢失）与显式检索分流（Block K 18 项，含非白名单前缀与普通 `search_query` URL 的正反例）均由真实 mpv/yt-dlp 驱动核销；
4. **零漂移度量衡**：全生命周期的测试、编译与静态分析在宿主机本地完全闭环；代码规模不设人工行数魔数，遵循 Need-based 零冗余原则——每项新特性必须证明无法由既有原语或 Agent 自身承担，且不得突破纯 Go 标准库与双原语依赖的红线。
