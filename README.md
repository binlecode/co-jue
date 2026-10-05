# co-ting (ting)

**co-ting**（CLI 二进制命令为 `ting`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的轻量端侧视听感知与播放微外设（Go 静态单二进制，零 cgo，零第三方库，收容于 `~/workspace_genai/co-ting/`）。版本遵循 SemVer 规范，由根目录 `VERSION` 统一定义（当前版本：`1.2.0`）。

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
ting events --until track_ended                                      # 阻塞等待曲毕信号 (退出码 0，自动切歌/续播)
ting events --until queue_ended                                      # 阻塞等待整个队列放毕 (续添新歌单)
ting events                                                         # 管道流式监听事件 (snapshot / paused / resumed)

# 2. 端侧播放执行平面 (Actuation)
ting play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18   # 后台起播 (自动 Lazy-start 托管单实例 mpv，替换整个队列)
ting play "ytsearch1:周杰伦 晴天"                                   # 显式检索前缀起播 (返回解析后的规范 watch URL)
ting queue add "ytsearch1:周杰伦 七里香"                             # 空闲即起播，在播则追加到 mpv 瞬态内存队列
ting queue list                                                     # 查看队列 (pos / count / items)
ting queue clear                                                    # 清空待播，当前曲目继续
ting status                                                         # 时空遥测 (当前播放头、状态、音量)
ting control pause                                                  # 暂停
ting control resume                                                 # 继续
ting control next                                                   # 下一首 (prev 上一首；越界退出码 4)
ting control seek +30                                               # +N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
ting control volume 60                                              # 音量调节
ting control stop                                                   # 停止并自动退出 mpv
```

---

## 架构原则

- **极简细腰 (Need-based 零冗余)**：全仓仅保留 `internal/engine/` 与 `cmd/ting/`，零 cgo，零外部框架依赖，单一静态二进制直接编译执行；不设人工行数魔数，每一行以功能必要性为准绳。
- **零持久化、零自研爬虫**：队列只是 mpv 内存播放列表，随进程生灭；检索只接受显式 `ytsearch1:` 前缀并转交 yt-dlp 原生协议。
- **双通道控制闭环**：
  - **语义与编排通道**：人类与 Agent 自然语言对话（意图解析、精准段落问答、知识整理写入 `co-library 30-resources/`）；
  - **物理控制通道**：AirPods 耳机触控与 macOS 媒体键 (F8) 由 `mpv --input-media-keys=yes` 原生接管，即按即停，跳过大模型延迟。
- **Token 护盾**：原始 90KB+ 媒体元数据在进程内提炼为 <1KB 的紧凑结构化输出；字幕（含带 `--range` 的宽窗口）超出 300 条自动截断并标记 `truncated: true`。
- **四级退出码契约**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法、不支持的检索前缀；
  - `2`：外部依赖缺失（未装 `mpv` 或 `yt-dlp`）、底层网络中断；
  - `4`：业务未就绪（如媒体无公开字幕 `unavailable`、检索无结果、切歌越界、播放器空闲时执行控制）。

---

## 一键安装与部署

```sh
# 一键原子安装（编译并建立 ~/bin/{ting,co-ting} 双软链，并同步全局 Agent Skill 到 co-brain 与 ~/.agents/skills/）
./install.sh

# 一键卸载（自动清理 ~/bin 与全局 Skill 软链）
./install.sh --uninstall
```

---

## 构建与测试

```sh
# 本地编译二进制
go build -o ting ./cmd/ting

# Go 单元测试（离线，< 1s）
go vet ./... && go test ./...

# 端到端契约与工作流测试套件（真实驱动 mpv 与网络端点，零 Mock，12 块 A–L，~11s）
bash tests/test_suite.sh
```

---

## 架构正本与规范

- **系统架构正本**：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- **第一性原理调研**：[`docs/RESEARCH-agent-media-engine-refactor.md`](docs/RESEARCH-agent-media-engine-refactor.md)
- **演进全表与更新日志**：[`CHANGELOG.md`](CHANGELOG.md)
- **Agent 交互手册**：[`docs/USER_MANUAL.md`](docs/USER_MANUAL.md) · [认知击穿幻灯版](docs/USER_MANUAL-slides.html)
- **Agent Skill 规范**：[`SKILL.md`](SKILL.md)
