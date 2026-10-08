# PLAN —— 视觉感知信封打磨、流直链缓存加速与人机交付闭环实施方案

## 1. 方案背景与核心矛盾分析 (Background & Problem Analysis)

在针对 Case 1 真实网络视频进行端到端多模态感知评测（Eval）中，系统证实了 `jue frame` 能够通过无头 mpv 瞬态流解码直接捕获指定时点的画面关键帧，成功打破长视频纯文本字幕的信息茧房。但在高频交互与人机协作的实战碰撞中，暴露了 4 个关键工程与交互痛点：

1. **信封信息贫瘠与底层元数据丢弃**：
   在 [frame.go:54-69](internal/engine/frame.go#L54-L69 "::@750ca174") 中，`parseDurationFromOutput` 已经从 mpv 的 `--term-playing-msg` 或 ffmpeg demuxer 日志中提取到了视频总时长 `dur`（如 1120.0s），并在 [frame.go:274-277](internal/engine/frame.go#L274-L277 "::@b57eff8f") 执行越界门禁拦截（`at >= dur`）。然而，在 [frame.go:302-314](internal/engine/frame.go#L302-L314 "::@b03f76fa") 组装 `FrameResult` 时，该字段被彻底丢弃。Agent 为获知目标帧在全片中的相对进度，不得不额外发起一轮耗时 1.5s+ 的 `jue inspect` 网络调用，造成严重的 Token 与网络往返浪费。
2. **`actual_at: null` 语义噪音**：
   在 [frame.go:24-36](internal/engine/frame.go#L24-L36 "::@836eb849") 的 `FrameResult` 声明中，`ActualAt *float64` 缺少 `omitempty` 标签。当无法从 demuxer 精确提取 PTS 时，固定输出 `"actual_at":null`。在 [verbs.go:59-65](cmd/jue/verbs.go#L59-L65 "::@b9603f51") 紧凑 JSON 序列化后产生 16 字节的无效 Token 浪费，且极易引发大模型误判为“时间戳对齐失败（Clock sync failed）”，产生猜忌与多余追问。
3. **同视频密集连续抽帧的冷启动重复惩罚**：
   在复杂长视频研读中，Agent 往往需要针对同一视频在不同时点连续采样（例如第 3m 架构总览、第 8m 控制面拓扑、第 14m 详细数据流）。由于缺乏直链复用机制，每次执行 `jue frame` 均需由 mpv 内部拉起 `yt-dlp` 子进程，重复执行网页拉取、签名解密与 Manifest（DASH/HLS/MPD）解析，单次固定损耗 2.0s ~ 3.5s。连续 3 次抽帧耗时累计达 8~10 秒，交互流速迟滞严重。
4. **人类呈现层最后一公里体验断层**：
   - **私有路径泄露与 Web GUI 裂图**：[SKILL.md:58-61](SKILL.md#L58-L61 "::快速入门@bb64505b") 与 [USER_MANUAL.md:263-279](docs/USER_MANUAL.md#L263-L279 "::实操对话示例-5@3d1e0029") 未严格隔离“机器自用”与“人机呈现”。Agent 在获取 `path` 后直接在 Markdown 中输出 `![Frame](/var/folders/.../frame.jpg)`。Web GUI（`http://127.0.0.1:3080`）作为浏览器沙箱，无法加载宿主机私有临时路径，导致图片直接显示为裂图图标；
   - **转场过渡态与伪影黑屏**：幻灯片（Keynote/PPT）往往包含淡入淡出或飞入动画（0.5s~1.5s）。若 Agent 机械采用字幕起始点（`at = start`）抽帧，极易捕获到转场动画未定型的残影或黑屏。

---

## 2. 架构设计与系统接缝契约 (Architecture Design & Contract Specifications)

### 2.1 核心信封重构 (`FrameResult`)

严格遵循“零冗余紧凑 JSON”哲学，对 [frame.go:24-36](internal/engine/frame.go#L24-L36 "::@836eb849") 的 `FrameResult` 信封进行字段修正：

```go
type FrameResult struct {
	Status    string   `json:"status"`
	URL       string   `json:"url"`
	At        float64  `json:"at"`
	Duration  float64  `json:"duration,omitempty"`   // 媒体总时长（秒），底层探测到时回填；为 0 时收缩
	ActualAt  *float64 `json:"actual_at,omitempty"`  // 精确 PTS（秒），为 nil 时 omitempty 收缩，杜绝 null 噪音
	Path      string   `json:"path"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	SizeBytes int64    `json:"size_bytes"`
	Format    string   `json:"format"`
}
```

- **信封字段契约保证**：
  - 当解析到媒体总时长时，信封直接携带 `"duration": 1120.04`，Agent 零额外网络开销获知全片进度；
  - 当 `ActualAt == nil` 时，JSON 序列化彻底收缩该键，消除 `"actual_at":null` 16 字节语义噪音；若底层未来支持 PTS 精确回填，则序列化为 `"actual_at": 73.12`；
  - [verbs.go:123-156](cmd/jue/verbs.go#L123-L156 "::@de01ac9e") 的 `runFrame` 接口保持完全透明，单行紧凑 JSON 格式不变。

---

### 2.2 轻量流直链缓存引擎 (`internal/engine/cache.go`)

在 `$TMPDIR/jue-<uid>/` 沙箱内建立纯 Go 标准库实现的流直链短期缓存机制，架构规范如下：

```
$TMPDIR/jue-<uid>/                         [目录权限 0700, 自身 UID 属主]
├── jue.lock                               [文件权限 0600]
├── mpv.sock                               [IPC 管道]
├── scratch-frame-<rand>/                  [瞬态抽帧目录，0700]
│   ├── frame_73s.jpg                      [图片产物，0600]
│   └── pgid                               [子进程组标识，0600]
└── cache/                                 [缓存根目录，0700, 自身 UID 属主]
    ├── 3a7f8b9c...d4e5.json               [缓存条目，0600，SHA-256 索引]
    └── e2f1a0c8...8b12.json               [缓存条目，0600，TTL 10m]
```

#### 2.2.1 物理收容与权限拓扑
- **目录隔离**：基于 [daemon.go:20-46](internal/engine/daemon.go#L20-L46 "::@6435d886") 的 `runtimeDir` 安全底座，缓存目录位于 `filepath.Join(rDir, "cache")`；
- **权限门禁**：缓存根目录创建权限必须为 `0700`，单条 JSON 文件权限必须为 `0600`；严禁使用符号链接，属主必须严格匹配当前进程 UID。

#### 2.2.2 索引与数据结构
采用 SHA-256 对规范化媒体目标 URL 计算哈希指纹，以十六进制小写串命名文件：
```go
// StreamCacheEntry 记录解析出的媒体流直链及其时效元数据
type StreamCacheEntry struct {
	URL         string            `json:"url"`                    // 原始规范化目标 URL
	DirectURL   string            `json:"direct_url"`             // 真实媒体流 HTTP/HTTPS 直链
	Duration    float64           `json:"duration,omitempty"`     // 视频总时长
	HTTPHeaders map[string]string `json:"http_headers,omitempty"` // 必要防盗链请求头（如 User-Agent, Referer）
	Fingerprint string            `json:"fingerprint"`            // 条目内容防竞态版本指纹（DirectURL+CreatedAt哈希）
	CreatedAt   time.Time         `json:"created_at"`             // 写入时间
	ExpiresAt   time.Time         `json:"expires_at"`             // 到期时间（CreatedAt + 10m）
}
```

- **TTL 设定**：默认 `10 * time.Minute`（YouTube googlevideo 直链有效期一般为 6 小时，Bilibili 为 2 小时，10 分钟短期缓存具备极高的安全边际且无脏数据滞留风险）。

---

### 2.3 状态机时序：冷启动写入、热命中寻址与失效自愈

```
                 Agent 执行 jue frame <url> --at <time>
                                   │
                                   ▼
                     校验参数 (at, width, quality)
                     规范化目标 URL: targetURL
                                   │
                                   ▼
                    探查 cache/ 下 SHA256(targetURL).json
                                   │
                ┌──────────────────┴──────────────────┐
           [缓存未命中 / 已过期]                    [缓存命中且未过期]
                │                                     │
                ▼                                     ▼
        拉起冷启动 mpv 解流                  时长快速门禁: entry.Duration > 0 && at >= entry.Duration?
     (通过 ytdl_hook 调度 yt-dlp)                 ├── 是: 0ms 直接退出码 4 (out of range)
                │                                 └── 否: 携带 DirectURL + --no-ytdl
                ▼                                         拉起热命中 mpv 极速抽帧 (预算 <= 2.0s)
        从标准输出/错误中解析                                  │
    STREAM_OPEN_FILENAME 与 DURATION                           │
                │                                              ▼
                ▼                                        抽帧是否成功?
        严格单 HTTP(S) 直链白名单过滤                         ├── 成功: 输出 FrameResult (<0.8s)
        (非 EDL、非多轨分片、非直播)                          └── 媒体打开失败 (403/网络失效):
        原子写入 cache/*.json (0600)                               │   版本校验后安全清除缓存文件
                │                                                  └── 降级回退至冷启动 (分配剩余 <=10.5s)
                ▼
        输出 FrameResult (含 duration, 总预算 <=17.5s)
```

#### 2.3.1 严格单点播直链准入、鉴权状态检测与正向证据协议 (Strict Single-Stream White-Listing)
在 [frame.go:170-190](internal/engine/frame.go#L170-L190 "::@03f6685c") 的 mpv 启动参数中，扩展 `--term-playing-msg`：
```go
"--term-playing-msg=STREAM_URL:${stream-open-filename}\nDURATION:${=duration}\nHTTP_HEADERS:${file-local-options/http-header-fields}\nUSER_AGENT:${file-local-options/user-agent}\nLAVF_OPTS:${file-local-options/stream-lavf-o}"
```
- **直链白名单与可独立复播正向证据协议（C1 核心防御）**：
  - mpv 的 `ytdl_hook` 改写为 `stream-open-filename` 的目标可能是复合 EDL、自适应分段 HLS/DASH manifest 或依赖动态注入请求头的临时连接。**严禁通过通用正则粗暴从 `edl://` 中截取单个 URL**（否则会抽到局部分片、音频轨或初始化段，直接破坏时间轴）；
  - **正向准入证据链与获取通道**：
    1. 点播属性证据：`${=duration}` 必须存在、有限且严格大于零（排除直播流与无限流）；
    2. URL 协议证据：从 `STREAM_URL` 提取的目标必须以 `http://` 或 `https://` 开头，且严禁以 `edl://` 起首，路径与参数不包含 `.m3u8`、`.mpd` 自适应分段清单特征，必须为确定性的点播单媒体流；
    3. 请求头与 Cookie 检测通道：通过 mpv 属性 `${file-local-options/http-header-fields}` 与 `${file-local-options/user-agent}` 捕获注入的请求头并存入 `HTTPHeaders map[string]string`；同时通过 `${file-local-options/stream-lavf-o}` 检测是否存在动态 Cookie 注入（如包含 `cookies=` 参数）；若检测到私有 Cookie 依赖，直接判定为不可脱离 hook 独立复播，**拒绝准入缓存**；
    4. 热命中回放：热命中调用 mpv 时，若条目中存有 `HTTPHeaders`，通过 `--http-header-fields` 与 `--user-agent` 完整透传给 mpv，确保脱离 ytdl 后 100% 具备独立可复播性；
  - **无法确认即安全跳过**：若无法完整提取流直链、或发现复杂鉴权 Cookie 绑定，**坚定跳过写缓存，后续直接走冷启动**；当前抽帧成功依然正常返回，绝不破坏核心路径。

#### 2.3.2 热命中极速抽帧与调用级 17.5s 统一绝对 Deadline (C2 核心防御)
- **参数优化**：热命中时，传给 mpv 的目标为 `entry.DirectURL`，并附加 `--no-ytdl` 选项：
  - 彻底切断 mpv 对 Lua 脚本 `ytdl_hook` 的触发；
  - mpv 直接通过 HTTP Range 请求对应关键帧，消除进程派生与 Manifest 解析开销；
  - 耗时由冷启动的 2.5s+ 骤降至 0.4s~0.7s。
- **调用级统一绝对 Deadline 机制与非阻塞锁协议（彻底防范 35s 挂起与死锁）**：
  整个 `Frame` 函数执行以顶层调用时点 `callDeadline := time.Now().Add(17500 * time.Millisecond)` 为统一绝对基准；总流程最多包含至多一次热尝试和至多一次冷尝试，涵盖缓存 IO、进程启动、管道等待与收尾全程，端到端物理硬上限严格锁定在 17.5s 内：
  - **非阻塞缓存锁协议 (LOCK_NB)**：
    所有缓存的探查、写入、失效删除与 GC，均采用非阻塞排他锁：`syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)`。若发生高并发锁争抢，获取不到锁时**立即跳过可选缓存维护动作**，直接执行无缓存解流，绝不发生锁阻塞等待，绝不侵蚀 17.5s 主时间预算；
  - **预算耗尽拦截与收尾裁剪**：
    - 启动任何子进程前，检查剩余预算：若 `time.Until(callDeadline) <= 2500*time.Millisecond`（不足以完成一次完整子进程及安全收割），立即阻断并返回超时 Exit 2，严禁预算耗尽时强起新进程；
    - 热尝试子进程 Context 超时：动态计算为 `min(2.0s, time.Until(callDeadline) - 2500*time.Millisecond)`；
    - 每次收尾探测：轮询探测截止时刻严格裁剪为 `min(500*time.Millisecond, time.Until(callDeadline))`，绝不让收尾超出调用截止时刻；
    - 冷回退子进程 Context 超时：动态计算为 `time.Until(callDeadline) - 2500*time.Millisecond`（上限 10.5s）；
  - **硬上限保证与必需交付步骤预留**：
    - 核心交付必需步骤（图片重命名、os.Chmod 0600、尺寸解码验证与信封组装）预留专门的硬预算（200ms），若在执行前发现 `time.Until(callDeadline) <= 200*time.Millisecond` 直接返回超时 Exit 2；
    - 可选维护（缓存读写、GC 扫描）严格裁剪：GC 的截止时刻设置为 `min(50ms, time.Until(callDeadline) - 300ms)`，且在目录遍历循环内部逐项检查截止时间，超时立即 break 退出；
    - 子进程执行、管道等待与收尾循环严格保证 `2.0s + 2.5s + 10.5s + 2.5s = 17.5s`；在正常系统运行条件下确保全流程有界受控返回。

#### 2.3.3 失效自愈降级与错误分类细化 (M1 & M2 核心防御)
- **零值时长陷阱排除**：仅当 `entry.Duration > 0` 且为有限正浮点数时，才允许基于缓存时长执行 0ms 越界拦截；若 `entry.Duration == 0`（未知时长），放行进入抽帧，绝不误判合法请求；
- **错误类型精准区分**：
  - 若热请求返回**无视频流（Exit 4）**或**确认越界（Exit 4）**，此属媒体客观事实，**直接返回 Exit 4，绝不进行无意义重试**；
  - 若为本地参数、沙箱或磁盘错误，**直接返回，绝不重试**；
  - **仅当热路径遭遇媒体网络打开/解码失败（如 HTTP 403 Forbidden 或 demux 连接失败）**时，判定为 CDN 临时 Token 过期，执行带校验的原子清除缓存，并触发仅此一次的冷启动回退重新拉流。

---

### 2.4 人机双轨交付范式与时钟中心锚定启发式

#### 2.4.1 机器消费 vs 人类呈现双轨隔离规范
在 `SKILL.md` 与 `docs/USER_MANUAL.md` 中立项人机交付硬契约：

| 交互轨道 | 面向主体 | 权限与网络环境 | 规范范式 | 违背反模式（禁止） |
|---|---|---|---|---|
| **机器轨道 (Machine Track)** | Agent 自身多模态感知 | 宿主本地文件系统 (Local POSIX) | Agent 从 `FrameResult` 读取本地 `path`，调用工具 `read_image(file_path: path)` 进行视觉识别 | 严禁调用外部视觉 API 传入本地私有路径；禁止将无视频轨音频强行喂给视觉工具 |
| **人类轨道 (Human Track)** | 终端用户 / Web GUI | 浏览器沙箱 (`http://127.0.0.1:3080`) | 1. 结构化总结提炼架构、参数、代码块<br>2. 附带外网带时戳可跳转链接 `[查看关键帧 (14:05)](<url>&t=845s)` | **严禁在 Markdown 中输出 `![Frame](/var/folders/...)`**（导致 Web GUI 图片裂开且泄漏私有路径） |

#### 2.4.2 “中点偏后（60%位）”时钟中心锚定启发式
当 Agent 依据字幕切片 `[start, end]`（例如 `14:00 - 14:06`）推导抽帧时间点时，建议作为默认推荐候选时点参考：

$$\text{at} = \text{start} + (\text{end} - \text{start}) \times 0.60$$

- **数量级设计依据与实证权衡**：
  - **0%（片段起始点）**：幻灯片（Keynote/PPT）或屏幕共享往往伴随 0.5s~1.2s 的转场动画（淡入、飞入、擦除）。在起始点采样极易捕获到上一页幻灯片的残留碎片或转场黑屏；
  - **50%（绝对中点）**：在语速较快的短片段（2~3 秒）中，50% 处偶尔仍处于动画阻尼震荡尾期；
  - **60%（中点偏后位，推荐候选）**：此时视觉过渡动画通常已定型稳定，主讲人核心论点已经展开，且尚未触发下一翻页操作，是全片视觉信噪比与文字清晰度的默认推荐采样时点（用户显式指定时点绝对优先；无指定时点时以此为初始探索点，Agent 视觉读图后若发现过渡态可就近微调）。

---

## 3. 分阶段工程实施步骤 (Phases Breakdown)

### Phase 1: 核心信封富化与噪音消除
1. **修改 `internal/engine/frame.go`**：
   - 扩充 `FrameResult` 声明：增加 `Duration float64 json:"duration,omitempty"`；
   - 为 `ActualAt *float64` 添加 `json:"actual_at,omitempty"` 标签；
   - 在 [frame.go:302-314](internal/engine/frame.go#L302-L314 "::@b03f76fa") 返回组装逻辑中，将 `parseDurationFromOutput` 提取到的 `dur` 赋给 `FrameResult.Duration`；
2. **契约校验**：
   - 检查 [verbs.go:123-156](cmd/jue/verbs.go#L123-L156 "::@de01ac9e")，确保 CLI 紧凑 JSON 序列化无断裂；
   - 确保 `jue frame` 输出的 JSON 中，不存在 `"actual_at": null`，且在有效视频上携带正确的 `duration`。

### Phase 2: 短期直链缓存引擎与毫秒级寻址加速
1. **新建 `internal/engine/cache.go`**：
   - 实现 `cacheDir() (string, error)`：在 `runtimeDir` 下创建并校验 `cache/`（`0700`，UID 校验，杜绝软链接）；
   - 实现 `getStreamCache(targetURL string) (*StreamCacheEntry, bool)`：SHA-256 寻址，校验类型、UID 与 `0600` 权限，TTL 10m 校验；
   - 实现 `putStreamCache(targetURL, directURL string, duration float64, headers map[string]string) error`：同目录唯一临时文件 `0600` 完整写入后原子 `Rename` 落盘，过滤非 HTTP(S) 或包含 `edl://` 的流；
   - 实现 `putStreamCache` 与 `deleteStreamCache(targetURL string, failedFingerprint string)`：所有写入、条件删除与 GC 共用条目专用非阻塞排他锁（flock LOCK_NB on cache/<sha256>.lock）；删除时在锁内重新读取磁盘条目，核验条目指纹与传入的 failedFingerprint 严格一致才执行 unlink，消除 TOCTOU 竞态；
   - 实现 `parseStreamURLFromOutput(out string) (string, bool)`：提取并严格白名单校验单一 HTTP(S) 点播流直链；
2. **集成进 `internal/engine/frame.go`**：
   - 在参数校验与 URL 规范化后探查缓存；
   - 时长门禁：仅当 `entry.Duration > 0 && at >= entry.Duration` 时快速返回 Exit 4；零值放行进入正常抽帧；
   - 预算切分：热尝试限制为 2.0s（配合 2.5s 等待收割）；若热路径遭遇媒体网络/解码失败，原子删除当前缓存，冷启动兜底分配剩余至多 10.5s（配合 2.5s 最终收割），调用级总物理硬上限严格锁定在 17.5s；
   - 错误精细分类：若热路径返回纯音频无视频轨（Exit 4）或越界（Exit 4）或本地错误，直接返回，不重试；仅媒体打开/连接失败（如 HTTP 403）触发冷重试；
   - 冷启动成功后：通过白名单校验的单 HTTP(S) 直链回填缓存；
   - 在 `syncOpportunisticGC` 中增加对 `cache/` 目录下过期条目的顺手清理。

### Phase 3: 人机协同策略与呈现范式更新
1. **更新 `SKILL.md`**：
   - 在 [SKILL.md:58-61](SKILL.md#L58-L61 "::快速入门@bb64505b") 补充单帧视觉感知的人机分离规则；
   - 在 [SKILL.md:78-87](SKILL.md#L78-L87 "::a-视频研读与时间戳问答@35b82ace") 的工作流 a) 中明确：
     - Agent 端自用：调 `read_image` 读本地 `path`；
     - 人类端呈现：严禁贴 `/var/folders/` 本地路径，必须使用文字卡片 + 原声引用 + 外网带时戳链接 `&t=`；
     - 字幕推导抽帧建议使用 60% 推荐候选启发式 `at = start + (end - start) * 0.60`（显式指定时点绝对优先）；
2. **更新 `docs/USER_MANUAL.md`**：
   - 调整 [USER_MANUAL.md:230-262](docs/USER_MANUAL.md#L230-L262 "::场景-7关键帧研读与视觉证据链-roi-@bba5a816") 场景 7 的流程与示例卡片，移除破坏 Web GUI 的伪图片链接；
   - 在 [USER_MANUAL.md:280-286](docs/USER_MANUAL.md#L280-L286 "::关键交互点-3@f2af6652") 中增加“60% 时钟中心锚定启发式”与“双轨交付范式”条目。

### Phase 4: 测试套件扩充与全量回归
1. **单元测试 (`internal/engine/cache_test.go` & `frame_test.go`)**：
   - 编写缓存写入、命中、过期、文件权限 `0700`/`0600`、软链接拒绝测试；
   - 编写信封富化测试：断言 `FrameResult` 序列化后包含 `duration`，且不包含 `actual_at` 字段；
   - 编写模拟回退测试：验证热命中失败时能够自动删除脏缓存并降级重试；
2. **端到端契约回归 (`tests/test_suite.sh`)**：
   - 修改 [test_suite.sh:1130-1160](tests/test_suite.sh#L1130-L1160 "::@345b893d") Block M：
     - 断言 `jue frame` 输出的 JSON 信封中 `.duration > 0` 且 `has("actual_at") | not`；
     - 增加缓存命中与冷启动双轨验证断言：针对可缓存源断言热命中跳过 yt-dlp 调度，针对不可缓存源断言冷启动正常兜底，避免将网络波动作为绝对功能断言；
     - 校验 `$TMPDIR/jue-<uid>/cache/` 目录模式 `0700`，内部 `.json` 模式 `0600`；
   - 重构 [test_suite.sh:555-585](tests/test_suite.sh#L555-L585 "::@11e87b7b") Block E-g：对齐人机双轨规范，断言卡片包含外部时戳直达链接与原声引用，不再强求贴本地私有绝对路径；
   - 运行全量 `tests/test_suite.sh`，确保 260+ 项全绿，零残留、零泄漏。

---

## 4. 实施清单与自愈门禁 (Execution Checklist & Verification Gates)

```
[ ] Phase 1: 核心信封富化与噪音消除
    [ ] 1.1 internal/engine/frame.go: FrameResult 补入 Duration float64
    [ ] 1.2 internal/engine/frame.go: actual_at,omitempty 噪音消除
    [ ] 1.3 internal/engine/frame.go: 回填解析到的总时长 dur
    [ ] 1.4 cmd/jue: 编译验证，确认紧凑 JSON 输出符合预期

[ ] Phase 2: 短期直链缓存引擎与毫秒级寻址加速
    [ ] 2.1 internal/engine/cache.go: 建立 0700/0600 权限目录与 SHA256 索引
    [ ] 2.2 internal/engine/cache.go: 10m TTL 读写与过期判定
    [ ] 2.3 internal/engine/cache.go: 单一 HTTP(S) 直链提取与白名单过滤（拒收 EDL）
    [ ] 2.4 internal/engine/frame.go: 接入热命中与零值安全时长门禁
    [ ] 2.5 internal/engine/frame.go: 接入调用级 17.5s 绝对预算切分与失效冷重试
    [ ] 2.6 internal/engine/frame.go: syncOpportunisticGC 联动清理过期缓存

[ ] Phase 3: 人机协同策略与呈现范式更新
    [ ] 3.1 SKILL.md: 规范 Agent 自用 read_image 与用户端外网 &t= 呈现范式
    [ ] 3.2 SKILL.md: 固化字幕推导抽帧 60% 推荐采样候选启发式
    [ ] 3.3 docs/USER_MANUAL.md: 更新场景 7 交付卡片与双轨规范

[ ] Phase 4: 测试套件扩充与全量回归
    [ ] 4.1 internal/engine/cache_test.go: 单元测试覆盖读写、权限、EDL拒收与降级
    [ ] 4.2 tests/test_suite.sh: Block M 补充 duration 校验与 has("actual_at")|not
    [ ] 4.3 tests/test_suite.sh: Block E-g 对齐人机多模态卡片规范
    [ ] 4.4 tests/test_suite.sh: 全量 260+ 项回归 100% 绿灯验证
    [ ] 4.2 tests/test_suite.sh: Block M 补充 duration 与无 actual_at 断言
    [ ] 4.3 tests/test_suite.sh: Block M 增加连续抽帧命中加速断言
    [ ] 4.4 tests/test_suite.sh: 校验 cache 目录权限 0700 / 文件 0600
    [ ] 4.5 全量测试: go vet ./... && go test ./... 100% PASS
    [ ] 4.6 全量测试: bash tests/test_suite.sh 260+ 项全绿，零残留
```

---

## 5. 方案吸收与退出条件 (Absorb-then-Delete Criteria)

遵循 `co-brain/docs/ARCH-doc-taxonomy.md` §3.1 单项施工方案（`PLAN-*`）生命周期规约：

1. **完工退出**：当 Phase 1 ~ Phase 4 全部施工完成，代码已合入主线，单元测试与 `test_suite.sh` 全量通过；
2. **知识沉淀**：
   - 缓存设计与时空寻址架构思想沉淀至 `docs/ARCHITECTURE.md` 感知层小节；
   - 多模态交互范式与 60% 锚定策略已固化至 `SKILL.md` 与 `docs/USER_MANUAL.md`；
3. **物理删除**：施工完毕并通过所有验证门禁后，在当次阶段提交中原子执行 `git rm docs/PLAN-frame-perception-polishing-and-cache.md`，不在代码库遗留临时施工脚手架。
