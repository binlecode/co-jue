# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ⚠️ 关键陷阱 —— 开工前必看

开工前必须明确以下四条硬底线，**违反会导致脚本在受限环境当场挂死或破坏核心契约**：

- 🔴 **bash 3.2 是硬性冻结底线**：macOS 系统自带 `/bin/bash` 是唯一运行基准。严禁使用 bash 4+ 特性（`declare -A` 关联数组、`${var,,}`/`${var^^}`、`mapfile`/`readarray`、`${arr[-1]}`、`&>>`、`|&`、`${!prefix@}`）。在 `set -u` 下展开空数组必须写为 `${arr[@]+"${arr[@]}"}` 或前置判断 `((${#arr[@]}))`；模式替换 `${var//pat/}` 存在多字节二次方耗时陷阱，严禁在热路径使用。
- 🔴 **零新增运行时依赖**：外部依赖严格锁定为五个（`yt-dlp`、`jq`、`mpv`、带 `-U` 的 `nc`、`curl`；`openssl` 仅限 `ne-search` 引擎局部加密使用）。严禁为任何功能引入 `socat`、`chafa`、`img2sixel`、`fzf` 等新依赖。
- 🔴 **CLI 契约本身就是产品与安全边界**：本套件不设 MCP 包装层，面向 Agent 直接暴露可执行命令；退出码严格遵循四级分类法（`0` 成功 / `1` 命令行用法错 / `2+` 外部工具透传失败 / `4` 业务语义未生效）。任何功能改动均严禁静默修改既有信封字段或退出码分配。
- 🔴 **状态目录与临时文件严格隔离**：运行时临时目录必须收容在 `$TMPDIR/ting-<uid>/`，持久化状态仅限 `$TING_STATE_DIR`（旧名 `$UT_STATE_DIR` 仍受理；默认 `~/.local/state/ting/`，改名前的 `~/.local/state/uting/` 在没有新目录时继续沿用），脚本自测临时产物一律限在 `tmp/` 下，严禁向源码树写脏文件。

---

## 🔴 第一条：不要自作聪明

**有疑问或做技术选型时，严格按两步执行，顺序不可颠倒：**

1. **先 grounding** —— 查外部平台（YouTube / Bilibili / 网易云）最新接口与风控机制、核查 `yt-dlp` 与 `mpv` 实际 IPC 行为、通读本地脚本与测试套件，**深入代码实现与真实运行输出，严禁凭空假设向下推演**。
2. **再确认** —— 严禁静默新增/废弃选项、修改信封字段、私自放宽门控或重新解释需求。若实测推翻了前提，如实报送发现并提问，**不要自行改动设计范围或重排优先级**。

---

## 项目性质

**ting**（听）—— 面向人机双界面的轻量级流媒体终端引擎。由 10 个独立可执行脚本组成，平级无内核。

- **两面 100% 自有**：
  - **Agent 优先的 CLI 契约面**：单行 JSON 信封（`-j`）、确定性退出码、脱离终端的后台播放生命周期控制（`-d` / `--status` / `--stop`）；
  - **人机交互的终端面**：基于原生终端转义序列自绘的原地重绘单视图 TUI（`shell/ting`）。
- **职责彻底解耦**：音源站点知识完全关在引擎对（`yt-*`, `bili-*`, `ne-*`）内；音频解码与进程生命周期完全关在播放器（`t-play`）内；人机交互完全关在 TUI 内。

---

## 文档分工（先读，别重复摸索）

全部架构文档统一位于 `docs/`。

- 🔴 **全文档唯一正本路由在 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)**，这是架构伞文档。
- 🔴 **每份 `ARCH-*.md` 文首必带「模块功能和结构」与边界表** —— 判断「某功能或改动归哪一份管」先看边界表。
- 🔴 **未决议题、待做特性与记录在案的 NO 只认 [`docs/ROADMAP.md`](docs/ROADMAP.md)**。

| 文种 | 命名规范 | 职责与生命周期 |
|---|---|---|
| 路线图 | `ROADMAP.md` | 悬着的议题总表（记录在案的 NO、重开触发器、待做事项）；只记决定与条件，不记流水账；议题完结即删 |
| 计划书 | `PLAN-<topic>.md` | 单项重大特性的设计与实施草案；**主体做完蒸馏入 `ARCH-*.md` 后当场 `git rm` 删除** |
| 架构正本 | `ARCHITECTURE.md` + `ARCH-<scope>.md` | 系统总览与各模块已建成架构的正本（why 与 how，不记可读源码的 what）；文首必带边界表 |
| 外部调研 | `RESEARCH-<topic>.md` | 外部竞品与业界方案的调研及实测数据（不入 SDLC 链，供决策参考，测量过时后清理） |

- **系统结构图硬规则**：一律使用纯 ASCII 字符（`+ - | = v ^ < >`）绘制，严禁使用制表符（`┌─│`），对齐严格按 CJK 双倍字宽计算。
- **修改文档正文一律使用编辑工具**，严禁使用 `sed` 破坏文档排版。

---

## 常用命令

