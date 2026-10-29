# CLAUDE.md / AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) and all coding agents working in this repository.

## ⚠️ 关键底线 —— 开工前必看

开工前必须明确以下五条硬底线，**违反会导致进程权限异常、契约破坏或工程漂移**：

- 🔴 **纯 Go 标准库与单静态二进制**：全仓代码仅使用 Go 标准库，零 cgo（`CGO_ENABLED=0` 保证可编译），零第三方外部包。代码规模遵循 **Need-based 零冗余原则**：不设人工行数魔数，每一行以功能必要性为准绳，新增能力必须证明无法由既有原语或 Agent 自身承担，拒绝投机性抽象与冗余代码。
- 🔴 **双外部原语依赖**：外部依赖严格锁定为两个（`yt-dlp` 与 `mpv`；可选 `deno` 作为 yt-dlp 的 JS 运行时）。严禁引入任何额外外部二进制或 C 库。
- 🔴 **四级退出码与机器信封**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法；
  - `2`：外部依赖缺失（未装 mpv 或 yt-dlp）、网络底层错误；
  - `4`：业务未就绪（如媒体不存在/下架/无可用字幕 `unavailable`、上游风控频控阻断 `error`、播放器未在播放时执行控制）。
  - 默认输出单行紧凑 JSON，严禁静默修改既有信封字段。
- 🔴 **状态隔离与目录安全**：运行时 Socket 严格收容在 `$TMPDIR/jue-<uid>/`，目录权限必须为 `0700`、属主等于自身 UID、严禁为符号链接。临时产物一律限在 `tmp/` 下，严禁污染源码树。
- 🔴 **本地闭环 CI/CD（本地能做的，绝不推给 GitHub）**：全生命周期的测试、语法静态分析、SemVer 校验、多架构静态编译构建、打包发布与 Skill 同步一律通过本地工具链（`.githooks/`、`install.sh`、`scripts/release.sh`、`tests/test_suite.sh`）闭环完成。**严禁引入 `.github/workflows/` 等远端臃肿 CI**，不将本地可完全胜任的任务推到 GitHub Actions 虚拟机上浪费资源。
- 🔴 **文档分类与 Changelog 归位**：严格遵循全域规约（`ARCH-doc-taxonomy.md`）。系统演进历史、版本更新日志与阶段全表（Stage chronicles）一律归入根目录 `CHANGELOG.md`，严禁在 `ARCHITECTURE.md` 或 `docs/` 尾部以附录堆积历史日志。

---

## 🔴 第一条：不要自作聪明

**有疑问或做技术选型时，严格按两步执行，顺序不可颠倒：**

1. **先 grounding** —— 查外部平台（YouTube / Bilibili / 网易云）最新端点、核查 `yt-dlp` 与 `mpv` 实际 IPC 行为、通读本地代码与测试套件，**深入代码实现与真实运行输出，严禁凭空假设向下推演**。
2. **再确认** —— 严禁静默新增/废弃动词、修改信封字段、私自放宽门控。若实测推翻了前提，如实报送发现并提问。

---

## 项目性质

**co-jue**（CLI 二进制命令为 `jue`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的端侧视听感知与播放微外设（Go 静态单二进制，收容于 `~/workspace_genai/co-jue/`）。版本严格遵循 SemVer 语义化规范（单一真源声明于根目录 `VERSION`；演进全表与更新日志见 `CHANGELOG.md`）。

- **感知平面 (Ingest)**：
  - `jue inspect <url>`：极简提取章节时间轴（Chapters）与元数据，Token 开销 < 100 Tokens；对本地媒体文件、本地回环 HTTP 流（如 `qnap-media`）或 `.m3u8` 直链通过瞬态无头 mpv 提取真实 `duration`、容器 `chapters` 与音视频/字幕轨道 `tracks`；
  - `jue transcript <url> [--range START-END]`：提取声称级逐字原话证据（防机翻污染、带相交判定裁剪、超出 300 条自动截断；支持直链/同目录外挂 `.srt/.vtt/.ass/.ssa/.lrc` 及本地/回环容器内封文本字幕轨），无字幕如实返回 `unavailable`（退出码 4）；
  - `jue events [--until <EVENT>] [--timeout SEC]`：毫秒级事件感知面（曲目放毕、AirPods 触控、章节切换、`--until queue_ended` 整个队列放毕、`audio_device_changed` 音频输出拓扑变化），彻底根除轮询 Token 开销；
  - 显式检索前缀：`inspect` / `transcript` / `play` / `queue add` 均接受 `ytsearch1:<关键词>`（白名单仅 `ytsearch1:` 与 `ytsearch:`，其余检索前缀退出码 1），复用 yt-dlp 原生检索，零自研爬虫。
