# PLAN — TUI 改用 Go，CLI 契约成为唯一接缝

> **Status**: 实施中 · 三项决定已确认（2026-09-24），`--watch` 形状四项已确认（2026-09-25），`--capabilities` 形状已确认（2026-09-25），引擎发现与分发已定（2026-09-25）· **暂停于第 4 步之前**：引擎形状与配置键由 [`PLAN-single-entry.md`](PLAN-single-entry.md) 重定（2026-09-26），它先做完，本计划再继续  
> **Priority**: 第一梯队  
> **Target Branch**: main  
> **Roadmap 关联**: [`docs/ROADMAP.md`](ROADMAP.md)「Go 重写 NO」（TUI 一半已由本计划反转）  
> **Governing Docs**: [`docs/ARCHITECTURE.md`](ARCHITECTURE.md)「分析：驱动决定的六条发现」、[`docs/ARCH-cli-contract.md`](ARCH-cli-contract.md)「`ting` —— 交互式终端 UI」、[`docs/ARCH-player.md`](ARCH-player.md)「运行时 IPC 控制」、[`docs/ARCH-tui.md`](ARCH-tui.md)  
> **Verification**: `bash -n shell/*`、`tests/contract.sh --offline`、`tests/contract.sh`、`tests/playback.sh`、`go test ./...`、tmux 驱动段  
> **Scope Boundary**: 只换人机那张脸。引擎对（`*-search` / `*-resolve`）、播放器 `t-play`、两个存储（`t-playlist` / `t-history`）留在 shell、bash 3.2、零新增运行时依赖，既有 argv、信封字段、reason 与退出码一字不改。契约只**加**三样东西（`t-play --watch`、`--capabilities -j`、`t-play --engines -j`），各自是一次 minor。引擎对与配置键的形状随后由 PLAN-single-entry.md 改写（那是它的范围，不是本计划的）。

---

## 1. 为什么现在做，以及它不是什么

定位变了：人机那张脸要的呼吸感（亚秒时钟、插值动效、差量渲染、真正的事件循环）是
bash 3.2 给不了的——`read -t` 只收整数秒，tty VTIME 能到 100 ms 但每拍要 fork，
`ARCH-tui.md` 已把这笔账记全。「六条发现」第 1 条早就承认：`ting` 的大头是在手工重做
Go TUI 栈免费给的东西，重写会**删掉**这份负债而不是搬迁它。当时拒绝的理由是"只有内部收益"；
现在内部收益（能做出那张脸）本身就是目标，所以 TUI 这一半反转。

**不是**全套重写。第 2、5、6 条对引擎与播放器一条未动：站点知识要原地可改，
yt-dlp/mpv 在任何语言里都是子进程，生命周期的回归无法二分。所以 Go 那一侧永远只是
**这套 CLI 的又一个调用方**，与 agent 平级。

---

## 2. 切分原则：TUI 能碰的只有 argv 与信封

```
  +-----------------------+        +-----------------------+
  |  agent (Claude Code)  |        |  ting (Go, Bubbletea) |
  +-----------+-----------+        +-----------+-----------+
              |   argv + -j envelope + exit code   |
              |   t-play --watch -j (NDJSON)       |
              +-----------------+------------------+
                                v
  +----------------------------------------------------------+
  |  t-play | t-playlist | t-history   (shell, bash 3.2)     |
  +----------------------------+-----------------------------+
                               v
        t-engine-<site>（底层，经 t-play 转发，PLAN-single-entry.md）
                               v
                  mpv socket / yt-dlp / curl   (private)
```

一条判据：**Go 二进制里出现 mpv、yt-dlp、socket 路径或针对某个站点的行为，就是一次分层违规。**
引擎名（`yt`/`bili`/`ne`）作为不透明字符串显示、转发不算——它们本来就是契约里的词。
一件只有 TUI 能做的事，要么下沉成一个动词（agent 同时得到它），要么不做。
`CLAUDE.md`「严禁单侧新增 TUI 键位」保持原样，而且从此有了机械的检验方式。

---

## 3. 今天的 bash TUI 在哪里绕过了契约（实测清单）

这些是切分必须关掉的洞；每一条都给出去向。

