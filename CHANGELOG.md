# CHANGELOG — co-jue (jue) 架构演进与版本变更历史

`co-jue`（二进制命令 `jue`，前身 `co-ting`）是 `co` 生态面向 AI Agent 的轻量端侧视听感知与播放微外设。系统经历了从早期 Gen-1 终端 TUI 播放器到 Gen-2 纯 Go 100% Agentic 视听感知微外设的根本性跃迁。

依据全域统一文档分类法规约（`ARCH-doc-taxonomy.md`），系统演进历史、阶段全表（Stage chronicles）与版本更新日志一律集中收拢于本文件。

---

## 系统阶段演进全表 (Stages 0–8)

| 阶段 (Stage) | 对应版本 | 时间节点 | 触发原因 | 核心架构动作 | 揭示的核心原则 |
|---|---|---|---|---|---|
| **Stage 0** | `v0.8.0` ~ `v0.27.0` | 2026-07 ~ 2026-09 | 试图同时满足人类终端交互与 Agent 命令行调用双面需求 | 基于 bash 3.2 脚本管理 mpv detached 进程生命周期；基于 Go Bubbletea 构建终端多栏 TUI 界面；维护个人歌单、历史记录与终端封面渲染；累计膨胀至 2.4 万行代码（1.3w 行 shell + 1.1w 行 Go） | **双向妥协导致双向平庸**：人类面缺乏现代播放器体验与快捷键生态，Agent 面输出 100KB JSON 撑爆上下文，沦为黑盒“音箱开关”。 |
| **Stage 1** | `v1.0.0` | 2026-10-03 | 跃迁至“可抛弃客户端软件”范式（Post-App Paradigm），确立 **The Agent is the UX** | 彻底清退 2.4 万行旧 Shell 脚本与 TUI 历史负债；重构为 ~1,000 行纯 Go 标准库极简静态单二进制；依赖严格锁定为 `yt-dlp` 与 `mpv` 双原语；交付 4 大原子动词（`inspect`, `transcript`, `play`, `control`/`status`）；开启 `mpv --input-media-keys=yes` 原生打通 macOS 键盘媒体键与 AirPods 耳机暂停键；确立四级退出码与单行紧凑 JSON 信封 | **Agent 本身就是唯一的端侧播放 UX**；微外设应当做极致细腰、极低 Token 消耗与事实证据去幻觉，拒绝客户端界面与本地数据库包袱。 |
| **Stage 2** | `v1.1.0` | 2026-10-04 | 统一 `co` 生态命名规范，实现零远端 CI 的本地工程闭环与驱动级加固 | 仓名正名并收容至 `workspace_genai/co-ting/`；确立 SemVer 单一真源与 `.githooks` 本地门禁；交付多架构静态交叉编译与发布脚本 `scripts/release.sh`；交付原子化安装与卸载脚本 `install.sh`；加固 macOS CoreAudio 暂停（`audio-format s16`）与 IPC 解交错；交付 9 块 131 项零 Mock 端到端测试套件；编写人机协同操作手册与认知击穿演示幻灯片 | **本地能做的绝不推给 GitHub**；全生命周期在宿主机闭环；微外设质量由确定性端到端测试与硬件契约把关。 |
| **Stage 3** | `v1.2.0` | 2026-10-05 | Turn-based Agent 回显后即挂起，无法常驻后台接力续播；语义点歌被单 URL 强校验阻断 | 暴露 mpv 原生瞬态内存播放列表（`queue add/list/clear`、`control next/prev`），零持久化；显式 `ytsearch1:` 检索前缀复用 yt-dlp 原生检索，零自研爬虫；新增 `queue_ended` 事件；废除 ~1,400 行人工魔数，改行 Need-based 零冗余原则；端到端套件扩至 12 块 232 项 | **连续性下沉到播放器而非 Agent**：Agent 一次性编排歌单后即可离场，队列由 mpv 内存自驱动，随进程生灭；规模由需要决定，而非由数字决定。 |
| **Stage 4** | `v2.0.0` | 2026-10-06 | 视觉感知完全缺席，长视频技术研读遭遇物理泥潭；系统命名与语义升维重锚 | 命名全域蜕变升维为 `co-jue`（`jue` / 觉），无须兼容彻底大改；交付原子动词 `jue frame` 单帧视觉感知（One-shot mpv `--vo=image` 零常驻内存）；独立进程组与负 PID 广播 SIGKILL 统一收割；lavfi 双边等比外框滤镜（横竖屏均为 ~690 Tokens）；0700 沙箱与同步机会式 GC；端到端物理回归套件扩充至 13 块 255+ 项全绿 | **命名是容器，能力是流体**；从听觉单维扩展为视听觉知全模态；感知平面与执行平面动静彻底隔离。 |
| **Stage 5** | `v2.1.0` | 2026-10-07 | 认知信源扩展：跨平台结构化转录协议、免 Cookie 规范与海外顶级 IT/AI 信源拓扑落地 | 泛化 WebVTT/SRT 5 状态 FSM 清洗；下沉 Podcast 2.0 RSS 与 ID3v2 章节解包；落地方案 A 纯日常浏览器 Cookie 透传（零落盘文件）；全量端到端扩展至 267 项断言 | **开放协议标准化下沉，动态风控坚守底线**；静态协议入标准库，会话缓存留浏览器，只读凭证库绝不污染。 |
| **Stage 6** | `v2.2.0` | 2026-10-08 | 语音 Agent（co-s2s）边放音乐边说话时人声被音乐淹没；设备热插拔对 Agent 不可见 | `control duck` 脉冲/边沿闪避：mpv 内嵌瞬态 Lua 助手独占 `volume-gain`，20ms 升余弦渐变与 30s 看门狗活在播放器里，CLI 一条 `script-message-to` 立返；版本化就绪标记判定助手在场；`events` 新增 `audio_device_changed`（首条有效通知基线 + 排序指纹）；端到端套件按六大能力平面重构为 20 块 333 项 | **包络归播放器，策略归 Go**：调用方可以崩溃，音乐不会被锁在低电平；投递回执不等于执行回执，在场与否要靠对方亲口写下的标记。 |
| **Stage 7** | `v2.3.0` | 2026-10-22 | 队列随 mpv 生灭，`stop` 或换会话后“刚才放的那首是什么”无从回答 | 新增 `jue history [list] [--limit N]\|last\|clear`：成功的 `play` / `queue add` 记入 `$XDG_STATE_HOME/jue/history.jsonl`（0700 目录 / 0600 文件 / 拒收符号链接，与运行时目录共用 `privateDir` 门禁），`history.lock` 串行化、temp+rename 原子重写、500 条封顶；端到端套件新增 Block O，扩至 21 块 377 项 | **唯一的持久状态必须有界、原子、可丢弃**：记录是播放的副产品，失败只能丢记录，不能丢声音；只记事实，不做歌单与检索。 |
| **Stage 8** | `v2.4.0` ~ `v2.4.3` | 2026-10-25 ~ 2026-10-27 | 家庭 NAS（`qnap-media`）等私有流媒体源接入时，裸容器（MKV/MP4）与 `.m3u8` 直链缺乏元数据/字幕提取且视频播放与音频守护分裂；YouTube `tv_samsung` 广告门控引发 403；拔电转电池或播放完毕长期挂起耗电 | 确立“源（`qnap-media`）与器（`jue`）正交解耦”；`play`/`queue add` 新增 `--video` 基于 mpv 条目级 `vid=auto` 在同一守护进程内按需开关原生视频窗口，对 `.m3u8` 与回环流注入 `ytdl=no` 跳过冗余 `yt-dlp` 解析，且 `control stop` 同步等待对端 IPC 断开消除退出竞态；内嵌 `jue_duck.lua` 新增 AC→Battery 拔电自动退出与 600s `idle-active` 空闲自动回收；`inspect` 新增 mpv 裸容器与 `.m3u8` 探测（`duration`/`chapters`/`tracks`）；`transcript` 新增直链/同目录外挂 `.srt/.vtt/.ass/.ssa/.lrc` 与内封文本字幕轨（`sub-text` 观察器）提取；`frame` 新增本地回环流与 `.m3u8` 直通 `--no-ytdl` 快路；`mpvArgs` 与 `frame` 冷启动注入 `--ytdl-raw-options=extractor-args=youtube:player_client=-tv_samsung` 旁路 TV 广告门控 403 | **源只供流，器管感知与播放**：不造第二个播放器进程；条目级 `vid=auto` 让单实例守护在无头声卡与桌面影院间无态切换；既有 `mpv` 原语足以解包裸容器元数据与内封字幕，无需破戒引入 `ffprobe`。 |

