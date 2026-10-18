# R1/R3：历史分页、组合筛选与旧素材恢复验收

执行时间：2026-10-17 17:44:40–18:00:55（Asia/Shanghai）；17:54:23 起扩展并重验旧 200/500 窗口。本轮使用真实本机服务 `http://127.0.0.1:8091`、PostgreSQL 持久化数据、Chromium 151.0.7922.34（1280×720）及独立 QA Demo 账号与浏览器 context。未修改生产源码，未操作 8092 或其他用户数据。

**结果：本次本机 Demo 场景通过，实际覆盖原 200/500 条上限。** 最终 201 个活动项目、主项目 201 笔任务与 501 份素材，浏览器分别翻至第 9、9、21 页取得最早记录；406 条真实积分流水也可翻至第 17 页。旧项目、旧任务和旧素材均能服务端搜索；任务四条件组合、条件变化回第一页与空结果刷新通过。第 501 个素材在画布、分镜选定及双侧比较中恢复，原生下载字节与上传源一致。

## 运行版本与追溯标识

| 对象 | 实际值 |
| --- | --- |
| 8091 监听进程 | PID 25456，`.runtime/studio-release-20261017.exe` |
| 可执行文件 SHA-256 | `a484eb6c10ee7b1f707f124dbf2f781d076f1b5da38520a3c1c16a3cb25b7d23` |
| 浏览器加载脚本 | `/assets/index-Bkzbj50-.js` |
| 系统状态 | version=0.1.0，demo=true，storage_driver=local，media_configured=false，payment_enabled=false |
| QA 用户 | `usr_9db80a4f623e7819ba22cd0ef2aaa0f5`，`qa-history-mvc7muiv@example.test` |
| 最早项目 | `prj_a61e162683914dca6154e4c8634ce5d0`，QA H1 历史项目 01 mvc7muiv |
| 第二项目 | `prj_05a00c5853143b66c984ec096ea6472b` |
| 归档项目 | `prj_87f208215a1dd3ff3cdaa1db9892075b`，QA H1 历史项目 28 mvc7muiv |
| 最早选定素材 | `ast_09f480bbc8eda8e5e659445c274f1a1b`，qa-h1-01-mvc7muiv.png |
| 第二候选素材 | `ast_cd6d6d221257c8a48bed81e3edcfee75`，qa-h1-02-mvc7muiv.png |
| 旧任务搜索目标 | `task_37ed1a71afd750eb06aedd83d13c8e88`，提示词包含 HISTORY 02 |
| 分镜 | `qa-h1-shot-mvc7muiv`，shots revision=1 |
| Demo 调账审计 | `audit_2a55e847ccdaaa2c9ce9c662ff09486f`，amount=270，demo=true |
| 扩展 Demo 调账审计 | `audit_a8d6371f2b28e67318082d8206586888`，amount=1750，demo=true |

密码、Cookie、CSRF、验证 token 和管理员凭据未写入本文或证据 JSON。随机账号密码仅保存在已忽略的 `.runtime/h1/credentials.json`。完成后，本轮用户及管理员会话分别登出返回 200，独立 context 与浏览器均已关闭；没有重置共享 Node 内核或关闭其他验收会话。

## 数据建立与隔离

