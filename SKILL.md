---
name: jue
description: "Audio-visual perception & playback peripheral for AI agents in co ecosystem (co-jue). Extract visual frames, chapters, and verbatim transcripts; stream audio in background via mpv; duck music while agent speaks. Trigger: jue, frame, inspect, transcript, play/pause/duck music, 抽帧, 画面, 截图, 听歌, 放首歌, 字幕, 第几分钟讲了什么, 压低音乐, 刚才那首, 播放历史."
---

# jue —— Agent 的视听感知与播放外设

jue 是专为 Agent 设计的端侧微外设：
- **输入端（感知）**：把网络媒体压缩为极简、可核验的事实（`<100 Tokens` 的章节路标、`<500 Tokens` 的逐字字幕）；
- **输出端（声卡）**：在后台无头播放流媒体并支持毫秒级时空遥测与控制。

**Agent 是唯一的呈现层**：用户看到的是你整理的几行 Markdown 卡片或原话问答，不是 jue 的 JSON。

---

## 快速入门

```sh
jue --version                                   # 查：版本信息 (严格遵循 SemVer)
jue inspect <url>                               # 看：极简提取章节时间轴 (Chapters) 与时长
jue transcript <url> [--range 14:00-18:00]       # 读：纯净逐字原话证据 (支持秒数或分秒相交切片)
jue frame <url> --at <time> [--width N]         # 截：单帧视觉感知 (输出临时图片路径直通 Agent read_image)
jue play <url> [--start 60]                     # 放：后台起播 (单实例 mpv，替换整个队列，已等待声音就绪)
jue queue add <url|query>                       # 排：空闲时即起播；在播时追加到队尾并立即返回
jue queue list                                  # 排：查看队列 (pos 当前下标、count 总数、items)
jue queue clear                                 # 排：清空待播，当前曲目继续
jue status                                      # 查：时空遥测 (在播状态、当前秒数头、总时长、音量)
jue control pause | resume | stop               # 控：即时静音、恢复或停止并退出 (stop 连同队列一起销毁)
jue control next | prev                         # 切：下一首 / 上一首 (已等待新曲出声；越界退出码 4)
jue control seek +30 | seek -15 | seek 14:05    # 跳：+N / -N 相对跳转；不带符号为绝对位置 (秒数或 mm:ss)
jue control volume 60                           # 调：音量 (0-100)
jue control duck [--duration 5] [--level 20] [--fade 200]   # 让：脉冲闪避，压到 20%，5s 后开始以 2×fade 渐显 (立即返回)
jue control duck on [--level 20] [--fade 200]   # 让：持续闪避直至 off (30s 无续期自动恢复)
jue control duck off [--fade 400]               # 让：恢复原电平 (未闪避时为空操作，退出码 0)
jue history [list] [--limit N] | last | clear   # 忆：持久播放历史 (最新在前，默认 20 条，上限 500；last 空历史退出码 4)
jue events [--until <EVENT>] [--timeout SEC]    # 听：事件感知面 (单次阻塞 until 或管道流式；支持 track_started|track_ended|paused|resumed|chapter_changed|queue_ended|audio_device_changed)
```

**显式检索前缀**：手里没有 URL 时，在 `play` / `queue add` / `inspect` / `transcript` 的 `<url>` 位置写 `ytsearch1:<关键词>`，由 yt-dlp 在 YouTube 取第一条结果：

```sh
jue play "ytsearch1:周杰伦 晴天"                     # 检索并起播；信封 url 返回解析后的规范 watch URL
jue queue add "ytsearch1:周杰伦 七里香"              # 检索并排队；排队项 url 显示为 ytdl://ytsearch1:…，轮到它时才解析
jue inspect "ytsearch1:Karpathy tokenizer"          # 检索并取章节
jue transcript "ytsearch1:Never Gonna Give You Up" --range 18-30
```

- 只认 `ytsearch1:`（及等价的 `ytsearch:`），关键词必须加引号；`ytsearch5:`、`scsearch:` 等其它检索前缀一律退出码 1；
- 前缀必须显式写出：裸关键词（如 `jue play 晴天`）会被当成本地文件路径，返回退出码 4；
- 检索无结果返回退出码 4 `unavailable`；
- 只覆盖 YouTube。B 站、网易云等其它站点仍需 Agent 自行拿到直链。

