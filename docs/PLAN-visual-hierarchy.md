# PLAN — 三层灰阶对比规范与按键/底噪弱化（Visual Hierarchy）实施草案

> **Status**: 草案 (Draft) · 待实施  
> **Priority**: 第一梯队（中高 ROI · TUI 审美质感与现代极简主义设计系统收敛）  
> **Target Branch**: main  
> **Roadmap 关联**: [`docs/ROADMAP.md`](ROADMAP.md)「待办与待决事项」——【中高 ROI · 待做 · 视觉层级】三层灰阶对比规范与按键/底噪弱化（Visual Hierarchy）  
> **Governing Docs**: [`docs/ARCHITECTURE.md`](ARCHITECTURE.md)「定位与设计目标」、[`docs/ARCH-tui.md`](ARCH-tui.md)「界面设计决定」「主题与偏好」「焦点行歌词窥探」「封面」  
> **Verification**: `go vet ./... && go test -short ./...`、`tests/contract.sh --offline`、`tests/contract.sh`、`tests/drive.sh` (全量 13 套主题 × 暗/亮底 × 窄宽屏实测验证)  
> **Scope Boundary**: 严格零新增外部运行时依赖；不破坏任何既有 CLI 信封、选项或退出码；纯 Go 实现（收敛于 `internal/tui/style.go` 与 `internal/tui/view.go`）；严格维持 CJK 字符宽度度量不变量；不引入第三方样式库或 CSS 解析器；保持 `paletteFor` 作为 `(theme, background, truecolor)` 纯函数的设计准则。

---

## 1. Grounding 基线与问题根因

### 1.1 现状代码核查与视觉信噪比缺陷

经对 `internal/tui/style.go` 与 `internal/tui/view.go` 的全面核查，当前 TUI 的色彩分层存在严重的**二元化坍塌**与**视觉底噪过载**问题：

1. **色彩层级坍塌为单一 `\x1b[2m`（Dim 泛滥）**：
   - 当前 `palette` 结构体仅定义了 `Reset, Bold, Dim, Accent, Mark, Play, PauseC, RowHL, RowEnd, Keycap`。
   - 所有非强调信息无差别共用 `p.Dim`（即 ANSI SGR 2 代码 `\x1b[2m`）：
     - 列表行右轨时长（`rail`）：`p.Dim + rail`
     - 列表滚动条（`gut`）：`p.Dim + gut`
     - 状态栏元数据（引擎、数量、排序、音质、音量）：`m.p.Dim + strings.Join(items, " "+m.g.Sep+" ")`
     - 分割点（`·`）：混在 `p.Dim` 中与文字无阶梯差
     - 播放横幅的时间进度（`【01:23/04:56】`）：`p.Dim + right`
     - 详情面板 Meta（频道、播放量、时长、格式）：`p.Dim + d`
     - 底部按键说明标签（`label`）：`p.Dim + h.label`
     - 进度条未播槽底（`rest`）：`m.p.Dim + m.p.Accent + rest.String()`
   - **痛点**：人类视网膜无法在同为 `p.Dim` 的界面中区分“辅助决策的关键元数据”（如曲目时长、作者、播放进度）与“环境背景脚手架”（如按键提示、分割线、滚动条轨道）。信息密度被抹平，失去主次纵深。

2. **终端对 `\x1b[2m`（SGR 2 Dim/Faint）解释离散不可控**：
   - ANSI SGR 2 在现代终端（iTerm2、Alacritty、Kitty、Ghostty、WezTerm、Apple Terminal）表现极不稳定：部分终端仅降微弱对比度（几乎与正文无异），部分终端产生模糊灰色，部分终端完全不支持而直接回退到标准前景色。
   - 在已支持 24-bit TrueColor（`COLORTERM=truecolor`）的现代环境中，系统未利用精确 RGB 混合计算灰阶，导致社区精致主题（如 Catppuccin、Tokyo Night、Nord、Rose Pine、Everforest）的高级质感大打折扣。

