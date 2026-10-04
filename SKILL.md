---
name: ting
description: Drive ting as the agent's audio-visual perception and playback peripheral — read a video's chapters and verbatim transcript to answer timestamped questions, play music or talks in the background, and pause/resume/seek/stop/query the playhead. Sources are YouTube, Bilibili and NetEase Cloud Music. Trigger words: ting, play music, background music, listen to, what did the video say at, transcript, chapters, pause, resume, now playing, 听歌, 放首歌, 背景音乐, 字幕, 第几分钟讲了什么, 暂停, 继续, 现在放到哪.
---

# ting —— Agent 的视听感知与播放外设

ting 对 Agent 来说是两件外设：**输入端**把一条媒体压成小而可核验的事实（章节路标、逐字字幕），**输出端**在后台放声并接受控制。Agent 是唯一的呈现层：用户看到的是你写的几行 Markdown，不是 ting 的 JSON。

本文件对应 `docs/PLAN-agentic-media-plane-refactor.md` 的 Step 0：只用现有 CLI（`ting-play`），不改一行代码。命令与参数的正本是 `ting-play --help`，本文件只讲怎么用、为什么这样用。

## 快速入门

```sh
ting-play --search -j -n 5 -- "lofi hip hop"          # 找（缺省 YouTube；--engine bili|ne 换源）
ting-play --info -j -- <id|URL>                       # 看：标题、时长、章节
ting-play --transcript -j -- <id|URL>                 # 读：逐字字幕 / 歌词全文
ting-play -d -j [--start SEC] -- <id|URL>             # 放：后台起播，立即返回
ting-play --status -j                                 # 查：在播什么、放到哪
ting-play --pause | --resume | --stop  [--id ID] -j   # 控
ting-play --seek +30 | --seek-to 600   [--id ID] -j   # 跳（相对必须带符号）
```

- 命令都在 `PATH` 上（Homebrew 安装）；在本仓库开发时用 `shell/ting-play` 跑工作树版本。
- **永远带 `-j`**：单行 JSON 信封，`status` 字段先看。
- 句柄是 URL 或站点 id。YouTube 11 位 id 直接可用；B 站与网易云**传完整 URL**（或加 `--engine bili|ne`），否则会被缺省引擎当成坏的 YouTube id 拒掉。
- 退出码：`0` 成功；`1` 用法错，或该引擎根本没有这个动词（改参数，不要重试）；`2+` 外部工具 / 网络失败（可重试一次）；`4` 语义未生效 —— 如没有在播的 player（`not_playing`）、有多个 player 而没给 `--id`（`ambiguous`）、player 还在加载（`ipc_failed`）。

## Agent 默认策略

1. **小结果集**。搜索默认 `-n 5`，最多 10；给用户看的是 3–5 行候选，不是信封。
2. **先投影再入上下文**。信封里有大字段：`--info` 的 `description` 动辄数 KB，`--transcript` 的全文十几 KB，`--segments` 版本是全文的两三倍。一律经 `jq` 只取要的字段，**不要把原始信封整个读进上下文**：

   ```sh
   ting-play --search -j -n 5 -- "$q" | jq -c '.results[] | {id, title, url, channel, duration_fmt}'
   ting-play --info -j -- "$u" | jq -c '{id, title, channel, duration, chapters: [.chapters[]? | {s: .start_time, t: .title}]}'
   ```

3. **字幕证据优先**。回答“视频里说了什么”必须引用 `--transcript` 的原句并带时间戳；没有字幕就如实说没有，**不要凭标题、简介或常识编造内容**。摘要是你的事，ting 只出逐字证据。
4. **按时间窗取字幕，不读全文**。要某一段时用 `--segments` 并在 `jq` 里按区间相交裁剪（窗口 ≤ 5 分钟，一段约 1KB）：

   ```sh
   ting-play --transcript -j --segments -- "$u" | jq -r --argjson s 840 --argjson e 900 '
     .segments[] | select(.start < $e and .start + .duration > $s)
     | "[\(.start | floor / 60 | floor):\(.start | floor % 60 | tostring | if length < 2 then "0" + . else . end)] \(.text)"'
   ```

   整体问题（“这期讲了什么”）先用章节定位，再按章取段；只有短视频（`chars` < 5000）才读 `.text` 全文。
