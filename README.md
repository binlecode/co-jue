# ting (听)

**ting** —— 面向 AI Agent（Claude Code、OpenCode、co-cli）的轻量端侧视听感知与播放微外设（Go 静态单二进制，零 cgo，零第三方库）。版本遵循 SemVer 规范，由根目录 `VERSION` 统一定义（当前版本：`1.1.0`）。

进入“可抛弃客户端软件”时代后，ting 彻底废除所有 TUI/GUI 客户端界面与搜索推荐包袱，**让 Agent 成为唯一的端侧播放 UX**。ting 仅作为 Agent 挂载在宿主机上的两件基础外设：
1. **输入端（感知外设）**：在 `yt-dlp` 之上提供极简字段投影与章节索引，提供可核验的逐字事实证据（严格防范 Token 爆炸与机翻污染）；
2. **输出端（声卡驱动）**：通过 Flock 互斥与单实例无头 `mpv` 守护进程，提供毫秒级确定性播放控制与时空同步遥测，并原生打通 macOS 键盘与 AirPods 耳机暂停键。

---

## 快速使用

```sh
# 1. 视听感知平面 (Ingest)
ting inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"           # 提取章节与元数据 (<100 Tokens)
ting transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ"        # 提取逐字原话证据 (带相交判定切片)
ting transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30

# 2. 端侧播放执行平面 (Actuation)
ting play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18   # 后台起播 (自动 Lazy-start 托管单实例 mpv)
ting status                                                         # 时空遥测 (当前播放头、状态、音量)
ting control pause                                                  # 暂停
ting control resume                                                 # 继续
ting control seek +30                                               # +N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
ting control volume 60                                              # 音量调节
ting control stop                                                   # 停止并自动退出 mpv
```

---

## 架构原则

- **极简细腰 (~800–1,000 行纯 Go)**：全仓仅保留 `internal/engine/` 与 `cmd/ting/`，零 cgo，零外部框架依赖，单一静态二进制直接编译执行。
- **双通道控制闭环**：
  - **语义与编排通道**：人类与 Agent 自然语言对话（意图解析、精准段落问答、知识整理写入 `co-library 30-resources/`）；
  - **物理控制通道**：AirPods 耳机触控与 macOS 媒体键 (F8) 由 `mpv --input-media-keys=yes` 原生接管，即按即停，跳过大模型延迟。
- **Token 护盾**：原始 90KB+ 媒体元数据在进程内提炼为 <1KB 的紧凑结构化输出；字幕（含带 `--range` 的宽窗口）超出 300 条自动截断并标记 `truncated: true`。
- **四级退出码契约**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法；
  - `2`：外部依赖缺失（未装 `mpv` 或 `yt-dlp`）、底层网络中断；
  - `4`：业务未就绪（如媒体无公开字幕 `unavailable`、播放器空闲时执行控制）。

---

## 一键安装与部署

```sh
# 一键原子安装（自动编译 Go 二进制至 ~/bin/ting，并注入全局 Agent Skill）
./install.sh

# 一键卸载
./install.sh --uninstall
```

---

## 构建与测试

```sh
# 本地编译二进制
go build -o ting ./cmd/ting

# Go 单元测试（离线，< 1s）
go vet ./... && go test ./...

# 端到端契约与工作流测试套件（真实驱动 mpv 与网络端点，零 Mock，~11s）
bash tests/test_suite.sh
```

---

## 架构正本与规范

- **系统架构正本**：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- **第一性原理调研**：[`docs/RESEARCH-agent-media-engine-refactor.md`](docs/RESEARCH-agent-media-engine-refactor.md)
- **Agent 交互手册**：[`docs/USER_MANUAL.md`](docs/USER_MANUAL.md) · [认知击穿幻灯版](docs/USER_MANUAL_SLIDES.html)
- **Agent Skill 规范**：[`SKILL.md`](SKILL.md)