- **执行平面 (Daemon)**：
  - `jue play <url> [--start SEC] [--video]`：Flock 互斥与 Lazy-start 托管单实例后台 mpv（默认无头 `--no-video`；传 `--video` 时通过条目级 `vid=auto` 动态弹出原生视频窗口，切回音频自动销毁窗口），替换整个队列，超时等待声音就绪（`file-loaded`），返回 mpv 解析后的规范 URL；
  - `jue queue add <url|query> [--video] | list | clear`：mpv 原生瞬态内存播放列表（零持久化，随 mpv 生灭）；空闲时 add 即起播，播放中 add 追加立返；clear 只清待播；
  - `jue control <pause|resume|seek|volume|stop|next|prev>`：毫秒级确定性控制，`next`/`prev` 越界返回退出码 4 `unavailable`，`stop` 后 mpv 自动优雅退出，不留僵尸守护；
  - `jue control duck [on|off] [--duration SEC] [--level 0-100] [--fade MS]`：声学闪避，Agent 说话时压低音乐；只写 mpv 独立增益级 `volume-gain`（与 `volume` 正交），渐变/保持/30s 看门狗全在 mpv 内嵌 Lua 助手 `jue_duck.lua`（`embed` 内嵌、`launch()` 写入运行时目录 0600 并 `--script=` 加载）里，CLI 发一条 `script-message-to` 立返；助手就绪标记 `user-data/jue/duck-helper` 缺失或协议不符为退出码 4 `unavailable`；
  - `jue status`：时空同步遥测（返回播放头秒数、状态、总时长、音量；视频窗口开启时带 `"video":true`，闪避中另带瞬时电平 `duck`）。
- **记忆平面 (History)**：
  - `jue history [list] [--limit N] | last | clear`：持久播放历史。`play` / `queue add` 成功后各记一条 `{timestamp, action:"play"|"queue_add", input, url, title, duration}`（`input` 为原始入参如 `ytsearch1:…`，`url` 为解析结果；排队未加载项 `title:""`、`duration:0`）。落盘 `$XDG_STATE_HOME/jue/history.jsonl`（未设或非绝对路径回落 `~/.local/state/jue/`），目录 0700/属主/非符号链接（与运行时目录同一门禁 `privateDir`），文件 0600、`O_NOFOLLOW` 读、temp+rename 原子重写、`history.lock` 串行化并发写，上限 500 条裁旧，坏行跳过。`list` 最新在前、默认 20 条、`--limit` 取 1–500；空历史 `last` 退出码 4 `unavailable`；读与 `clear` 不创建目录。记录失败一律吞掉，绝不让已出声的播放报错。
- **物理逃生口**：
  - mpv 默认开启 `--input-media-keys=yes`，原生直通 macOS 键盘与 AirPods 耳机暂停键，跳过大模型延迟。

---

## 常用命令

```sh
# 本地一键安装与全局 Agent Skill 部署校验
./install.sh
./install.sh --uninstall

# 本地多架构交叉编译与发布打包 (支持 --publish 配合 local gh CLI 直发 Release)
./scripts/release.sh

# 语法与静态检查 + Go 单元测试（纯逻辑与协议解析，离线，< 1s）
go vet ./... && go test ./...
go build -o jue ./cmd/jue

# 端到端契约与工作流测试（真实 jue / mpv / yt-dlp / 外网，零 Mock，需 jq）
bash tests/test_suite.sh

# 核心动词抽检
./jue inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
./jue transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30
./jue play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18
./jue status
./jue control pause | resume | volume 50 | duck on | duck off | next | stop
./jue queue add "ytsearch1:Never Gonna Give You Up"
./jue queue list
./jue events --until queue_ended --timeout 600
```

---

## 测试分层

