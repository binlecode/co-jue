# RESEARCH —— ting 重构调研：从双面终端播放器走向 100% Agentic 视听感知与端侧播放适配层

调研日期：2026-10-03。  
对标生产一手来源：`public-clis/bilibili-cli`（Agent CLI 契约、Token 极简 YAML、字幕优先降级与 16kHz mono WAV ASR 切片架构）、`aidevops/yt-dlp` 与 `ytfetch-mcp`（Agent 面向 yt-dlp 的元数据裁剪与紧凑抽象规范）、`python-mpv-jsonipc` / `spotuify` / `termusic`（Headless 常驻 mpv JSON-IPC 守护进程、纳秒级套接字控制与状态外推）、Anthropic 官方 Agent Tool 设计规约（`writing-effective-tools-for-ai-agents.md`：Token 效率、上下文预算控制、消除内部实现泄漏）。  
我方工程范围：`workspace_fullstack/ting/`（重构目标：退役 TUI 与旧 shell 脚本，重塑为轻量引擎工具）、`workspace_genai/co-cli`（Tool 注册面与执行调度）、`workspace_genai/co-s2s`（实时语音感知与环境音反馈）、`co-library`（`30-resources/` 媒体知识资产摄取与闭环）。

---

## 1. 调研背景与第一性原理重锚

### 1.1 核心痛点与历史假设破产

