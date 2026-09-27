# PLAN — 第四媒体源选型与 SoundCloud 实施门禁

> **Status**: 草案 (Draft) · 选型建议已形成，待容器路径实测闭环后确认  
> **Priority**: 第二梯队 · 中高 ROI  
> **Target Branch**: main  
> **Roadmap 关联**: [`docs/ROADMAP.md`](ROADMAP.md)「待办与待决事项」——第四媒体源选型  
> **Governing Docs**: [`docs/ARCHITECTURE.md`](ARCHITECTURE.md)「站点知识的边界」、[`docs/ARCH-cli-contract.md`](ARCH-cli-contract.md)「加一个引擎 —— 清单」、[`docs/ARCH-engine.md`](ARCH-engine.md)「模块功能和结构」  
> **Verification**: `bash -n shell/*`、`tests/contract.sh --offline`、`tests/contract.sh`、`tests/playback.sh`  
> **Scope Boundary**: 在不改变既有 argv、JSON 信封、reason 枚举和退出码分类的前提下，判断 SoundCloud (`sc`) 能否作为第四个内置引擎，并给出通过门禁后的实施顺序。开放播客 (`pod`) 只保留为后续候选；本计划不预先批准第五媒体源，也不设计断点续播的新公共契约。

---

## 1. Grounding 基线

外部平台与工具的探测命令、版本和原始结论只维护在 [`RESEARCH-tui-player.md`](RESEARCH-tui-player.md)「下一个媒体源候选与准入筛查」「第四音源候选复核」。本计划只消费其中四个会改变实施路线的结论：

1. SoundCloud 搜索、单曲解析与 mpv 直链解码已贯通；
2. SoundCloud Set 的 flat 条目缺少现有容器信封必需的时长，`--items` 尚未闭环；
3. SoundCloud 搜索会返回一至两小时的 DJ Mix，不能把它当作没有长音频代差的短曲源；
4. Apple 单集路径可用，但 Lookup 的 200 集截断、RSS 身份与去中心化 host 让“开放播客引擎”仍是未决架构问题。

外部行为会变化。Milestone 1 必须按研究正本的方法复测；测量结果推翻上述任一前提时，先更新 RESEARCH，再重审本计划，不静默改实现范围。

---

## 2. 选型建议与确认门

### 2.1 当前建议

**建议 SoundCloud 作为第四媒体源，开放播客继续留在 ROADMAP 候选位。** 理由限于已经成立的三点：

1. SoundCloud 是单一站点，能维持显式 host 白名单和 `engine` 路由语义；
2. 搜索、单曲解析和 mpv 直链播放已实测贯通，且零新增运行时依赖；
3. Track/Set 与现有单曲/容器模型接近，增量小于开放播客的去中心化 host、RSS 身份和长音频状态设计。

这是一项**待确认建议**，不是已生效的架构决定。只有下面四道门全部通过，ROADMAP 才从【待决】改为【待做】：

| 门 | 通过条件 |
|---|---|
| 容器完整性 | 找到零新增依赖的 Set 取数路径；每条 `items[]` 都有可解析的 `engine/id/url/title/duration/duration_fmt`，并能诚实填写 `total/has_more/next_cursor` |
| 请求预算 | 对小 Set、接近批次上限的 Set 各测两次；记录请求数、耗时与 429 行为，证明不会用逐曲完整解流把一次列表放大成不可接受的请求风暴 |
| 冻结契约 | 新脚本通过所有动态跨引擎门；不新增 flag、信封键、reason 或退出码语义 |
| 内置源判据 | 双半边、零新增全局依赖、bash 3.2、mpv 原生直链、匿名弱风控五项全部有测试证据 |

任一门失败，SoundCloud 不以缩水契约进入 `shell/`；它转为 `$TING_ENGINE_DIR` 的仓外引擎候选，第四内置源议题继续开放。

### 2.2 不在本计划里预先决定的事项

- 不把开放播客直接命名为第五内置源；先单独裁决“Apple 站点引擎”还是“开放 RSS 引擎”，以及后者如何遵守 host 白名单。
- 不在 `t-history` 中直接增加 `playback_position`；持久 schema、写入频率和恢复策略需要自己的 Plan。
- 不预定 Shift+方向键或 `i`/`c` 的新语义；TUI 键位必须先实测终端输入，再与 Agent 动词同时设计。
- 不依据纽约网络探测推导中国大陆可达性；区域结论需要对应网络环境的独立测量。

---

## 3. SoundCloud 契约增量

通用 argv、信封与退出码只认 `ARCH-cli-contract.md` 正本。本节只写 SoundCloud 的站点映射和实现分歧，不复制公共字段清单。

### 3.1 引擎文件与配置

