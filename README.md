# co-jue (jue)

**co-jue**（CLI 二进制命令为 `jue`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的轻量端侧视听感知与播放微外设（Go 静态单二进制，零 cgo，零第三方库，收容于 `~/workspace_genai/co-jue/`）。版本遵循 SemVer 规范，由根目录 `VERSION` 统一定义（当前版本：`2.1.0`）。

进入“可抛弃客户端软件”时代后，jue 彻底废除所有 TUI/GUI 客户端界面与搜索推荐包袱，**让 Agent 成为唯一的端侧播放 UX**。jue 仅作为 Agent 挂载在宿主机上的两件基础外设：
1. **输入端（感知外设）**：在 `yt-dlp` 与 `mpv --vo=image` 之上提供极简字段投影、章节索引与单帧视觉感知，提供可核验的时空原话与图像证据（严格防范 Token 爆炸与机翻污染）；
2. **输出端（声卡驱动）**：通过 Flock 互斥与单实例无头 `mpv` 守护进程，提供毫秒级确定性播放控制与时空同步遥测，并原生打通 macOS 键盘与 AirPods 耳机暂停键。

---

## 快速使用

```sh
# 1. 视听感知平面 (Ingest)
jue inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"           # 提取章节与元数据 (<100 Tokens)
jue transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ"        # 提取逐字原话证据 (带相交判定切片)
jue transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30
jue frame "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --at 73     # 瞬态抓取指定秒数视觉帧 (<80KB, ~690 Tokens)
jue events --until track_ended                                      # 阻塞等待曲毕信号 (退出码 0，自动切歌/续播)
jue events --until queue_ended                                      # 阻塞等待整个队列放毕 (续添新歌单)
jue events                                                         # 管道流式监听事件 (snapshot / paused / resumed)

# 2. 端侧播放执行平面 (Actuation)
jue play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18   # 后台起播 (自动 Lazy-start 托管单实例 mpv，替换整个队列)
jue play "ytsearch1:周杰伦 晴天"                                   # 显式检索前缀起播 (返回解析后的规范 watch URL)
jue queue add "ytsearch1:周杰伦 七里香"                             # 空闲即起播，在播则追加到 mpv 瞬态内存队列
jue queue list                                                     # 查看队列 (pos / count / items)
jue queue clear                                                    # 清空待播，当前曲目继续
jue status                                                         # 时空遥测 (当前播放头、状态、音量)
jue control pause                                                  # 暂停
jue control resume                                                 # 继续
jue control next                                                   # 下一首 (prev 上一首；越界退出码 4)
jue control seek +30                                               # +N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
jue control volume 60                                              # 音量调节
jue control stop                                                   # 停止并自动退出 mpv
```

---

## 架构原则

- **极简细腰 (Need-based 零冗余)**：全仓仅保留 `internal/engine/` 与 `cmd/jue/`，零 cgo，零外部框架依赖，单一静态二进制直接编译执行；不设人工行数魔数，每一行以功能必要性为准绳。
- **零持久化、零自研爬虫**：队列只是 mpv 内存播放列表，随进程生灭；检索只接受显式 `ytsearch1:` 前缀并转交 yt-dlp 原生协议。
- **双通道控制闭环**：
  - **语义与编排通道**：人类与 Agent 自然语言对话（意图解析、精准段落问答、知识整理写入 `co-library 30-resources/`）；
  - **物理控制通道**：AirPods 耳机触控与 macOS 媒体键 (F8) 由 `mpv --input-media-keys=yes` 原生接管，即按即停，跳过大模型延迟。
- **Token 护盾**：原始 90KB+ 媒体元数据在进程内提炼为 <1KB 的紧凑结构化输出；字幕（含带 `--range` 的宽窗口）超出 300 条自动截断并标记 `truncated: true`；默认双边等比外框滤镜使常见 16:9 视频 Token 预算对称收敛于约 690 Tokens。
- **四级退出码契约**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法、不支持的检索前缀；
  - `2`：外部依赖缺失（未装 `mpv` 或 `yt-dlp`）、底层网络中断；
  - `4`：业务未就绪（如媒体无公开字幕/无视频轨 `unavailable`、检索无结果、切歌越界、播放器空闲时执行控制）。

---

## 一键安装与部署

```sh
# 一键原子安装（编译并建立 ~/bin/jue 单一软链，并同步全局 Agent Skill 到 co-brain 与 ~/.agents/skills/）
./install.sh

# 卸载
./install.sh --uninstall
```

---

## 本地研发与测试验证

```sh
# 语法与静态检查 + 单元测试 (< 1s)
go vet ./... && go test ./...
go build -o jue ./cmd/jue

# 端到端契约与工作流测试 (并行测试套件)
bash tests/test_suite.sh
```