3. **底部快捷键（Keycaps & Bottom Nav）高底噪喧宾夺主**：
   - 现行 `p.Keycap` 在 TrueColor 下计算公式为：
     `c := blend(ground, far, 14)`（暗底提升 14%，亮底下降 10%）
     按键文本为 `p.Keycap + p.Bold + h.key + p.Reset`（带反色背景块与**加粗**白字）。
   - 在 16 色或 Minimal 模式下，直接使用高对比度反色块：`\x1b[100;97m`（亮黑底、高亮白字）。
   - 当 `keys=full` 展开时，屏幕底部出现 3 行共计 20~30 枚实体胶囊徽章。高频视觉块密集轰炸用户余光，不仅抢占视觉焦点，更彻底破坏了现代极简主义所追求的“内容呼吸感”（Breathing Room）。

4. **滚动条（Scrollbar）与分割线样式缺失与异常**：
   - 游标行（`i == m.cursor`）在 `renderRows` 中存在渲染失误：
     `b.WriteString(p.Mark + mark + num + p.Reset + p.Bold + title + p.Reset + fill + p.Dim + rail + p.Reset + " " + gut)`
     在 `p.Reset` 后直接打印 `" " + gut`，导致光标所在行的滚动条滑块/轨道**未被 Dim 弱化**，意外呈现刺眼的高亮白；而在普通行与在播行上却带 `p.Dim`。
   - 滚动条滑块（`Thumb: "█"`）与轨道（`Track: "│"`）缺乏独立的色彩控制，无法建立“轨道微弱、滑块明确”的空间定位感。

---

## 2. 三层视觉层级系统（Visual Hierarchy Specification）

向 Linear / Modern Minimalist 设计语言对齐，建立严格的**三层视觉层级模型**。

```
+--------------------------------------------------------------------------------+
|  Tier 1: Foreground (前景色 / 焦点 / 语义主体)                                  |
|  - Contrast: 最高对比度 (>= 7:1)                                                |
|  - Elements: 焦点标题、播放歌词、横幅标题、状态高亮 (Play/Pause)、输入光标       |
|  - Token:    p.Bold, p.Accent, p.Play, p.PauseC, Standard FG                   |
+--------------------------------------------------------------------------------+
                                       |
                                       v
+--------------------------------------------------------------------------------+
|  Tier 2: Secondary (次级信息 / 核心决策元数据 / 中灰阶)                         |
|  - Contrast: 中对比度 (4.5:1 ~ 6:1, WCAG AA)                                    |
|  - Elements: 曲目时长、行序号、分 P/章节、状态栏状态项、横幅时间进度、Details Meta|
|  - Token:    p.Secondary                                                       |
+--------------------------------------------------------------------------------+
                                       |
                                       v
+--------------------------------------------------------------------------------+
|  Tier 3: Muted / Tertiary (环境底噪 / 结构线 / 暗灰阶)                          |
|  - Contrast: 低对比度 (2:1 ~ 3:1, 自然融入背景)                                 |
|  - Elements: 按键提示 (Keycaps/Labels)、滚动条轨道、分割点 (·)、系统负载、槽底  |
|  - Token:    p.Muted, p.Keycap, p.KeyText, p.KeyLabel, p.Track, p.Thumb        |
+--------------------------------------------------------------------------------+
```

### 2.1 语义元素映射权威表