```sh
# 语法与静态检查 —— 每次 commit 前必跑
bash -n shell/*

# 契约与单元测试（自动化测试）
tests/contract.sh --offline               # 离线半边检查（约 48s，不发网络包，覆盖 TUI 启动与 CLI 门控）
tests/contract.sh                         # 全量契约检查（约 195s，665 项检查，含真实端点探测）
tests/contract.sh --only undo             # 只跑 TUI 撤销那一段（约 47s，真实搜索 + 真实播放器）
tests/playback.sh                         # 真实 detached 播放器生命周期回归测试（约 110s）
tests/drive.sh -x 62 -y 20                # tmux 窄终端 TUI 键盘自动化驱动与截屏测试

# 核心入口功能抽检（bash 3.2 下运行）
/bin/bash shell/yt-search -j -n 5 -- "lofi hip hop"       # YouTube 搜索
shell/bili-search -j -n 5 -- "周杰伦"                     # Bilibili 搜索
shell/ne-search -j -n 5 -- "钢琴"                         # 网易云搜索
shell/ne-resolve --transcript -j -- 1824020871           # 歌词字幕提取
shell/t-play -d -j --engine yt -- "URL"                   # 后台启动播放
shell/t-play --status -j                                  # 查看全部播放状态
shell/t-play --stop -j --id <player-id>                   # 停止播放
shell/t-play --engines -j                                 # 已装引擎与两半的路径
shell/t-playlist --ls -j                                  # 查看歌单库
shell/t-history --ls -n 20 -j                             # 查看最近播放历史
shell/ting --version                                      # 响应版本（不触发依赖门控）
```

---

## 架构要点

- **站点知识与播放生命周期彻底隔离**：播放器 `t-play` 绝不直接运行 `yt-dlp`，不知道站点 Cookie 或格式代码；通过拼接命令名调用 `<engine>-resolve -j` 获取最终流媒体 URL 及 HTTP 请求头，以 `--no-ytdl` 注入 `mpv`。
- **单视图原地重绘**：`ting` 仅拥有一套统一的滚动渲染视图，无全屏清屏闪烁；所有非搜索数据（歌单 `b`、历史 `h`、分 P `c`、章节 `i`）均作为“临时替换行源”接入该视图。
- **多字节与 CJK 精确宽度**：按键处理以单字节累积并由 `utf8_complete` 还原字符；显示宽度由 `disp_w` 按 EAW 表准确分配，保证不同终端与语言下绝对不撕裂排版。
- **配置继承链与偏好写回**：配置查找按 `Flag > Env (TING_* > UT_*) > User Config (~/.config/ting/config) > Shipped Config` 顺序继承；`config`、`state`、`engines` 三处路径与每个环境变量都是「新名优先、旧名兜底」的两名链。出厂 `config` 永远只读，`ting` 退出时将 11 个偏好键写回用户个人配置文件。

---

## 改动时的红线

动手改动代码前必须确认以下硬约束：

1. **严禁破坏 bash 3.2 兼容性**：任何修改必须在 macOS 默认系统 bash 下通过验证。
2. **严禁引入新运行时依赖**：坚守 5 大外部依赖，严禁私自引入新工具或 C 库。
3. **严禁单侧新增 TUI 键位**：每个新增用户面功能必须同时具备对应的 Agent 命令行动词与 `-j` JSON 信封。
4. **严禁破坏已冻结的公共 CLI 契约**：不得私自修改公共命令参数名、退出码语义及 JSON 输出 Schema。
5. **一个事实只在一处声明**：文档严禁重复复制代码中已有陈述的 what（选项、退出码、字段清单）；文档只记录 why 与 how。
6. **改动代码后必须同步更新对应 `ARCH-*.md` 文档**。

---

## 测试与回归约定

- **Harden before you extend**：扩展功能前先确认现有测试用例全绿，排查 bug 时先编写能复现失败的测试。
- 🔴 **零 fixture / 零 mock / 零 stub —— 全部是真实功能测试**：测试必须驱动真实入口、发真实网络请求、解析真实信封、管理真实进程。严禁在 `tests/` 内自造替身，也严禁用预先捏造的数据（seed 好的存储记录、写死的信封、staged 配置键）去喂一个本该自己产出这份数据的命令。
  - **隔离不是 fixture**：`TMPDIR` / `UT_STATE_DIR` / 一个空的 `UT_CONFIG` 只是把写操作挡在用户真实文件之外，它们不预置任何行为；一旦某个文件预置了键值去驱动被测行为，那就是 fixture，必须改成由真实命令跑出来。
  - **就绪一律轮询真实信号**（socket 出现、标题回填、帧上的 ready 标记），严禁 `sleep` 猜时间。
- **最少提交门禁**：每次 commit 前必须运行 `bash -n shell/*` 并通过 `tests/contract.sh --offline`。

---

## 环境与规范

- **运行平台**：macOS 优先，主流 Linux 发行版兼容（探测支持 `-U` 的 `nc` 或 `ncat`）。
- **版本规范**：严格遵循 SemVer 2.0.0，版本号管理针对 CLI 契约而非内部实现。版本升级独占一次 commit，严禁随功能提交混杂 bump。
- **钩子管理**：通过 `.githooks/pre-commit` 与 `.githooks/pre-push` 本地门禁，无自动化 CI。
