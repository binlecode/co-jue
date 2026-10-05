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
  - `4`：业务未就绪（如媒体无可用字幕 `unavailable`、播放器未在播放时执行控制）。
  - 默认输出单行紧凑 JSON，严禁静默修改既有信封字段。
- 🔴 **状态隔离与目录安全**：运行时 Socket 严格收容在 `$TMPDIR/ting-<uid>/`，目录权限必须为 `0700`、属主等于自身 UID、严禁为符号链接。临时产物一律限在 `tmp/` 下，严禁污染源码树。
- 🔴 **本地闭环 CI/CD（本地能做的，绝不推给 GitHub）**：全生命周期的测试、语法静态分析、SemVer 校验、多架构静态编译构建、打包发布与 Skill 同步一律通过本地工具链（`.githooks/`、`install.sh`、`scripts/release.sh`、`tests/test_suite.sh`）闭环完成。**严禁引入 `.github/workflows/` 等远端臃肿 CI**，不将本地可完全胜任的任务推到 GitHub Actions 虚拟机上浪费资源。
- 🔴 **文档分类与 Changelog 归位**：严格遵循全域规约（`ARCH-doc-taxonomy.md`）。系统演进历史、版本更新日志与阶段全表（Stage chronicles）一律归入根目录 `CHANGELOG.md`，严禁在 `ARCHITECTURE.md` 或 `docs/` 尾部以附录堆积历史日志。

---

## 🔴 第一条：不要自作聪明

**有疑问或做技术选型时，严格按两步执行，顺序不可颠倒：**

1. **先 grounding** —— 查外部平台（YouTube / Bilibili / 网易云）最新端点、核查 `yt-dlp` 与 `mpv` 实际 IPC 行为、通读本地代码与测试套件，**深入代码实现与真实运行输出，严禁凭空假设向下推演**。
2. **再确认** —— 严禁静默新增/废弃动词、修改信封字段、私自放宽门控。若实测推翻了前提，如实报送发现并提问。

---

## 项目性质

**co-ting**（CLI 二进制命令为 `ting`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的端侧视听感知与播放微外设（Go 静态单二进制，收容于 `~/workspace_genai/co-ting/`）。版本严格遵循 SemVer 语义化规范（声明于根目录 `VERSION`，如 `1.2.0`；演进全表与更新日志见 `CHANGELOG.md`）。

- **感知平面 (Ingest)**：
  - `ting inspect <url>`：极简提取章节时间轴（Chapters）与元数据，Token 开销 < 100 Tokens；
  - `ting transcript <url> [--range START-END]`：提取声称级逐字原话证据（防机翻污染、带相交判定裁剪、超出 300 条自动截断），无字幕如实返回 `unavailable`（退出码 4）；
  - `ting events [--until <EVENT>] [--timeout SEC]`：毫秒级事件感知面（曲目放毕、AirPods 触控、章节切换、`--until queue_ended` 整个队列放毕），彻底根除轮询 Token 开销；
  - 显式检索前缀：`inspect` / `transcript` / `play` / `queue add` 均接受 `ytsearch1:<关键词>`（白名单仅 `ytsearch1:` 与 `ytsearch:`，其余检索前缀退出码 1），复用 yt-dlp 原生检索，零自研爬虫。
- **执行平面 (Daemon)**：
  - `ting play <url> [--start SEC]`：Flock 互斥与 Lazy-start 托管单实例后台无头 mpv，替换整个队列，超时等待声音就绪（`file-loaded`），返回 mpv 解析后的规范 URL；
  - `ting queue add <url|query> | list | clear`：mpv 原生瞬态内存播放列表（零持久化，随 mpv 生灭）；空闲时 add 即起播，播放中 add 追加立返；clear 只清待播；
  - `ting control <pause|resume|seek|volume|stop|next|prev>`：毫秒级确定性控制，`next`/`prev` 越界返回退出码 4 `unavailable`，`stop` 后 mpv 自动优雅退出，不留僵尸守护；
  - `ting status`：时空同步遥测（返回播放头秒数、状态、总时长、音量）。
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
go build -o ting ./cmd/ting

