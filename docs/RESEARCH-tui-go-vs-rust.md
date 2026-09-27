# RESEARCH — 现代 TUI 技术栈选型：Go (Bubbletea) vs Rust (Ratatui) 深度对比与 ting 的选型决策

> **先说这份调研是为什么做的**：  
> ting 的人机交互面（`shell/ting`）在 Bash 3.2 下把冷兵器极简与排版确定性做到了极致，但受限于 Bash 3.2 整数秒时钟（`read -t 1`）、单进程无并发、无浮点算力与缺乏双缓冲机制的物理铁壁，已无法进一步实现现代极简设计所追求的“高刷新率流体微动效、平滑呼吸感与亚秒级即时心流”。  
> 在确立了**“底层音源引擎与播放生命周期继续留在 Shell，仅将 TUI 独立重构为编译型前端”**（见 [`docs/PLAN-go-tui.md`](PLAN-go-tui.md) 与 [`docs/AUDIT-go-tui.md`](AUDIT-go-tui.md)）的前提下，我们需要在当今现代 TUI 领域的两大绝对霸主——**Go (Bubbletea 生态)** 与 **Rust (Ratatui 生态)** 之间进行系统性对比与实测评估，锁定最适合 ting 演进路线的技术栈。

---

## 1. 调研方法与评估维度

本调研不讨论过时的 C/C++（`ncurses` 内存安全性差、状态变异复杂）或 Python/Node.js（启动延迟高、运行时体积庞大、终端环境依赖重），聚焦于目前开源界生产级应用最成熟的两个现代编译型生态：
1. **Go 体系**：以 `charmbracelet/bubbletea` 为核心，配合 `lipgloss`（样式布局）、`bubbles`（组件库）与 `harmonica`（物理微动效）；
2. **Rust 体系**：以 `ratatui`（原 `tui-rs`）为核心，配合 `crossterm`（终端后端）、`tokio`（异步运行时）与 Cassowary 约束布局引擎。

评估围绕以下 6 个核心工程维度展开：
- **架构范式与状态机模型**（开发心智与代码健壮度）
- **排版引擎与字符度量**（复杂分屏、CJK 双字宽与超窄终端容错）
- **高并发与多路 I/O 汇聚**（外部 Socket 长连接、CLI 子进程编排与事件循环）
- **视觉微质感与氛围感生态**（呼吸感、渐变色彩阶、物理微动效）
- **音视频与系统底层集成度**（外部进程胶水 vs 原生进程内嵌）
- **交付成本与演进经济学**（单二进制体积、跨平台编译、开发人效）

---

## 2. 核心架构范式对比：TEA vs Immediate-Mode

### 1. Go (Bubbletea)：纯函数式 Elm 架构（The Elm Architecture）

Bubbletea 将前端著名的 Elm 单向数据流（Unidirectional Data Flow）完整引入终端开发：

```
  [外部事件 / 定时器 / 管道]
             |
             v
         tea.Msg (不可变数据载荷)
             |
             v
  +--------------------------------------------------------------+
  |                   Model.Update(tea.Msg)                      |
  |                                                              |
  |   纯函数状态转移: 输入旧状态与消息 -> 产出新状态与异步 Cmd   |
  +------------------------------+-------------------------------+
                                 |
                                 v
  +--------------------------------------------------------------+
  |                       Model.View()                           |
  |                                                              |
  |   纯只读投影: 将当前 Model 状态完整渲染为一个多行 String    |
  +------------------------------+-------------------------------+
                                 |
                                 v
        Bubbletea 运行时差量比对 (Diffing) -> 输出 ANSI 至终端
```

- **开发心智**：极简。界面永远是状态的纯函数投影，彻底根除了传统 TUI 中“局部命令式擦写导致字符残留乱飞”的噩梦。
- **并发安全性**：所有异步 I/O 完成后都被规整为 `tea.Msg` 推入主事件通道，`Update()` 永远串行执行，**业务层天然无锁（Lock-Free）**。

### 2. Rust (Ratatui)：即时模式双缓冲画布（Immediate-Mode Canvas）

Ratatui 继承了 Dear ImGui 等游戏图形界面的即时模式（Immediate-Mode）渲染机制：

```
  主事件循环 (Event Loop / Tokio Tick)
             |
             v
  +--------------------------------------------------------------+
  |              terminal.draw(|f: &mut Frame| {                 |
  |                  let chunks = Layout::default().split(...);  |
  |                  f.render_widget(Paragraph::new(...), area); |
  |              })                                              |
  +------------------------------+-------------------------------+
                                 |
                                 v
  Ratatui 双缓冲对比: 前帧 Buffer vs 当前帧 Buffer (Cell 级位运算)
                                 |
                                 v
                 生成最小 ANSI 字符序列写入 stdout
```