| UI 组件区域 | 具体展示元素 | 归属层级 | 视觉规范 (Token) | 审美目的与行为特征 |
|---|---|---|---|---|
| **标题与状态行** | 品牌标识发声态 (`【 听 】`) | Tier 1 | `p.Bold + p.Accent` | 视觉顶端发声锚点 |
| | 查询词内容 (`query='...'`) | Tier 1 | 标准正文 (FG) | 明确当前检索上下文 |
| | 状态项（引擎/条数/排序/音质等） | **Tier 2** | `p.Secondary` | 清晰可见，不喧宾夺主 |
| | 分割符（`·`） | **Tier 3** | `p.Muted` | 结构占位，视觉退避 |
| **通知与进度** | 提示标题 (`noticeL`) | Tier 1 | `p.Bold` | 关键通知即时引起注意 |
| | 提示正文 / 加载中 (`busy`) | **Tier 2** | `p.Secondary` | 状态反馈清晰舒适 |
| | 撤销引导 (`nu: "· z 撤销"`) | **Tier 3** | `p.Muted` | 次要兜底动作，余光感知 |
| **在播横幅** | 播放状态图标与文字 (`▶ 播放中:`) | Tier 1 | `color (p.Play / p.PauseC)` | 权威状态感知 |
| | 当前播放曲目标题 (`title`) | Tier 1 | `p.Accent` | 核心消费对象强化 |
| | 播放时钟进度 (`【01:23/04:56】`) | **Tier 2** | `p.Secondary` | 听觉伴随核心决策数据 |
| | 系统资源监控 (`· cpu 2% · ram 45M`)| **Tier 3** | `p.Muted` | 极客诊断数据，彻底底噪化 |
| | 兜底操作按键 (`[Space 暂停 ...]`) | **Tier 3** | `p.Muted` | 无空间时的紧凑按键弱化 |
| **播放进度条** | 已播放进度实体 (`m.g.Fill`) | Tier 1 | `p.Accent` | 鲜明感知当前进度 |
| | 未播放槽底线 (`m.g.Rest: "─"`) | **Tier 3** | `p.Muted` | 轨道槽底，自然隐入背景 |
| **主列表行** | 焦点行标题 (`i == m.cursor`) | Tier 1 | `p.Bold` (+ 光标 `p.Mark`) | 绝对视觉重心 |
| | 在播行标题 (`i == playing`) | Tier 1 | `p.RowHL + p.Bold` | 播放条目全行高光底纹 |
| | 普通行标题 (`default`) | Tier 1 | 标准正文 (FG) | 干净清爽的内容呈现 |
| | 右轨时长 (`rail: 03:45 / LIVE`) | **Tier 2** | `p.Secondary` | 快速扫视时一眼辨别时长 |
| | 行号 (`m.opt.RowIndex: " 1."`) | **Tier 3** | `p.Muted` (光标行用 `p.Secondary`) | 消除行号对歌名的视觉割裂 |
| | 滚动条滑块 (`gut: █`) | **Tier 2** | `p.Thumb` (或 `p.Secondary`) | 明确视窗定位 |
| | 滚动条轨道 (`gut: │`) | **Tier 3** | `p.Track` (或 `p.Muted`) | 微弱边界线 |
| **详情面板** | 发声歌词正文 (`lyric`) | Tier 1 | `p.Accent` (切换时 `Bold+Accent`) | 当前聆听核心语义 |
| | 歌词前奏/间奏 (`♪  · · ·`) | **Tier 3** | `p.Muted` | 等待状态弱化 |
| | 基础元数据 (作者/播放量/年份) | **Tier 2** | `p.Secondary` (分割点用 `p.Muted`)| 辅助判断条目质量 |
| | 解码格式 (`flac · 44.1kHz`) | **Tier 2** | `p.Secondary` (分割点用 `p.Muted`)| 纯音频发烧友信息 |
| | 简介正文 (`Desc`) | **Tier 3** | `p.Muted` (或柔和中灰) | 作为副文本，不压过列表 |
| **底部导航区** | 按键胶囊背景 (`p.Keycap`) | **Tier 3** | 极淡微胶囊或幽灵底 (5%~7% 混合)| 彻底消除黑色/高亮斑马纹 |
| | 按键字符 (`h.key: ⏎ / Space / q`)| **Tier 2** | `p.KeyText` (柔和中灰，取消 Bold) | 保证字形清晰但放弃刺眼加粗 |
| | 按键动作说明 (`h.label: 播放 / 退出`) | **Tier 3** | `p.KeyLabel` (暗灰阶 `p.Muted`) | 彻底弱化，只有寻找时可见 |

---

## 3. 数学色彩模型与 `style.go` 架构重构

### 3.1 调色板结构体（`palette`）增量演进

保持零外部依赖，纯原生 SGR 序列。扩展后的 `palette` 显式承载三层灰阶与组件级语义 token：

```go
type palette struct {
    // 基础与兼容
    Reset, Bold, Dim string
    on, tc, mono     bool

    // Tier 1: Foreground & Status Accents
    Accent string // 主题强调色 (Hex 或 ANSI-16)
    Mark   string // 光标标识 (Bold + Accent)
    Play   string // 播放状态色
    PauseC string // 暂停状态色
    RowHL  string // 在播行背景高光
    RowEnd string // 在播行结束符

    // Tier 2: Secondary (中灰阶)
    Secondary string // 次级关键信息：时长、行号、状态项、时间进度

    // Tier 3: Muted / Tertiary (暗灰阶与底噪)
    Muted string // 结构骨架、分割符、环境线、歌词间奏

    // 按键与交互弱化专有 Token
    Keycap   string // 按键胶囊背景 (极浅对比)
    KeyText  string // 按键字符样式 (中灰，取消粗体)
    KeyLabel string // 按键功能说明 (暗灰阶)

    // 滚动条专有 Token
    Thumb string // 滑块样式 (中灰/微亮)
    Track string // 轨道样式 (暗灰阶)
}
```

