---
name: ting
description: "Audio-visual perception & playback peripheral for AI agents in the co ecosystem (co-ting). Read video chapters and verbatim transcripts to answer timestamped questions; stream audio in background via mpv. Trigger: ting, co-ting, play music, background music, transcript, chapters, pause, resume, 听歌, 放首歌, 背景音乐, 字幕, 第几分钟讲了什么."
---

# ting —— Agent 的视听感知与播放外设

ting 是专为 Agent 设计的端侧微外设：
- **输入端（感知）**：把网络媒体压缩为极简、可核验的事实（`<100 Tokens` 的章节路标、`<500 Tokens` 的逐字字幕）；
- **输出端（声卡）**：在后台无头播放流媒体并支持毫秒级时空遥测与控制。

**Agent 是唯一的呈现层**：用户看到的是你整理的几行 Markdown 卡片或原话问答，不是 ting 的 JSON。

---

## 快速入门

```sh
ting --version                                   # 查：版本信息 (严格遵循 SemVer)
ting inspect <url>                               # 看：极简提取章节时间轴 (Chapters) 与时长
ting transcript <url> [--range 14:00-18:00]       # 读：纯净逐字原话证据 (支持秒数或分秒相交切片)
ting play <url> [--start 60]                     # 放：后台起播 (单实例 mpv，替换整个队列，已等待声音就绪)
ting queue add <url|query>                       # 排：空闲时即起播；在播时追加到队尾并立即返回
ting queue list                                  # 排：查看队列 (pos 当前下标、count 总数、items)
ting queue clear                                 # 排：清空待播，当前曲目继续
ting status                                      # 查：时空遥测 (在播状态、当前秒数头、总时长、音量)
ting control pause | resume | stop               # 控：即时静音、恢复或停止并退出 (stop 连同队列一起销毁)
ting control next | prev                         # 切：下一首 / 上一首 (已等待新曲出声；越界退出码 4)
ting control seek +30 | seek -15 | seek 14:05    # 跳：+N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
ting control volume 60                           # 调：音量 (0-100)
ting events [--until <EVENT>] [--timeout SEC]    # 听：事件感知面 (单次阻塞 until 或管道流式；支持 track_started|track_ended|paused|resumed|chapter_changed|queue_ended)
```

**显式检索前缀**：手里没有 URL 时，在 `play` / `queue add` / `inspect` / `transcript` 的 `<url>` 位置写 `ytsearch1:<关键词>`，由 yt-dlp 在 YouTube 取第一条结果：

```sh
ting play "ytsearch1:周杰伦 晴天"                     # 检索并起播；信封 url 返回解析后的规范 watch URL
ting queue add "ytsearch1:周杰伦 七里香"              # 检索并排队；排队项 url 显示为 ytdl://ytsearch1:…，轮到它时才解析
ting inspect "ytsearch1:Karpathy tokenizer"          # 检索并取章节
ting transcript "ytsearch1:Never Gonna Give You Up" --range 18-30
```

- 只认 `ytsearch1:`（及等价的 `ytsearch:`），关键词必须加引号；`ytsearch5:`、`scsearch:` 等其它检索前缀一律退出码 1；
- 前缀必须显式写出：裸关键词（如 `ting play 晴天`）会被当成本地文件路径，返回退出码 4；
- 检索无结果返回退出码 4 `unavailable`；
- 只覆盖 YouTube。B 站、网易云等其它站点仍需 Agent 自行拿到直链。

- **全命令默认输出单行紧凑 JSON**；
- **四级退出码**：
  - `0`：成功；
  - `1`：参数错误或用法不对；
  - `2`：外部工具（`mpv`/`yt-dlp`）缺失或网络错误；
  - `4`：业务未就绪（如 B 站无公开字幕报 `unavailable`、检索无结果、`next`/`prev` 越界报 `end of playlist`/`start of playlist`、播放器未在播放时执行控制或 `queue clear`）。
- **`status` 的 `state`**：`idle` / `loading`（已换曲、尚无播放头，还没出声）/ `playing` / `paused`；新的 `play`、起播的 `queue add` 与 `control next|prev` 都会解除暂停。
- **`queue add` 的 `state`**：`playing`（播放器原本空闲，已起播并等到出声，`pos:0, count:1`）或 `queued`（已追加到队尾，立即返回）。
- **队列是瞬态的**：它就是 mpv 的内存播放列表，`stop` 或 mpv 退出即消失，不落盘；全部放完后队列视为空，再 `add` 从头开始。`queue list` 里尚未轮到的条目没有 `title`，只有 `current` 项带 `current: true`。
- **300 条上限**：`transcript` 无论是否带 `--range`，超过 300 条只返回前 300 条并标 `truncated: true`，此时收窄时间窗再取。

---

## Agent 核心策略