---

## 版本更新日志 (SemVer Releases)

## [2.4.3] - 2026-10-27

**AC → Battery 拔电自动停止、600s 空闲守护回收与第一性原理测试体系重构（`internal/engine/jue_duck.lua`, `internal/engine/frame.go`, `tests/test_suite.sh`）。**

- **AC → Battery 拔电边沿自动退出（`internal/engine/jue_duck.lua`）**：内嵌 Lua 助手新增异步电源状态探测（macOS `/usr/bin/pmset -g batt`；非 macOS 首探失败即停用定时器，零无效轮询）。当 `mpv` 在 `AC Power` 下运行并切换至 `Battery Power` 时立即执行 `mp.command("quit")` 退出，防止拔电离席后后台音视频继续耗电；若在电池供电下主动发起 `jue play`（初态即为 `Battery Power`）则正常播放。
- **600s 空闲自动回收（`internal/engine/jue_duck.lua`）**：监听 `idle-active` 属性，队列播毕进入 `idle` 满 600s 自动执行 `mp.command("quit")` 退出守护进程，杜绝 `--idle=yes` 守护进程跨夜常驻。
- **`firstEntryGuard.setPgid` 启动竞态闭环（`internal/engine/frame.go`）**：修复 `cmd.Start()` 返回前子进程快速输出多条 `Playing: ` 行导致 `pgid` 尚未写入而漏发 `SIGKILL` 的启动竞态。
- **第一性原理 + KISS 测试分层重构（`cmd/jue/*_test.go`, `internal/engine/*_test.go`, `tests/test_suite.sh`, `install.sh`）**：
  - **单元层提速 ~4x（8.98s → 2.29s）**：修复歌单截断单测中 `sh` 子进程 `posix_spawn` 与 `killpg` 竞态触发 2s `WaitDelay` 的问题；内存 `net.Pipe` 超时统一收紧至 30ms；合入 `TestSemverParity` 并删除独立 `version_test.go` 硬编码版本号；表驱动化缓存流解析与 GC 样板，合并重复的 2MB 超长坏行历史测试。
  - **端到端层正交去重（21 块 → 15 块，304 项断言，35s 稳定全绿）**：`Block A` 收敛为每动词 1 条代表性用法闸门；原 `Block H` 文本纯净度与 `unavailable` → `inspect` 兜底并入 `Block B`；原 `Block G` 200ms 起播即停/暂停冻结并入 `Block C`；6 路冷启动 `queue add` 并发并入 `Block D`；原 7 个工作流块剔除 Bash `printf` 假卡片同义反复，收敛为 `E-a` 多模态章节研读与 `E-b` 实时播放头锚定；`now_ms()` 改用 `perl -MTime::HiRes` 且 `samples()` 改为单进程 Python 循环采样，根除 `pyenv` shim 与循环 fork 导致的 `N6` 采样饥饿；清退 `install.sh` 中旧 `ting` / `co-ting` 过渡期清理分支。

## [2.4.2] - 2026-10-27

**`control stop` 同步挂断等待、`.m3u8`/回环流 `ytdl=no` 快路径与原生 `.ass`/`.ssa` 外挂/直链字幕解析。**

- **`jue control stop` 消除异步退出竞态（`internal/engine/ipc.go`, `internal/engine/daemon.go`）**：新增 `IPCClient.WaitClosed(timeout)`；`Control("stop")` 在收到 `mpv` 对 `"quit"` 的 `success` 回执后等待对端关闭 IPC Socket（上限 500ms）再返回，确保紧随其后的 `jue status` 或新一次 `jue play` 确定性观测到 `idle` 状态，杜绝在 `loading` 阶段停止时连上垂死 `mpv.sock`。
- **`.m3u8` 直链与本地回环流跳过冗余 `yt-dlp` 套娃（`internal/engine/daemon.go`, `internal/engine/ingest.go`, `internal/engine/frame.go`）**：
  - `loadfileOpts` 对 `.m3u8` 直链（`isDirectHLS`）与本地回环流（`isLoopbackHTTP`）自动附加条目级 `ytdl=no`，免除 `mpv` 在 1,900+ 分片 HLS 流上先跑一遍 `ytdl_hook` 导致的 >10s 起播卡顿（实测起播降至 ~1.2s）；
  - `Inspect` 与 `Frame` 将 `isDirectHLS` 纳入 `--no-ytdl` 直通快路，`jue inspect <m3u8>` 直接返回完整 `duration` 与音视频 `tracks`（`h264`/`aac`）。
- **原生 `.ass` / `.ssa` 字幕解析（`internal/engine/ingest.go`）**：新增纯标准库 `parseASS`（支持自适应 `[Events] Format:` 列序、剥离 `{...}` 样式标签与 `\p1..\p0` 矢量绘图块、折叠 `\N`/`\n`/`\h` 换行并合并同时间戳双语行），并将 `.zh.ass` / `.chs.ass` / `.ja.ass` / `.jpn.ass` / `.en.ass` / `.eng.ass` / `.ass` / `.ssa` 纳入 `sidecarSuffixes` 与 `subExtRe`，使 `jue transcript` 与 `jue play --video` 的外挂字幕能力完全对齐。
- **单元测试（`internal/engine/daemon_test.go`, `internal/engine/ingest_test.go`）**：新增 `TestControlStopWaitsForPeerHangup`、`TestDirectHLSAndLoopbackBypassYtdlHook` 及 `TestMpvInspectAndContainerTranscript` 中的 `.ass` 外挂/直链双语与绘图块过滤断言。

## [2.4.1] - 2026-10-27

**YouTube `tv_samsung` 广告门控 HTTP 403 旁路加固（`play` / `queue add` / `frame`）。**