`ting` 在初创期立足于“人机双界面”（既给人类作为终端 TUI 播放器，又面向有 Shell 能力的 Coding Agent 提供单行 JSON 信封 CLI 契约）。为了在受限环境中达成这一定位，系统施加了极其苛刻的架构约束：
- 冻结在 bash 3.2 运行基准；
- 零新增运行时依赖（严格锁定 yt-dlp、jq、mpv、nc、curl 五大工具）；
- 历史架构为平级无内核、一站一脚本（旧 shell/ 脚本群）；
- Go TUI 被强制作为无头 CLI 的纯受限调用方（旧内部结构），严禁直接连接 mpv 或处理站点行为。重构后统一收敛至 [main.go:1-20](cmd/ting/main.go#L1-L20 "::@ba3f392a") 与 [verbs.go:16-30](cmd/ting/verbs.go#L16-L30 "::@d559dfbd")。

随着代码演进，这套双向妥协导致系统规模膨胀到 24,000 余行代码（13,000 行 bash 3.2 脚本 + 11,000 行 Go TUI 代码），并引发了严重的“双向平庸与架构死锁”：
1. **对人类用户（GUI/TUI 面）而言不好用**：
   - 界面动作均需跨越 Go 到 Bash 的子进程生成、参数组装与 JSON 信封解析，产生天然的 IPC 延迟；
   - 恪守弱风控与无私有 API 原则，导致个人歌单、算法日推、高清音质全面缺席；
   - 缺乏现代桌面播放器的系统级集成（如 macOS NowPlaying 控制中心、蓝牙耳机切歌线控、全局媒体快捷键）。
2. **对 Agent（Tool 面）而言不够用**：
   - Agent 真正需要的是**视听多模态感知与知识摄取**（内容是什么、时间戳定位、字幕/ASR 提取、关键论点摘录并沉淀进知识库）；
   - `ting` 供给的却是重度的“脱离终端后台播放生命周期控制”（围绕 `player_id`、socket 锁、墓碑回收展开的机械控制）；
   - 输出未针对 LLM 上下文做 Token 预算剪裁，缺乏语义检索，缺乏音频切片与语音模型适配。

### 1.2 第一性原理裁决：进入“可抛弃客户端软件”时代

在 Agentic 前沿（以 Claude Code、OpenCode、co-cli 为代表），用户交互范式已经发生根本性跃迁：**Agent 本身就是用户端侧的完整播放交互界面（The Agent is the UX）**。
- 人类不再需要打开一个专门的 TUI 终端窗口去用方向键浏览媒体列表；
- 人类直接通过自然语言（终端对话或 `co-s2s` 语音）向 Agent 表达意图：“放点专注的背景音乐”、“听一下这个李德毅院士的具身智能演讲，第 15 分钟讲了什么”；
- Agent 负责意图理解、调度检索、信息提炼、呈现交互式卡片、并在后台调度播放；
- **重构结论**：**彻底剥离独立客户端软件属性（删去全部 TUI 渲染与旧壳脚本），ting 100% 转型为专注服务于 Agent 的底层视听感知与端侧播放适配层（Agentic Tool / API Client on top of yt-dlp & mpv）。**

---

## 2. 生产级机制拆解与关键指标

结合对标的四大生产实践（`bilibili-cli`、`yt-dlp` agent tools、`python-mpv-jsonipc`、Anthropic Tool Guidelines），提炼出三大核心工业级机制：

```
+-----------------------------------------------------------------------------------+
|                        Human (自然语言/语音交互: "放点写代码的慢歌")                 |
+-----------------------------------------------------------------------------------+
                                          |
                                          v
+-----------------------------------------------------------------------------------+
|                      The Agent (co-cli / Claude / OpenCode)                       |
|                          * 唯一的端侧播放 UX 呈现层 *                             |
|  - 意图识别与上下文调度                                                            |
|  - 呈现轻量 Markdown 媒体卡片 / 实时播放状态反馈                                  |
|  - 知识提炼并写入 co-library 30-resources/                                         |
+-----------------------------------------------------------------------------------+
                      |                                        |
                      | 1. 感知与摄取 (Perception)              | 2. 执行与播放 (Actuation)
                      v                                        v
+-----------------------------------------------------------------------------------+
|                           ting (Agentic Media Tool)                               |
|                     yt-dlp 之上的 Agent 视听感知与播放适配层                      |
|                                                                                   |
|   【感知平面: Ingestion Plane】               【执行平面: Playback Plane】         |
|   - 极简 Token 预算投影 (YAML/紧凑 JSON)      - 单实例常驻 mpv JSON-IPC Daemon   |
|   - 结构化章节解析 (Chapters)                 - 毫秒级 Unix Socket 异步控制       |
|   - 三级转录漏斗 (字幕 -> AI摘要 -> ASR切片)   - macOS MPNowPlayingInfoCenter 桥接 |
+-----------------------------------------------------------------------------------+
             |                                                |
             v                                                v
+-----------------------------+               +-------------------------------+
|    yt-dlp / 平台直接接口    |               |       mpv (Headless Daemon)   |
| (多源解流、字幕提取、元数据) |               |  (本地声卡输出、无终端、低内存) |
+-----------------------------+               +-------------------------------+
```

### 2.1 机制 1：Token 极简主义与多级转录漏斗 (Token-Budgeted Cognitive Funnel)

在 LLM 工具链中，直接暴露原语输出是致命的反模式。以 `yt-dlp -j` 为例，单条视频的完整 JSON dump 体积在 50KB 至 300KB 之间，一次工具调用就会吞噬 15,000 ~ 80,000 Tokens，迅速造成上下文污染与成本失控。

生产标准架构（参考 `bilibili-cli`）建立了一套确定性的多级过滤漏斗：
1. **极简元数据投影**：
   Agent 检索仅需 6 个核心字段：`id`, `title`, `duration`, `uploader`, `publish_date`, `chapters`。输出严格采用紧凑 YAML（非 TTY 默认），相比展开的 JSON 节省 **35%~50%** 的 Token 消耗。
2. **章节感知 (Chapter-Aware Navigation)**：
   长视频/音频优先提取 `chapters` 列表（时间戳区间 + 章节标题）。这为 Agent 提供了低成本全局路标，Agent 可据此决定是全文转录还是仅针对特定时间段提取内容。
3. **三级转录梯级 (Transcript Cascade)**：
   - **L1 官方/自动字幕**：优先提取纯净文本或带秒级时间戳的段落，剥除字体大小、颜色、屏幕坐标等冗余标记；
   - **L2 平台 AI 摘要降级**：当字幕缺失或超长时，抓取平台原生的总结（如 B 站 AI 摘要），以 <400 Tokens 获得全局理解；
   - **L3 音频切片与 ASR 降级**：若前两者皆无，利用 `ffmpeg` 快速抓取音频流并按指定长度（如 25s~60s）切片为 16kHz mono WAV，交由本地 Whisper 或 Agent 语音模型处理。

### 2.2 机制 2：单实例常驻 Headless Daemon 与 JSON-IPC (Single-Instance mpv Daemon)

旧 `ting` 架构中采用 detached 子进程模型：每次播放启动一个独立的 `mpv` 进程，由 shell 脚本维护 PID、状态目录与 socket 文件。这带来了沉重的工程负债：
- 进程冷启延迟达 200~500ms；
- macOS 平台上 `nc -U` 频繁出现由于管道未及时释放引发的挂死或僵尸状态；
- 多播放器并发存在竞争与不可靠的锁文件状态。

生产标准架构（参考 `spotuify`, `termusic`, `python-mpv-jsonipc`）：
1. **单实例无头常驻**：
   单台机器仅维持一个后台 `mpv --idle --no-video --input-ipc-server=<sock>` 实例；
2. **纯 Unix Domain Socket JSON-IPC 通信**：
   由 Go 或 Python 控制器直接与 socket 通信（不通过 `nc` 中转），发送标准 JSON-RPC 命令：
   ```json
   {"command": ["loadfile", "https://..."], "request_id": 1}
   {"command": ["set_property", "pause", true]}
   {"command": ["seek", 120, "absolute"]}
   {"command": ["get_property", "time-pos"]}
   ```
   单次指令响应时间缩短至 **<5ms**，内存开销稳定在 20MB ~ 30MB 之间；
3. **系统媒体键与 NowPlaying 桥接**：
   通过轻量 macOS 原生框架（如 Objective-C/Swift 桥接 `MPNowPlayingInfoCenter` 与 `MPRemoteCommandCenter`），将当前曲目信息推送到系统控制中心，支持通过物理键盘媒体键或 AirPods 触控直接控制暂停/切歌，彻底补足人机体验短板。

### 2.3 机制 3：Agent-as-UX 交互范式 (The Agent is the Player Interface)

在丢弃独立 GUI/TUI 之后，播放与收听的交互心流全部收拢到 Agent 的会话流中：

| 用户操作 / 语音输入 | Agent 行为 | 调用的 ting 动词 | Agent 呈现给人类的 UX |
|---|---|---|---|
| “放点轻柔的写代码音乐” | 意图识别，搜索并筛选最匹配单曲 | `ting search -q "coding lofi" --max 3` ➔ `ting play <id>` | 呈现精简媒体卡片（曲名、UP主、时长），告知已在后台播放 |
| “跳到第 10 分钟” | 解析时间偏移，执行绝对跳转 | `ting seek 600` | 简短确认：“已跳转至 10:00 [第三章: 架构设计]” |
| “刚才讲的核心观点是什么？” | 读取当前播放头前后文本切片并解析 | `ting status` ➔ `ting transcript <id> --range <t-60, t+60>` | 自然语言直接总结回答用户的业务问题 |
| “把这篇演讲的重点记下来” | 获取全文摘要或字幕，整理格式 | `ting transcript <id>` | 生成标准 Markdown 资源页，写入 `co-library/30-resources/` |
| “停一下 / 声音小点” | 调节播放器属性 | `ting pause` / `ting volume -10` | 瞬时生效，状态反馈 |

---

## 3. 与我方现状（Current Implementation）的差距矩阵

| 架构维度 | 当前 ting 实现 (As-Is) | 目标重构形态 (To-Be) | 演进差距与技术根因 |
|---|---|---|---|
| **核心定位** | 人机双界面（TUI + CLI）双重妥协 | 100% 面向 Agent 的 API/Tool 适配层 | 摆脱人类客户端 GUI/TUI 负债，Agent 独占 UX 层 |
| **代码规模** | ~24,000 行（1.3w 行 bash + 1.1w 行 Go） | 预计 <2,500 行单一现代语言（Go/Python） | 砍去 90% 的渲染逻辑与 shell 边缘语法胶水 |
| **呈现层** | 复杂的 Bubbletea 单视图、舞台模式、多栏布局 | **零原生 UI**，完全由 Agent 生成 Markdown 卡片与语音反馈 | 彻底根除 ANSI 比对、终端 resize、CJK 双倍宽排版税 |
| **播放进程模型** | 每次起 detached mpv 进程 + bash `nc -U` 轮询 | 单实例常驻 Headless mpv Daemon + 本地 JSON-IPC | 消除进程冷启延迟、文件锁冲突与 macOS nc 挂死风险 |
| **站点解析边界** | 过去三个独立的 bash 脚本处理站点解析与进程锁 | 统一收敛至 Go 标准库 [ingest.go:40-60](internal/engine/ingest.go#L40-L60 "::@c5d2be96") 与 [daemon.go:20-40](internal/engine/daemon.go#L20-L40 "::@5922a528") 原生内核 | 消除 bash 3.2 下手工拼接 curl/openssl/jq 的维护地狱 |
| **Token 消耗意识** | 单行 JSON 包装，未做 LLM 上下文预算裁剪 | 紧凑 YAML / 结构化投影，严格限制 Token 上限 | 解决 Agent 连续调用时容易撑爆 Context 的隐患 |
| **多模态感知深度** | 仅有基础的 `--transcript` 文本输出 | 章节定位 + 多级字幕 + 16kHz mono WAV ASR 预切片 | 使 Agent 具备真正的视听长程认知与细粒度寻址能力 |
| **系统级集成** | 零系统集成，无媒体中心与按键支持 | 支持 macOS MPNowPlayingInfoCenter / MPRIS | 让后台播放具备操作系统原生外设与快捷键控制能力 |

---

## 4. 选型权衡与落地实施建议（指导后续 PLAN-）

### 4.1 技术栈选型决议：Go 静态单二进制 vs Python CLI 工具

| 维度 | 方案 A：Go 单二进制（推荐） | 方案 B：Python CLI 工具（如 uv tool / pipx） |
|---|---|---|
| **分发与运行时** | **静态单二进制**，无 Python 环境与虚拟环境开销，开箱即用 | 需 Python 3.10+，通过 `uv` 管理虚拟环境 |
| **mpv IPC 通信** | 通过标准库 `net.Dial("unix", sock)` 原生实现，高并发极速 | 需引入第三方库（如 `python-mpv-jsonipc`） |
| **yt-dlp 集成** | 作为外部子进程受控执行（带流式解析与超时控制） | 可作为 Python 包直接 `import yt_dlp` 进程内调用 |
| **ASR 与音频切片** | 调外部 `ffmpeg` 执行切片 | 可借由 `PyAV` / `soundfile` 原生处理 |
| **权衡裁决** | **采纳方案 A（Go 单二进制）**。`ting` 仓已有成熟的 Go 工具链与构建脚本；Go 单二进制分发极致干净，符合无常驻容器与单机极简哲学；`yt-dlp` 与 `ffmpeg` 保持为系统级 CLI 原语，职责清晰。 |

### 4.2 清退与保留清单

- **彻底清退 (Delete)**：
  - `cmd/ting` 及 `internal/tui/` 全部源码（~11,000 行 Go TUI 相关代码）；
  - `shell/` 目录下全部 6 个 bash 3.2 脚本（`ting-play`, `ting-playlist`, `ting-history`, `ting-engine-*`，~13,000 行）；
  - 相关的 TUI 自动化驱动测试（`tests/drive.sh`、`capture-pane` 相关脚本）。
- **保留与重构 (Keep & Refactor)**：
  - 精华抽离：将 `ting-engine-*` 中积累的核心 UA、Referer、网易云直链抽取逻辑与 B 站 WAF 防御规则，重写为 Go 内部模块；
  - 核心保留：`yt-dlp` 与 `mpv` 依然作为底层执行原语；
  - 新建构件：
    - `cmd/ting/`: 纯 API/CLI 统一入口；
    - `internal/daemon/`: mpv IPC 常驻守护进程管理；
    - `internal/ingest/`: 章节提取、字幕结构化过滤、ASR 切片；
    - `internal/format/`: Token 预算裁剪与 YAML/紧凑 JSON 输出引擎；
    - `SKILL.md`: 开箱即用的 Agent Skill 规范，包含 Agent 驱动播放与媒体分析的标准 Prompt 与调用指引。

### 4.3 实施路线三阶段规划 (Phase Roadmap)

1. **第一阶段：契约重立与底层 IPC 验证（P0）**
   - 编写 `internal/daemon`，实现 Go 对无头 `mpv --idle` 的常驻拉起与 Unix Domain Socket JSON-IPC 控制（play/pause/seek/status/volume）；
   - 输出第一版极简 CLI 动词（`ting play`, `ting pause`, `ting seek`, `ting status`），实测单次控制延迟降至 <10ms。
2. **第二阶段：多源感知与 Token 紧凑投影（P1）**
   - 接入 `yt-dlp` 紧凑调用包装，实现对 YouTube / Bilibili / 播客源的元数据与章节抽取；
   - 落地三级字幕与 ASR 切片逻辑（`ting transcript`, `ting chunk`），实现紧凑 YAML 输出；
   - 编写根目录 `SKILL.md`，规范化 Agent Tool 契约。
3. **第三阶段：历史负债清理与生态联通（P2）**
   - `git rm` 彻底清空 `shell/` 与旧 `internal/tui/`，消除 2.4 万行历史负债；
   - 将 `ting` 接入 `workspace_genai/co-cli` 工具面，在真实日常 Coding 会话中完成“人类说话 - Agent 调度 - 终端播放”的端到端闭环验证。