- **全命令默认输出单行紧凑 JSON**；
- **四级退出码**：
  - `0`：成功；
  - `1`：参数错误或用法不对；
  - `2`：外部工具（`mpv`/`yt-dlp`）缺失或网络错误；
  - `4`：业务未就绪（如 B 站无公开字幕报 `unavailable`、检索无结果、`next`/`prev` 越界报 `end of playlist`/`start of playlist`、播放器未在播放时执行控制或 `queue clear`、空历史 `history last`）。
- **`status` 的 `state`**：`idle` / `loading`（已换曲、尚无播放头，还没出声）/ `playing` / `paused`；新的 `play`、起播的 `queue add` 与 `control next|prev` 都会解除暂停。
- **`queue add` 的 `state`**：`playing`（播放器原本空闲，已起播并等到出声，`pos:0, count:1`）或 `queued`（已追加到队尾，立即返回）。
- **队列是瞬态的**：它就是 mpv 的内存播放列表，`stop` 或 mpv 退出即消失，不落盘；全部放完后队列视为空，再 `add` 从头开始。`queue list` 里尚未轮到的条目没有 `title`，只有 `current` 项带 `current: true`。
- **播放历史 (`history`)**：每次成功的 `play` / `queue add` 记一条 `{timestamp, action, input, url, title, duration}`，`stop` 后仍在。`input` 是你传入的原文（如 `ytsearch1:晴天`），`url` 是解析结果；在播时排队的条目 `title` 为空、`duration` 为 0（未加载），`url` 仍是 `ytdl://ytsearch1:…`。信封 `{"status":"ok","count":N,"items":[…]}` / `{"status":"ok","item":{…}}`；空历史 `last` 为 4 `unavailable`。历史目录被拒（宽权限、符号链接）时 `history` 报退出码 2，但播放照常。
- **300 条上限**：`transcript` 无论是否带 `--range`，超过 300 条只返回前 300 条并标 `truncated: true`，此时收窄时间窗再取。
- **声学闪避 (`control duck`)**：只压 mpv 独立增益级 `volume-gain`，与 `control volume` 正交；成功信封 `{"status":"ok","action":"duck"|"duck_on"|"duck_off"}`，立即返回、不等渐变。`--duration` 为 `(0,30]` 秒（可带 `s`），`--level` 为 `[0,100]`（`0` = −96 dB 近乎静音），`--fade` 为 `[0,2000]` 毫秒（可带 `ms`，`0` = 即时）；`on` 不收 `--duration`，`off` 只收 `--fade`，重复 flag 即退出码 1。空闲时 4 `not_playing`；升级前启动的播放器没有助手，返回 4 `unavailable`，`stop` 后重新 `play` 即可。闪避跨暂停、seek、换歌、`play` 替换保持。
- **`status.duck`**：闪避中才出现，是**此刻**的电平百分比（与 `--level` 同刻度，渐变途中读到中间值）；未闪避时没有这个键，`volume` 始终是人设的音量。`events` 的 `snapshot` 同。
- **看门狗**：任何一条 `duck` / `duck on` 之后最迟 30s **开始**渐显（`on` 再用 400ms 恢复，脉冲用 `2 × --fade`）；最新指令胜出——`on` 之后发脉冲会把恢复点改为脉冲到期，重复 `on` 续期且同电平不重启渐变。
- **`audio_device_changed`**：系统音频输出拓扑变化（插拔耳机、AirPods 连断）时派发，带 `devices`（按 `name` 排序）与 `audio_device`（mpv 的输出选择设置，通常为 `auto`，**不是**实际出声设备）；订阅初值不报，同一拓扑换序不报；只在控制中心切换默认输出不触发；`--until audio_device_changed` 在空闲播放器上也会等待。
- **单帧视觉感知**：`jue frame` 输出单行紧凑信封（包含 `path`, `duration`, `width`, `height`, `size_bytes`，`actual_at` 为 nil 时自动收缩）；图片落盘于 0700 沙箱中（模式 0600）。**双轨交付铁律**：Agent 内部直接以 `path` 传给 `read_image` 读图；面向人类交付时严禁在 Markdown 贴本地 `/var/folders/` 路径（避免 Web GUI 裂图），必须提供外网直达时间戳链接（`&t=...s`）。无视频轨或超出时长如实返回退出码 4 `unavailable`。