- **常驻播放守护（`internal/engine/daemon.go`）与单帧视觉抽取（`internal/engine/frame.go`）**：在 `mpvArgs` 与 `frame` 冷启动参数中注入 `--ytdl-raw-options=extractor-args=youtube:player_client=-tv_samsung`。`yt-dlp` 默认客户端列表（`tv_samsung`, `visionos`, `web`）在遇到触发 YouTube TV 端 10s 广告门控的视频时，`tv_samsung` 的 `c=TVHTML5` 直链会在同 `itag` 去重中胜出但被 `googlevideo.com` 拒收（HTTP 403）；剔除 `tv_samsung` 后直接由 `visionos`/`web` 取流，免除慢速 `deno` JS 解谜并保留儿童视频自动回退 `web_embedded` 及非 YouTube 站点兼容。
- **测试加固（`internal/engine/daemon_test.go`, `internal/engine/frame_test.go`）**：在 `TestMPVArgsLoadDuckHelper` 与 `TestFrameHotFailureFallbackToCold` 中锁定 `--ytdl-raw-options=extractor-args=youtube:player_client=-tv_samsung` 参数断言。

## [2.4.0] - 2026-10-25

**按需桌面视频窗口（`--video`）、裸媒体容器元数据与字幕轨感知（`inspect` / `transcript`）、本地回环流抽帧快路（`isLoopbackHTTP`）。**

- **`jue play <url> [--start SEC] [--video]` 与 `jue queue add <url> [--video]`**：通过 mpv `loadfile` 条目级选项 `vid=auto`（`start=S,vid=auto`）在单实例常驻 `mpv` 守护进程上按需打开原生视频窗口（`vo-configured: true`），成功信封带 `"video":true`（未指定 `--video` 时省略，既有信封逐字节不变）。条目放毕或切回普通音频条目时，mpv 自动恢复全局 `--no-video` 并关闭视频窗口，复用同一套 Flock 互斥、`pause/resume/seek/volume`、`control duck`、`events` 与 `history`。`status` 在 `vo-configured` 为真时附带 `"video":true`。重复 `--video` 或重复值参数一律退出码 1。
- **`jue inspect` 裸媒体容器探测（`mpvInspect`）**：对本地媒体文件、本地回环 HTTP 流（`http://127.0.0.1` / `localhost` / `[::1]`）或 `yt-dlp` 未能给出 `duration` 的直接媒体容器 URL，调用瞬态无头 `mpv --no-ytdl --vo=null --ao=null --no-sub --frames=1 --end=0.1` 提取真实 `duration`、内封 `chapters` 与流轨道列表 `tracks`（`[{"id","type":"video"|"audio"|"sub","codec","lang","title"}]`）。公网平台（YouTube 等）信封保持 `tracks` 省略，Token 预算断言零漂移。
- **`jue transcript` 外挂与内封字幕感知**：
  1. 直接传入 `.vtt` / `.srt` / `.lrc`（HTTP URL 或本地文件）时直接拉取解析；
  2. 对本地媒体文件或本地回环 HTTP 媒体流，优先探测同目录同名外挂字幕（`.vtt` / `.srt` / `.zh.vtt` / `.zh.srt` / `.chs.srt` / `.en.vtt` / `.en.srt` / `.eng.srt` / `.lrc`）；
  3. 无外挂字幕时通过 `mpvInspect` 检查容器 `tracks`，若存在文本字幕轨（`subrip`/`srt`/`ass`/`ssa`/`webvtt`/`mov_text`），在 0700 沙箱内通过瞬态无头 `mpv` Lua `sub-text` 观察器提取时间窗切片，无字幕轨则立即返回退出码 4 `unavailable`。
- **`jue frame` 本地回环直链快路（`isLoopbackHTTP`）**：对 `127.0.0.1` / `localhost` / `[::1]` 流地址，Cold 首帧直挂 `--no-ytdl` 跳过无意义的 `yt-dlp` 探测，Hot 缓存命中复用完整 `frameTimeout`（15s）预算，消除异地隧道（`cf-qnap`）拉取大体积 MP4 `moov` 索引时被 2s CDN 短超时误杀折返的问题。

## [2.3.0] - 2026-10-22

**持久播放历史（`jue history`）。**

- **`jue history [list] [--limit N] | last | clear`**：`list`（裸 `history` 等同）最新在前，默认 20 条，`--limit` 取 1–500，信封 `{"status":"ok","count":N,"items":[…]}`，空为 `{"status":"ok","count":0,"items":[]}`；`last` 为 `{"status":"ok","item":{…}}`，空历史退出码 4 `{"status":"unavailable","error":"history is empty"}`；`clear` 为 `{"status":"ok","action":"clear"}`。未知子动作、多余参数、`--limit` 缺值/非整数/越界一律退出码 1，先于任何文件访问。
- **记录**：`Play` 与 `QueueAdd` 成功后各记一条 `{"timestamp","action":"play"|"queue_add","input","url","title","duration"}`；`input` 为原始入参（如 `ytsearch1:…`），`url` 为解析后的规范地址。`title` / `duration` 取自 mpv `media-title` / `duration`，仅在 jue 已等到该条目加载且未被顶替时读取；在播时排队的条目为 `""` / `0`、`url` 为 `ytdl://` 归一化输入。失败的播放不记录。
- **存储与门禁（`internal/engine/history.go`）**：`$XDG_STATE_HOME/jue/history.jsonl`，未设或非绝对路径回落 `~/.local/state/jue/`。`runtimeDir` 的目录门禁抽出为 `privateDir`，历史目录同样要求真实目录、本 UID、恰为 0700；文件以 `O_NOFOLLOW` 读取，必须是本 UID 的 0600 常规文件，否则退出码 2。写入在 `history.lock`（0600，非阻塞重试至多 1s）下读全量、追加、保留最新 500 条、写临时文件后 `rename` 覆盖，读者只见完整的新旧文件之一，预埋符号链接被替换而非跟随，并发写零丢失。坏行（非 JSON、缺 `action`、超 1 MiB）跳过，下次记录重写干净。`list` / `last` / `clear` 均不创建目录。
- **韧性**：记录失败一律丢弃，`play` / `queue add` 的信封与退出码不变；`history` 动词自身如实报出拒收原因。
- **测试**：单元层新增 `history_test.go`（`TestRecordAndList`、`TestHistoryLast`、`TestHistoryClear`、`TestHistoryCapTrimming`、`TestHistoryConcurrentRecords`、`TestHistoryCorruptLines`、`TestHistorySecurityGates`、`TestPlayAndQueueAddRecordHistory`、`TestHistoryFailureDoesNotFailPlay`），包级 `TestMain` 将 `XDG_STATE_HOME` 指向临时目录，既有 `Play`/`QueueAdd` 单测不再可能写入真实历史；`cmd/jue/verbs_test.go` 的 `TestVerbUsageGates` 增 16 项 history 用法闸门（并断言不留状态目录），新增 `TestHistoryVerbAccepts`。端到端新增 Block O（33 项），Block A +9（history 用法闸门与零状态目录）、J +1（6 路并发 add 历史零丢失）、K +1（检索词 `input` 与解析后 `url`、标题）；`block()` 为每块导出独立 `XDG_STATE_HOME`，套件绝不触碰用户真实历史。共 21 块 377 项。
- **文档**：`ARCHITECTURE.md` §1.2 边界表新增 `history` 行，原“歌单与历史数据库 — 彻底剔除”收窄为“歌单与可检索数据库”；新增 §4.3.1；信封与退出码表补 history。`SKILL.md` 增动词、契约与策略 10；`RUNBOOK-user-manual.md` 增场景 10；`README.md` 补记忆平面并修正陈旧版本号（2.1.0 → 2.3.0）。

## [2.2.0] - 2026-10-08

**毫秒级声学闪避（`control duck`）与 CoreAudio 设备拓扑遥测（`audio_device_changed`）（完结吸收 PLAN-acoustic-ducking-and-device-telemetry）。**