5. **一次只留一个 player**。起播前看 `--status`：已有活的 player 而用户要的是“换一首”，先 `--stop --id <旧ID>` 再起新的；用户明确要叠放才保留。之后所有控制都带上起播信封里的 `--id`，避免多 player 时 `ambiguous`。
6. **起播是异步的**。`-d` 在零点几秒内返回 `status: "started"`，此时还没出声；`--status` 里该 player 的 `position` 从 `null` 变成数字才算真正在放（YouTube 实测数秒）。起播后要立刻控制或报“正在播放”，先轮询到这个信号（每 0.5 秒一次，上限约 30 秒），别靠 `sleep` 猜。
7. **控制前不必先查状态**。直接发 `--pause` 等，看退出码与 `status`；`4` 就把原因翻译给用户（“现在没有在放的东西”），不要重试。

## 典型工作流

### a) 视频研读与时间戳问答

用户：“这个演讲第 14 分钟讲了什么？” / “把这期视频的要点整理一下。”

1. `--info` 投影出标题、时长、章节，找到问题落在哪一章（章节有 `start_time` / `end_time`）。
2. `--transcript --segments` 按该章或问题时间点前后的窗口裁剪，读原句。
3. 回答：先给结论，再引用 1–3 条原句并标时间戳，附可跳转链接（YouTube 用 `&t=<秒>s`）。
4. 用户要“边听边看”时，`-d --start <秒>` 从那里起播。
5. **沉淀到 co-library**：用户要留存时，产出一份结构化笔记（来源 URL、标题、作者、章节路标、带时间戳的要点与原句引用），经 `/brain-storm` 或 library 自己的 `library-*` 技能写入 `~/co-library/30-resources/`。写入是持久动作，**先给用户看草稿、得到确认再写**；不要直接往 library 里写文件或全库 grep。

站点差异（实测）：

- **YouTube**：有人工字幕或自动字幕；信封 `is_auto` 标出是否为自动识别，自动字幕的引用要提醒用户可能有识别误差。`--sub-lang` 可指定语言。
- **Bilibili**：没有公开字幕轨，`--transcript` 直接以 `1` 拒绝。改用 `--info` 的章节与简介，并明说“B 站这条没有字幕证据”。
- **网易云**：`--transcript` 给的是歌词（`lang` 为 `null`），时间轴是歌词行；纯音乐没有歌词。

### b) 背景伴随听歌

用户：“放点适合写代码的音乐。” / “来首周杰伦。”

1. 把意图改写成搜索词，`--search -n 5` 并投影；音乐优先 `--engine ne`，长时段背景音（lofi、白噪音、现场）用 YouTube，必要时 `--min-duration 1800`。
2. 直接挑最合适的一条，不必让用户选（用户要求“给我几个选”时才列候选）。
3. 按策略 5 处理旧 player，`-d -j` 起播，记下 `id`；按策略 6 等到 `position` 非空。
4. 回一张轻量卡片，然后回到用户手头的工作：

   ```markdown
   **正在播放** · 晴天 — Jay · 01:52 · 网易云
   [打开原页面](https://music.163.com/song?id=3440441479) · 说“暂停 / 下一首 / 停”即可控制
   ```

   卡片 2 行以内；不贴缩略图、不贴 JSON、不报 socket 路径与 pid。

想连续播放多首时，把搜索信封直接喂给队列：`ting-play --search -j -n 5 -- "$q" | ting-play -d --queue - -j`，之后 `--next` 切歌。

### c) 物理控制与时空同步

用户：“暂停一下” / “刚才那句再放一遍” / “现在放到哪了？”

- **暂停 / 继续 / 停止 / 音量**：`--pause`、`--resume`、`--stop`、`--set-volume N`，带 `--id`。
- **查时间头**：`--status -j` 投影出播放头，再换算成 `mm:ss`：

  ```sh
  ting-play --status -j | jq -c '.players[] | {id, title, url, position, duration, paused, volume}'
  ```

  `players` 为空就是没有在放。`position` 是整数秒。
- **时空同步问答**（“刚才他说的那个词是什么”）：取 `position`，对 `url` 拉 `[position-30, position+5]` 窗口的字幕，引用原句回答。这是 ting 让 Agent 与用户“在同一时刻”的关键用法。
- **回放 / 跳转**：“再放一遍”用 `--seek -15`；“跳到讲反向传播那里”先用章节或字幕找到秒数，再 `--seek-to <秒>`。信封里的 `position` 是 mpv 实际落点。

用户也可能用耳机或键盘媒体键直接暂停，所以每次回答播放状态前都以 `--status` 为准，不要沿用你上一轮记下的状态。

## 边界

- ting 不做推荐、歌单策划、摘要；这些是你的事。歌单与历史另有 `ting-playlist`、`ting-history`，用户问起时才用。
- 不要绕过 ting 直接调 `mpv` 或 `yt-dlp`：裸调会失去后台生命周期、控制通道与小信封。
- 交互式终端浏览器 `ting` 是给人用的 TUI，Agent 不启动它。