### 3.2 纯数学色彩推导逻辑（`paletteFor`）

`paletteFor(colors bool, theme string, bg Background, truecolor bool) palette` 必须严格保持为纯函数，实时响应 `t` 键主题轮转与环境变化：

#### 1. TrueColor 模式（24-bit RGB）
依据背景基底色 `ground` 与反方向极点色 `far`（暗底为 `rgb{255, 255, 255}`，亮底为 `rgb{0, 0, 0}`）进行精确阶梯混合：

- **Secondary（中灰阶）**：
  - 暗底（Dark）：`blend(ground, far, 60)` -> 产生清晰典雅的中浅灰（Luminance ~60%），对比度约 5.5:1，高度易读；
  - 亮底（Light）：`blend(ground, far, 55)` -> 产生优雅的中深灰（Luminance ~45%），对比度约 5.0:1。
  - SGR: `\x1b[38;2;R;G;Bm`
- **Muted（暗灰阶）**：
  - 暗底（Dark）：`blend(ground, far, 28)` -> 产生沉静的暗灰（Luminance ~25%~30%），对比度约 2.2:1，退居环境背景；
  - 亮底（Light）：`blend(ground, far, 22)` -> 产生柔和的浅灰（Luminance ~75%~80%），对比度约 2.2:1。
  - SGR: `\x1b[38;2;R;G;Bm`
- **按键底噪弱化计算（Keycaps & Labels）**：
  - 现行 14% 的暗底抬升产生过强的“发光棋盘”效应。将其大幅弱化收敛至 **6%**（暗底）与 **5%**（亮底）：
    `c := blend(ground, far, 6)`
  - 按键字符 `KeyText`：不套加粗白字，直接赋以 `blend(ground, far, 75)` 或 `Secondary`；
  - 按键说明 `KeyLabel`：赋以 `Muted`；
  - **效果**：底部原本喧闹夺目的几十个色块瞬间收敛为柔和、极简的微胶囊提示，终端视线完全集中于曲目内容区。
- **滚动条计算（Thumb & Track）**：
  - `Track`（轨道 `│`）：赋以 `Muted`；
  - `Thumb`（滑块 `█`）：赋以 `blend(ground, far, 70)` 或 `Secondary`。

#### 2. ANSI-16 降级回退模式（8/16 色终端）
不引入复杂的颜色模拟，使用最稳健的标准 ANSI 色号：
- `Secondary`: 暗底 `\x1b[37m`（标准浅白/灰）；亮底 `\x1b[30m`（标准黑）或 `\x1b[90m`；
- `Muted`: 暗底 `\x1b[90m`（亮黑/深灰）；亮底 `\x1b[37m`（浅白灰）或 `\x1b[2m`；
- `Keycap`: 废弃刺眼的 `\x1b[100;97m`，改为柔和的 `\x1b[2m`（Faint/Dim）或取消背景填充；
- `KeyText`: `\x1b[37m`（正常浅色，取消粗体）；
- `KeyLabel`: `\x1b[2m`（弱化）；
- `Track`: `\x1b[90m`；`Thumb`: `\x1b[37m`。

#### 3. Monochrome 极简模式（`mono`）与无颜色模式（`NO_COLOR`）
- `mono` 主题：
  - `Secondary` 为标准正文（空 escape 或 `\x1b[0m`）；
  - `Muted` 为 `\x1b[2m`；
  - `Keycap` 为空，`KeyText` 为 `\x1b[1m`（粗体），`KeyLabel` 为 `\x1b[2m`；
  - `Track` 为 `\x1b[2m`，`Thumb` 为 `\x1b[1m`。
- 无颜色模式（`!colors`）：全部字段均返回空字符串，保证宽度度量与原生终端 100% 纯净。

---