- **开发心智**：偏底层图形学思维。开发者直接在每一帧的虚拟画布切片（Area）上绘制 Widget。
- **架构自由度**：Ratatui 本身只管绘制，不强制任何架构；开发者可以自由采用 MVC、组件树或自建 TEA 状态机（如 `tuirealm`）。

**对比结论**：
- **Bubbletea** 提供了高度内聚的一体化运行时，状态转移与 UI 声明边界极其清晰，重构成本极低；
- **Ratatui** 拥有更纯粹的图形管线与极低的抽象开销，但在大型复杂应用中需要开发者自行设计事件流与状态同步架构。

---

## 3. 排版与布局系统：CSS 声明式 vs 数学约束求解器

在终端排版中，最硬核的挑战在于**双栏自适应对齐、窗口动态拉伸缩放、以及 CJK（中日韩）双字宽与 Emoji 零撕裂**。

```
  [Go: Lipgloss 盒模型流式排版]
  +------------------------------------+--------------------------+
  | left := style.Padding(1).Render()  | right := style.Render()  |
  +------------------------------------+--------------------------+
  row := lipgloss.JoinHorizontal(lipgloss.Center, left, right)

  [Rust: Ratatui Cassowary 线性约束布局]
  Layout::default()
      .direction(Direction::Horizontal)
      .constraints([Constraint::Percentage(70), Constraint::Min(20)])
      .split(terminal_area)
```

| 维度 | Go (Lipgloss) | Rust (Ratatui Layout) | 实战评判 |
|---|---|---|---|
| **排版引擎算法** | **声明式盒模型 / Flexbox 语义**。<br>提供 Padding、Margin、Border、Align、`JoinHorizontal` 与 `JoinVertical`。 | **Cassowary 线性算术约束求解算法**。<br>（与 macOS AutoLayout 相同的工业级数学求解器）。 | **Ratatui 数学严密性更强**。<br>Lipgloss 更接近 Web 前端直觉。 |
| **超窄窗口（Terminal Resize）容错** | 依赖开发者在父级容器进行 `MaxHeight` / `MaxWidth` 裁剪，极端窄屏下若未做截断，拼接字符串可能折行下沉。 | 极强。约束求解器在空间受到物理挤压时按比例或硬下限自然收缩，布局永不塌陷。 | **Ratatui 略胜**。在极端小终端下表现更坚固。 |
| **字符度量衡（CJK / Emoji）** | 底层绑定 `rivo/uniseg` 与 `mattn/go-runewidth`，自动计算全半角字宽与 ZWJ 连字。 | 底层绑定 `unicode-width`，在字符单元格（Cell）级别精准跳格。 | **平手**。两者均完全解决了 CJK 锯齿与双栏中轴错位问题。 |

---

## 4. 并发与异步时序模型（Concurrency & Event Multiplexing）

ting 这类流媒体工具的技术特征决定了其核心是一个**重度多路 I/O 汇聚系统**：
1. `ting-play --watch -j` 的长连接状态流（持续读取）；
2. 异步网络拉取（网易云/YouTube 歌词与封面解析，偶发且有延迟）；
3. 键盘原始字节输入与转义序列解码（即时响应）；
4. 30 FPS / 60 FPS 呼吸感微动效时钟（平滑驱动）。

```
                 Go (Bubbletea)                            Rust (Ratatui)
       +--------------------------------+        +--------------------------------+
       |     Goroutine (协程并发)       |        |    Tokio Task (异步任务)       |
       |   - 读取 --watch NDJSON 流     |        |   - async/await 读取流         |
       |   - 异步拉取歌词与图片         |        |   - 异步拉取歌词与图片         |
       +---------------+----------------+        +---------------+----------------+
                       |                                         |
                       | p.Send(msg) (无锁通道)                   | sender.send(Event)
                       v                                         v
       +--------------------------------+        +--------------------------------+
       |   Bubbletea 主事件串行循环     |        |   tokio::select! 宏多路分发    |
       +--------------------------------+        +--------------------------------+
```

### 1. Go 的并发体验
- **心智负担接近于零**：后台读 socket 开一个轻量 `go func()`，读到数据后调用 `program.Send(MyEventMsg{})` 即可；
- **免加锁**：Bubbletea 底层将消息排队推入主更新循环，业务状态永远只在主 Goroutine 中读写，无需任何 `sync.Mutex`。

