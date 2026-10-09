# CLAUDE.md / AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) and all coding agents working in this repository.

## ⚠️ 关键底线 —— 开工前必看

开工前必须明确以下五条硬底线，**违反会导致进程权限异常、契约破坏或工程漂移**：

- 🔴 **纯 Go 标准库与单静态二进制**：全仓代码仅使用 Go 标准库，零 cgo（`CGO_ENABLED=0` 保证可编译），零第三方外部包。代码规模遵循 **Need-based 零冗余原则**：不设人工行数魔数，每一行以功能必要性为准绳，新增能力必须证明无法由既有原语或 Agent 自身承担，拒绝投机性抽象与冗余代码。
- 🔴 **双外部原语依赖**：外部依赖严格锁定为两个（`yt-dlp` 与 `mpv`；可选 `deno` 作为 yt-dlp 的 JS 运行时）。严禁引入任何额外外部二进制或 C 库。
- 🔴 **四级退出码与机器信封**：
  - `0`：成功（Success）；
  - `1`：命令行用法错、参数格式非法；
  - `2`：外部依赖缺失（未装 mpv 或 yt-dlp）、网络底层错误；
  - `4`：业务未就绪（如媒体不存在/下架/无可用字幕 `unavailable`、上游风控频控阻断 `error`、播放器未在播放时执行控制）。
  - 默认输出单行紧凑 JSON，严禁静默修改既有信封字段。
- 🔴 **状态隔离与目录安全**：运行时 Socket 严格收容在 `$TMPDIR/jue-<uid>/`，目录权限必须为 `0700`、属主等于自身 UID、严禁为符号链接。临时产物一律限在 `tmp/` 下，严禁污染源码树。
- 🔴 **本地闭环 CI/CD（本地能做的，绝不推给 GitHub）**：全生命周期的测试、语法静态分析、SemVer 校验、多架构静态编译构建、打包发布与 Skill 同步一律通过本地工具链（`.githooks/`、`install.sh`、`scripts/release.sh`、`tests/test_suite.sh`）闭环完成。**严禁引入 `.github/workflows/` 等远端臃肿 CI**，不将本地可完全胜任的任务推到 GitHub Actions 虚拟机上浪费资源。
- 🔴 **文档分类与 Changelog 归位**：严格遵循全域规约（`ARCH-doc-taxonomy.md`）。系统演进历史、版本更新日志与阶段全表（Stage chronicles）一律归入根目录 `CHANGELOG.md`，严禁在 `ARCHITECTURE.md` 或 `docs/` 尾部以附录堆积历史日志。

---

## 🔴 第一条：不要自作聪明

**有疑问或做技术选型时，严格按两步执行，顺序不可颠倒：**

1. **先 grounding** —— 查外部平台（YouTube / Bilibili / 网易云）最新端点、核查 `yt-dlp` 与 `mpv` 实际 IPC 行为、通读本地代码与测试套件，**深入代码实现与真实运行输出，严禁凭空假设向下推演**。
2. **再确认** —— 严禁静默新增/废弃动词、修改信封字段、私自放宽门控。若实测推翻了前提，如实报送发现并提问。

---

## 项目性质

**co-jue**（CLI 二进制命令为 `jue`）—— `co` 生态面向 AI Agent（Claude Code、OpenCode、co-cli、co-s2s）的端侧视听感知与播放微外设（Go 静态单二进制，收容于 `~/workspace_genai/co-jue/`）。版本严格遵循 SemVer 语义化规范（声明于根目录 `VERSION`，如 `2.2.0`；演进全表与更新日志见 `CHANGELOG.md`）。

- **感知平面 (Ingest)**：
  - `jue inspect <url>`：极简提取章节时间轴（Chapters）与元数据，Token 开销 < 100 Tokens；
  - `jue transcript <url> [--range START-END]`：提取声称级逐字原话证据（防机翻污染、带相交判定裁剪、超出 300 条自动截断），无字幕如实返回 `unavailable`（退出码 4）；
  - `jue events [--until <EVENT>] [--timeout SEC]`：毫秒级事件感知面（曲目放毕、AirPods 触控、章节切换、`--until queue_ended` 整个队列放毕、`audio_device_changed` 音频输出拓扑变化），彻底根除轮询 Token 开销；
  - 显式检索前缀：`inspect` / `transcript` / `play` / `queue add` 均接受 `ytsearch1:<关键词>`（白名单仅 `ytsearch1:` 与 `ytsearch:`，其余检索前缀退出码 1），复用 yt-dlp 原生检索，零自研爬虫。