# 端到端契约与工作流测试（真实 ting / mpv / yt-dlp / 外网，零 Mock，十二块 A–L 并行约 11s，需 jq）
bash tests/test_suite.sh

# 核心动词抽检
./ting inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
./ting transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30
./ting play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18
./ting status
./ting control pause
./ting control resume
./ting control volume 50
./ting queue add "ytsearch1:Never Gonna Give You Up"
./ting queue list
./ting control next
./ting events --until queue_ended --timeout 600
./ting control stop
```

---

## 测试分层

- **单元层** `internal/engine/*_test.go`：`ingest_test.go`（ParseRange/ParseTime、json3 去重清洗、LRC 时间推导、网易云 id、pickTrack 语言链、检索前缀白名单与头部锚定、检索 playlist 外壳 `entries[0]` 解包与空结果）、`ipc_test.go`（request_id 匹配、事件入队、`WaitForPlaybackSuccess` 的加载成功/失败/被顶替/redirect/超时/任意条目 `-1`）、`daemon_test.go`（runtimeDir 0700 放行，宽权限/符号链接/普通文件拒收，只读动词不落盘；`queue add` 空闲起播/播放中追加/并发同锁、`queue list` 信封与空闲不落盘、`queue clear`、`next`/`prev` 及越界、play 返回规范 URL）、`events_test.go`（snapshot 与初值压制、`start-file` 刷新 URL、`queue_ended` 触发/stop 不触发/空闲拒收/中途退出）。IPC 测试在内存管道另一端扮演 mpv 的线协议。
- **端到端层** `tests/test_suite.sh`：A 安全边界 · B 感知契约 · C 播放状态机（本地 + 网络音频、全部 control、stop 后进程回收、kill -9 后恢复）· D 冷/热并发 play 争抢 · E 六条人/Agent/ting 工作流（a 章节研读、b 背景听歌卡片、c 暂停追问播放头附近原话、d 自动续播 DJ、e AirPods 触控即时倒带、f 双语歌词精读）· F Token 预算（信封无多余字段/转义/浮点噪声，越界窗口返回空）· G 声学人机（起播即停 200ms 内回收，暂停 200ms 内播放头冻结）· H 落地为资产（带 `&t=` 时间戳的逐字引用块；无字幕/纯音乐时 inspect 兜底）· I 事件感知契约（推流信标、snapshot、eof 触发、边沿差分去抖、零僵尸守护）· J 瞬态队列生命周期（冷/热 add、list 信封、next/prev 及越界、暂停态切歌自动解除暂停、clear 保留当前曲、play 替换整队、6 路并发冷启动 add 单 mpv 零丢失、空闲 list 不落盘）· K 显式检索解析（`ytsearch1:` 起播返回规范 watch URL、inspect/transcript 检索解包、非白名单前缀 exit 1、`search_query` 普通 URL 不误判、非法 URL/缺失文件契约回归）· L 队列放毕感知（`--until queue_ended` 捕获、空闲 exit 4、stop 中断 `player exited`）。共 12 块 232 项断言。各块独立 `TMPDIR`、各自的 mpv，并行执行；网络音频前先 `volume 0`，套件全程静音。退出时只按本套件 socket 路径回收 mpv 并删除临时目录，最后一行断言零残留。

---

## 架构要点

- **短命 CLI + 单实例 Daemon**：`ting play` 在后台唤醒 `mpv --idle=yes --no-video --input-media-keys=yes`，其余命令通过 Unix Domain Socket 发送标准 JSON 指令。
- **request_id 解交错与事件缓冲**：mpv 事件流与命令回执解耦，`WaitForPlaybackSuccess` 确保两阶段事件就绪，杜绝伪成功。
- **统一锁 `flockAction`**：`play` 与 `queue add` 在同一把 `ting.lock` 内完成拨号/拉起、状态探查（`playlist-pos == -1` 即空闲）与指令分发，锁外等待出声。
- **检索非对称分流**：传给 mpv 补 `ytdl://` 前缀（`normalizeForMPV`），传给 yt-dlp 剥离（`normalizeForYtdlp`）。
- **零 UI 负债**：没有 TUI，没有自研搜索爬虫，没有本地数据库（队列只是 mpv 内存播放列表），Agent 就是唯一的交互呈现层。