1. **章节路标优先**：长视频/音频先用 `ting inspect` 拿到全局 Chapters 列表（消耗 <100 Tokens），先定位到问题属于哪一章，再进行局部切片。
2. **时间窗切片取证**：读字幕严禁用全文读入，必须使用 `--range <start-end>`（格式支持 `14:00-18:00` 或 `600-900`），只取目标 3~5 分钟的逐字原话，严格把上下文消耗锁定在 500 Tokens 以内。
3. **事实证据优先**：回答用户“视频里说了什么”必须引用 `transcript` 的逐字原句并标明时间戳。若返回 `unavailable`（退出码 4），如实告知用户该视频无公开字幕，**绝不凭空编造内容**。
4. **单实例自动互斥**：`ting play` 内部采用 Flock 文件锁，每次起播会自动平滑替换上一次的播放，并已在底层等待真实解码发声（`file-loaded`）就绪，无需 Agent 额外写轮询等待代码。
5. **物理逃生直通**：底层 `mpv` 已开启系统媒体键支持，用户按 macOS 媒体键 (F8) 或捏按 AirPods 耳机即可即时硬件暂停，跳过大模型交互延迟。
6. **歌单连播交给队列**：用户要多首连续播放时，一次性用 `ting queue add` 把整组曲目压进队列（第一首起播，其余排队），由 mpv 自行接力，Agent 输出卡片后即可结束本轮，无需挂在后台等待曲毕。`play` 会替换整个队列，往已有歌单里加歌必须用 `queue add`。
7. **事件感知与续添**：严禁用死循环轮询 `status`（消耗 Token 并产生迟滞）。需要在队列放完时续添新歌，调用 `ting events --until queue_ended` 单次阻塞等待；只关心单曲边界时用 `--until track_ended`（`reason: "eof"` 为自然放毕）。
8. **检索前缀按需使用**：用户只给歌名或主题时，可直接用 `ytsearch1:<歌手 歌名>` 起播或排队，省掉一轮 `web_search`；需要 B 站/网易云源，或对结果准确性要求高时，仍应先确认直链。起播后以信封返回的规范 URL 为准写入卡片。

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

1. 有直链（YouTube、B站、网易云音乐、本地文件）就用直链；只有歌名时直接用 `ting play "ytsearch1:周杰伦 晴天"`；
2. 执行 `ting play <url>` 起播（已等待声音就绪，信封 `url` 为解析后的规范地址）；
3. 提取结果，在回复中呈现精简的 Markdown 播放卡片（不超过 2 行）：
   ```markdown
   **正在播放** · 晴天 — 周杰伦 · 网易云音乐
   说“暂停 / 快进 30 秒 / 停”即可控制，也可直接按 AirPods 耳机暂停
   ```
4. 用户说“停一下”或“大点声”，执行 `ting control pause` 或 `ting control volume 70`。

### c) 时空同步追问

用户：“等等，他刚才那句话说了什么？”

1. 执行 `ting status` 拿到当前播放头精准秒数（例如 `time_pos: 845.2`）；
2. 计算前后窗口（`840-855`），执行 `ting transcript <url> --range 840-855`；
3. 自然语言回答用户刚才听到的那一句话具体是什么，并询问是否需要跳转回去重新听（若需要则 `ting control seek -15` 并 `resume`）。

### d) 章节路标与定向分章点播

用户：“这个演讲分哪几部分？把第三章放给我听。”

1. 执行 `ting inspect <url>` 获取章节路标与总时长，呈现紧凑大纲；
2. 获取目标章节起始时间（如 `start: 1125`），执行 `ting play <url> --start 1125` 直接定位起播；
3. 输出播放卡片，告知正在播放该章节并可随时快进/跳转。

### e) 播控穿梭与进度探查

用户：“快进 30 秒” / “跳到 14 分 05 秒” / “现在播到哪了，还剩多久？”

1. 跳转：执行 `ting control seek +30` / `seek -15`（相对）或 `seek 14:05` / `seek 0`（绝对）；
2. 进度查询：执行 `ting status`，提取 `time_pos` 与 `duration`，输出人性化进度卡片。

### f) 歌单连播

用户：“放几首适合写代码的周杰伦，连着放。”

1. Agent 选好曲目，逐条执行 `ting queue add`：第一条返回 `state: "playing"`（已出声），其余返回 `state: "queued"` 并立即返回：
   ```sh
   ting queue add "ytsearch1:周杰伦 晴天"
   ting queue add "ytsearch1:周杰伦 七里香"
   ting queue add "ytsearch1:周杰伦 稻香"
   ```
2. 输出一张卡片（当前曲目 + 队列长度，不超过 3 行），本轮结束；mpv 自动接力放完整个队列；
3. 用户说“下一首 / 上一首”，执行 `ting control next` / `prev`；返回 `end of playlist` 时如实告知已是最后一首；
4. 用户问“后面还有什么”，执行 `ting queue list`，按 `pos` 与 `items` 列出待播；
5. 若用户要求放完后自动续添，执行 `ting events --until queue_ended` 阻塞等待，命中后再 `queue add` 新一组。

### g) 队列管理

用户：“把这首加到后面” / “后面的都不要了” / “换成这首，别的都清掉”。

1. 加到后面：`ting queue add <url|query>`（不打断当前曲目）；
2. 清掉待播只留当前：`ting queue clear`；
3. 整体替换：`ting play <url|query>`（替换整个队列并立即起播）；
4. 全部停止：`ting control stop`（队列随 mpv 一起销毁）。

---

*完整交互场景与话术矩阵详见 [`docs/USER_MANUAL.md`](docs/USER_MANUAL.md)。*