- **单元层** `cmd/jue/verbs_test.go` & `internal/engine/*_test.go`：`verbs_test.go`（`VERSION` SemVer 单一真源校验、`parseDuck` 语法与边界、九个动词 `TestVerbUsageGates` 在清空 `PATH`/`TMPDIR`/`XDG_STATE_HOME` 下逐项退出码 1 且不留状态）、`ingest_test.go`（ParseRange/ParseTime、json3/VTT/SRT/ASS/LRC/Podcast 2.0/ID3v2 解析、网易云 id、pickTrack 语言链、检索前缀白名单、容器内封与外挂字幕）、`frame_test.go`（参数与检索前缀门禁、GC 30 项门限与进程存活/防饥饿、`firstEntryGuard` 歌单首项截断、超时强杀与进程组回收、流缓存冷/热回退与回环直连）、`cache_test.go`（0700/0600 与符号链接拒收、TTL 过期与 TOCTOU 指纹删除、正证据单流解析表）、`ipc_test.go`（request_id 匹配、事件入队、`WaitForPlaybackSuccess` 状态机）、`daemon_test.go`（runtimeDir 0700/宽权限/符号链接门禁、只读动词不落盘、`play`/`queue`/`control`/`status` 线协议与并发同锁、`stop` 同步等待对端挂断）、`events_test.go`（snapshot 与初值压制、`start-file` 刷新 URL、`queue_ended`、`audio_device_changed` 基线与排序指纹）、`duck_test.go`（助手 0600 原子写入与符号链接防御、就绪标记与电池/空闲看门狗令牌、三模式线协议）、`history_test.go`（倒序列表与 `--limit`、500 条原子裁旧、并发零丢失、坏行/超长行跳过与重写净化、0700/0600/FIFO/符号链接门禁、历史不可写不影响播放）。
- **端到端层** `tests/test_suite.sh`：按六大能力平面编排 15 个并行隔离块（共 304 项断言）。**Boundary**：A 安全边界与各动词代表性用法闸门 · F Token 预算（章节与字幕信封字节上限、越界窗口为空）。**Ingest**：B 感知契约（章节、窗口、300 条截断、纯净无标签文本、B 站/网易云纯音乐 `unavailable` 与 `inspect` 兜底、LRC）· K 显式检索解析（`ytsearch1:` 全动词解析与非白名单前缀拦截）· M 视觉帧感知（0700/0600、纯音频/封面 MP3 拒收、真实流单帧、流缓存冷/热、本地 HTTP 容器 `inspect`/外挂字幕、外框等比缩放、零残留）。**Playback**：C 播放状态机与声学人机（本地 + 网络音频、起播即停 200ms 回收与暂停 200ms 冻结、全部 control、kill -9 死 socket 恢复）· D 并发（6 路冷/热 `play` 竞态与 6 路冷启动 `queue add` 竞态，同锁单 mpv 零丢失）· J 瞬态队列状态机 · L 队列放毕（`--until queue_ended`）· O 持久历史。**Acoustic**：N 声学闪避（脉冲/持续/恢复、单进程采样渐变、音量正交、−96 dB 地板、非有限值防御、零重配置、跨曲/跳转/暂停保持、旧助手/协议不符、符号链接防御、AC→Battery 拔电自动退出）· NW 闪避 30s 生产看门狗。**Events**：I 事件契约。**Agent workflows**：E-a 多模态章节研读（`inspect` → `transcript --range` → `frame --at`）· E-b 实时播放头原话锚定与倒带续播（`play` → `pause` → `transcript` → `seek` → `resume` → `stop`）。

---

## 架构要点

- **短命 CLI + 单实例 Daemon**：`jue play` 在后台唤醒 `mpv --idle=yes --no-video --input-media-keys=yes`，其余命令通过 Unix Domain Socket 发送标准 JSON 指令。
- **request_id 解交错与事件缓冲**：mpv 事件流与命令回执解耦，`WaitForPlaybackSuccess` 确保两阶段事件就绪，杜绝伪成功。
- **统一锁 `flockAction`**：`play` 与 `queue add` 在同一把 `jue.lock` 内完成拨号/拉起、状态探查（`playlist-pos == -1` 即空闲）与指令分发，锁外等待出声。
- **检索非对称分流**：传给 mpv 补 `ytdl://` 前缀（`normalizeForMPV`），传给 yt-dlp 剥离（`normalizeForYtdlp`）。
- **零 UI 负债**：没有 TUI，没有自研搜索爬虫，没有本地数据库（队列只是 mpv 内存播放列表；唯一持久状态是 500 条封顶的 `history.jsonl` 追加日志，不建索引、不做歌单），Agent 就是唯一的交互呈现层。