## 4. TUI 渲染流水线全面改造（`internal/tui/view.go`）

### 4.1 列表渲染器改造（`renderRows`）

修复滚动条漏色 Bug，严格落实行号、标题、时长、滚动条的四阶分离：

```go
// 修正后的 renderRows 核心着色逻辑示意
railText := p.Secondary + rail + p.Reset
gutText  := p.Track + gut + p.Reset
if thLen > 0 && i-f.start >= thTop && i-f.start < thTop+thLen {
    gutText = p.Thumb + gut + p.Reset
}

switch {
case i == playing:
    // 在播行：RowHL 背景通栏贯穿，标题加粗，滚动条外置
    b.WriteString(p.RowHL + mark + num + p.Bold + title + fill + rail + p.RowEnd + " " + gutText)
case i == m.cursor:
    // 光标行：Mark 强调前缀，序号 Secondary，标题 Bold，右轨 Secondary，滚动条 Track/Thumb
    b.WriteString(p.Mark + mark + p.Secondary + num + p.Reset + p.Bold + title + p.Reset + fill + railText + " " + gutText)
default:
    // 普通行：序号 Muted，标题常规 FG，右轨 Secondary，滚动条 Track/Thumb
    b.WriteString(mark + p.Muted + num + p.Reset + title + fill + railText + " " + gutText)
}
```

### 4.2 状态栏与分割点改造（`statusLine` & `statusBlock`）

消灭大一统的 `p.Dim` 拼接，将元数据赋予 `Secondary`，将结构分割点赋予 `Muted`：

```go
func (m *Model) statusLine(items []string) string {
    sep := " " + m.p.Muted + m.g.Sep + m.p.Secondary + " "
    return m.p.Secondary + strings.Join(items, sep) + m.p.Reset
}
```

### 4.3 在播横幅改造（`banner`）与进度条（`progressBar`）

- 播放横幅右侧信息分级：
  ```go
  // tail: 【01:23/04:56】 -> 核心进度数据属于 Tier 2 Secondary
  tailFormatted := p.Secondary + tail + p.Reset
  // res: · cpu 2% · ram 45M -> 系统负载属于 Tier 3 Muted
  resFormatted := p.Muted + res + p.Reset
  // hintS: [Space 暂停 ...] -> 兜底按键属于 Tier 3 Muted
  hintFormatted := p.Muted + hintS + p.Reset
  ```
- 进度条槽底（`progressBar`）：
  ```go
  // 未播放槽底线使用 Muted，彻底消除未播放区域带主题色的视觉干扰
  return m.p.Accent + played.String() + m.p.Reset + m.p.Muted + rest.String() + m.p.Reset
  ```

### 4.4 详情面板与歌词改造（`detailLines` & `lyricLine`）

- **元数据行**：艺术家、播放量、时长赋予 `p.Secondary`，连接点赋予 `p.Muted`。
- **解码信息行**：`flac · 44.1kHz · 920kbps` 赋予 `p.Secondary`，连接点赋予 `p.Muted`。
- **歌词正文**：发声中赋予 `p.Accent`（跳句瞬间 `p.Bold + p.Accent`）。
- **歌词间奏**：前奏/间奏 `♪  · · ·` 赋予 `p.Muted`。
- **曲目描述**：副文本赋予 `p.Muted`，作为轻量补充信息，绝不抢占曲目列表的视线。

### 4.5 底部导航区改造（`navLines`）

彻底弱化按键底噪，重塑内容呼吸感：

```go
for _, l := range f.navLines {
    var cells []string
    for _, h := range l {
        // 微胶囊底纹 + 柔和中灰字符（取消粗体加粗）
        c := p.Keycap + p.KeyText + h.key + p.Reset
        if h.label != "" {
            // 暗灰阶功能说明
            c += " " + p.KeyLabel + h.label + p.Reset
        }
        cells = append(cells, c)
    }
    line("  " + strings.Join(cells, keycapSep))
}
```

---

## 5. 实施里程碑与落地步骤（Milestones）

实施严格划分为四个阶段，保证每一步均可独立测试、零退化：

