# CHANGELOG — co-jue (jue) 架构演进与版本变更历史

`co-jue`（二进制命令 `jue`，前身 `co-ting`）是 `co` 生态面向 AI Agent 的轻量端侧视听感知与播放微外设。系统经历了从早期 Gen-1 终端 TUI 播放器到 Gen-2 纯 Go 100% Agentic 视听感知微外设的根本性跃迁。

依据全域统一文档分类法规约（`ARCH-doc-taxonomy.md`），系统演进历史、阶段全表（Stage chronicles）与版本更新日志一律集中收拢于本文件。

---

## 系统阶段演进全表 (Stages 0–4)

| 阶段 (Stage) | 对应版本 | 时间节点 | 触发原因 | 核心架构动作 | 揭示的核心原则 |
|---|---|---|---|---|---|
| **Stage 0** | `v0.8.0` ~ `v0.27.0` | 2026-07 ~ 2026-09 | 试图同时满足人类终端交互与 Agent 命令行调用双面需求 | 基于 bash 3.2 脚本管理 mpv detached 进程生命周期；基于 Go Bubbletea 构建终端多栏 TUI 界面；维护个人歌单、历史记录与终端封面渲染；累计膨胀至 2.4 万行代码（1.3w 行 shell + 1.1w 行 Go） | **双向妥协导致双向平庸**：人类面缺乏现代播放器体验与快捷键生态，Agent 面输出 100KB JSON 撑爆上下文，沦为黑盒“音箱开关”。 |
| **Stage 1** | `v1.0.0` | 2026-10-03 | 跃迁至“可抛弃客户端软件”范式（Post-App Paradigm），确立 **The Agent is the UX** | 彻底清退 2.4 万行旧 Shell 脚本与 TUI 历史负债；重构为 ~1,000 行纯 Go 标准库极简静态单二进制；依赖严格锁定为 `yt-dlp` 与 `mpv` 双原语；交付 4 大原子动词（`inspect`, `transcript`, `play`, `control`/`status`）；开启 `mpv --input-media-keys=yes` 原生打通 macOS 键盘媒体键与 AirPods 耳机暂停键；确立四级退出码与单行紧凑 JSON 信封 | **Agent 本身就是唯一的端侧播放 UX**；微外设应当做极致细腰、极低 Token 消耗与事实证据去幻觉，拒绝客户端界面与本地数据库包袱。 |
| **Stage 2** | `v1.1.0` | 2026-10-04 | 统一 `co` 生态命名规范，实现零远端 CI 的本地工程闭环与驱动级加固 | 仓名正名并收容至 `workspace_genai/co-ting/`；确立 SemVer 单一真源与 `.githooks` 本地门禁；交付多架构静态交叉编译与发布脚本 `scripts/release.sh`；交付原子化安装与卸载脚本 `install.sh`；加固 macOS CoreAudio 暂停（`audio-format s16`）与 IPC 解交错；交付 9 块 131 项零 Mock 端到端测试套件；编写人机协同操作手册与认知击穿演示幻灯片 | **本地能做的绝不推给 GitHub**；全生命周期在宿主机闭环；微外设质量由确定性端到端测试与硬件契约把关。 |
| **Stage 3** | `v1.2.0` | 2026-10-05 | Turn-based Agent 回显后即挂起，无法常驻后台接力续播；语义点歌被单 URL 强校验阻断 | 暴露 mpv 原生瞬态内存播放列表（`queue add/list/clear`、`control next/prev`），零持久化；显式 `ytsearch1:` 检索前缀复用 yt-dlp 原生检索，零自研爬虫；新增 `queue_ended` 事件；废除 ~1,400 行人工魔数，改行 Need-based 零冗余原则；端到端套件扩至 12 块 232 项 | **连续性下沉到播放器而非 Agent**：Agent 一次性编排歌单后即可离场，队列由 mpv 内存自驱动，随进程生灭；规模由需要决定，而非由数字决定。 |
| **Stage 4** | `v2.0.0` | 2026-10-06 | 视觉感知完全缺席，长视频技术研读遭遇物理泥潭；系统命名与语义升维重锚 | 命名全域蜕变升维为 `co-jue`（`jue` / 觉），无须兼容彻底大改；交付原子动词 `jue frame` 单帧视觉感知（One-shot mpv `--vo=image` 零常驻内存）；独立进程组与负 PID 广播 SIGKILL 统一收割；lavfi 双边等比外框滤镜（横竖屏均为 ~690 Tokens）；0700 沙箱与同步机会式 GC；端到端物理回归套件扩充至 13 块 255+ 项全绿 | **命名是容器，能力是流体**；从听觉单维扩展为视听觉知全模态；感知平面与执行平面动静彻底隔离。 |
| **Stage 5** | `v2.1.0` | 2026-10-07 | 认知信源扩展：跨平台结构化转录协议、免 Cookie 规范与海外顶级 IT/AI 信源拓扑落地 | 泛化 WebVTT/SRT 5 状态 FSM 清洗；下沉 Podcast 2.0 RSS 与 ID3v2 章节解包；落地方案 A 纯日常浏览器 Cookie 透传（零落盘文件）；全量端到端扩展至 267 项断言 | **开放协议标准化下沉，动态风控坚守底线**；静态协议入标准库，会话缓存留浏览器，只读凭证库绝不污染。 |

---

## 版本更新日志 (SemVer Releases)

## [2.1.0] - 2026-10-07

**跨平台结构化转录：WebVTT/SRT 通用清洗、Podcast 2.0 RSS 与 ID3v2 章节；浏览器 Cookie 透传（落地 RESEARCH-transcript-protocols-and-sources 阶段 1–3）。**

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