- **`jue control duck [on|off] [--duration SEC] [--level 0-100] [--fade MS]`**：脉冲（默认 5s / 20% / 200ms 渐隐、`2 × --fade` 渐显）、`on`（持续压低，30s 看门狗，400ms 渐显）、`off`（默认 400ms 渐显，未闪避时为空操作、退出码 0）；成功信封 `{"status":"ok","action":"duck"|"duck_on"|"duck_off"}`，`ControlResponse` 结构不变。
- **进程内瞬态助手 `internal/engine/jue_duck.lua`**：`embed` 内嵌，`launch()` 在锁内、拉起 mpv 前以 temp+rename 写入 `$TMPDIR/jue-<uid>/jue_duck.lua`（0600 常规文件，预埋符号链接被替换而非跟随），`--script=` 加载，随 mpv 生灭；只有常驻 mpv 加载，`frame` 的一次性 mpv 不加载。20ms 升余弦插值、`--fade 0` 同步即时写入、同目标冗余指令不重启渐变（心跳续期零扰动）、最新指令胜出、一次性恢复定时器走墙钟。抽出 `mpvArgs` 供单测断言。
- **`volume-gain` 闪避独占**：只写 mpv 独立增益级，与 `control volume` 正交，`status.volume` 语义不漂移；`--level 0` 落到 −96 dB 地板。
- **投递 ≠ 执行**：`script-message-to` 的 `success` 只证投递，助手在全部处理函数注册后最后写 `user-data/jue/duck-helper = "1"`；`Duck` 依次过空闲闸门、读标记（缺失/版本不符 → 4 `unavailable`，不下发）、下发一条字符串参数指令。Go 侧 `parseDuck` 与 Lua 侧 `num()` 校验域逐项一致；Lua 侧的有限值+区间校验防止外部发送方以 `nan`/`inf`/`1e309` 污染状态。
- **专用 `parseDuck`**：子动作只能是首参数；flag 出现性追踪，重复/未知/缺值/与模式不符（`on --duration`、`off --level|--duration`）一律退出码 1，解析先于连接（无播放器时仍为 1）；数值拒收 NaN/Inf/溢出/越界，`--duration` 可带 `s`、`--fade` 可带 `ms`。
- **`status` / `snapshot` 新增可选 `duck`**（D1）：非空闲且 `volume-gain` 低于 −0.05 dB 时出现，瞬时电平百分比（与 `--level` 同刻度，渐变途中为中间值）；未闪避时信封逐字节不变，空闲态不读不出现。
- **`audio_device_changed` 事件**：`events` 订阅 `observe_property 4 audio-device-list`，以首条有效通知为基线并压制（不另发 `Get`，消除回执与订阅初值的先后竞态），指纹为按 `name`/`description` 排序后的 JSON（换序不误报，`null` 与 `[]` 等同），变化时发 `{"event":"audio_device_changed","audio_device":…,"devices":[…]}`；字段按 D5 定名 `audio_device`（mpv 配置选择器，`--no-config` 下为 `auto`，不是实际路由设备——mpv 0.41 无 `audio-out-detected-device`）。`--until audio_device_changed` 在空闲播放器上合法。
- **上游风控对抗与流解析韧性（方向 4 落地，yt-dlp 原语演进与 JS 运行时边界）**：
  - **`ytdlpFail` 状态机四级收敛（`internal/engine/ingest.go`）**：严格划分退出码 1（用法/URL 非法）、退出码 2（底层瞬态网络/Cookie 库锁）、退出码 4 `unavailable`（视频不存在/已删除/私有/版权下架）与退出码 4 `error`（HTTP 429 频控/Botguard 验证/地区封锁），彻底解决将平台终态阻断粗暴归入退出码 2 的缺陷。
  - **测试套件重试闸门精准化（`tests/test_suite.sh`）**：`transient()` 明确排除包含 `upstream blocked` 与 `rate limited` 的退出码 4 阻断，仅对纯物理网络故障（退出码 2）与 mpv 播放器底层瞬态断流发起退避重试，斩断加剧 IP 封禁的重试风暴。
  - **安装感知与运行时引导（`install.sh`）**：环境检查阶段新增可选 JS 运行时 `deno` 探测与安装提示（`brew install deno`），保持零硬性阻断与优雅降级。
  - **测试矩阵与回归**：`ingest_test.go` 补全 429、Bot 验证、地区封锁、下架视频状态机断言；全量 20 块 333 项端到端物理回归全绿。
- **Phase 0 实测（mpv 0.41.0，本机）**：
  - P1：运行时 `set_property volume-gain` 0 → −14 → −96 → 0 全部成功，期间事件流零 `audio-reconfig`（N16 固化）；可闻爆音试听**未做**（需人耳）。
  - P2/P7：沿用 Codex 实测结论（投递回执语义、`tonumber` 非有限值污染），由就绪标记与 `num()` 校验覆盖，N15/N23/N24 回归。
  - P3：JSON IPC 下 `script-message-to` 的数值参数被拒（`invalid parameter`），字符串参数正常——按设计一律 `strconv.FormatFloat` 发字符串。
  - P4：真实 mpv 上 1000ms 渐隐每 50ms 采样得 18 个单调中间值；20ms 定时器抖动打点与 zipper 试听**未做**（需人耳）。
  - P5：`observe_property audio-device-list` 订阅后立即推送一条带 `data` 的初值（本机 5 个设备），`audio-device` 为 `auto`；热插拔事件序列**未实测**（需人工插拔）。
  - P6：真实 mpv 上三次往返（`playlist-pos`、就绪标记、`script-message-to`）约 62µs；`BenchmarkDuckRoundTrip`（假 mpv、真实 Unix socket，含拨号）约 54µs/次，远低于 2ms。
  - P8：Lua `mp.set_property("user-data/jue/duck-helper","1")` 可写、IPC 可读；`pause` 下 `mp.add_timeout` 按墙钟照常触发（NW 块固化）。