- **执行平面 (Daemon)**：
  - `jue play <url> [--start SEC]`：Flock 互斥与 Lazy-start 托管单实例后台无头 mpv，替换整个队列，超时等待声音就绪（`file-loaded`），返回 mpv 解析后的规范 URL；
  - `jue queue add <url|query> | list | clear`：mpv 原生瞬态内存播放列表（零持久化，随 mpv 生灭）；空闲时 add 即起播，播放中 add 追加立返；clear 只清待播；
  - `jue control <pause|resume|seek|volume|stop|next|prev>`：毫秒级确定性控制，`next`/`prev` 越界返回退出码 4 `unavailable`，`stop` 后 mpv 自动优雅退出，不留僵尸守护；
  - `jue control duck [on|off] [--duration SEC] [--level 0-100] [--fade MS]`：声学闪避，Agent 说话时压低音乐；只写 mpv 独立增益级 `volume-gain`（与 `volume` 正交），渐变/保持/30s 看门狗全在 mpv 内嵌 Lua 助手 `jue_duck.lua`（`embed` 内嵌、`launch()` 写入运行时目录 0600 并 `--script=` 加载）里，CLI 发一条 `script-message-to` 立返；助手就绪标记 `user-data/jue/duck-helper` 缺失或协议不符为退出码 4 `unavailable`；
  - `jue status`：时空同步遥测（返回播放头秒数、状态、总时长、音量；闪避中另带瞬时电平 `duck`）。
- **物理逃生口**：
  - mpv 默认开启 `--input-media-keys=yes`，原生直通 macOS 键盘与 AirPods 耳机暂停键，跳过大模型延迟。

---

## 常用命令

```sh
# 本地一键安装与全局 Agent Skill 部署校验
./install.sh
./install.sh --uninstall

# 本地多架构交叉编译与发布打包 (支持 --publish 配合 local gh CLI 直发 Release)
./scripts/release.sh

# 语法与静态检查 + Go 单元测试（纯逻辑与协议解析，离线，< 1s）
go vet ./... && go test ./...
go build -o jue ./cmd/jue

# 端到端契约与工作流测试（真实 jue / mpv / yt-dlp / 外网，零 Mock，需 jq）
bash tests/test_suite.sh

# 核心动词抽检
./jue inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
./jue transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30
./jue play "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --start 18
./jue status
./jue control pause
./jue control resume
./jue control volume 50
./jue control duck on
./jue control duck off
./jue queue add "ytsearch1:Never Gonna Give You Up"
./jue queue list
./jue control next
./jue events --until queue_ended --timeout 600
./jue control stop
```

---

## 测试分层