1. 使用正常注册、开发邮箱验证与登录接口创建 QA 用户；响应分别为 202、200、200。没有直接写入数据库夹具。
2. 先建立 28 个项目并正常归档第 28 个，再新增 174 个活动项目，最终 201 active、1 archived。第 1 个项目先上传 27 份 4582 字节 PNG，以不同文件名形成独立记录，保存最早两份为分镜候选、第一份为选定作品及画布素材节点；其余项目随后创建，保持第 1 个位于历史末页。全部项目 POST 均为 201。
3. 管理员通过正式调账接口先补 270、再补 1750 Demo 测试积分，去重键分别为 `qa-history-mvc7muiv`、`qa-history-extended-mvc7muiv`，原因明确标注本机验收及无真实充值/模型费用。审计接口精确查询该用户的 `wallet.adjust`，恰好返回上述两条，合计 2020、demo=true。
4. 使用上架的 `demo-image` 正常报价和提交 202 笔任务（最初 27、追加 175），quotes/tasks 均返回 201，每笔报价均断言 credits=10、demo=true。第二项目 1 笔，最早项目 201 笔，提示词分别标注 `QA-H1 OTHER`、`QA-H1 HISTORY`。实际 Demo Worker 自然完成全部任务，每笔归档一份 PNG，均为 completed / ready / consumed。
5. 等 Worker 完成后先查询主项目已有 228 份素材，仅再正常上传差额 273 份，使主项目恰好 501 份。最终账号 502 份素材：300 上传、202 Demo 生成。追加任务受理约 1.17 秒、174 项目创建约 0.38 秒、273 上传约 1.80 秒；这些是本机操作耗时，不是生产 SLA。未使用 route.fulfill、SQL 状态改写、支付订单、真实模型、SMTP 或 S3。

## R1：真实浏览器分页、搜索与恢复

| 操作与实际请求 | 通过证据 |
| --- | --- |
| 项目 active 默认页，limit=25、offset=0 | total=201，25 个项目；逐页至 offset=200 返回最早项目 1 个，界面为第 9/9 页 |
| 在项目第 9 页搜索 `历史项目 01` | 服务端 q 查询返回 200、total=1、offset=0，唯一 ID 为最早项目 |
| 素材库选择最早项目，默认 q 为空 | total=501；逐页至 offset=500，200、items=1，为最早上传及选定素材，界面第 21/21 页 |
| 从素材第 21 页搜索完整旧文件名 | 200、total=1、offset=0，唯一 ID 为最早素材 |
| 积分流水翻历史末页并搜索最早原因 | total=406，offset=400 返回 6 条、含最早调账；搜索 `QA 历史分页 R1/R3` 返回 200、total=1、offset=0 |
| 打开最早项目工作台 | 素材 limit=100、offset=0/100/200/300/400/500 共 6 次 200，返回 100/100/100/100/100/1；任务 offset=0/100/200 共 3 次 200，返回 100/100/1 |
| 核对旧 500 素材边界与画布恢复 | 前五次素材响应去重恰好 500 个 ID，不含旧选定；第六次取回该素材。画布仍解码该图像为 1024×576 |
| 分镜比较最早两个候选 | 左侧选定 ID 为第一份，右侧为第二份；两侧图像均解码 1024×576，“已选成品”按钮禁用 |
| 下载并刷新 | 通过“预览素材→下载原文件”触发浏览器原生下载，failure=null；刷新重开比较后，两图仍可见、选定 ID 不变，canvas/shots revision 均保持 1 |

旧选定下载 HTTP 200、Content-Type=image/png、实际 4582 字节。下载与上传源 SHA-256 均为 `3c5957db44eb6faeaef438f3ca8bd27b1fb6db0adf669b2a8f71d4c350714187`，逐字节相等。此次被动查看、比较和刷新未写回画布或分镜。

扩展前的基础样本还验证过项目/素材搜索不存在词为 total=0、items=[]、清除后恢复，以及归档页可达；素材空结果后整页刷新会清空搜索/项目条件并回第一页。扩展后的末页、历史搜索、工作台、比较、原生下载与刷新均再次实际执行。

## R3：全局任务中心组合筛选

| 浏览器操作 | 实际结果 |
| --- | --- |
| 全部项目进入任务中心并翻页 | total=202，第 1 页 25 条、第 9 页 offset=200 为 2 条；项目下拉有 203 个选项（全部+202 项目），含最早与已归档项目 |
| 在第 9 页选择最早项目 | 真实请求 project_id=最早项目、limit=25、offset=0；total=201，items 均属于该项目 |
| 同时选 image、completed，搜索 `QA-H1 HISTORY` | 200、total=201、offset=0；返回项均匹配项目/图片/完成/提示词四个条件 |
| 四条件下逐页翻第 9 页 | 200、total=201、offset=200、items=1，为 HISTORY 02 旧任务；界面显示第 9/9 页 |
| 第 9 页搜索完整旧任务 ID | total=1、offset=0，唯一任务为上述旧任务 ID；基础样本另验证过提示词 HISTORY 02 搜索 |
| 搜索不存在词并点击“刷新状态” | 两次均为 200、total=0、items=[]、offset=0；项目、image、completed 与 q 保持原条件 |
| 清除搜索，再选择 video 或 failed | 清除后恢复 total=201、第 1/9 页；video 和 failed 各自返回空结果 |
| 换第二项目、归档项目 | 第二项目 image+completed 为 1 条；归档项目为 0 条，没有串入其他项目任务 |
| 整页刷新 | 过滤条件恢复初始值、total=202、第 1/9 页，无旧分页位置残留 |