- 引擎标识符：`sc`
- 可执行文件：`shell/t-engine-sc`（一个文件，只由 `t-play` 调用）
- 新增引擎级配置：
  - cookie 来源读套件键 `TING_COOKIE_BROWSER`，供 `--auth` 与 `--stream` 的 cookie 决定使用；`none` 强制匿名。
  - `TING_SC_AUDIO_FORMAT=ba/b`：SoundCloud 只提供音频，五种规范 `-f` 模式都落到此格式。
- 搜索条数、上限与排序继续读套件级 `TING_SEARCH_RESULTS`、`TING_SORT_FIELD`（上限是引擎自己的常量）；不得新增 `TING_SC_SEARCH_LIMIT` 形成第二份默认值。
- `--quality` 必须接受 `auto|low|medium|high`。实施前以匿名格式表和可获得的 OAuth 格式表实测 (mode, tier) 到 `--format-sort` 的映射；`auto` 不发 sort。

### 3.2 `--search`

- 完整继承引擎 `--search` 的 flag 门、URL 拒绝、单行信封、失败信封和 2+ 外部失败分类。
- 传输基线为 `scsearch<N>:` 的一次 yt-dlp 进程；`--min-duration`/`--max-duration` 在本地按规范化整数秒过滤，`--sort` 只对已取窗口排序。
- `id` 取数字 Track ID，`url` 取 HTTPS `webpage_url`，`channel` 取 uploader，`view_count` 取 playback count。
- `duration` 向下取整，`duration_fmt` 由同一份 `JQ_PRELUDE` 导出；没有 duration 且没有直播理由的记录在信封前丢弃。
- `live_status:null`、`kind:"track"`、`access:"full"` 是当前已测映射；引擎自检的 `--raw` 也必须由归一化字段覆盖原始同名键。
- `thumbnail` 选择带数值宽度且宽度不小于 200 的最小 HTTPS 项；没有合格项时按已有跨引擎规则返回 HTTPS fallback 或 `null`，不得把无 width 的 `original` 当作已满足阈值。

### 3.3 `--stream` 与只读动词

- `--stream` 完整继承内部协议的共享 flag，包括 `--quality`；公开只读动词为 `--info`、`--auth`、`--items`，并为 `--items` 实现通用 `--cursor o:<offset>` 门；`--capabilities` 如实列出它们。
- host 白名单为 `soundcloud.com` 及其真正的点分子域；必须拒绝 userinfo 混淆、尾点、非 HTTPS/HTTP 形状和 `evilsoundcloud.com`，不得用裸后缀匹配。
- 单曲句柄接受：数字 Track ID、规范 SoundCloud Track URL、`on.soundcloud.com` 短链以及 extractor 已支持的 `api.soundcloud.com/tracks/<id>` 形状。Set URL 在流解析与 `--info` 主路径拒绝并指向 `--items`。
- `start_seconds` 在任何网络请求前处理：
  - 每个引擎都必须理解套件自有 `?t=<非负秒数>`，包括 `?t=0`；
  - SoundCloud 额外理解已实测的 `#t=H:MM:SS` / `#t=M:SS`；
  - 只剥离被识别的偏移，保留媒体身份所需的其余 query；解析失败填 `null`，不报用法错误；
  - `--stream` 与 `--info` 信封填写同一个值，规范 `url` 中不保留偏移。
- `--stream` 信封必须原样满足内部协议的 schema：`stream_urls`、非凭据 `http_headers`、`format`、`selected`、`selected_resolution`、`retried` 和 `start_seconds` 均按正本语义生成。外部失败只使用共享 reason：`forbidden | unavailable | format_unavailable | network | cookies | unknown`；引擎不产生退出码 4。
- `--auth` 在依赖门之前回答，不发网络、不跑 yt-dlp；它只报告 cookie 是否会发送，不宣称会话有效或一定获得 Go+ 音质。

### 3.4 `--items` 阻断实验

不得直接采用已经证伪的 `--flat-playlist` 投影。实施前在 `tmp/` 隔离目录完成两条候选路径的真实对照：

1. **yt-dlp 单进程非 flat 路径**：测量它为 N 条 Set 实际发起的请求、耗时、是否提前解出签名流，以及 429 边界；
2. **yt-dlp 可暴露的列表元数据路径**：只接受 yt-dlp 已提供的稳定输出，不自行复制公共 client ID 抓取或新增 SoundCloud 私有 API 实现。

选择标准不是“能列标题”，而是能形成与当前三个引擎相同的容器信封：