| 现状（`shell/ting`） | 为什么当初这样 | 去向 |
|---|---|---|
| `fetch_play_times` 每拍在 `--status` 公布的 `sock` 上直读 14 个 mpv 属性 | 每拍 fork 一条 `t-play` 太贵 | `t-play --watch`（§4.1） |
| `mpv_get_prop core-idle` 判断起播就绪 | 同上 | `--watch` 的状态事件 |
| `fetch_play_res` 对播放器的**进程组**跑 `ps` 读 CPU/内存——依赖 `t-play` 内部的 `set -m`（pgid == 记录里的 pid） | 没有动词答这个 | `--watch` 的心跳每 3 拍取样带出 `cpu`/`mem`；进程组知识留在 `t-play` |
| `-`/`=` 音量走 `send_mpv_ipc`，先 `get_property volume` 再 set | 按住连发：socket 10 ms/次，`--set-volume` 60 ms/次 | Go 端合并连按（只保留最新目标值、同一时刻最多一个在途调用），走 `--set-volume`；当前音量来自 `--watch` |
| 封面：TUI 自己起一个 `mpv --vo=image` 做转码 | bash 解不了图 | Go 解码搜索信封里的 `thumbnail`，自己发 Kitty 协议；mpv 从 TUI 里消失（细节见 §5「封面」） |
| `--parts` / `--info` 支不支持，靠调一次、嗅 stderr 的用法错 | 没有发现动词 | 先是 `<engine>-resolve --capabilities -j`（§4.2）；之后归 `t-play --engines -j` 的 `flags[]`（PLAN-single-entry.md §2） |
| 引擎发现：本目录 → `TING_ENGINE_DIR` → PATH 三处扫描，与 `t-play` 各写一份 | 没有动词答这个 | `t-play --engines -j`（§4.3）；之后 Go 根本不执行引擎文件，引擎动词一律经 `t-play` 转发（PLAN-single-entry.md） |

**已经干净、原样沿用的**：起播/停止/暂停/seek/循环/队列八个动词、`--undo --owner PID`
（Go 进程传自己的 PID）、`--status` 接管已在跑的播放器、`t-playlist` / `t-history` 全部动词。

---

## 4. 契约增量

### 4.1 `t-play --watch [--id ID] -j` —— 播放器状态的事件流

- **形状（已定 2026-09-25）**：stdout 每行一个信封（NDJSON），**每行都是完整状态**——
  `--status` 里那一条播放器记录的全部字段，加上 `event`（这一行为什么出）、`ready`
  （本曲是否已出声）、`cpu`/`mem`。读者不做合并、不存状态，中途加入也不缺什么。
  字段用播放器自己的词（与 `--status` 同源），**不**透传 mpv 属性名 ——
  mpv 仍是私有的，换掉它不应改变这份输出。
- **目标与退出码（已定）**：`--id` 缺省与其他 socket 动词同一条规则（唯一那个；多个报
  `ambiguous`、没有报 `not_playing`，都是 4）。播放器自己结束或被停，发最后一行
  `end` 后以 0 退出。
- **只发离散事件，每条都带位置**：`snapshot`（每次接上 socket 的首行）、`ready`、
  `pause`/`resume`、`volume`、`seek`、`transitioning`、`end`；外加心跳 `heartbeat`，
  在播放头每跨过一个整秒时发一次（所以心跳里的整数 `position` 在发出那一刻是准的；暂停时没有心跳）。
  媒体事实与 `duration` 的变化不单独成事件，随下一行带出（`audio_bitrate` 是滑动估计，逐变化转发就是噪声）。
  连续的播放位置**不**逐帧转发：实测 mpv 对 `time-pos` 约
  19 次/秒（5 秒 95 条），管道成本可以忽略（`nc` 0.0%、`jq --unbuffered` 0.1% CPU，
  到达时间与播放时钟恒定偏移，没有缓冲积压）——不转发的理由是**调用方**：
  一个 agent 读这条流，每秒 19 行是它要付 token 的噪声。节流在那一个常驻 `jq` 里做
  （比较整秒），不为节流 fork。Go 用最近一条事件的位置加单调时钟外推，暂停时停止外推。