---

## Agent 核心策略

1. **章节路标优先**：长视频/音频先用 `jue inspect` 拿到全局 Chapters 列表（消耗 <100 Tokens），先定位到问题属于哪一章，再进行局部切片。
2. **时间窗切片取证**：读字幕严禁用全文读入，必须使用 `--range <start-end>`（格式支持 `14:00-18:00` 或 `600-900`），只取目标 3~5 分钟的逐字原话，严格把上下文消耗锁定在 500 Tokens 以内。
3. **事实证据优先**：回答用户“视频里说了什么”必须引用 `transcript` 的逐字原句并标明时间戳。若返回 `unavailable`（退出码 4），如实告知用户该视频无公开字幕，**绝不凭空编造内容**。
4. **单实例自动互斥**：`jue play` 内部采用 Flock 文件锁，每次起播会自动平滑替换上一次的播放，并已在底层等待真实解码发声（`file-loaded`）就绪，无需 Agent 额外写轮询等待代码。
5. **物理逃生直通**：底层 `mpv` 已开启系统媒体键支持，用户按 macOS 媒体键 (F8) 或捏按 AirPods 耳机即可即时硬件暂停，跳过大模型交互延迟。
6. **歌单连播交给队列**：用户要多首连续播放时，一次性用 `jue queue add` 把整组曲目压进队列（第一首起播，其余排队），由 mpv 自行接力，Agent 输出卡片后即可结束本轮，无需挂在后台等待曲毕。`play` 会替换整个队列，往已有歌单里加歌必须用 `queue add`。
7. **事件感知与续添**：严禁用死循环轮询 `status`（消耗 Token 并产生迟滞）。需要在队列放完时续添新歌，调用 `jue events --until queue_ended` 单次阻塞等待；只关心单曲边界时用 `--until track_ended`（`reason: "eof"` 为自然放毕）。
8. **检索前缀按需使用**：用户只给歌名或主题时，可直接用 `ytsearch1:<歌手 歌名>` 起播或排队，省掉一轮 `web_search`；需要 B 站/网易云源，或对结果准确性要求高时，仍应先确认直链。起播后以信封返回的规范 URL 为准写入卡片。
9. **视觉双轨交付与时钟中心锚定**：依据字幕推导抽帧时，建议取中点偏后 60% 位（`at = start + (end-start)*0.60`）作为推荐候选避开转场过渡态（显式指定时点绝对优先）；交付卡片必须提供外网直达时戳链接，严禁在 Markdown 贴本地私有临时图片路径。
10. **“刚才那首是什么”问历史**：播放器已停或换过歌时，用 `jue history last` / `jue history --limit 5` 回答，并以 `url` 重放（`jue play <url>`），不要重新检索；只问当前在放的用 `status`。
11. **说话前先让位**：语音场景下，Agent 开口前 `duck on`，说完 `duck off`（放在 finally 里，未闪避时也安全）；不要用 `control volume` 做闪避，那会改掉用户设的音量。

---

## 典型工作流

- **视频研读**：`jue inspect <url>` 锁章节 → `jue transcript <url> --range 14:00-18:00` 读逐字原话 → `jue frame <url> --at 14:05` 抽帧配图交付。
- **听歌卡片**：`jue play "ytsearch1:晴天"` 起播后输出不超过 2 行的精简卡片；支持 macOS 媒体键或 AirPods 暂停。
- **时空追问**：用户追问刚才那句 → `jue status` 读 `time_pos` → `jue transcript --range` 提取原话回答。
- **历史回溯与重放**：“刚才那首是什么” → `jue history last` 查得标题与规范 `url`，直接 `jue play <url>` 重放，不重复检索。
- **连播与队列**：多曲连播时首条 `queue add` 起播，余条追加进队；`control next|prev` 切歌；`events --until queue_ended` 等待整队放毕续添。
- **与语音协同**：说话前 `jue control duck on`（压至 20%），说完 `jue control duck off`（400ms 渐显）；短句发一条 `control duck --duration <S>` 脉冲。

---

*完整交互场景与话术矩阵详见 co-jue 操作手册。*
