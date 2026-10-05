# PLAN —— Transient In-Memory Queue, Explicit Query Resolution & Queue-Ended Perception (co-ting v1.2.0)

> **状态**：Approved (已通过 Round 4 对抗式终审，全阻断关闭，准予实施)  
> **目标组件**：`co-ting` (`cmd/ting`, `internal/engine`)  
> **归属路径**：`~/workspace_genai/co-ting/`  
> **生效范围**：二进制 `ting`、全局 Agent 契约 (`SKILL.md`) 与端到端测试套件

---

## 1. 背景与核心动因 (Context & Drivers)

`co-ting` 在 v1.1.0 确立了极简微外设与双原语（`mpv` + `yt-dlp`）架构，并以 `ting events --until track_ended` 实现了曲毕事件推流。但在真实 Agentic 交互与日常沉浸使用中，暴露出三大阻抗：

1. **Turn-based Agent 与连续后台播放的阻抗失配**：
   - 现役 LLM Agent（Claude Code、Pi、co-cli）是按轮次（Turn-based）驱动的。Agent 完成单轮回显（输出播放卡片）后，会话挂起等待人类输入，无法在宿主机后台常驻阻塞监听 `ting events` 并手动触发下一首。
   - 导致 Agent 推荐的“写代码歌单”放完单曲即陷入死寂，缺少底层播放器自驱动的连续播放能力。
2. **严格单 URL 依赖与语义点歌阻抗**：
   - 人类习惯下达语义点歌（如“放首周杰伦的晴天”）。
   - 当前 `ting play` 强校验单一精确 URL，迫使 Agent 必须先调用外部爬虫或 `web_search` 解析 URL，拉长交互链路且极易被反爬阻断。
3. **播放列表生命周期感知的缺失**：
   - 拥有队列后，Agent 需要感知的核心信号从“单曲放毕（`track_ended`）”升级为“整个队列播放完毕（`queue_ended`）”，以决定何时为主播/用户续添新歌单，避免反复轮询。

---

## 2. 架构硬边界与不可动摇原则 (Invariants)

依据 `ARCHITECTURE.md` 第一性原理与 Master 裁决，本项扩展严守以下红线：

- 🔴 **绝对零持久化歌单数据库**：严禁引入 SQLite、BoltDB 或本地 JSON 文件维护播放列表；**仅暴露 mpv 内存中原生瞬态播放列表（Transient In-Memory Playlist）**，随 mpv 进程启停生灭。
- 🔴 **绝对零自研搜索爬虫算法**：严禁在 Go 侧内置网易云/B站/YouTube 抓取爬虫；**全面复用 `yt-dlp` 原生检索协议（`ytsearch1:`）与 mpv `ytdl://` 内部解析机制**。
- 🔴 **纯 Go 标准库与单静态二进制**：`CGO_ENABLED=0`，零第三方外部包。**代码规模严格遵循 Master 裁决的 Need-based 原则**：废除历史遗留的人工 ~1400 行死板魔数（已由 Master 明确裁决废除），代码以功能必要性、极致精简与零冗余为准绳，并在 Phase 5 同步更正 `CLAUDE.md` 与 `CHANGELOG.md` 的历史陈旧描述。
- 🔴 **四级退出码与机器信封绝对一致**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法、非法非 URL 输入、不支持的检索前缀；
  - `2`：外部工具缺失、底层网络/IPC 错误；
  - `4`：业务未就绪/不可用（队列到底 `end of playlist`、无字幕 `unavailable`、检索无结果、本地文件不存在、空闲时 control）。
- 🔴 **彻底剔除 `ting schema`，保持单一事实来源**：拒绝在微外设二进制内硬编码 MCP/OpenAI/Anthropic 工具 Schema。外部生态（如 Claude Code、co-cli）统一通过 `SKILL.md` 驱动 bash 命令；若需原生 MCP 支持，应由独立的外部适配器进程承载。

---

## 3. 实测技术确证 (Empirical Verification & Grounding)

经宿主机物理原语行为深度实测（macOS darwin-arm64, mpv 0.41.0, yt-dlp 2025.x），确证以下线协议事实：