- **实现可行性（已实测）**：macOS 自带 `/usr/bin/nc -U` 保持一条长连接，对 mpv 发
  `observe_property` 后，另一个客户端改 volume / pause 时，`property-change` 事件被实时推到
  这条连接上；每个被观察的属性（包括当下不可用的）都会先来一条初值通知，所以"全部到齐"就是首行快照的时机。
  前提是 nc 的 stdin 一直不关——`--watch` 自己以读写方式打开一个 FIFO 当 nc 的 stdin，
  不另起一个 `tail -f` 之类的持有进程（实测那种持有进程在 nc 退出后会残留）。
- **生命周期跟播放器走，不跟 socket 走**：队列每首是一个新的 mpv 进程，换曲时旧连接关闭，
  新 mpv 要等引擎解析完下一首的直链才起来，中间有一段空窗。实测 mpv 退出后**socket 文件留在原地**，
  所以 `-S` 为真不代表连得上：空窗里重连会被拒。`--watch` 以**播放器进程组**是否还活着为准：
  活着就轮询重连，stdout 不断。mpv 在关连接之前先发 `end-file`，`--watch` 据此发 `transitioning`，
  它带的是**刚结束的那一首**与结束方式（`ended`：finished / interrupted / failed）；
  下一首由重连后的 `snapshot` 报出——不在 `transitioning` 里预告，因为那一刻子进程还没推进队列文件。
- **结束**：播放器进程组消失后，`end` 行带最后已知的状态，外加 `exit_code`/`reason`
  （有墓碑时取墓碑，正常结束或被停为 null），然后以 0 退出。读者先关掉 stdout 时，
  `--watch` 在下一次写时收掉自己的 nc 再退出；暂停中没有写，所以要等到下一个事件——
  Go 那一侧拥有这个子进程，退出时直接 kill 它。**已落地**（ARCH-player.md「状态流」）。
- **agent 同样受益**：`ARCHITECTURE.md`「六条发现」第 4 条列过"流式进度"为 bash 给不了的诉求——
  它给得了，只是当时没有人要。

### 4.2 `<engine>-search` / `<engine>-resolve --capabilities -j`

> **被 PLAN-single-entry.md 取代**：公开面上不再有各半边的 `--capabilities`，能力清单并进 `t-play --engines -j` 的 `flags[]`；
> `t-engine-<site> --capabilities` 留作 `t-play` 与引擎之间的内部协议。下文记的是已落地的 0.16.0 形状。

一次调用答出这个引擎半边支持的动词与选项（`--parts`、`--info`、`--transcript`、
`--items`……），替代嗅探 stderr。三个内置引擎都加；`ARCH-cli-contract.md`
「加一个引擎 —— 清单」把它列为必备项，仓外引擎缺它时调用方按"只有最小集"处理。

- **形状（已定 2026-09-25）**：`{status, engine, flags[]}`，一个扁平清单，不分动词与修饰符
  （分了就要每个引擎都判对一次分类）。清单与 unknown-flag 拒绝出自脚本里同一个数组。
  取值枚举（`-f` 模式、`--quality` 档位）不进去，今天各引擎一样。
- **已落地**（ARCH-cli-contract.md「命令规格」与「数据契约」、ARCH-engine.md「探一个引擎有哪些动词」）：
  `shell/ting` 的 `c`/`i` 门改读它（ARCH-tui.md），套件的动词发现也改读它，并对每个引擎验它说的是真话。

### 4.3 `t-play --engines -j`

> **形状被 PLAN-single-entry.md 改写**：`{status, engines:[{name, bin, flags[]}]}`，每个引擎一个文件（`t-engine-<name>`）；
> `bin` 只供诊断，Go 不执行它。下文记的是已落地的 0.17.0 形状。

- **形状（已定 2026-09-25）**：`{status, engines:[{name, search, resolve}]}`，发现顺序，两条都是
  一次播放真正会跑的绝对路径。放在 `t-play`：它本来就拥有 resolver 查找与 `TING_ENGINE_DIR`，
  bash `ting` 退役后 shell 里没有别的家。代价是启动时多 fork 一次（全 PATH 扫描实测 9 ms）。