工作台与下拉框的全量请求和列表的 25 条分页是分别实测的：项目下拉真实读取 `status=all&limit=100` 的 offset=0/100/200 三页，最早项目在末页，仍可正常选择；并未把第一页的 100 条当成全部项目。

## 钱包、任务与错误核对

| 检查项 | 最终实际结果 |
| --- | --- |
| 活动/归档项目 | 201 / 1 |
| 账号任务 / 最早项目任务 | 202 / 201，全部 completed / ready / consumed，demo=true，credits=10 |
| 账号素材 / 最早项目素材 | 502 / 501，其中上传 300、Demo 生成 202 |
| 正常调账与审计 | 2 次：+270、+1750，合计 2020；恰好两条 wallet.adjust，均 demo=true |
| 本账号积分流水 | 406 条：adjustment=2、freeze=202、consumed=202；全部分页取回、按 ID 唯一 |
| 流水差额与最终钱包 | available_delta 合计 0、frozen_delta 合计 0；最终 available=0、frozen=0、demo=true |
| 本账号充值订单 | 0 条；没有创建或伪造订单历史 |
| 浏览器错误 | pageerror=0；初次未登录进入应用的 `/me` 探测 401 共 1 次，符合预期；登录后未观察到 API 4xx/5xx |

自动化过程中修正了比较器的精确文字定位器，以及一次在网络记录监听器尚未完成解析前读取结果的断言时序；随后等待实际响应及旧任务内容，最终断言通过。这些是验收脚本定位/等待问题，未更改应用或伪造网络响应。本次范围内未发现需要修改的生产缺陷。

## 证据文件与覆盖边界

所有本机原始证据均在已忽略的 `.runtime/h1/`；无需把测试凭据或大量截图放进生产仓库：

- `evidence.json`：账号/项目/素材/任务 ID，真实 API 状态与 total/limit/offset、浏览器请求、任务终态、406 条流水、两次审计、字节哈希、断言与登出结果；不含会话秘密。
- `projects-page9.png`、`assets-page21.png`、`tasks-combined-page9.png`、`ledger-page17.png`：跨原窗口后的真实末页。
- `workspace-501-canvas.png`、`old-candidate-compare-501.png`、`old-selected-download-501.png`：501 素材项目的旧画布引用、选定比较与下载字节。
- 基础样本的 page2/page3 截图、比较和下载仍保留，用于追溯扩展前的默认 25 条回归；最终结论以上述大样本为准。

另据主任务实际执行记录，[IMPLEMENTATION_STATUS.md 的“自动化证据”](IMPLEMENTATION_STATUS.md#自动化证据)：`go test -race ./... -count=1 -timeout=120s` 全过，server 31.473 秒，覆盖 `user_lists_integration_test.go` 的 231 条项目/任务/流水/订单及 501 素材、相同时间排序和跨账户过滤。该命令由主任务执行，本代理未为写文档重复运行；不能把本次 0 订单的浏览器环境说成已验证 200+ 真实支付订单。

本次直接证明本机 PostgreSQL + 正常 API + Demo Worker + 真实浏览器下的 200/500 历史可达性与 R3 组合筛选。R2 进行中详情轮询见独立 [验收记录](QA_TASK_POLLING_2026-10-17.md)。本次未覆盖真实图片/视频模型、真实资金/支付订单、生产 S3/HTTPS、其他设备、弱网或大媒体/大画布的性能容量，也未承诺这些场景已达到上线标准。

