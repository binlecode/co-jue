# PLAN — `t-play` 成为唯一 CLI 入口，引擎退到底层（`t-engine-<site>`）

> **Status**: 实施中（2026-09-26 开工）· §8 七项按建议定下（`ACCENT` 两键留到第 5 步再定）；第 1 步（`t-engine-yt`）、第 2 步（`t-engine-bili`、`t-engine-ne`）、第 3 步（`t-play` 入口，连同第 4 步的调用方切换）已落  
> **Priority**: 第一梯队，**立即做**：先于 `PLAN-go-tui.md` 第 4 步，Go 那侧等本计划落地再继续  
> **Target Branch**: main  
> **Governing Docs**: [`ARCHITECTURE.md`](ARCHITECTURE.md)「命令拓扑与文件布局」、[`ARCH-engine.md`](ARCH-engine.md)、
> [`ARCH-cli-contract.md`](ARCH-cli-contract.md)「命令规格」「数据契约」「配置面」「加一个引擎 —— 清单」、[`PLAN-go-tui.md`](PLAN-go-tui.md)  
> **Verification**: `bash -n shell/*`、`tests/contract.sh --offline`、`tests/contract.sh`、`tests/playback.sh`、`go test ./...`  
> **Scope Boundary**: 改的是**形状**：命令拓扑、公开 flag 面、配置键名。站点知识、播放器生命周期、信封里既有字段的
> 语义、四级退出码都不变。bash 3.2 与零新增运行时依赖两条红线照旧。

---

## 1. 为什么：切分依据要跟着知识走

今天一个引擎是两个公开命令（`<site>-search` / `<site>-resolve`），切分依据是「操作」。但知识的边界是**站点**：
同一个站的 User-Agent、Referer、cookie 来源、域名规则在两个文件里各写一份。实测成段重复的代码行：
yt 这一对 392 行（占 `yt-search` 代码的 63%），bili、ne 各约 214 行。

拆分当初给的理由，逐条都不需要两个文件：

- 搜索发生在浏览时、解析发生在播放时 —— 这是两个**动词**，一个文件同样能带。
- 依赖局部化（`openssl` 只进网易云搜索）—— 按动词懒检查即可，`t-play` 对 `nc` 就是这么做的。
- 「窄动词才敢给小模型调」—— `*-resolve` 早已一身多动词（`--info`、`--transcript`、`--items`、`--parts`、`--auth`）；
  而 `ARCHITECTURE.md` 说 resolve「不是给模型用的」，README 却把 `yt-resolve --transcript -j` 当 agent 的主推用法。
  这条理由已经名存实亡。

再往下一层：`*-resolve` 其实是两样东西 —— **底层管道**（句柄 → 直链 + 请求头，调用方只有 `t-play`）与
**站点只读动词**（元数据、容器、字幕、登录探测，在使用边界上）。公开面与底层混在一个名字下，
所以每修一次站点风控都可能碰到公开契约。

结论：**一个站点一个底层文件，公开面只有一个入口**。

---

## 2. 新架构与边界

```
  agent / 人（ting，Go）
          |
          |  唯一公开契约：t-play 的 argv + -j 信封 + 退出码
          v
  +-----------------------------------------------------------+
  |  t-play   播放与生命周期（不变）                           |
  |           + 引擎动词转发：--search --info --items          |
  |             --transcript --auth（按 --engine 拼名，exec）  |
  |           + --engines（发现 + 能力，一次答完）             |
  +------------------------+----------------------------------+
                           |  内部协议（不属于公开契约）
                           v
  +-----------------------------------------------------------+
  |  t-engine-yt | t-engine-bili | t-engine-ne | 仓外 ...       |
  |    公开动词的实现 + --stream（直链）+ --capabilities       |
  |    站点知识只在这里：UA / Referer / cookie / 域名 / 风控    |
  +------------------------+----------------------------------+
                           v
            yt-dlp / curl / openssl（引擎私有原语）

  t-playlist / t-history   两个存储，照旧独立（§8 待确认）
```