- **单元层** `internal/engine/*_test.go`：`ingest_test.go`（ParseRange/ParseTime、json3 去重清洗、LRC 时间推导、网易云 id、pickTrack 语言链、检索前缀白名单与头部锚定、检索 playlist 外壳 `entries[0]` 解包与空结果）、`frame_test.go`（参数与溢出校验、NaN/Inf/1e8 拦截、width/quality 边界、检索前缀分流、GC 30 项门限与进程存活安全门禁、超时强杀与 WaitDelay 回收）、`ipc_test.go`（request_id 匹配、事件入队、`WaitForPlaybackSuccess` 的加载成功/失败/被顶替/redirect/超时/任意条目 `-1`）、`daemon_test.go`（runtimeDir 0700 放行，宽权限/符号链接/普通文件拒收，只读动词不落盘；`queue add` 空闲起播/播放中追加/并发同锁、`queue list` 信封与空闲不落盘、`queue clear`、`next`/`prev` 及越界、play 返回规范 URL）、`events_test.go`（snapshot 与初值压制、`start-file` 刷新 URL、`queue_ended` 触发/stop 不触发/空闲拒收/中途退出、`audio_device_changed` 首条有效通知为基线/排序指纹/null 与 [] 等同/空闲可等）、`duck_test.go`（助手 0600 temp+rename 写入且不跟随符号链接、就绪标记两侧一致、三模式线协议逐字、空闲/标记缺失/协议不符/投递拒绝、`BenchmarkDuckRoundTrip`）；`daemon_test.go` 另含 `mpvArgs` 加载助手且不设 `volume-gain`、`status.duck` 换算；`cmd/jue/verbs_test.go`（`parseDuck` 默认值、后缀、边界、NaN/Inf/溢出、重复与模式不符；`TestVerbUsageGates` 八个动词语法闸门在清空 `PATH`/`TMPDIR` 下逐项退出码 1）。IPC 测试在内存管道另一端扮演 mpv 的线协议。
- **端到端层** `tests/test_suite.sh`：按六大能力平面分组，块字母为稳定标识。**Boundary**：A 安全边界与用法闸门（0700/符号链接拒收、无播放器各动词不落盘、全部动词用法错 exit 1）· F Token 预算（信封无多余字段/转义/浮点噪声，越界窗口返回空）。**Ingest**：B 感知契约（章节、窗口、300 条截断、unavailable、LRC）· K 显式检索解析（`ytsearch1:` 各动词解析为规范 watch URL、非白名单前缀 exit 1、`search_query` 普通 URL 不误判）· M 视觉帧感知（0700/0600、纯音频与内嵌封面 MP3 拒收、时长越界、真实网络流单帧、流缓存冷/热、双边等比外框、零残留）。**Playback**：C 播放状态机（本地 + 网络音频、全部 control、stop 回收、kill -9 后恢复）· D 冷/热并发 play · J 瞬态队列（冷/热 add、list、next/prev 及越界、暂停态切歌、clear、play 替换整队、6 路并发冷启动 add）· L 队列放毕（`--until queue_ended`、空闲 exit 4、stop 中断 `player exited`）。**Acoustic**：G 声学人机（起播即停 200ms 内回收、暂停 200ms 内冻结）· N 声学闪避（非阻塞、渐变中间值、`--fade 0`、脉冲与续期、音量正交、−96 dB 地板、外部非有限值、零 `audio-reconfig`、生命周期、旧助手/协议不符、符号链接防御）· NW 闪避看门狗（生产 30s 墙钟，独立成块与 N 并行）。**Events**：I 事件契约（snapshot、边沿、eof、stop、超时、`audio_device_changed` 基线）。**Agent workflows**：E-a 章节研读 · E-b+f 背景听歌卡片后取播放头下的歌词行（一条 NetEase 流）· E-c 暂停追问播放头附近原话 · E-d 自动续播 DJ · E-e AirPods 触控倒带 · E-g 视觉证据卡片 · H 落地为资产（`&t=` 逐字引用块；无字幕时 inspect 兜底）。共 20 块 333 项断言。韧性：跨网动词经 `net`/`fetch` 只对瞬态结果（exit 2，或 exit 4 且 `status=="error"`）最多重试 3 次并在报告中标注；`need` 前置条件失败止于一条 FAIL；每块 300s 看门狗，`mpv_ipc`（测试侧 `python3` 直读 mpv socket）5s 超时；`subscribe` 以 `lsof` 确认事件订阅已连接再触发。各块独立 `TMPDIR`、各自的 mpv，并行执行；网络音频前先 `volume 0`，套件全程静音。退出时只按本套件 socket 路径回收 mpv 并删除临时目录，最后一行断言零残留。

---

## 架构要点

- **短命 CLI + 单实例 Daemon**：`jue play` 在后台唤醒 `mpv --idle=yes --no-video --input-media-keys=yes`，其余命令通过 Unix Domain Socket 发送标准 JSON 指令。
- **request_id 解交错与事件缓冲**：mpv 事件流与命令回执解耦，`WaitForPlaybackSuccess` 确保两阶段事件就绪，杜绝伪成功。
- **统一锁 `flockAction`**：`play` 与 `queue add` 在同一把 `jue.lock` 内完成拨号/拉起、状态探查（`playlist-pos == -1` 即空闲）与指令分发，锁外等待出声。
- **检索非对称分流**：传给 mpv 补 `ytdl://` 前缀（`normalizeForMPV`），传给 yt-dlp 剥离（`normalizeForYtdlp`）。
- **零 UI 负债**：没有 TUI，没有自研搜索爬虫，没有本地数据库（队列只是 mpv 内存播放列表），Agent 就是唯一的交互呈现层。
