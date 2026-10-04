---
name: ting
description: Drive ting as the agent's audio-visual perception and playback peripheral — read a video's chapters and verbatim transcript to answer timestamped questions, play music or talks in the background, and pause/resume/seek/stop/query the playhead. Sources are YouTube, Bilibili and NetEase Cloud Music. Trigger words: ting, play music, background music, listen to, what did the video say at, transcript, chapters, pause, resume, now playing, 听歌, 放首歌, 背景音乐, 字幕, 第几分钟讲了什么, 暂停, 继续, 现在放到哪.
---

# ting —— Agent 的视听感知与播放外设

ting 是专为 Agent 设计的端侧微外设：
- **输入端（感知）**：把网络媒体压缩为极简、可核验的事实（`<100 Tokens` 的章节路标、`<500 Tokens` 的逐字字幕）；
- **输出端（声卡）**：在后台无头播放流媒体并支持毫秒级时空遥测与控制。

**Agent 是唯一的呈现层**：用户看到的是你整理的几行 Markdown 卡片或原话问答，不是 ting 的 JSON。

---

## 快速入门

```sh
ting inspect <url>                               # 看：极简提取章节时间轴 (Chapters) 与时长
ting transcript <url> [--range 14:00-18:00]       # 读：纯净逐字原话证据 (支持秒数或分秒相交切片)
ting play <url> [--start 60]                     # 放：后台起播 (单实例 mpv，已等待声音就绪)
ting status                                      # 查：时空遥测 (在播状态、当前秒数头、总时长、音量)
ting control pause | resume | stop               # 控：即时静音、恢复或停止并退出
ting control seek +30 | seek -15 | seek 14:05    # 跳：+N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
ting control volume 60                           # 调：音量 (0-100)
```

- **全命令默认输出单行紧凑 JSON**；
- **四级退出码**：
  - `0`：成功；
  - `1`：参数错误或用法不对；
  - `2`：外部工具（`mpv`/`yt-dlp`）缺失或网络错误；
  - `4`：业务未就绪（如 B 站无公开字幕报 `unavailable`、播放器未在播放时执行控制）。
- **`status` 的 `state`**：`idle` / `loading`（已换曲、尚无播放头，还没出声）/ `playing` / `paused`；新的 `play` 总会解除暂停。
- **300 条上限**：`transcript` 无论是否带 `--range`，超过 300 条只返回前 300 条并标 `truncated: true`，此时收窄时间窗再取。

---

## Agent 核心策略

1. **章节路标优先**：长视频/音频先用 `ting inspect` 拿到全局 Chapters 列表（消耗 <100 Tokens），先定位到问题属于哪一章，再进行局部切片。
2. **时间窗切片取证**：读字幕严禁用全文读入，必须使用 `--range <start-end>`（格式支持 `14:00-18:00` 或 `600-900`），只取目标 3~5 分钟的逐字原话，严格把上下文消耗锁定在 500 Tokens 以内。
3. **事实证据优先**：回答用户“视频里说了什么”必须引用 `transcript` 的逐字原句并标明时间戳。若返回 `unavailable`（退出码 4），如实告知用户该视频无公开字幕，**绝不凭空编造内容**。
4. **单实例自动互斥**：`ting play` 内部采用 Flock 文件锁，每次起播会自动平滑替换上一次的播放，并已在底层等待真实解码发声（`file-loaded`）就绪，无需 Agent 额外写轮询等待代码。
5. **物理逃生直通**：底层 `mpv` 已开启系统媒体键支持，用户按 macOS 媒体键 (F8) 或捏按 AirPods 耳机即可即时硬件暂停，跳过大模型交互延迟。

---

## 典型工作流

### a) 视频研读与时间戳问答

用户：“这个演讲第 14 分钟讲了什么？” / “把这期视频的核心观点整理进知识库。”

1. 执行 `ting inspect <url>` 获取章节路标与总时长，锁定问题处于哪个章节；
2. 执行 `ting transcript <url> --range 14:00-18:00` 提取该窗口的逐字原话；
3. **回答用户**：先给出高层概括，再引用 1~2 条带时间戳的关键原话（如 `[14:25] "架构的核心在于状态与事件解耦"`），并附带跳转时间链接；
4. **沉淀到 co-library**：如果用户需要留存，按照标准资源格式整理（来源 URL、作者、章节大纲、核心论据与原话证据），经 `/brain-storm` 审议后写入 `~/co-library/30-resources/`。

### b) 背景伴随听歌

用户：“放点适合写代码的背景音。” / “来首周杰伦的晴天。”

1. Agent 利用自身的 `web_search` 或直链获取目标媒体 URL（支持 YouTube、B站、网易云音乐）；
2. 执行 `ting play <url>` 起播（已等待声音就绪）；
3. 提取结果，在回复中呈现精简的 Markdown 播放卡片（不超过 2 行）：
   ```markdown
   **正在播放** · 晴天 — 周杰伦 · 网易云音乐
   说“暂停 / 快进 30 秒 / 停”即可控制，也可直接按 AirPods 耳机暂停
   ```
4. 用户说“停一下”或“大点声”，执行 `ting control pause` 或 `ting control volume 70`。

### c) 时空同步追问

用户：“等等，他刚才那句话说了什么？” / “现在播放到哪里了？”

1. 执行 `ting status` 拿到当前播放头精准秒数（例如 `time_pos: 845.2`）；
2. 计算前后窗口（`840-855`），执行 `ting transcript <url> --range 840-855`；
3. 自然语言回答用户刚才听到的那一句话具体是什么，并询问是否需要跳转回去重新听。