| 层 | 管什么 | 不管什么 |
|---|---|---|
| `t-play` | 公开 flag 面、`--engine` 解析与拼名、转发、播放与生命周期 | 任何站点细节；不读引擎文件，不持有引擎名单 |
| `t-engine-<site>` | 该站全部知识；自己那几个动词的 flag 门 | 播放、进程、渲染；不承诺 argv 稳定 |
| `ting`（bash，退役前）/ Go | 只调 `t-play`、`t-playlist`、`t-history` | 引擎文件、mpv、yt-dlp |
| agent | 与 `ting` 平级，看到的面完全一样 | 引擎文件（能调，不受契约保护） |

**转发的规矩**

- 在 `t-play` 的一切依赖门之前完成：只搜索不需要装 mpv，这一点今天成立，之后也必须成立。`--engines` 已是这个位置。
- `t-play` 只认出「这是哪个引擎动词」与 `--engine`，其余参数原样 `exec` 给 `t-engine-<name>`。flag 门只有一道，在引擎里：
  `--sub-lang` 只有 yt 认，`t-play --search -d` 由引擎按未知 flag 退 1。信封与退出码一字不差地透传。
- 内部动词（`--stream`、`--capabilities`）不在 `t-play` 的转发表里，从公开入口调它们就是未知 flag，退 1。
- `--engine` 缺省照旧取 `UT_DEFAULT_ENGINE`（改名见 §4）。

**发现**：`t-play --engines -j` 改为 `{status, engines:[{name, bin, flags[]}]}`。`bin` 是找到的那一个文件（本目录 →
`UT_ENGINE_DIR` → PATH，先到先得），供诊断「用的是哪一份」，不是给调用方执行的；`flags` 是该引擎公开动词与修饰符的
平铺清单，取自 `t-engine-<name> --capabilities`。每引擎多 fork 一次，实测 `t-play` 起一次约 7 ms，`--engines` 整条约 7 ms。
各引擎两半各自的公开 `--capabilities` 随之消失：能力是引擎的属性，归注册表回答。

---

## 3. flag 清理（KISS）

判据两条：**一个公开 flag 用站点无关的词描述调用方想要的东西**；**同一件事只有一种说法**。

| 今天 | 之后 | 理由 |
|---|---|---|
| `<site>-search [flags] -- q` | `t-play --search [--engine E] [flags] -- q` | 唯一入口 |
| `<site>-resolve --info / --items / --transcript / --auth` | `t-play --info` 等，同名 | 同上 |
| `bili-resolve --parts` | 并入 `--items`（多 P 视频就是一个装着分 P 的容器；`parts[]` 与 `items[]` 同形只差 `id`） | 同一件事一种说法（§8 待确认） |
| 裸 `<site>-resolve -j -f M -- h` | `t-engine-<site> --stream`，内部 | 底层管道不上契约 |
| `-n` | 保留（搜索） | 人人认得 |
| `-m` / `-M` / `-s` | `--min-duration` / `--max-duration` / `--sort` | 入口 40 多个 flag，只对一个动词有意义的单字母容易看错；读者是 agent，长名自解释 |
| `--cursor`、`--sub-lang` | 保留 | 各自修饰一个动词，名字已自解释 |
| `-l` | 删 | 散文本来就是默认输出，`-l` 什么也不改变 |
| `-J`（原始 yt-dlp / 站点记录） | 删；站点原始记录改名 `--raw`，只在引擎文件上作自检，`t-play` 拒转发；转写的完整形态改为 `--transcript -j --segments` | 站点原始字段无法版本化。唯一的使用者是 bash `ting` 的 `load_track_url`，它从原始记录里读 `ar[0].name`、`al.picUrl`、`pic` —— 站点知识漏进了 TUI，本身就是分层违规。改为把这些字段补进 `--info` 信封，与搜索行同形 |
| `-S`（yt-dlp format-sort） | 删 | yt-dlp 语法进了公开契约；档位抽象是 `--quality`，按模式的格式覆盖已有配置键 `*_FORMAT` |
| 各半边的 `--capabilities` | 从公开面删，由 `--engines` 的 `flags[]` 回答 | 一个问题一个动词 |
| `-f`、`--quality`、`--color`、`-j` | 不变 | |

`t-play` 自己的播放与生命周期 flag 不在本次清理范围：它们已经是站点无关的词，各自一种说法。

---

## 4. 配置清理（KISS）

扫了出厂 `config` 的全部 51 个键，加上不进文件的 6 个（`UT_STATE_DIR`、`UT_START_RESULTS`、`UT_ENGINE_DIR`、`UT_CONFIG`、
`YT_LANG`、`YT_ASCII`）。