1. **检索协议的非对称性（强制双向转换）**：
   - **mpv 侧**：原生**不认**裸 `ytsearch1:...`（会被当成本地相对路径报错退出码 2）。mpv 强制要求 `ytdl://ytsearch1:...` 伪协议头以触发其内置 ytdl 钩子。
   - **yt-dlp 侧**：**严禁** 出现 `ytdl://` 前缀，否则报错 `Unsupported url scheme: "ytdl"`。
   - **确证规则**：传给 mpv 时统一补充 `ytdl://`；传给 yt-dlp 时统一剥离 `ytdl://`。严格收敛仅支持 `ytsearch1:` 与 `ytsearch:`，其余以 `^[a-z0-9]*search[a-z0-9]*:` 开头的检索协议一律报 exit 1。
2. **检索解包、原始字节替换与空结果处理**：
   - 执行 `yt-dlp --dump-single-json "ytsearch1:Query"` 时，输出根对象 `_type: "playlist"`，真实视频嵌套于 `entries[0]`。
   - 当无结果时，`entries` 为空数组。此时必须显式返回退出码 4（`unavailable`），杜绝将 playlist 外壳当成有效视频返回伪成功。
   - `ytTranscript` 调用 `--load-info-json` 时，必须以 `entries[0]` 的原始字节序列替换，防止外壳污染语言筛选。
   - **保护规则**：仅在输入显式以 `ytsearch1:` / `ytsearch:` 开头时执行解包降维；标准 YouTube 播放列表 URL 严禁自动降维，保持既有契约。
3. **mpv 队列属性的瞬态事实与规范 URL**：
   - 通过 `loadfile <url> append-play` 追加至队列的待播条目，在未真正轮到其解码发声前，mpv 的 `playlist` 属性中**仅存在 `filename` (`url`) 和 `id`，不存在 `title`**。待播项的 `title` 必须 `omitempty`。
   - 当条目被 mpv 解码起播后，mpv 内部 ytdl 钩子完成 redirect，其 `path` 属性即变为真实的规范流媒体 URL（如 `https://www.youtube.com/watch?v=...`）。
   - **URL 契约**：起播项（`state=="playing"`）返回规范解析 URL；排队项（`state=="queued"`）返回输入/归一化 URL。由于 `Play` 的 `url` 从回显改为规范 URL，此项作为信封变更记入 `CHANGELOG.md`。
4. **`playlist-next` 与 `playlist-prev` 的边界**：
   - 显式传递 `weak` 参数彻底避免 force 模式下的播放器终止；越界时 mpv 返回 `error: "error running command"`。
   - 在 Go 侧需依据动作映射为退出码 4：`fail(4, "unavailable", "end of playlist")` 与 `fail(4, "unavailable", "start of playlist")`。
   - 切歌成功后，需显式解除暂停（`set pause false`）并调用 `WaitForPlaybackSuccess(-1)`，确保声音真正就绪。
5. **放毕转 idle 后的播放列表状态**：
   - mpv 队列全部放毕后，`idle-active` 变为 `true`，`playlist-pos` 变为 `-1`，但 `playlist` 内存中仍保留历史条目。
   - 因此当播放器处于 idle 时加入新曲目，必须先执行 `playlist-clear` 清空历史条目，再起播新曲，杜绝 `count` 历史膨胀。
6. **`start-file` 时 `path` 已就绪**：
   - 实测证明在收到 `start-file` 事件瞬间，mpv 的 `path` 属性已刷新为新曲目的路径。在 `start-file` 时同步更新 `curURL` 可彻底消除此前切歌失败时 `track_ended` 误报上一首 URL 的历史 bug。

---

## 4. 详细命令与协议契约规范 (Contract Specification)

### 4.1 显式检索协议 (Query Resolution)

坚持显式前缀，杜绝隐式猜测，完美保住现有非法 URL（exit 1）与缺失文件（exit 4）的安全契约：

- **支持语法**：
  ```sh
  ting play "ytsearch1:周杰伦 晴天"                            # 显式 YouTube 检索起播
  ting queue add "ytsearch1:Never Gonna Give You Up"          # 显式检索排队
  ting inspect "ytsearch1:周杰伦 晴天"                         # 显式检索章节与元数据
  ting transcript "ytsearch1:周杰伦 晴天" --range 60-90        # 显式检索字幕切片
  ```