- `count` 是本批保留下来的可播调用数，`total` 是站方声明值；
- `ITEMS_MAX` 仍是引擎内常量，不新增 flag；
- `has_more:true` 时必须签发 `o:<offset>`，为假时 `next_cursor:null`；
- 缺 URL、ID、正时长且无直播理由的行在信封前丢弃；私人或不可用条目不能靠 `select(.url != null)` 一条规则假装已经判完；
- 不存在的 Set 在发包后返回 reason `unavailable`、退出 2；非容器句柄在发包前退出 1；空 Set 成功返回空数组。

若两条候选均不能在请求预算内满足这些条件，本计划在选型确认门停止，不实现缩水版 `--items`。

---

## 4. 测试与交付

### Milestone 1：关闭容器阻断并确认选型

- 在 `tmp/` 留存可复跑的探测命令与测量摘要，源码树不写运行产物。
- 完成小 Set、大 Set、空 Set、不存在 Set、含不可用条目的 Set、游标第二批实测。
- 四道确认门全部通过后，才把本计划状态改为 Approved，并把 ROADMAP 项从【待决】改成【待做】。

### Milestone 2：实现引擎

- 先增加能复现预期失败的真实契约检查，再实现 `t-engine-sc`。
- 严守 bash 3.2；不得使用关联数组、大小写展开、`mapfile`、负数组下标或空数组的不安全展开。
- 临时文件全部位于 `$TMPDIR/ting-<uid>/`；不引入第六个运行时依赖。
- 在出厂 `config` 一次声明 `TING_SC_AUDIO_FORMAT`（cookie 走套件键；文件里只读 `TING_` 键，载入块不用动）；不增加搜索专属默认条数。

### Milestone 3：把测试从“三个样本”升级到“四个样本”

动态跨引擎循环会自动覆盖部分门，但不能把它当作完整接入。至少补齐：

- 离线内置引擎顺序期望（`t-play --engines -j`）与 `TING_SC_*` 配置读取；
- SoundCloud host 正例、子域正例及 userinfo/尾点/相似域反例；
- 经 `t-play` 的 `-j` 搜索行、`--info`、`--auth`，引擎自检的 `--raw` 与内部 `--stream` 的键集与一行 JSON；
- `?t=601`、`?t=0`、无偏移和 `#t=1:30`；
- Set 的完整信封、空/不存在/过滤、游标往返与两批不重叠；
- 搜索行经 `t-play -d --engine sc` 到真实 playhead 的端到端播放；
- SoundCloud 直链在 `mpv --no-ytdl` 下解码，且播放器正确应用 `http_headers`。

### Milestone 4：文档闭环与版本

- 同步 `ARCHITECTURE.md`、`ARCH-cli-contract.md`、`ARCH-engine.md`、必要的 `ARCH-player.md`、README 与 `config` 中的命令数量、内置源、原语拓扑和实测边界。
- 实现主体完成后将本计划的稳定 why/how 蒸馏进对应 `ARCH-*.md`，随后 `git rm` 本文件并从 ROADMAP 移除已完成议题。
- 新增公开命令与配置键属于 SemVer 加法；按仓库规范在功能提交之后，用独立 commit 完成版本升级，不与功能提交混杂。
- 最终门禁依次为 `/bin/bash -n shell/*`、`tests/contract.sh --offline`、`tests/contract.sh`、`tests/playback.sh`；涉及 TUI 可见行为时再运行 `tests/drive.sh -x 62 -y 20`。

---

## 5. 风险登记

| 风险 | 影响 | 控制措施 |
|---|---|---|
| Set flat 条目没有 duration | 阻断 | 在 Milestone 1 先证明可接受的完整条目路径；失败则不进主仓 |
| 非 flat 容器触发逐曲请求与 429 | 高 | 实测请求数与大 Set；不在循环内自行重试，429 统一映射 `network` |
| client ID 失效或 extractor 改版 | 中 | 保留 yt-dlp 自带的一次刷新逻辑；最终失败按共享枚举分类，通过安装渠道更新 yt-dlp，不承诺自更新一定可用 |
| 搜索行无法判断 preview/Go+ | 中 | 当前只陈述同一次 flat 响应能知道的事实；若无信号则保持通用默认，不新增逐行探测 |
| SoundCloud DJ Mix 同样是长音频 | 中 | 不把短曲体验当选型优势；断点续播另立横切 Plan，第四源不阻塞现有显式 `--start`/seek |
| 地区可达性未经目标网络测量 | 中 | 只承诺代理环境变量自然透传；不写“全球可达”或“中国大陆直连” |
| 测试中仍有三引擎硬编码 | 高 | 在新增脚本进入 `shell/` 的同一功能提交中补齐，不依赖动态循环掩盖空白 |
| Bash 3.2 或临时目录边界回归 | 高 | 运行冻结门禁，并对新增临时路径做 `$TMPDIR/ting-<uid>/` 检查  |