### 2. Rust 的并发体验
- **极致无开销，但生命周期与借用机制严酷**：
  - 通常依赖 `tokio::select!` 宏或跨线程 MPSC 通道；
  - 共享状态必须包装为 `Arc<Mutex<AppState>>` 或 `Arc<RwLock<AppState>>`；
  - 在异步任务闭包中捕获环境时，频繁遭遇 `'static` 生命周期与所有权（Ownership）转移的编译期博弈，代码膨胀度高。

---

## 5. 视觉微质感与氛围感生态（Aesthetics & Micro-styling）

这是弥合感官断层、实现“极简呼吸感与沉浸感”的核心战场。

| 维度 | Go 阵营 (Charm 生态) | Rust 阵营 (Ratatui 生态) |
|---|---|---|
| **调色盘与灰阶质感** | **行业审美标杆**。原生支持 Adaptive Color（亮暗终端智能适配）、TrueColor 灰阶调教极其成熟，原生贴合 Slate/Zinc 极简性冷淡风。 | 同样完整支持 24-bit TrueColor，但官方调色体系偏原始 RGB，需要开发者自行设计灰阶色标。 |
| **物理微动效 (Physics)** | **官方提供 `charmbracelet/harmonica`**：开箱即用的**弹簧衰减物理学系统（Spring-damper）**，光标移动与数值滑动自带物理惯性微颤。 | 社区缺乏统一的物理动画库，微动效通常需要开发者自己手搓线性插值（`lerp`）或贝塞尔缓动函数。 |
| **开箱即用微动效组件** | **`bubbles` 官方套件**：内置 20+ 款高质感微光加载指示器（`spinner`）、平滑渐变进度条（`progress`）、光标呼吸输入框（`textinput`）。 | 内置组件极其硬核（Canvas 矢量画板、火花线 Sparkline、实时图表 BarChart），但风格偏“工业监控台 / 数据密集型”，少轻量呼吸感。 |

**实战断语**：
Charm 团队本质上是一家设计驱动的极客团队，其整个工具链的基因就是**“把现代化 Web/GUI 级别的视觉微质感搬到字符网格中”**；而 Ratatui 社区以系统工程师为主，更偏向工业级、数据密集型的仪表盘构建。

---

## 6. 底层音视频与系统集成度（Media Integration）

在多媒体流媒体领域，两种语言展现出了完全不同的生态位：

### 1. Go：天生适合做“高阶客户端编排器（Client Orchestrator）”
- **CGO 的代价**：在 Go 中通过 CGO 绑定底层 C 库（如链接 `libmpv`）会导致交叉编译极其繁琐，且会影响 Go 调度器运行效率；
- **最佳姿态**：Go 极其擅长通过标准 I/O 管道、Unix Domain Socket 和进程命令行（argv）调动宿主已有的外部工具链（`ting-play`、`mpv`、`curl`）。

### 2. Rust：具备“直接吞并底层、全进程内嵌”的终极统治力
- **原生 FFI 零成本**：Rust 可以通过 `libmpv-rs` 直接将 mpv 核心静态编译进可执行文件，完全不需要走外部管道或 socket 文件；
- **纯原生解码库**：拥有纯 Rust 实现的音频解码与重采样库（`symphonia`、`rodio`），甚至可以在完全不依赖外部 `mpv` 的情况下直接驱动声卡（如 `spotify-player`）。

---

## 7. 全维度技术指标终极对比矩阵

| 评估指标 | **Go (Bubbletea + Lipgloss)** | **Rust (Ratatui + Crossterm)** | 胜出者与评判 |
|---|---|---|---|
| **架构哲学** | 声明式单向数据流 (TEA / MVU) | 即时模式双缓冲 (Immediate-Mode) | **Go 更利于状态维护**，Rust 自由度更高 |
| **极限渲染吞吐** | 稳定维持 30 ~ 60 FPS | 轻松突破 60 ~ 120 FPS，吞吐上限最高 | **Rust 略优**（原生 Cell 位比对） |
| **开发人效与交付速度** | **极高**。心智负担低，1~2 天即可出高质量原型 | 中等。需与异步生命周期、锁与借用检查搏斗 | **Go 压倒性胜出** |
| **视觉微质感与呼吸感** | **极高**。CSS 盒模型 + 官方弹簧动效，审美开箱即用 | 工业冷峻。约束严密，但动效与微调需手搓 | **Go 胜出**（审美生态更现代） |
| **并发与状态机心智** | **极顺畅**。Goroutine + Channel 无痛接入主循环 | 较重。Tokio select + Arc/Mutex 样板代码较多 | **Go 胜出** |
| **单二进制体积** | ~15MB - 30MB（包含 Go 运行时与 GC） | **~2MB - 5MB**（纯静态，无 GC） | **Rust 胜出** |
| **内存底噪与 CPU 占用** | 常驻内存约 20MB ~ 40MB，GC 抖动 <1ms | **常驻内存约 5MB ~ 15MB，零 GC** | **Rust 胜出** |
| **跨平台交叉编译** | `CGO_ENABLED=0 go build` 极其简单 | 借助 `cross` / `cargo build --target`，略繁琐 | **Go 略优** |
| **重构灵活性** | 增删状态字段极其敏捷，类型系统宽容度高 | 字段重构牵一发动全身，生命周期标注易连锁失效 | **Go 胜出** |