- **头部锚定判定与双向归一化**：
  - **白名单规则**：白名单严格收敛为 `ytsearch1:` 与 `ytsearch:`。
  - **头部锚定正则**：判定前缀是否包含非法检索协议时，必须头部锚定（如 `^[a-z0-9]*search[a-z0-9]*:`），严禁全串子串匹配，防止误伤普通 URL（例如 `https://www.youtube.com/results?search_query=...` 绝不能误判为非法检索前缀）。
  - **yt-dlp 侧** (`normalizeForYtdlp`)：
    1. 若以 `ytdl://` 开头，剥离该前缀；
    2. 检查头部是否匹配 `^[a-z0-9]*search[a-z0-9]*:`：若是且不在白名单内，返回退出码 1（`usageErr("unsupported search prefix")`）；
    3. 必须在 `dump()` 顶层调用，统一覆盖 `Inspect` 与 `Transcript`（并在 `neteaseID` 判定前执行）。
  - **mpv 侧** (`normalizeForMPV`)：
    1. 若以 `ytdl://` 开头，剥离该前缀，校验是否包含非法 `^[a-z0-9]*search[a-z0-9]*:`；
    2. 若属于检索白名单，统一部署为 `ytdl://` + 检索前缀；若已为普通 URL 或本地文件，原样直通。

### 4.2 瞬态队列管理 (`ting queue`)

新增一级原子动词 `queue`，涵盖 `add`, `list`, `clear`，并扩展 `control next|prev`：

#### A. 追加曲目与统一锁：`ting queue add <url|query>`
- **统一锁机制 (`flockAction`)**：
  - 签名重构为：`flockAction(dir string, fn func(c *IPCClient) (entryID int64, err error)) (*IPCClient, int64, error)`；
  - `Play` 与 `QueueAdd` 均在 `ting.lock` 排他锁保护下完成 IPC 连接/拉起、状态探查与指令分发；
  - 函数在锁内完成命令分发后立即释放锁，并将连接 `c` 与 `entryID` 交给调用方。若 `entryID > 0`，调用方在锁外调用 `WaitForPlaybackSuccess(entryID)` 等待出声并读取规范 `path`，最后关闭 `c`；若 `entryID == 0`（仅排队），立即关闭 `c` 并返回。彻底杜绝死锁与并发互斥冲突。
- **`QueueAdd` 生命周期**：
  - 检查 `playlist-pos`（以 `playlist-pos == -1` 判定空闲，不依赖跳变延迟的 `idle-active`）：
    - **冷启动或 idle 态（`playlist-pos == -1`）**：
      发送 `playlist-clear` 清除历史条目，发送 `loadfile <url> append-play`，强制 `set pause false` 解除残留暂停，记录返回的 `playlist_entry_id`，释放锁。
      在锁外调用 `WaitForPlaybackSuccess(entryID)` 等待出声。
      读取 mpv 的 `path` 属性作为规范 URL。
      返回 `state: "playing"`, `pos: 0`, `count: 1`（快照值）。
    - **正在播放态（`playlist-pos >= 0`）**：
      发送 `loadfile <url> append-play`，读取更新后的 `playlist-count`，释放锁。
      **非阻塞立返**：返回 `state: "queued"`, `pos: count-1`, `count: count`，`url` 为输入的归一化 URL。
- **返回信封 (0 ok)**：
  ```json
  {"status":"ok","action":"add","state":"playing","url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ","pos":0,"count":1}
  {"status":"ok","action":"add","state":"queued","url":"ytdl://ytsearch1:七里香 周杰伦","pos":1,"count":2}
  ```

#### B. 查看队列：`ting queue list`
- **逻辑**：查询 mpv `playlist`、`playlist-pos`、`playlist-count`。以 `playlist-pos == -1` 识别空闲。
- **空闲/未运行规范**：若播放器未运行或处于 idle，**不创建运行时目录**（对齐 status，并入只读动词），直接返回：
  ```json
  {"status":"ok","pos":-1,"count":0,"items":[]}
  ```
- **播放中返回信封 (0 ok)**（符合 Block F Token 预算，冗余字段清洗）：
  ```json
  {
    "status": "ok",
    "pos": 0,
    "count": 2,
    "items": [
      {"index": 0, "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "title": "晴天 - 周杰伦", "current": true},
      {"index": 1, "url": "ytdl://ytsearch1:七里香 周杰伦"}
    ]
  }
  ```
  - `title` 标记为 `omitempty`，待播未加载项不伪造标题；
  - `current` 标记为 `omitempty`，仅当前播放项为 `true`，杜绝重复冗余的 `false`。