**前缀：作用域写在名字里，只有一个前缀。** 今天是 `UT_`、`YT_`、`TING_`（部分改名）、`BILI_`、`NE_` 五套，其中 `YT_` 一身两义：
`YT_THEME`、`YT_LANG` 属于套件，`YT_COOKIE_BROWSER`、`YT_AUDIO_FORMAT` 属于 YouTube 引擎。之后：

- 套件键一律 `TING_<KEY>`：`TING_DEFAULT_ENGINE`、`TING_THEME`、`TING_LANG`……
- 引擎键一律 `TING_<ENGINE>_<KEY>`：`TING_YT_AUDIO_FORMAT`、`TING_BILI_UA`……仓外引擎自带作用域，今天它们根本没有合法前缀可用。
- 安全规矩收成一句：**文件里只读 `TING_` 键**。`TING_ENGINE_DIR` 照旧只认环境变量。
- 播放器给自己 detached 子进程的四个私有变量（`YT_IPC_SOCK`、`YT_DETACHED`、`YT_PLAYER_ID`、`YT_DETACHED_LOG`）
  不是配置，改名 `_TING_*`，前导下划线表示私有，不进任何键表。
- **迁移**：每个加载器按「新名 → 旧名」读一次；`ting` 写回时把旧键名原地改成新键名，注释与对齐照旧保留。
  你本机的配置只有 10 个写回键，第一次写回就迁完。旧名兜底保留一个版本后删除。

**删或并（每一条都要你签字，见 §8）**

| 键 | 处理 | 理由 |
|---|---|---|
| `YT_ / BILI_ / NE_COOKIE_BROWSER` | 并成 `TING_COOKIE_BROWSER` | 一个人一个浏览器；三处同义 |
| `UT_START_RESULTS` | 并入 `TING_SEARCH_RESULTS` | 「第一次取多少」与 `-n` 的缺省是同一件事，两个键说它 |
| `UT_MAX_SEARCH_RESULTS` | 删，改为各引擎自己的常量 | 它保护的是站方请求预算（B 站 20 条一页），是站点知识，不是用户偏好 |
| `UT_MODE_CYCLE`、`UT_SORT_CYCLE`、`UT_QUALITY_CYCLE`、`UT_LOOP_CYCLE`、`UT_THEME_CYCLE` | 删，写死在 TUI 里 | 轮转顺序是产品设计（注释里已经写了每个顺序为什么这样排），五个键调的是没人会调的东西 |
| `UT_RESOURCE_TICKS`、`UT_DEAD_KEEP`、`BILI_RETRY_PAUSE` | 删，改为代码常量 | 实现调参，不是偏好 |
| `YT_ICON` | 删，固定 ♫ | 每次启动掷一次硬币，文档截帧还得专门钉住它 |
| `YT_BRAND` | 删 | 纯装饰开关 |
| `YT_TUI_ASCII` | 删 | `YT_ASCII` 的旧别名 |
| `UT_ACCENT`、`UT_ACCENT_LIGHT` | 待定 | 已有 13 套主题，`custom` 调色板是否还值两个键 |

其余保留、只改名：`HISTORY`、`DEFAULT_ENGINE`、`SEARCH_RESULTS`、`SORT_FIELD`、`PLAY_MODE`、`VOLUME`、`PLAY_QUALITY`、
`VIZ_STYLE`、`VIZ_COLOR`、`ASCII_VO`、`MPV_INPUT_CONF`、`PAGE_ROWS`、`FETCH_BATCH`、`LOOP_MODE`、`RESOURCE`、`KEYS`、
`LIST_MODE`、`ROW_INDEX`、`THEME`、`BG`、`SYNC`、`IMAGE`、`AMBIG_WIDE`、`LANG`、`ASCII`、`STATE_DIR`、`CONFIG`、`ENGINE_DIR`，
以及引擎的 `*_FORMAT*`、`*_UA`、`BILI_BUVID`、`YT_SUB_LANG_CHAIN`、`NE_INCLUDE_VIP`。
出厂文件里的键从 51 个减到 38 个（`ACCENT` 两个未计入删除）。

---

## 5. 契约变更与版本

