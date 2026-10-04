# CLAUDE.md / AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) and all coding agents working in this repository.

## ⚠️ 关键底线 —— 开工前必看

开工前必须明确以下四条硬底线，**违反会导致进程权限异常或契约破坏**：

- 🔴 **纯 Go 标准库与单静态二进制**：全仓代码仅使用 Go 标准库，零 cgo（`CGO_ENABLED=0` 保证可编译），零第三方外部包。代码规模严格控制在 ~1,000 行内，保持极致轻量。
- 🔴 **双外部原语依赖**：外部依赖严格锁定为两个（`yt-dlp` 与 `mpv`；可选 `deno` 作为 yt-dlp 的 JS 运行时）。严禁引入任何额外外部二进制或 C 库。
- 🔴 **四级退出码与机器信封**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法；
  - `2`：外部依赖缺失（未装 mpv 或 yt-dlp）、网络底层错误；
  - `4`：业务未就绪（如媒体无可用字幕 `unavailable`、播放器未在播放时执行控制）。
  - 默认输出单行紧凑 JSON，严禁静默修改既有信封字段。
- 🔴 **状态隔离与目录安全**：运行时 Socket 严格收容在 `$TMPDIR/ting-<uid>/`，目录权限必须为 `0700`、属主等于自身 UID、严禁为符号链接。临时产物一律限在 `tmp/` 下，严禁污染源码树。

---

## 🔴 第一条：不要自作聪明

**有疑问或做技术选型时，严格按两步执行，顺序不可颠倒：**

1. **先 grounding** —— 查外部平台（YouTube / Bilibili / 网易云）最新端点、核查 `yt-dlp` 与 `mpv` 实际 IPC 行为、通读本地代码与测试套件，**深入代码实现与真实运行输出，严禁凭空假设向下推演**。
2. **再确认** —— 严禁静默新增/废弃动词、修改信封字段、私自放宽门控。若实测推翻了前提，如实报送发现并提问。

---

## 项目性质

**ting-gen-2** —— 面向 AI Agent（Claude Code、OpenCode、co-cli）的端侧视听感知与播放微外设（Go 静态单二进制）。

- **感知平面 (Ingest)**：
  - `ting inspect <url>`：极简提取章节时间轴（Chapters）与元数据，Token 开销 < 100 Tokens；
  - `ting transcript <url> [--range START-END]`：提取声称级逐字原话证据（防机翻污染、带相交判定裁剪、超出 300 条自动截断），无字幕如实返回 `unavailable`（退出码 4）。
- **执行平面 (Daemon)**：
  - `ting play <url> [--start SEC]`：Flock 互斥与 Lazy-start 托管单实例后台无头 mpv，超时等待声音就绪（`file-loaded`）；
  - `ting control <pause|resume|seek|volume|stop>`：毫秒级确定性控制，`stop` 后 mpv 自动优雅退出，不留僵尸守护；
  - `ting status`：时空同步遥测（返回播放头秒数、状态、总时长、音量）。
- **物理逃生口**：
  - mpv 默认开启 `--input-media-keys=yes`，原生直通 macOS 键盘与 AirPods 耳机暂停键，跳过大模型延迟。

---

## 常用命令

```sh
# 语法与静态检查
go vet ./... && go test ./...
go build -o ting ./cmd/ting

# 自动化契约与功能测试（真实驱动 mpv 与网络端点，零 Mock）
bash tests/test_gen2.sh

# 核心动词抽检
./ting inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
./ting transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30
./ting play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18
./ting status
./ting control pause
./ting control resume
./ting control volume 50
./ting control stop
```

---

## 架构要点

- **短命 CLI + 单实例 Daemon**：`ting play` 在后台唤醒 `mpv --idle=yes --no-video --input-media-keys=yes`，其余命令通过 Unix Domain Socket 发送标准 JSON 指令。
- **request_id 解交错与事件缓冲**：mpv 事件流与命令回执解耦，`WaitForPlaybackSuccess` 确保两阶段事件就绪，杜绝伪成功。
- **零 UI 负债**：没有 TUI，没有搜索算法，没有本地数据库，Agent 就是唯一的交互呈现层。
