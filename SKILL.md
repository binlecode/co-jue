---
name: jue
description: "Audio-visual perception & playback peripheral for AI agents in co ecosystem (co-jue). Extract visual frames, chapters, and verbatim transcripts; stream audio in background via mpv; duck music while agent speaks. Trigger: jue, frame, inspect, transcript, play/pause/duck music, 抽帧, 画面, 截图, 听歌, 放首歌, 字幕, 第几分钟讲了什么, 压低音乐, 刚才那首, 播放历史."
---

# jue —— Agent 的视听感知与播放外设

- **输入端（感知）**：把长音视频压缩为可核验事实（`inspect` 章节路标 `<100 Tokens`、`transcript --range` 逐字原话 `<500 Tokens`、`frame --at` 单帧画面 `~690 Tokens`）。
- **输出端（播控）**：单实例后台 `mpv` 播放流媒体，支持队列连播、历史回溯与毫秒级控制（**Agent 是唯一呈现层**，向人类只输出精简 Markdown 卡片，不倾倒原始 JSON）。

---

## 1. 命令速查

```sh
# 感知面 (One-shot 无状态)
jue inspect <url|ytsearch1:关键词>                  # 章节时间轴 (chapters)、时长与容器轨道 (tracks)
jue transcript <url|ytsearch1:关键词> [--range S-E] # 时间窗逐字字幕 (支持 14:00-18:00 或秒数；超 300 条自动截断)
jue frame <url|ytsearch1:关键词> --at <time> [--width N] # 单帧截图 (返回临时图片 path 供 read_image 读取)

# 播控与记忆面 (单实例后台守护)
jue play <url|ytsearch1:关键词> [--start SEC] [--video] # 起播并替换整队 (默认无头音频；--video 弹出原生视频窗口)
jue queue add <url|ytsearch1:关键词> [--video]      # 空闲即起播；在播追加队尾立返
jue queue list | clear                             # 查看队列 (current 项含 title) | 清空待播保留当前曲目
jue status                                         # 遥测 (state: idle|loading|playing|paused, url, time_pos, duration, volume)
jue control pause | resume | stop | next | prev    # 播控 (stop 销毁进程与队列；next/prev 越界退出码 4)
jue control seek <+SEC|-SEC|SEC|MM:SS> | volume <0-100> # 相对/绝对跳转 | 音量调节
jue history [list] [--limit N] | last | clear      # 持久播放历史 (跨 stop 保留最近 500 条；查当前曲名或重放上一首)

# 语音闪避与事件流 (主要供语音宿主或后台任务调用)
jue control duck [on|off] [--duration SEC] [--level 0-100] [--fade MS] # 压低音乐 volume-gain (与 volume 正交，30s 看门狗自恢复)
jue events [--until <EVENT>] [--timeout SEC]       # 阻塞等待事件 (track_started|track_ended|paused|resumed|chapter_changed|queue_ended|audio_device_changed)
```

- **检索与私有源**：无直链时在 `<url>` 传 `"ytsearch1:<关键词>"`（仅限 YouTube 首条，必须加引号）；B 站/网易云需先拿直链；家庭 NAS 媒体通过 `nas-media search|url` 转为本地 `http://127.0.0.1:8999/...` URL 后直接传入 `jue`。
- **四级退出码**：`0` 成功 · `1` 参数/用法错 · `2` 依赖或网络故障 · `4` 业务未就绪（无字幕/无视频轨/越界/未在播 `unavailable` 或 `not_playing`）。

---

## 2. Agent 核心工作流与策略

1. **视频研读（先章后词再图）**：先 `jue inspect <url>` 定位章节时间窗 → 再 `jue transcript <url> --range S-E` 取 3~5 分钟原话（严禁全文硬读；若 `truncated: true` 则收窄区间） → 按需 `jue frame <url> --at <time>`（字幕区间推荐取 `start + 0.6*(end-start)` 避开转场）并用 `read_image` 读图。
2. **点播、看片与歌单连播（感知 vs 播放、音频 vs 视频零歧义）**：
   - **真播禁假探**：用户要求“play / 播放 / 看片”时，必须直接调用 `jue play`（严禁用 `curl`/`urllib` 拉流测试替代实际起播）；仅在用户要求“查信息 / 字幕 / 抽帧分析”时才用 `inspect` / `transcript` / `frame`。
   - **视频必加 `--video`**：听歌/纯音频用默认无头 `jue play <url>`；只要播放目标是**电影、剧集、MV 或视频文件**（如 NAS `Movies/`、`Videos/` 下的 `.mp4/.mkv/.mov/.webm` 或用户明示看视频/看电影），`jue play` 与 `jue queue add` **必须显式传 `--video`** 弹出原生视频窗口。
   - **连播不轮询**：单曲/单片用 `jue play`；多首连播用 `jue queue add` 一次性入队由 mpv 自动接力，输出卡片后即结束当前轮次，**严禁在前景轮询 `status`**（需等待队列放毕续添时放后台调 `jue events --until queue_ended`）。
3. **查歌名与历史重放**：`jue status` 只含 `url` 与播放头 `time_pos`；需获取曲目 `title` 或回答“刚才放的是什么”时调 `jue history last`（在播查完整队列用 `jue queue list`），重放直接用返回的规范 `url`。
4. **物理逃生与语音让位**：用户可随时按 macOS 媒体键或 AirPods 硬件暂停；语音插话前调 `jue control duck on`、结束调 `jue control duck off`（切勿改 `volume`）。

---

## 3. 准出判据 (Acceptance Criteria)

- **1. 播控与卡片交付**：起播/入队以 JSON 信封 `status: "ok"` 及解析后的规范 `url` 为准（电影/视频点播必保信封含 `"video": true`），向人类仅输出 ≤ 2 行 Markdown 卡片（严禁倾倒原始 JSON 或后台轮询 `status`）。
- **2. 字幕与视觉验真**：台词/内容问答必引 `transcript --range` 逐字原话与时间戳（返回退出码 4 `unavailable` 时据实告知无字幕，0 脑补）；纯画面问答或抽帧对内用 `read_image` 读图并标明时间戳（`--at`），对外公网视频给外网时间戳链接、私有 NAS 媒体给时间戳与画面描述，**严禁在 Markdown 贴 `/var/folders/` 本地私有路径**。
- **3. 闪避恢复闭环**：语音插话场景下，`duck on` 结束后必保 `duck off` 已执行（或用带 `--duration` 的脉冲闪避），不遗留压低状态。