- **为什么不在 Go 里复刻**：复刻之后规矩仍是两份，只是换成两种语言；动词让它只剩一份，
  Go 也不必知道 `TING_ENGINE_DIR` 的 XDG 默认链与旧名兜底。agent 同时得到"装了哪些源"。
- **已落地**（ARCH-cli-contract.md「命令规格」`t-play` 一节、「数据契约」、「加一个引擎」）：
  门与 `--capabilities` 同一种，不要 `jq`；bash `ting` 暂时照旧自己扫，套件断言两者同表同序。

三项增量各自先落地、各自 bump（版本独占一次 commit），且都在 Go 代码写第一行之前完成。

---

## 5. Go 一侧

- **位置**：同仓，`go.mod` 在根，入口 `cmd/ting/`。bash 3.2 与"零新增运行时依赖"两条红线
  继续只管 `shell/`；Go 二进制本身不是运行时依赖，Go 工具链是**构建**依赖。
- **分发（已定 2026-09-25）**：tap 从源码编译（`depends_on "go" => :build`），不发 bottle ——
  没有 CI，tap 只有一个用户，以后要换也不难。二进制装在 `libexec/shell/ting`，就是今天那个
  脚本的位置，于是"真实路径旁边找兄弟"与"上一级找 `VERSION`/`config`"两条规矩原样成立，
  formula 只换安装那一行。开发时构建到 `shell/.ting-go`（gitignore）：点开头的名字不进
  `shell/*` 的 glob，所以 `bash -n shell/*`、pre-push 的语法门与套件的入口点清单都碰不到它，
  而它的真实目录就是兄弟脚本所在，不会回落到 PATH 上装好的旧版本。
- **栈**：Bubbletea（事件循环、resize）、Lipgloss（样式）、`go-runewidth`/`uniseg`（显示宽度，
  取代 `disp_w` 的 EAW 表）、`harmonica`（弹簧动效，可选）。
- **一个 `verb` 包**：唯一执行子进程的地方。拼 argv、解信封、把退出码映射成类型化错误；
  所有键处理器只调它。它同时是"Go 里不许出现 mpv"那条判据的落点。
- **找兄弟动词**：与今天的 shell 脚本同一条规则——先解开 Go 二进制自身的符号链接
  （`os.Executable` + `filepath.EvalSymlinks`），在它真实所在的目录找，再回落 PATH。
  这只用来找 `t-play` / `t-playlist` / `t-history`；引擎动词（搜索、`--info`、容器、字幕）一律调 `t-play` 的转发动词，
  Go 不执行引擎文件（PLAN-single-entry.md）。不加新环境变量；公开命令收成四个（`t-play`、`t-playlist`、`t-history`、`ting`）
  装在 `bin/` 下，`t-engine-*` 留在 `libexec/shell/`，由 `t-play` 在自己真实目录里找到。`t-playlist` / `t-history` 缺席时
  按契约降级，不是启动失败。
- **配置面**：Go 实现同一条继承链（flag > 环境变量 > 用户配置 > 出厂配置）与
  偏好键的原地写回。键名按 PLAN-single-entry.md §4 统一为 `TING_*`（旧名兜底一个版本，写回时原地改成新名），
  Go 的配置移植已按新键表重做（一张旧名 → 新名表，与 shell 载入块的 `CFG_RENAMED` 同一份），「TING_ 只认改名清单」的怪癖随之消失。文件格式是既有契约，不改。写回不用任何配置库（它们会在
  反序列化-序列化之间丢掉注释），逐行搬运，并保住今天 bash 版的每一条保证：
  值后面的行内注释与它的对齐空白原样保留；文件里还没有的键追加到末尾；文件不存在时
  带说明头新建；写临时文件后 rename，且保持原文件权限（今天是 `cp -p`）；环境变量里
  钉住的键不写回。
- **封面**：YouTube 的 `hq720.jpg` 实际以 `image/webp` 返回（实测 content-type），
  bili 与 ne 是 JPEG，所以需要 `golang.org/x/image/webp`（构建依赖）。Kitty 负载按块发送
  （今天的 `m=1` 分块），重排与退出时删除 placement。tmux 下保持关闭——今天
  `TING_IMAGE=auto` 在 tmux 下就是关的，并没有 tmux 穿透可移植。