#### C. 清空待播：`ting queue clear`
- **逻辑**：向 mpv 发送 `playlist-clear`。保留当前正在发声的曲目，清除队列中后续所有等待曲目。
- **特殊状态**：若无播放器在运行或 `playlist-pos == -1`（空闲），返回退出码 4（`fail(4, "not_playing", "no player running")` 或 `"player is idle"`，对齐 `Control`）。
- **返回信封 (0 ok)**：
  ```json
  {"status":"ok","action":"clear"}
  ```

#### D. 切歌控制扩展：`ting control next` 与 `ting control prev`
- **扩展动词**：在既有 `controlArgs` 中注册 `next: 1` 与 `prev: 1`。
- **执行逻辑**：
  - 发送 `playlist-next weak` 或 `playlist-prev weak`；
  - 若 mpv 返回错误：
    - `next` 越界：返回退出码 4，`fail(4, "unavailable", "end of playlist")`；
    - `prev` 越界：返回退出码 4，`fail(4, "unavailable", "start of playlist")`；
  - 若切换成功：发送 `set pause false` 确保解除暂停，调用 `WaitForPlaybackSuccess(-1, 30*time.Second)` 确认新曲目解码就绪。
- **返回信封 (0 ok)**：
  ```json
  {"status":"ok","action":"next"}
  ```

### 4.3 队列放毕事件感知 (`queue_ended`)

在 `ting events` 中新增核心事件 `queue_ended`：
- **触发条件与边沿差分**：
  - 启动时通过 `StatusOn` 初始化 `lastIdle := (st.State == "idle")`；
  - 监听 `idle-active` 属性跳变，仅在 `false -> true` 边沿发生、且上一首曲目以 `eof` 或 `error`（如流中断）结束时派发 `queue_ended` 事件。
- **边缘状态规范**：
  - 若启动 `events --until queue_ended` 时播放器已处于 idle，立即返回退出码 4（`fail(4, "not_playing", "player is idle")`）；若播放器未运行，返回退出码 4（`fail(4, "not_playing", "no player running")`）；
  - 若监听中途播放器异常退出或被 `control stop` 终止，返回退出码 4（`fail(4, "not_playing", "player exited")`）。
- **事件信封**：
  ```json
  {"event":"queue_ended"}
  ```
- **CLI 用法**：`ting events --until queue_ended [--timeout SEC]`。

---

## 5. 模块改造方案 (Implementation Details)

### 5.1 `internal/engine/ingest.go`
1. **`normalizeForYtdlp(u string) (string, error)` 辅助函数**：
   - 若以 `ytdl://` 开头，剥离该前缀；
   - 检查头部是否匹配 `^[a-z0-9]*search[a-z0-9]*:`：若匹配且非 `ytsearch1:` / `ytsearch:`，返回 `fail(1, "error", "unsupported search prefix")`。
2. **`dump(u string)` 解析增强**：
   - 在 `dump` 入口第一行执行 `normalizeForYtdlp`，统一保护 `Inspect` 与 `Transcript`；
   - `rawInfo` 增加 `Type string json:"_type"` 与 `Entries []json.RawMessage json:"entries"`；
   - 仅当归一化后的输入以 `ytsearch1:` 或 `ytsearch:` 开头且 `info.Type == "playlist"` 时：
     - 若 `len(info.Entries) == 0`：返回 `fail(4, "unavailable", "no search results for %s", u)`；
     - 取 `info.Entries[0]` 反序列化覆盖 `info`，并用该 entry 的原始字节替换 `raw` 返回给 `ytTranscript`。
3. **`Transcript(u, ...)` 前缀适配**：
   - 在进入 `neteaseID(u)` 之前先执行 `normalizeForYtdlp`，防止 `ytdl://` 掩盖网易云域名识别。

### 5.2 `internal/engine/daemon.go`
1. **统一锁机制 (`flockAction`)**：
   - 签名：`flockAction(dir string, fn func(c *IPCClient) (entryID int64, err error)) (*IPCClient, int64, error)`；
   - 在锁内执行状态判断、loadfile 指令与 unpause，释放锁后将连接与 entryID 返回给调用层；
   - `Play` 与 `QueueAdd` 均在 `flockAction` 保护下分发，随后在锁外调用 `WaitForPlaybackSuccess`。
2. **`normalizeForMPV(u string) (string, error)` 辅助函数**：
   - 剥离前缀校验是否为非白名单的 `^[a-z0-9]*search[a-z0-9]*:`；
   - 若属于白名单检索前缀，补齐 `ytdl://` 前缀。