- 公开命令从 10 个减到 4 个：`t-play`、`t-playlist`、`t-history`、`ting`。六个 `<site>-search/-resolve` 退出 PATH。
- 引擎动词换了宿主（§3）；`-l`、`-J`、`-S`、各半边 `--capabilities` 从公开面删除；`-m`/`-M`/`-s` 改名；`--parts` 并入 `--items`。
- `t-play --engines -j` 改形状（§2）。
- `--info` 信封补齐与搜索行同形的字段。
- 配置键全部改名，旧名兜底一个版本（§4）。
- 信封里既有字段的语义不变，`engine` 字段照旧是持久化的路由键；退出码四级分类不变。
- 仓外引擎的约定从「一对文件」变成「一个 `t-engine-<name>` 文件」。
- 版本：见 §8。

---

## 6. 连带改动

- **bash `ting`**：`scan_engines` 改读 `t-play --engines -j`；搜索、`c`/`i`、容器展开全部改调 `t-play` 的转发动词；
  `load_track_url` 删掉原始字段解析，改读 `--info` 信封。它在第 6 步（Go 接过名字）之前仍是在役的 TUI，必须跟着改。
- **Go**（`PLAN-go-tui.md` 同步修订）：`verb.Search` 改为 `t-play --search`；`Engines()` 读新形状，不再持有引擎路径；
  `internal/config` 移植新键名与旧名兜底，那张「TING_ 只认改名清单」的怪癖随之消失。
- **测试**：`contract.sh` 的引擎段改为经 `t-play` 驱动，另保留一段直接驱动 `t-engine-*` 的内部协议检查
  （`--stream`、`--capabilities` 与 `flags[]` 说真话）；`playback.sh` 基本不动（它本来就走 `t-play`）。
- **文档**：`ARCHITECTURE.md`「命令拓扑」重写，推翻「为什么一个引擎是两个命令」一段，新判据写成
  「flag 面按动词窄，不按文件窄」；`ARCH-engine.md`、`ARCH-cli-contract.md` 的命令规格、数据契约、配置面、
  「加一个引擎 —— 清单」；README、`USER_MANUAL.md`、`CLAUDE.md`（「10 个独立可执行脚本」、常用命令）。
- **分发**：tap formula 只把四个公开命令链进 `bin/`，`t-engine-*` 留在 `libexec/shell/`，由 `t-play` 在自己真实目录里找到。

---

## 7. 实施顺序

每一步一个或几个 commit，每个 commit 照常过 `bash -n shell/*` 与 `--offline`；第 1、3、5 步完成时各跑一次全量 `contract.sh`
与 `playback.sh`。

1. **`t-engine-yt`**：两半合成一个文件（动词：`--search`、`--info`、`--items`、`--transcript`、`--auth`、`--stream`、
   `--capabilities`），站点事实只留一份；`--info` 补字段。`t-play` 临时双查：先找 `t-engine-<n>`，找不到再找旧的一对
   （第 3 步末尾删掉）。
2. **`t-engine-bili`、`t-engine-ne`**：同上；`--parts` 并入 `--items`（若 §8 同意）。
3. **`t-play` 入口**：转发动词、`--engines` 新形状、`--stream` 取直链、删 `-l`/`-J`/`-S`、`-m`/`-M`/`-s` 改名；
   删掉双查与旧的六个文件。
4. **调用方**：bash `ting` 与 Go 的 `verb` 包切到新入口；`go test ./...`。
5. **配置**：前缀统一、删并（按 §8 签过的清单）、旧名兜底、写回改键名；Go `internal/config` 同步。
6. **文档与分发**：§6 的文档全部改完；tap formula 改好但不发布（发布照旧由你决定）。
7. **bump**：独占一次 commit；本文件蒸馏进 `ARCH-*.md` 后 `git rm`。

---

## 8. 已定（2026-09-26）

1. **存储**：`t-playlist`、`t-history` 不并进 `t-play`。
2. **名字**：入口仍叫 `t-play`。
3. **过渡别名**：旧的六个命令名不留，第 3 步末尾直接删。
4. **`--parts` 并入 `--items`**：并。
5. **`-m`/`-M`/`-s` 改长名**：改为 `--min-duration` / `--max-duration` / `--sort`；`t-engine-*` 从第一天就只认长名。
6. **配置删并清单（§4 表）**：照表执行；`UT_ACCENT`、`UT_ACCENT_LIGHT` 的去留在第 5 步动配置时再定。
7. **版本号**：`0.18.0`。