### 功能对齐清单（工作清单，不是合入门槛）

`ting --help` 键表里的每一个键；TTY 双门；argv 与三类标志的转发；接管已在跑的播放器、
退出时只停本会话起的那个；撤销的 3 秒窗口；`b`/`h`/`c`/`i` 四种临时行源；`/` 过滤；
`Nj` 跳行；滚动/分页两种列表模式；中英 chrome；13 套主题与 truecolor/ANSI-16 回退、
ASCII 模式、亮暗背景探测；同步重绘（今天是 DCS `1q/2q`，tmux 下关）；封面（tmux 下关）。

---

## 6. 实施顺序

1. **`--watch`**（shell）：实现 + `tests/playback.sh` 里的真实播放器用例（改音量、暂停、
   跨曲目、播放器死亡，全部轮询真实事件，不 sleep）。bump。
2. **`--capabilities -j`**（shell）：三个引擎 + 契约段的跨引擎门。bump。
3. **Go 骨架**：先落 `t-play --engines -j`（shell，bump）；再 `verb` 包、`--watch` 消费、配置链，先能搜、能播、能停。
   **已落地**：`cmd/ting`、`internal/verb`（`Error` 按退出码分三类；`Watch` 把 `--watch` 放进自己的进程组，
   `Close` 连 nc/jq 一起收走，播放器不受影响）、`internal/config`（读链，含旧名兜底那张改名表）、
   `internal/tui`（搜 / 播 / 暂停 / 停 / 换源 / 新搜索，启动时接管最新那个在跑的播放器，退出只停本会话起的）。
   `go test ./...` 驱动真实命令；带网络与 mpv 的两例在 `-short` 下跳过。测试的 `TMPDIR` 放在仓内 `tmp/`：
   Go 每个测试的临时目录会把 mpv 的 socket 路径推过上限，mpv 于是**不建 socket 就起播**；
   `t-play -d` 现在在门口退 1 拒掉这种 `TMPDIR`（ARCH-player.md「运行时 IPC」，2026-09-26 定）。
   偏好写回、键表其余部分、主题与封面归第 4 步。
4. **对齐**：按 §5 清单逐项。**先等 PLAN-single-entry.md 全部落地**：它改的正是这一步要消费的东西
   （搜索与 `c`/`i` 走 `t-play` 的转发动词、`--engines` 新形状、配置新键名、删掉的五个 `*_CYCLE` 键）。
5. **测试迁移**：`tests/contract.sh` 的 tmux 段与 `tests/drive.sh` 改为驱动 Go 二进制；
   断言针对行为与帧结构，不针对 bash 实现细节。
6. **替换**：Go 接过 `ting` 这个名字、`git rm shell/ting`、更新 tap formula。
7. **蒸馏**：重写 `ARCH-tui.md`；改 `ARCHITECTURE.md` 定位一节与「六条发现」、
   `ARCH-cli-contract.md` 的 `ting` 一节、`CLAUDE.md`（"10 个脚本"、常用命令、`bash -n` 门禁），
   然后 `git rm` 本文件。

全部直接在 main 上做，不开分支、不设影子二进制、不搞切换仪式——目前唯一的用户就是作者本人。
§5 的对齐清单是**工作清单**，不是合入门槛；每个 commit 仍照常过 `bash -n shell/*` 与
`--offline`，所以 tmux 段在哪一步改为驱动 Go，就在哪一步随之迁移。

---

## 7. 未决

- 无。Linux 上 `--watch` 的长连接与 106 字节门限已在 Debian trixie 容器里实测（2026-09-26）：
  openbsd `nc` 下 `tests/playback.sh` 全过。ncat 下原本会卡死，已修：正常结束时由 mpv 的
  `end-file` 收掉连接，mpv 被 `kill -9` 或崩溃时由一个 `read -t 1` 看守兜底，不 fork
  （ARCH-player.md「状态流」）。修后 ncat 下 128/128、macOS 下 130/130，含新增的 `kill -9` 一例。