---

## 8. 选型推演与决策结论（The Verdict）

### 1. 为什么“推倒全套重写为单一二进制”在当前阶段被否决？

回顾 [`docs/ARCHITECTURE.md`](ARCHITECTURE.md)「分析：驱动决定的六条发现」与 [`docs/ROADMAP.md`](ROADMAP.md)「Go 重写 NO」：
1. **外部音源站点的生存依赖于“原地可改性”**：
   Bilibili、网易云等国内站点的风控机制、签名算法变动极其频繁。纯 Shell 编写的 `ting-engine-<site>` 允许用户或开发者随时在终端通过 `vi` 原地热修；若全量用编译型语言重写，每次外部站点微调都将沦为一次漫长的“改代码 -> 交叉编译 -> 打包发布”流程。
2. **外部依赖（yt-dlp、mpv）依然无法彻底消除**：
   只要还需要播放 YouTube 和 B 站高码率流，任何语言都必须依赖 `yt-dlp` 的庞大提取器网络。因此，**所谓纯单一无依赖二进制（Single-binary）在流媒体场景下本身就是虚幻的**。

### 2. 为什么在“独立 TUI 客户端”场景下，坚决选择 Go (Bubbletea)？

在已经确定**“保持底层 7 个 Shell 脚本（3 个引擎文件 + 3 个 CLI 命令 + 原 bash ting）负责音源抽取、生命周期控制与持久存储，仅重构人机那一面”**的前提下，新 TUI 的核心使命发生了根本性转变：

> **新 TUI 不是媒体播放内核，它是这套 CLI 引擎最强大的“高级 VIP 前端与编排器”。**

在这个清晰的生态位上，**Go (Bubbletea) 展现出了对 Rust (Ratatui) 的压倒性优势**：
1. **使命高度对齐**：重构 TUI 的唯一驱动力就是**“打破 Bash 3.2 极限，补齐感官协同 Gap，做出高颜值的呼吸感与现代心流”**。在这方面，Charm (Bubbletea + Lipgloss + Harmonica) 是目前整个终端世界公认的设计与审美天花板，能够以最低的开发成本直接兑现极致质感；
2. **人效与风险收益比（ROI）最优**：使用 Bubbletea 可以在数天内无痛对齐既有 8800 行 Bash 的交互细节，并快速接上 `--watch` 管道流；而如果选择 Rust，大量精力将被消耗在异步通道编排、锁竞争规避与生命周期注解上，与“快速改善人机交互体验”的核心诉求背道而驰；
3. **架构解耦的试金石**：选择 Go 编写独立 TUI，能够倒逼底层 CLI 契约（`ting-play --watch -j`、`--capabilities -j`）做到绝对正规化与彻底解耦，使 Go TUI 成为与 Agent（Claude Code 等）地位完全平等的纯契约消费者。

---

## 9. 结论落定

- **最终技术选型**：**Go (Bubbletea + Lipgloss + Harmonica)**；
- **分层边界锁定**：
  - Go 只负责绘制终端画面、管理交互事件循环、并在内存中做 60 FPS 平滑微动效插值；
  - Go 内部严禁出现 `mpv`、`yt-dlp` 进程直接调用或站点逆向逻辑，所有底层行为严格通过 `pkg/verb` 调动既有 Shell CLI 动词完成。
- **关联设计与施工依据**：
  - 架构实施计划详见：[`docs/PLAN-go-tui.md`](PLAN-go-tui.md)
  - 核心暗礁与规避补丁详见：[`docs/AUDIT-go-tui.md`](AUDIT-go-tui.md)