- **手工设备验收（§5.3）：待人工执行**。插拔有线耳机、AirPods 连断、USB 声卡下 `audio_device_changed` 的条数与内容，以及 `duck`/`duck on`/`duck off`/`duck on --level 0` 的听感（爆音、zipper），无法在套件内构造（不引入虚拟音频驱动），本版本发布时尚未完成。
- **Codex 对抗评审 16 条全部吸收**：版本化就绪标记、非有限值防护、设备基线改用首条有效通知、排序指纹与 nil/空规范化、`audio_device` 语义、冗余渐变跳过、`--fade 0` 即时与 duration 定义、看门狗准确表述（最迟 30s **开始**渐显）、单步最大 2.41 dB 与 −96 dB 地板、生命周期表、`volume-gain` 独占、瞬时电平语义、专用 `parseDuck`、退出码矩阵、N 块补非有限值/零重配置/CLI 墙钟、保留 30s 看门狗以暂停+续期压缩测试时长。
- **测试**：单元层新增 `duck_test.go`、`cmd/jue/verbs_test.go`，`daemon_test.go` 补 `TestMPVArgsLoadDuckHelper`/`TestStatusDuckField`，`events_test.go` 补五项设备遥测；`startFakeMPV` 接受 `testing.TB` 且 nil 回复即挂断。端到端新增 Block N（51 项）与 Block NW（5 项，生产 30s 看门狗），Block A +16、Block I +4；
- **端到端套件第一性原理重构（`tests/test_suite.sh`）**：
  - **按能力平面编排**：报告按 Boundary（A、F）· Ingest（B、K、M）· Playback（C、D、J、L）· Acoustic（G、N、NW）· Events（I）· Agent workflows（E-a/b+f/c/d/e/g、H）六个平面分组；块字母不变（历史条目仍可引用）。块清单单表驱动启动与报告，不再两处维护。
  - **网络抖动有界重试 `net`/`fetch`**：此前 `v play … || { sleep 1; v play …; }` 的重试因 `v` 恒返回 0 从未生效（基线一次 YouTube 加载失败连锁 8 项 FAIL）。现只重试退出码契约定义的瞬态结果——退出码 2，或退出码 4 且 `status=="error"`（流加载失败）——最多 3 次、2s/4s 退避，单次时长由 jue 自身界定（yt-dlp 90s、HTTP 20s、加载 30s、frame 17.5s）；重试过的断言在报告里标注 `[attempt N of 3]`。
  - **前置条件闸门 `need`**：后续步骤依赖的步骤失败时，记一条 FAIL 加一行 `--` 跳过说明并收掉本块播放器，不再一错连锁。
  - **挂起有界**：每块 300s 看门狗（超时即杀进程树与本块 mpv 并报告）；`mpv_ipc` 每次套接字操作 5s 超时，无回应时打印诊断而非挂死。
  - **事件订阅就绪同步 `subscribe`/`collect`**：以 `lsof` 确认 `jue events` 已持有套接字连接后才触发（原为“输出文件已存在 + sleep 0.2”的竞态）；`events --until track_ended` 的短音轨由 1s 加长到 3s，消除并行负载下错过 eof 的窗口。
  - **去重与归位**：各动词的用法闸门与“无播放器不落盘”统一收归 A（原散落于 A/I/J/L/M）；K 与 A/C 重复的 `inspect notaurl`、缺失文件回归删除；`silence` 生成器取代四处内联 WAV 构造。
  - **去同义反复、改测真实事实**：E-f 原为“播静音 WAV + 复查 B 已测的 LRC 首行 + grep 测试自己写入的卡片”，现与 E-b 合并为一条 NetEase 流：现在播放卡片后暂停、读播放头、取覆盖播放头的那一行歌词（也让全套 NetEase 起播数减半）；E-g 改为引用帧时刻最近的真实字幕行并断言两者相距 < 5s，删去恒真的“卡片不含本机路径”检查。
  - **可移植与隔离**：M 块去掉写死的本机 MP3 路径（且该文件无封面），改由 mpv 现场生成带 mjpeg 内嵌封面的 MP3，覆盖 `audio-display=no` 的真正场景；M 的 yt-dlp 残留检查改用其它块不会发出的检索词，消除与 K 并行时的误报；前置依赖检查补 `python3`、`lsof`。
  - **NW 保持独立块**：它需让一个暂停的播放器空等 34.5s 墙钟；并入 N 会把这段时间串行加到 N 上（全套墙钟由最慢块决定），故并列于 Acoustic 平面并行执行。
  - 单元层新增 `cmd/jue/verbs_test.go` `TestVerbUsageGates`：八个动词的语法闸门在清空 `PATH`/`TMPDIR` 下逐项为退出码 1（漏过闸门者会答 2 或 4），且隔离的 `TMPDIR` 事后为空（用法错不落盘）。
  - **防假通过加固（Codex 对抗评审）**：块子 shell 非零退出（`set -u`、信号）记一条 `block crashed` FAIL，不再让未执行的断言读作通过；看门狗 `kill_tree` 先 TERM、0.5s 后对仍存活者 KILL；`subscribe` 3s 内未连上即记 FAIL 并返回 1；`failc` 把详情内换行转义为 `\n`，保持一行一断言；`mpv_ipc` 每次请求整体 5s 单调时限，事件持续涌入也不会无限等 `request_id`；`expect` 另以 `jq -s` 断言单行只含一个 JSON 值；N17–N20 先断言 `next`/`seek`/`resume`/`play` 本身成功再查闪避电平；E-e 倒带后播放头加上界（`< 暂停点 − 1s`），证明确实回退。
  - 全套 20 块 333 项（A 59 · F 7 · B 11 · K 15 · M 21 · C 35 · D 7 · J 31 · L 11 · G 13 · N 51 · NW 5 · I 14 · E-a 6 · E-b+f 8 · E-c 10 · E-d 6 · E-e 7 · E-g 5 · H 10 · 收尾 1），并行约 35s。断言数下降来自去重与删除恒真检查，不是覆盖收缩。

## [2.1.0] - 2026-10-07

**跨平台结构化转录（WebVTT/SRT、Podcast 2.0 RSS、ID3v2 章节）与端侧视觉感知信封富化、流直链缓存加速与人机交付闭环（完结吸收 PLAN-frame-perception-polishing-and-cache）。**

- **视觉信封富化与噪音消除**：`FrameResult` 补入 `duration`（秒，底层解析到时回填；为 0 时收缩），`actual_at` 增加 `omitempty` 标签（为 nil 时彻底收缩，杜绝 Token 浪费与模型对时钟漂移的虚假猜忌）。
- **短期流直链缓存引擎与毫秒级寻址加速 (`internal/engine/cache.go`)**：
  - 纯 Go 标准库与固定 32 分片稳定排他锁池（`flock LOCK_NB on cache/shard_xx.lock`，永不 unlink 消除 inode 复用竞态与无界累积），零锁争抢等待，不侵蚀执行预算；GC 仅对过期条目计数，彻底消除扫描饥饿；
  - 严格准入四大法定证据链：解复用器单媒体封装白名单（`mov,mp4,m4a,3gp,3g2,mj2` 或 `matroska,webm` 等，坚决拒收 `edl://`、HLS/DASH 自适应分段清单流）；点播有限正时长证据；通过 `stream-lavf-o` 检测私有 Cookie 注入并拒存；单条无歧义 Referer 与 User-Agent 原样结构化保存并在热命中时通过 `--http-header-fields` 与 `--user-agent` 完整透传，确保脱离 ytdl 后 100% 独立可复播；
  - 锁内指纹校验安全删除（`deleteStreamCache` 校验 `failedFingerprint`），彻底根除并发 A 读 B 写 A 删的 TOCTOU 竞态；
  - 调用级统一绝对 Deadline 机制：顶层划分 `workDeadline := callDeadline.Add(-200ms)`，子进程执行与等待严格死锁在 17.3s 调度预算内，留足 200ms 核心交付硬预算至 17.5s 物理硬上限；
  - GC 联动：改变一次性 `ReadDir(-1)` 为分批读取 `f.ReadDir(30)`，并顺手清理过期的 `cache/*.json`。