3. **`Play` 升级**：
   - 输入经 `normalizeForMPV` 校验并归一化；
   - 在 `flockAction` 内完成 `replace` 与 unpause；在锁外等待声音就绪；
   - 播放成功后，读取 mpv `path` 属性替换 `PlayResponse.URL`，输出解流后的规范 URL。
4. **`QueueAdd(u string) (*QueueAddResponse, error)`**：
   - 在 `flockAction` 保护下探查 `playlist-pos`：
     - 未运行或 `playlist-pos == -1`：执行 `playlist-clear`，发送 `loadfile u append-play`，解除暂停，记录 `playlist_entry_id`，释放锁；随后在锁外调用 `WaitForPlaybackSuccess(entryID)`，读取规范 `path`，返回 `state: "playing"`；
     - 正在播放（`playlist-pos >= 0`）：发送 `loadfile u append-play`，读取 `playlist-count`，释放锁；非阻塞立即返回 `state: "queued"`。
5. **`QueueList() (*QueueListResponse, error)`**：
   - 未运行或 `playlist-pos == -1` 返回空列表结构（`pos: -1, count: 0, items: []`），不创建运行时目录；
   - 运行中获取 `playlist`、`playlist-pos`、`playlist-count`，组装轻量结构，`title` 与 `current` 采用 `omitempty`。
6. **`QueueClear() (*QueueClearResponse, error)`**：
   - 播放器未运行或 `playlist-pos == -1` 返回 exit 4；运行中发送 `playlist-clear`。
7. **`Control(action string, ...)` 增强**：
   - 增加 `next` 与 `prev` 命令分支；发送带 `weak` 参数的 IPC；
   - 捕获 mpv failure 并精细化映射 `end of playlist` 与 `start of playlist`；
   - 成功切换后解除暂停并等待出声。

### 5.3 `internal/engine/events.go`
1. `ValidUntilEvents` 增加 `"queue_ended": true`；
2. 修复 `start-file` 时序：在 `start-file` 到达时即刷新 `curURL` 与 `curDur`，杜绝切歌失败时 `track_ended` 误报上一首 URL 的历史 bug；
3. 增加 `idle-active` 属性边沿监听；当曲目以 `eof` 或 `error` 结束且播放器转入 idle 时，派发 `queue_ended` 事件；
4. 启动时若播放器已处于 idle，且 `until == "queue_ended"`，直接返回 `fail(4, "not_playing", "player is idle")`；若中途退出，返回 `fail(4, "not_playing", "player exited")`。

### 5.4 `cmd/ting/verbs.go`
1. 注册动词 `"queue": runQueue`；
2. `controlArgs` 增补 `"next": 1`, `"prev": 1`；
3. `verbUsage` 增补 `ting queue add|list|clear` 与 `control next|prev`。

---

## 6. 测试与验证策略 (Test Plan)

遵循仓内“零 Mock、端到端真机闭环”铁律，增补以下测试分层：

### 6.1 单元测试 (`internal/engine/`)
- `ingest_test.go`：
  - 测试 `normalizeForYtdlp` 对白名单、非法检索前缀（如 `scsearch:`）以及正常带 search 参数 URL（如 `https://youtube.com/results?search_query=foo`）的正确处理；
  - 测试 `dump` 针对带 entries 的 json、空 entries 以及非搜索普通 playlist 的反序列化与解包行为。
- `daemon_test.go` & `ipc_test.go`：
  - 内存管道模拟 mpv：测试 `playlist-next weak` 成功与越界返回；
  - 测试 `QueueAdd` 在 idle 与 playing 状态下的命令分流与 `flockAction` 互斥；
  - 测试 `WaitForPlaybackSuccess` 指定 entryID 与通用 `-1` 的分支；
  - 断言 `queue list` 在播放器未运行时不落盘运行时目录。

### 6.2 端到端套件增补 (`tests/test_suite.sh`)

套件目前拥有 9 个一级块（A 到 I，含 E 的 b-f 工作流），本期严格增补 3 块，扩展至 12 块：