**实施中定下的两条**（第 1 步）：

- `--info` 补齐的字段是 `kind`、`access`、`thumbnail`。bili 的 `--info` 在第 1 步一并补上（与它的搜索同一判断），
  ne 留到第 2 步：它的 `access` 由搜索接口的 `fee` 算出，而 `--info` 走的 yt-dlp 记录里没有 `fee`，印 `full` 就是猜。
**实施中定下的三条**（第 2 步）：

- 视频的分 P 走 `--items`，信封就是容器信封（`total` = 分 P 数，`has_more`/`next_cursor` 照常按游标切片），
  旧 `--parts` 的 `total_duration`、`total_duration_fmt` 随之不再有。每个分 P 的 `id` 是解析它时 yt-dlp 给的那个：
  多 P 视频一律 `<BV>_p<N>`（连第 1 P 与裸 URL 都是 `_p1`），单 P 视频就是裸 BV（2026-09-26 实测）。
- ne 的 `--info -j` 多打一次 `api/v3/song/detail`（`--items` 已在用的同一个 GET），从 `fee` 算 `access`、
  从 `al.picUrl` 取封面，与搜索同一套判断。这次请求失败时 `--info -j` 整体失败（退 1，错误信封）：
  `access` 是闭集，没有「不知道」可印。散文与引擎自检的 `--raw` 不多打这一次。
- 两个站的错误分类各并成一份：ne 搜索若遇到 body code -404/-400/404，`reason` 从 `unknown` 变 `unavailable`（退出码不变）。

- 第 1、2 步期间 `--engines` 与 bash `ting` 仍按旧的一对找引擎：`t-play` 播放已走 `t-engine-<n> --stream`，
  TUI 的搜索与 `c`/`i` 仍调旧文件。两边同时在役，第 3 步删旧文件时一起收口。

**实施中定下的几条**（第 3 步，2026-09-26）：

- **调用方切换提前并进第 3 步**：删掉六个旧文件时 bash `ting` 与 Go `verb` 包还在调它们、读旧的 `--engines` 形状，
  删了就坏，所以第 4 步里「调用方切到新入口」这一半与第 3 步同一轮做完。第 4 步剩下的只有 Go 侧跟进
  （`internal/config` 属于第 5 步）。
- **`-J` 整个删掉，JSON 开关只有 `-j` / `--json`**（2026-09-26 你定）：`-J` 与 `-j` 只差大小写，而且各动词含义不一
  （`--search` 是带原始行的信封，`--transcript` 是加 `segments` 的信封，`--info`/`--items` 是站点原始记录）。
  业界 `-j`（iproute2、nft、smartctl、zfs）与 `-J`（util-linux）两派都有，没有人同时用两个。之后：
  `--segments` 是 `--transcript` 的公开修饰符（只在 `-j` 下，列进 `flags[]`）；站点原始记录叫 `--raw`，
  只在引擎文件上作自检，不进 `flags[]`，`t-play` 转发时唯一自己拒的就是它。
- **引擎动词也按 URL 认引擎**：没给 `--engine` 时，`--info`/`--items`/`--transcript` 的句柄若是 URL，按主机选引擎，
  与播放同一条规则；否则取缺省引擎。`--search` 不认（查询不是句柄）。
- **`-S` 从引擎里也删了**：`t-play` 不再转发它，引擎里的 `-S` 只剩死代码。
- **`--engines` 的实测开销**：约 160 ms（此前约 110 ms；多出的是三次 `--capabilities` fork）。§2 写的「约 7 ms」不对，
  `t-play` 单是起一次就约 100 ms（4400 行脚本的解析）。bash `ting` 启动因此多约 100 ms，但它不再自己 fork
  `--capabilities`，能力直接取自注册表。
- **TUI 的 `c` 键改按 `--items` 开放**：`--parts` 并入 `--items` 后，「这个引擎有分 P」不再是任何引擎能声明的能力
  （三家都有 `--items`）。于是 `c` 在 yt / ne 上也出现，按下时显示引擎自己的拒绝（「not a container」），
  不再是静默无反应。若要恢复「只在 B 站出现」，需要一个新的能力词，属于契约改动，待定。
- **分 P 视图的总时长**：新信封不再带 `total_duration_fmt`，`ting` 在拿到全部分 P（无 `has_more`）且每个都有时长时自己加总。