- **人机双轨交付与时钟中心锚定启发式**：`SKILL.md` 与 `docs/USER_MANUAL.md` 规范机器端本地 `path` 调 `read_image` 读图 vs 人类端外网直达时戳链接 `&t=` 交付范式（严禁在 Markdown 贴本地 `/var/folders/` 临时路径防裂图）；字幕推导抽帧建议取 60% 中点偏后候选时点避开转场未定型态。
- **测试套件扩充与全量回归**：`tests/test_suite.sh` 新增 `Block E-g`（视觉帧感知与多模态证据卡片装配工作流，7 项断言）；`Block M` 补齐 `.duration>0`、`has("actual_at") | not`、0700 缓存目录与 0600 缓存 JSON 检查、独立单流正向准入、缓存条目重定向热命中消费验证；全套 13 块 273 项断言 100% 绿灯全绿！
- **浏览器 Cookie 透传（仅 `--cookies-from-browser`，零 cookies 文件落盘）**：每次 yt-dlp 调用透传 `--cookies-from-browser <JUE_COOKIES_FROM_BROWSER>`，未设置默认 `chrome`，`none`/`off` 关闭；浏览器名非法 exit 1，Cookie 库缺失/锁定/无权限归一为 exit 2 并给出出路提示；错误行优先取 yt-dlp 的 `ERROR:` 行，不再被 Python traceback 尾行顶替。登录态下 YouTube 按账号返回 `language`，同一视频可能改选人工轨（端到端套件默认以 `none` 校验匿名契约）。
- **WebVTT / SRT 流式 FSM 清洗**：`parseVTT` / `parseSRT` 共用 5 状态逐行扫描（Header/Skip/Time/Text/Flush）；`NOTE`（须完整词边界：`NOTE`、`NOTE ` 或 `NOTE\t`，`NOTEBOOK-1` 之类 Cue 标识符照常保留）/`STYLE`/`REGION` 块整块跳过直到空行，块内形似时间轴的行也不发射；`[HH:]MM:SS` 点号/逗号毫秒；单 pass 只剥离合法标签 `<[/]?[a-zA-Z]...>` 与 karaoke `<[0-9]{1,2}:...>`，`x < 10 and y > 5` 等比较运算原文保留，再反转义实体；多行以空格拼接、空 Cue 过滤、相邻同文残影合并（与 json3 共用 `appendCue`）。
- **Podcast 2.0 RSS**：`transcript` 对 feed 形 URL（`.xml`/`.rss`、`feed`/`rss` 路径段、`feeds.` 主机）或 yt-dlp 无果而服务器回 XML 的 URL，以 `encoding/xml` 解包最新一期：优先 channel `<language>` 原语种（含 `en-US` 等子标签前缀匹配，缺省 `language` 属性视为 channel 语种）的 `<podcast:transcript>`，`text/vtt` 先于 SRT，无原语种轨才退化为首个 VTT/SRT，防机翻轨顶替原话；兜底 `<content:encoded>` 时间戳大纲（Substack 的目录 + 全稿双遍时取全稿）。
- **ID3v2 章节**：`inspect` 对 `.mp3`/`.m4a`/`.aac` 直链且 yt-dlp 未给章节时，以 `Range: bytes=0-262143` 探测 ID3v2.3/2.4 `CHAP` + `TIT2`，毫秒级章节；探测失败不影响信封。
- **`frame` 防御加固**：成功判定以物理产物为先，`00000001.jpg` 已落盘且非空时，日志里（如文件名/标题）出现 `Video: none` 不再误判 exit 4；显式 `--loop-playlist=no`，且第一项帧落盘后一旦 mpv 开始下一条目（stdout 行首 `Playing:`，文件名内嵌的同名串不计）即收割进程组，播放列表只抽首项；机会式 GC 改为读全目录、只对真正执行进程组探测的目录计数（至多 30 次或 50ms），根除前部未过期或 pgid 缺失/损坏目录造成的饥饿。
- **`install.sh` 死 socket 自愈**：`control stop` 后 socket 仍在、但无进程以其为 `--input-ipc-server` 时判定为崩溃残留，移除并继续安装；仍有活进程持有才中止。旧 ting 与 jue 两条停止路径合并为 `stop_daemon`。

## [2.0.0] - 2026-10-06

**全域蜕变升维 co-jue (jue)；端侧流式时空单帧视觉感知 (Frame Perception) 落地与双平面动静隔离。**

- **命名与语义升维蜕变（完结吸收 PLAN-jue-metamorphosis-and-frame-perception）**：
  - 彻底告别 `co-ting`（ting/听），全域升维为 **`co-jue`（jue/觉，Auditory & Visual Perception Peripheral for AI Agents）**；
  - 贯彻“无须兼容，彻底改”：删除所有别名与过渡期软链接，模块路径更新为 `github.com/binlecode/co-jue`，CLI 二进制单一收敛为 `jue`，运行时沙箱目录切换为 `$TMPDIR/jue-<uid>/`（严格目录权限 `0700`），互斥锁收敛为 `jue.lock`（文件权限 `0600`）；
  - 全局 Skill 资产正名为 `jue`，更新触发词与提示词契约。