#### Block J: Transient Queue & Playlist Lifecycle
1. `queue add $WAV1`：验证冷启动起播，断言 `state=="playing"`, `pos==0`, `count==1`；
2. `queue add $WAV2`：验证热态追加，断言 `state=="queued"`, `pos==1`, `count==2`；
3. `queue list`：断言 `count==2`, `pos==0`, 第 0 项 `current==true`, 第 1 项无 `current` 字段（Token Budget）；
4. `control next`：断言平滑跃迁至 `$WAV2`，`status.url` 变为 `$WAV2`；
5. 越界 `control next`：断言退出码 4，`status=="unavailable"`, `error=="end of playlist"`；
6. `control prev`：断言返回 `$WAV1`；
7. 越界 `control prev`：断言退出码 4，`status=="unavailable"`, `error=="start of playlist"`；
8. 暂停态下追加并切歌：断言自动解除暂停正常发声；
9. `queue clear`：断言待播清空，当前曲目不受影响；
10. `play $WAV3`：断言 replace 覆盖整个队列，`queue list` 重置为单条；
11. 6 路并发冷启动 `queue add`：断言仅启动单个 mpv，队列条目总数准确为 6 条且全部无损存在（验证锁分发无死锁且无丢失）；
12. 播放器未运行时 `queue list`：断言返回空列表且不落盘运行时目录（对齐 Block A）。

#### Block K: Explicit Query-to-Play Resolution
1. 网络静音保障：先设置 `volume 0`；
2. `ting play "ytsearch1:Never Gonna Give You Up"`：断言起播成功，`status.url` 成功解析为标准 YouTube watch 链接；
3. `ting inspect "ytsearch1:Never Gonna Give You Up"`：断言返回规范的 Title 与 11 位 YouTube ID；
4. `ting transcript "ytsearch1:Never Gonna Give You Up" --range 18-30`：断言成功截取逐字原话，验证原始 JSON 外壳解包正常；
5. 搜索不支持的前缀 `ytsearch5:foo` 或 `scsearch:foo`：断言退出码 1；
6. 正常带 search 参数的 URL：断言放行不误判；
7. 既有安全回归：断言 `inspect notaurl` 依旧保持 exit 1，`play missing.wav` 依旧保持 exit 4。

#### Block L: Queue-Ended Event Perception
1. 制作两段 2 秒 short silence wav（总长 4 秒）；
2. 依次压入队列：`queue add $SHORT1`，随后 `queue add $SHORT2`（播放器已处于活跃播放态，余量充足）；
3. 挂起后台监听：`ting events --until queue_ended --timeout 10`；
4. 等待后台 events 退出，断言在曲目放毕时顺利捕获 `event=="queue_ended"` 且退出码为 0；
5. 空闲时执行 `ting events --until queue_ended`：断言立即退出码 4，`status=="not_playing"`, `error=="player is idle"`。

---

## 7. 分阶段实施路径 (Phased Execution Steps)

| 阶段 | 任务内容 | 验证门禁 |
|---|---|---|
| **Phase 0** | 在 `tmp/` 中对 yt-dlp `entries[0]` 字幕完整性与 mpv `playlist-pos` 同步性执行前置探针校验 | 探针输出与 §3 假设 100% 吻合 |
| **Phase 1** | `ingest.go` 交付 `normalizeForYtdlp`、`entries[0]` 安全解包及空搜索防御 | `go test ./internal/engine -run TestIngest` 通过 |
| **Phase 2** | 交付 `flockAction` 统一锁重构，完成 `queue add/list/clear` 与 `control next/prev` | 单元测试覆盖 fakeMPV 所有分支与并发锁 |
| **Phase 3** | `events.go` 修复 `start-file` 时序并增补 `queue_ended` 状态机 | `events_test.go` 新增测试用例通过 |
| **Phase 4** | 增补 `tests/test_suite.sh` Block J, K, L (总计 12 块) | 全套件 12 块端到端测试并行全绿 (0 FAIL) |
| **Phase 5** | 全量同步文档：`SKILL.md`、`docs/ARCHITECTURE.md`、`docs/USER_MANUAL.md`、`CLAUDE.md`（更新动词表、测试分层、规则描述）及 `CHANGELOG.md`，升级 `VERSION` (1.2.0) | `scripts/release.sh` 本地全架构交叉编译通过 |

---

## 8. 风险与降级回退 (Risks & Rollback)

- **实施隔离保护**：
  - 本次变更严格在独立 feature 分支 `feat/transient-queue` 上实施；
  - 严禁直接在 main 上操作；若验证受阻，直接切回 main 丢弃分支。
