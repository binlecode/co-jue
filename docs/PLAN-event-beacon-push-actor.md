# PLAN —— ting 事件感知与外发微外设 (Event Beacon) 实现规划 (v2 · Claude 架构审查修订版)

**设计定锚**：在恪守“短命 CLI + 单实例无头 mpv”底线的前提下，利用 mpv 自身的多客户端原生 JSON-IPC 事件分发机制，新增 `ting events` 动词，为 `co-s2s`（实时语音）与 `co-cli`（代码助手）提供零额外守护进程、零外部依赖的毫秒级事件感知面。

---

## 1. 架构第一性原理纠偏 (Grounding Corrections)

经对抗性架构审查（Claude Audit），推翻 v1 方案中“自建 Go 常驻守护与 `events.sock`”的过度设计：

1. **宿主机唯一常驻进程仅为 mpv**：`ting` 绝不增加第二常驻守护进程。`ting play` 启动无头 mpv 后立即释放并退出；
2. **复用 mpv 原生多客户端事件能力**：mpv 的 Unix Domain Socket 本身就是全功能 Pub/Sub 广播中心——每个独立连接到 `$TMPDIR/ting-<uid>/mpv.sock` 的客户端都会收到全量异步事件，且客户端间拥有独立的事件队列与背压隔离；
3. **消除慢客户端背压与死锁隐患**：mpv 对每个 IPC 客户端维护有界环形队列，卡住的消费者由 mpv 核心自动丢弃，绝不会阻塞音频解码输出或其它控制连接；
4. **定时器回归编排层**：番茄钟属于外部业务编排（消费方自行调度或 `sleep` 控制），ting 坚决不承担状态机外的伪业务定时器，保持纯粹微外设细腰。

---

## 2. 动词契约设计：`ting events`

### 2.1 动词形式与执行模式

```sh
# 模式 A (Agent 友好模式 · 默认推荐)：单次阻塞直到目标事件触发，返回单行信封并退出 (退出码 0)
ting events --until <EVENT> [--timeout SEC]

# 模式 B (流式管道模式)：持续单行紧凑 JSON 打印，供 Unix 管道消费 (Ctrl+C 或下游断开退出)
ting events [--timeout SEC]
```

### 2.2 契约规则与退出码

- **退出码契约**：
  - `0`：成功捕获目标事件，或流式正常结束；
  - `1`：参数非法（如未知的 `--until` 事件名、非法的 `--timeout`）；
  - `2`：运行时目录异常或外部连接底层错误；
  - `4`：业务未就绪（如播放器未启动直接执行 events 报 `not_playing`，或 `--timeout` 超时未捕获目标事件报 `timeout`）。
- **Late Joiner 快照保障 (电平触发)**：
  - 客户端连接成功后，首行必定下发 `snapshot` 事件（内容等同 `status` 当前全量状态），防止在起播后订阅的客户端漏掉当前状态。

---

## 3. 五大确定性事件结构 (Event Schema)

每种事件拥有强类型结构定义，杜绝 `map[string]any` 字段漂移：

### 3.1 `snapshot` (初次连接即时电平快照)
```json
{"event":"snapshot","state":"playing","url":"https://...","time_pos":142.5,"duration":269.0,"volume":60}
```

### 3.2 `track_started` (音轨加载与发声确认)
```json
{"event":"track_started","url":"https://...","duration":269.0}
```

### 3.3 `track_ended` (音轨放毕或终止)
`reason` 精确映射为 4 种确定性状态：
- `eof`：自然播放完毕（**自动续播 DJ 的唯一决策信号**）；
- `replaced`：被并发的新 `play` 替换；
- `stopped`：收到 `control stop` 人工终止；
- `error`：解码或网络加载失败，携带 `error` 详情。
```json
{"event":"track_ended","url":"https://...","reason":"eof","duration":269.0}
```

### 3.4 `paused` / `resumed` (硬件与软件播放状态变更)
在用户轻捏 AirPods 耳机柄或按键盘媒体键（F8）暂停/恢复时即时派发：
```json
{"event":"paused","time_pos":84.2}
{"event":"resumed","time_pos":84.2}
```

### 3.5 `chapter_changed` (跨越章节边界)
监听 mpv 原生 `observe_property chapter`，播放头跨入新章节（或因 seek 跳入新章节）时触发：
```json
{"event":"chapter_changed","chapter_index":3,"title":"特殊 Token 陷阱","start":3510.0}
```

---

## 4. 落地实施细节 (Implementation Details)

### 4.1 核心代码增补 (`internal/engine/events.go`)
- **连接 mpv.sock**：使用既有的 `connect()` 逻辑，受 `runtimeDir` 0700 边界保护；
- **初始化注册**：
  ```go
  // 注册需要监听的高价值属性
  client.Command("observe_property", 1, "pause")
  client.Command("observe_property", 2, "chapter")
  client.Command("observe_property", 3, "idle-active")
  ```
- **首行输出 snapshot**：读取当前 `time-pos`、`pause`、`path`，合成并输出 `snapshot`；
- **事件翻译循环**：
  - 循环读取 mpv 异步响应；
  - 遇到 `end-file` 提取 `reason` 映射为 `eof|replaced|stopped|error`；
  - 遇到 `property-change` 属性分发为 `paused/resumed` 或 `chapter_changed`；
  - 若处于 `--until` 模式，命中目标事件即输出该行、优雅断开连接、退出码 0。

### 4.2 动词接入 (`cmd/ting/verbs.go`)
- 新增动词 `events`：
  ```go
  "events": runEvents,
  ```
- 继承 `runVerb` 单行紧凑 JSON 与退出码框架。

### 4.3 代码增量控制
- 纯 Go 标准库，预计增补代码 **~140 行**；
- 全仓代码规模保持在 **~1,240 行**，杜绝架构臃肿。

---

## 5. 验证与测试套件 (Block I: Event Contract)

在 `tests/test_suite.sh` 中新增测试块：
1. **快照断言**：新连接首先收到 `snapshot`，字段与 `status` 完全一致；
2. **自然放毕断言**：本地 1 秒音频自然放完，断言收到 `track_ended` 且 `reason == "eof"`；
3. **AirPods / 媒体键暂停感知**：通过外部向 mpv 发送 `set_property pause true`，断言收到 `paused` 事件；
4. **单次阻塞模式契约**：`ting events --until track_ended --timeout 5` 在曲目放完时即刻以单行退出 0；
5. **慢客户端绝不阻塞**：启动一个只读 1 字节即阻塞的连接，并发执行 `control seek/volume`，断言毫秒级返回且播放头不卡顿；
6. **零残留断言**：测试退出时所有 `events` 订阅连接均优雅释放，无任何僵尸。