- **端侧单帧视觉感知 (`jue frame <url> --at <time> [--width N] [--quality N]`)**：
  - 纯 Go 标准库与仅调用 mpv 原语，零 ffmpeg，零外部 C 库；
  - **动静彻底隔离**：坚持瞬态 One-shot mpv `--vo=image` 模型，秒级抽取并退出，常驻内存增量为 0，杜绝常驻守护开启视频导致内存暴增或切流冲垮音频伴播；
  - **独立进程组与统一收割**：配置 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`，统一收尾路径（`defer`）向独立进程组广播 SIGKILL，并在 500ms 内显式轮询探测 `syscall.Kill(-pgid, 0)` 确认收到 `ESRCH` 消亡后才删除目录；配置 `cmd.WaitDelay = 2 * time.Second` 杜绝后代管道写端死锁；总预算锁定为 17.5s 物理硬上限；
  - **双边等比外框约束**：装配 lavfi 滤镜 `--vf=lavfi=[scale=w='min(N,iw)':h='min(N,ih)':force_original_aspect_ratio=decrease,scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2']`，横屏（960x540）与竖屏（540x960）Token 预算严格对称一致（均为约 690 Tokens），单张 JPG 体积稳定在 30KB~80KB；
  - **沙箱与机会式 GC**：临时目录模式 `0700`，图片落盘显式 `os.Chmod(targetPath, 0600)`；启动时落盘 `0600` 的 `pgid` 文件，每次调用触发有界同步机会式 GC 扫描并清理过期无存活进程的 frame 目录；
  - **肯定证据四级退出码**：纯音频/无视频轨 Exit 4；明确时长越界 Exit 4；缺乏可靠时长流异常保守返回 Exit 2；参数非法 Exit 1；依赖缺失/超时 Exit 2。
- **全量物理回归扩充**：
  - 本地回归套件升级为 13 块 255+ 项零 Mock 端到端断言，新增 Block M 视觉帧感知契约实测全绿。

---

## [1.2.0] - 2026-10-05

**瞬态内存队列、显式检索前缀、`queue_ended` 队列放毕感知与规范 URL 解析；代码规模改行 Need-based 原则。**

- **瞬态内存队列 (`ting queue`，完结吸收 PLAN-transient-queue-and-query-resolution)**：
  - 新增一级动词 `queue add <url|query>` / `queue list` / `queue clear`，直接暴露 mpv 原生内存播放列表，**零持久化歌单数据库**，随 mpv 进程启停生灭；
  - `queue add`：播放器空闲或未运行（以 `playlist-pos == -1` 判定，不依赖跳变滞后的 `idle-active`）时先 `playlist-clear` 清掉已播历史，再 `loadfile … append-play` 起播并等待出声，返回 `state:"playing", pos:0, count:1`；正在播放时仅追加并非阻塞立返 `state:"queued", pos:count-1, count`；
  - `queue list`：返回 `{status, pos, count, items[]}`，`title` 与 `current` 均 `omitempty`（待播项尚未加载，不伪造标题；仅当前项标 `current:true`）；播放器未运行或空闲时返回 `{"status":"ok","pos":-1,"count":0,"items":[]}`，与 `status` 一样不创建运行时目录；
  - `queue clear`：清除全部待播，当前发声曲目继续；未运行或空闲返回退出码 4 `not_playing`；
  - `ting play` 语义明确为**替换整个队列**；
  - **统一锁 `flockAction`**：`play` 与 `queue add` 在同一把 `ting.lock` 排他锁内完成拨号/拉起、状态探查与指令分发，锁外再 `WaitForPlaybackSuccess` 等待出声，冷启动并发 add 只拉起一个 mpv 且条目零丢失。
- **切歌控制 (`ting control next|prev`)**：
  - 以 `playlist-next weak` / `playlist-prev weak` 下发，越界时 mpv 拒绝而非终止播放器，映射为退出码 4 `unavailable`（`end of playlist` / `start of playlist`）；
  - 切换成功后自动解除暂停并等待新曲出声，返回 `{"status":"ok","action":"next"}`。
- **显式检索前缀（Query Resolution）**：
  - `play` / `queue add` / `inspect` / `transcript` 均接受显式 `ytsearch1:<关键词>`（及等价的 `ytsearch:`），复用 yt-dlp 原生检索协议，**零自研爬虫**；坚持显式前缀，不做隐式猜测，非法 URL（退出码 1）与缺失本地文件（退出码 4）契约不变；
  - **非对称分流**：mpv 只认 `ytdl://ytsearch1:…`（裸前缀会被当成本地路径），yt-dlp 拒收 `ytdl://`——传给 mpv 时补 `ytdl://`（`normalizeForMPV`），传给 yt-dlp 时剥离（`normalizeForYtdlp`，并在网易云 id 识别前执行）；
  - 头部锚定判定 `^[a-z0-9]*search[a-z0-9]*:`，白名单外的检索前缀（如 `ytsearch5:`、`scsearch:`）返回退出码 1；`youtube.com/results?search_query=…` 等普通 URL 不受误伤；
  - 检索结果的 playlist 外壳以 `entries[0]` 的原始字节解包替换（`transcript --load-info-json` 读到的是视频本身），空结果返回退出码 4 `unavailable`；普通播放列表 URL 不做降维。
- **规范 URL 解析（信封变更）**：
  - `play` 与 `queue add`（起播分支）的 `url` 字段由**回显输入**改为**出声后读取 mpv `path` 的解析结果**：检索词或短链返回标准 watch URL；`queue add` 排队分支仍回显归一化输入（如 `ytdl://ytsearch1:…`）。
- **队列放毕感知 (`queue_ended`)**：
  - `ting events` 新增事件 `{"event":"queue_ended"}`：观察 `idle-active` 的 `false → true` 边沿，且最后一首以 `eof` 或 `error` 结束时派发；`next`/`play` 替换导致的越界属调用方自身动作，不报；
  - `events --until queue_ended` 启动时播放器已空闲返回退出码 4 `player is idle`，未运行返回 `no player running`，监听中途被 `stop` 或进程退出返回 `player exited`；
  - **修复**：`start-file` 到达时即刷新 `curURL`，切到一首加载失败的曲目时 `track_ended` 不再误报上一首的 URL。
- **代码规范调整（Need-based）**：
  - 废除历史遗留的“代码规模严格控制在 ~1,400 行内”人工魔数（由 Master 裁决），改以功能必要性、极致精简与零冗余为准绳；纯 Go 标准库、零 cgo、零第三方包、双外部原语红线不变；
  - 同步更正 `CLAUDE.md`、`README.md`、`docs/ARCHITECTURE.md` 与幻灯片中的陈旧行数描述。
- **测试**：
  - 单元层增补 `normalizeForYtdlp`/检索解包、`flockAction` 与队列分流、`playlist-next/prev` 越界、`queue_ended` 状态机等用例；
  - 端到端套件由 9 块扩至 **12 块（A–L）**，全套 **232 项断言全绿（并行约 11s）**：新增 `Block J` 瞬态队列生命周期（35 项，含 6 路并发冷启动 add）、`Block K` 显式检索解析（18 项）、`Block L` 队列放毕感知（13 项）。
- **文档**：同步 `SKILL.md`、`docs/ARCHITECTURE.md`、`docs/USER_MANUAL.md`、`README.md` 与 `CLAUDE.md`；依据规约完成架构正本吸收并删除施工方案 `docs/PLAN-transient-queue-and-query-resolution.md`。

---

## [1.1.0] - 2026-10-04

**`co-ting` 生态正名、本地工程闭环（Local CI/CD）、驱动级加固与 9 块端到端测试套件。**

- **生态正名与仓位归属**：
  - 仓库正式正名并迁移至 `workspace_genai/co-ting/`，二进制命令保持为 `ting`，软链与别名全面融入 `co-*` 家族；
  - 统一更新 `CLAUDE.md`、`README.md` 与 `docs/` 下全部正本引用。
- **本地闭环发版工程化 (`scripts/release.sh`)**：
  - 践行“本地能做的，绝不推给 GitHub”原则，坚决不引入 `.github/workflows/` 远端 CI；
  - 实现四架构纯静态编译（`darwin-arm64`、`darwin-amd64`、`linux-amd64`、`linux-arm64`，`CGO_ENABLED=0`，`-trimpath -ldflags "-s -w"`）；
  - 自动校验根目录 `VERSION` SemVer 规范，生成 `dist/SHA256SUMS.txt` 校验和；
  - 支持 `--publish` 配合本地 `gh release` 命令行直接发版。
- **原子化安装与卸载 (`install.sh`)**：
  - 本地编译后原子安装至 `~/bin/ting`，并自动创建 `~/bin/co-ting` 兼容别名软链；
  - 自动同步全局 Agent Skill 到 `~/co-brain/skills/ting/SKILL.md`（含 `co-ting` 软链）与 `~/.agents/skills/ting/SKILL.md`；
  - 支持 `./install.sh --uninstall` 一键彻底清理二进制软链与全局 Skill 引用。
- **驱动与核心引擎加固**：
  - **macOS CoreAudio 暂停加固** (`internal/engine/daemon.go`)：mpv 启动参数强制追加 `--audio-format=s16`，根治部分 macOS 声卡在浮点格式下直通声卡导致暂停键失效的顽疾；
  - **IPC 解交错与重定向抗扰** (`internal/engine/ipc.go`)：优化 `WaitForPlaybackSuccess` 逻辑，精准解交错 `loadfile`、`start-file`、`file-loaded` 与 `end-file` 事件，杜绝网络媒体发生 302 重定向或连续顶替起播时的伪成功；
  - **运行时目录权限安全** (`internal/engine/daemon.go`)：严格限制运行时目录为 `$TMPDIR/ting-<uid>/`，属主校验等于自身 UID、权限严格为 `0700`、拦截任何符号链接攻击。
- **事件感知微外设落地交付 (`ting events`，完结吸收 PLAN-event-beacon)**：
  - 新增 `ting events` 动词，恪守零第二守护底线，直连 mpv 原生 IPC 广播中心；
  - 提供模式 A（`--until <EVENT>` 阻塞等待，默认上限 600s，彻底消除自动续播轮询 Token 损耗）与模式 B（持续流式打印）；
  - 交付 5 类强类型事件 Schema（`snapshot`、`track_started`、`track_ended`、`paused`/`resumed`、`chapter_changed`）；
  - 落实边沿触发差分去抖，通过 `StatusOn` 压制 mpv 初始伪事件；
  - 增补 `internal/engine/events_test.go` 单元测试与测试套件 `Block I: Event Contract`（16 项物理断言）；
  - 依据规约完成架构正本吸收并物理删除施工方案 `docs/PLAN-event-beacon-push-actor.md`。
- **10 块端到端零 Mock 测试套件 (`tests/test_suite.sh`)**：
  - 全套件 147 项断言全绿（并行执行耗时约 10s），覆盖 10 大测试块：
    1. `Block A` 安全边界（0700 目录门禁、符号链接防御、参数用法四级退出码）；
    2. `Block B` 感知契约（YouTube 章节路标、原声字幕提取、网易云 LRC 歌词、B 站 unavailable 退出码 4、300 条截断上限）；
    3. `Block C` 播放状态机（本地/网络音频起播、pause/resume/seek/volume/stop 完整生命周期、mpv kill -9 崩溃自动恢复）；
    4. `Block D` 并发争抢（Flock 互斥排队，多并发请求平滑接管，单实例 mpv 严格不泄漏）；
    5. `Block E` 三条人/Agent/ting 协同工作流（a 章节研读与问答、b 背景听歌两行卡片、c 暂停追问播放头附近原话）；
    6. `Block F` Token 预算护盾（紧凑信封、无浮点噪声、范围过滤精确性）；
    7. `Block G` 声学人机交互（起播即停 200ms 内回收，暂停 200ms 内播放头严格冻结）；
    8. `Block H` 事实原件落地（带 `&t=` 时间戳的逐字原话引用块，纯音乐/无字幕时 inspect 优雅兜底）；
    9. `Block I` 事件感知契约（snapshot 电平、until 匹配、自然放毕 eof、AirPods/播控边沿触发、超时 4、stop 优雅断开）；
    10. `Cleanup` 进程与 Socket 零残留断言。
- **人机协同交互指南与配套演示**：
  - 编写面向终端用户的沉浸式指南 [`docs/USER_MANUAL.md`](docs/USER_MANUAL.md)，按 ROI 价值组织 10 大典型交互场景，提供自然语言话术速查表与契约矩阵；
  - 制作认知击穿配套 HTML 幻灯演示 [`docs/USER_MANUAL-slides.html`](docs/USER_MANUAL-slides.html)。

---

## [1.0.0] - 2026-10-03

**Gen-2 架构彻底跃迁（Breaking Change）：拥抱 Post-App 范式，Agent is the UX，纯 Go 单二进制微外设重构。**

- **架构跃迁：确立“The Agent is the UX”**：
  - 彻底抛弃独立 TUI/GUI 客户端定位，ting 沉降为 Agent 挂载在宿主机上的「视听感知外设（输入端）」与「端侧声卡驱动（输出端）」；
  - 人类不再需要终端界面按键，由 Agent 全面接管交互、搜索、排版呈现与记忆沉淀。
- **历史代码清退（Clarity by Subtraction）**：
  - 彻底清空旧 `shell/` 目录下全部 6 个 bash 3.2 脚本（约 13,000 行）；
  - 彻底清退 `cmd/ting` 旧入口与 `internal/tui/` 全部源码（约 11,000 行），删除 Bubbletea 框架与本地历史数据库；
  - 累计砍掉 24,000 行工程负债与 ANSI/CJK 双倍宽终端排版税。
- **纯 Go 标准库极简内核**：
  - 代码量缩减至 ~1,000 行纯 Go（`internal/engine/` + `cmd/ting/`），零 cgo，零外部第三方包依赖；
  - 外部依赖严格锁定为两个系统级原语：`yt-dlp` 与 `mpv`。
- **感知平面原子契约 (Ingestion Plane)**：
  - `ting inspect <url>`：在内存中对原始 90KB+ 媒体数据做极简投影（`id`, `title`, `duration`, `uploader`, `chapters`），输出体积严格控制在 <1KB（<100 Tokens）；
  - `ting transcript <url> [--range START-END]`：
    - 防机翻污染：优先提取原语言自动字幕轨（带有 `-orig`）与人工字幕，拒收机器翻译垃圾；
    - 清洗控制字符与相邻滚动重复行；
    - 相交区间裁剪（`--range <start-end>`），支持秒数或 `mm:ss`；
    - 300 条硬截断保护：超出 300 条自动截断并标记 `truncated: true`，杜绝撑爆 LLM 上下文；
    - 网易云音乐直连解析并推导逐行 LRC 歌词时间戳；无字幕如实返回 `status: "unavailable"`（退出码 4）。
- **执行平面原子契约 (Playback Plane)**：
  - `ting play <url> [--start SEC]`：
    - Flock 文件锁原子互斥拉起，单实例常驻无头 `mpv`（`--idle=yes --no-video`）；
    - 毫秒级 Unix Domain Socket JSON-IPC 通信；
    - 两阶段就绪判定（`WaitForPlaybackSuccess`），等待真实声音就绪（`file-loaded`）后返回，杜绝伪成功。
  - `ting control <pause|resume|seek|volume|stop>`：毫秒级确定性控制，`stop` 后 mpv 优雅退出，不留僵尸；
  - `ting status`：毫秒级时空遥测（返回当前播放头秒数、状态、总时长、音量）。
- **硬件直通物理逃生口**：
  - mpv 默认开启 `--input-media-keys=yes`，原生直通 macOS 键盘媒体键 (F8) 与 AirPods 耳机轻捏暂停，跳过大模型交互延迟（0ms，0 Token 开销）。
- **四级退出码契约与机器信封**：
  - 全命令默认输出单行紧凑 JSON；
  - 严格定义四级退出码：`0`（成功）、`1`（用法错误/参数非法）、`2`（外部工具缺失/网络错误）、`4`（业务未就绪/unavailable/not_playing）。
- **Agent Skill 交付**：
  - 规范根目录 [`SKILL.md`](SKILL.md)，提供开箱即用的 Agent Tool 调用规范与交互 Prompt。

---

## [0.27.0] - 2026-09-30

**Gen-1 终端播放器终版发布。**

- **TUI 界面与搜索优化**：
  - 空搜索直接导航回 feed 主页，移除顶部音乐符文；
  - 调整字标字形间距，优化终端紧凑排版。

---

## [0.8.0 ~ 0.26.2] - 2026-07 至 2026-09

**Gen-1 终端播放器与 Shell 脚本编排探索期（历史归档）。**

- **阶段探索**：
  - 基于 bash 3.2 运行基准与外部工具（`yt-dlp`、`mpv`、`nc`、`jq`、`curl`）编写了 6 个核心脚本，使用 detached 模式管理 mpv 播放生命周期；
  - 基于 Go Bubbletea 构建终端多栏 TUI 播放器，提供搜索、Feed 浏览、歌单管理、逐行歌词与阶段视图（Stage Mode）；
  - 实验了 HTML 封面渲染生成器（`docs/cover.html`）与 macOS 媒体键辅助方案。
- **证伪与结论**：
  - 双界面设计导致架构死锁：人类面体验无法对抗现代 GUI 播放器，Agent 面输出膨胀无法适配 LLM 预算；
  - 触发了 Gen-2 向 100% Agentic 视听外设的彻底跃迁。