```
+--------------------------------------------------------------------------------+
| Milestone 1: 色彩数学模型扩展 (internal/tui/style.go)                           |
| - 扩展 palette 结构体字段 (Secondary, Muted, KeyText, KeyLabel, Thumb, Track)  |
| - 实现基于 ground 的阶梯数学 blend 计算 (60% / 28% / 6% 混合比)                |
| - 完成 ANSI-16 与 mono 降级分支覆盖                                             |
| - 编写 palette_test.go 单元测试，断言对比度与 SGR 正确性                        |
+--------------------------------------------------------------------------------+
                                       |
                                       v
+--------------------------------------------------------------------------------+
| Milestone 2: 渲染管道色阶重构 (internal/tui/view.go)                            |
| - 重构 renderRows: 行号 Muted/Secondary、时长 Secondary、滚动条分色             |
| - 重构 statusLine: 状态项 Secondary、分割点 Muted                               |
| - 重构 banner: 时间进度 Secondary、资源与按键 Muted                             |
| - 重构 detailLines: 元数据/解码信息 Secondary、描述/间奏 Muted                  |
| - 重构 navLines: 微胶囊弱化底噪、KeyText/KeyLabel 阶梯化                         |
| - 修复光标行滚动条漏色 Bug                                                      |
+--------------------------------------------------------------------------------+
                                       |
                                       v
+--------------------------------------------------------------------------------+
| Milestone 3: 自动化测试与契约守护                                              |
| - 更新 internal/tui/view_test.go 现有测试用例                                   |
| - 增加三层灰阶分层断言测试 (Assert visual hierarchy tiers)                      |
| - 运行 go test -short ./... 全绿验证                                            |
| - 运行 tests/contract.sh --offline 保证 CLI/TUI 契约零退化                      |
+--------------------------------------------------------------------------------+
                                       |
                                       v
+--------------------------------------------------------------------------------+
| Milestone 4: 全主题实机视觉审计与微调 (Look & Feel Audit)                      |
| - 使用 tests/drive.sh 批量捕获 13 套内置主题在 Dark 与 Light 模式下的帧截图     |
| - 针对 Minimal, Tokyo Night, Catppuccin, Nord, Gruvbox 逐一审查呼吸感           |
| - 验证 80x24 窄屏下密集排版的视觉舒适度与无遮挡性                               |
| - 更新 docs/ARCH-tui.md 架构正本，在 ROADMAP.md 中勾除本议题                    |
+--------------------------------------------------------------------------------+
```

---

## 6. 验证与回归矩阵（Verification Matrix）

依照套件核心开发规范，严禁凭空推演，必须经过严格的本地自动化与终端驱动验证：

1. **语法与静态分析**：
   ```sh
   bash -n shell/*
   go vet ./...
   ```
2. **Go 单元测试与基准测试**：
   ```sh
   go test -v -short ./internal/tui/...
   ```
   - 验证 `paletteFor` 在 13 套主题下的数学单调性（Luminance(FG) > Luminance(Secondary) > Luminance(Muted) > Luminance(Ground) 在暗底成立；反之在亮底成立）；
   - 验证无色彩模式（`--color never`）下 SGR 字符串均为空；
   - 验证 CJK 字符宽度度量不变量（`width.of()` 绝不被颜色代码污染）。
3. **离线契约门禁**：
   ```sh
   tests/contract.sh --offline
   ```
   保证所有非 TTY / 启动 / 参数拒绝门禁 100% 保持原有退出码与信封标准。
4. **真实终端自动化驱动（`tests/drive.sh`）**：
   - 驱动 13 套主题轮转截图审查：
     ```sh
     for t in minimal catppuccin tokyonight nord gruvbox rosepine mono; do
       TING_THEME=$t TING_KEYS=full tests/drive.sh -x 80 -y 24 -q '周杰伦'
     done
     ```
   - 审查检查项：
     - [ ] 底部 3 行按键提示不再呈现刺眼的“黑白相间斑马块”，视觉余光感知柔和自然；
     - [ ] 列表行右轨时长比歌名略低一级亮度，阅读清晰但目光自然聚焦于歌名；
     - [ ] 滚动条轨道极暗，滑块位置清晰，游标移动时滚动条无闪烁或反常高光；
     - [ ] 状态栏分割点（`·`）退避，状态词主体清晰；
     - [ ] 横幅播放进度时钟一目了然，与后台资源监控（CPU/RAM）拉开明显层次。
