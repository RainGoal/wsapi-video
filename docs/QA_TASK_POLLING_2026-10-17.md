# R2：列表外任务详情独立轮询验收

执行日期：2026-10-17，以下时间为 Asia/Shanghai。环境为真实本机服务 `http://127.0.0.1:8091`、Chromium 1440×1000、独立 QA Demo 账号和浏览器 context。先读取 `web/src/pages.tsx`、`pagination.tsx`、`routing.ts`、`web/scripts/check-pagination.mjs` 以及现有验收记录，随后执行真实浏览器与服务验证；本轮未修改生产源码。

**结果：通过本次本机 Demo 场景。** 服务端项目筛选排除了目标任务，当前列表真实返回 total=0；详情从 `queued / pending / frozen` 自动更新为 `completed / ready / consumed`，期间没有刷新列表或手动点击“刷新状态”。本次没有使用 `route.fulfill`、网络响应夹具、数据库状态写入、lease/next_attempt_at 延迟或 Worker 暂停；结果由运行中的真实 Demo Worker 自然完成。

## 追溯标识

| 对象 | 标识 |
| --- | --- |
| QA 用户 | `usr_0575963e4de343795c39f74654f6af0b` |
| 目标任务所属项目 | `prj_b1111754519afed60e2eb957334170aa` |
| 列表当前筛选的空项目 | `prj_95a9ec4c03ddbfd5936cebb63d4e0aa2` |
| 任务 | `task_2cfe8132e6580356109f105ca1f54d3a` |
| 实际归档素材 | `ast_9aff660249f5a6d35088075652269761` |
| 10 Demo 积分调账审计 | `audit_737b6be54f70b4fb93fc71f0d9ff11ea` |

密码、Cookie、CSRF 和管理员凭据未写入本文或验收证据 JSON。账户与会话仅保存在已忽略的本机 `.runtime/r2-*` 文件中，与 R1/R3 的账号及数据隔离。未操作 8092，也未调用真实模型、支付、SMTP 或 S3。

## 真实服务与浏览器步骤

1. 通过页面注册、开发邮箱验证入口及登录建立独立会话。管理员调用正式调账接口，仅给此账号增加 10 Demo 测试积分，原因包含 `QA R2 independent old task detail polling verification; local DEMO 10 test credits only; no payment order`。审计精确查询得到一条 `wallet.adjust`，amount=10、demo=true。
2. 通过同一 Cookie/CSRF 会话创建目标项目和空筛选项目。任务中心选择空项目，浏览器真实请求 `/api/v1/tasks?project_id=prj_95a9ec4c03ddbfd5936cebb63d4e0aa2&limit=25&offset=0`，返回 200、total=0、items=[]。
3. 使用正式 `/quotes` 和 `/tasks` 接口报价并受理唯一任务，产品为 `demo-image`、固定费用 10、demo=true。受理响应为 `queued / pending / frozen`。立即打开 `#/tasks/task_2cfe8132e6580356109f105ca1f54d3a`，保留空项目筛选，详情最初显示排队中、等待处理、已冻结。
4. 不操作刷新按钮，观察页面自行发出第二次任务详情 GET；返回真实 Worker 的归档和结算结果。详情自动显示生成完成、可下载、已消费，并展示实际 Demo PNG，图像已加载且 naturalWidth=1024、naturalHeight=576。
5. 继续观察至 17:48:45.667，详情请求总数仍为 2；终态后至少 84 秒没有新增详情请求。Esc 关闭详情返回 `#/tasks`，项目筛选仍保持空项目，列表继续为空。浏览器 `pageerror` 为 0。

## 网络时间证据

下表记录浏览器收到响应或测试客户端收到受理响应的本机观察时间，不是模型性能或生产 SLA。

| 时间 | 真实事件 | 结果 |
| --- | --- | --- |
| 17:46:59.998 | 带空项目筛选的列表 GET | 200，total=0，items=[] |
| 17:47:17.360 | 唯一任务受理响应 | queued / pending / frozen |
| 17:47:17.367 | 深链首次详情 GET | 200，queued / pending / frozen，asset_ids=[] |
| 17:47:21.381 | 自动详情轮询 GET | 200，completed / ready / consumed，唯一归档素材 |
| 17:48:45.667 | 终态持续观察 | 详情 GET 总数仍为 2 |
| 17:48:45.718 | Esc 关闭后 | `#/tasks`，空项目筛选仍保留 |

两次详情响应相隔 **4.014 秒**。第二次响应的服务端 `updated_at` 为 `2026-10-17T17:47:17.884923+08:00`。任务受理后至关闭详情，浏览器没有发出任何任务列表 GET；因此详情更新不能归因于列表轮询或任务恰好位于当前列表窗口。

## 资金、任务与素材一致性

| 检查项 | 实际结果 |
| --- | --- |
| 调账后的钱包 | available=10，frozen=0，demo=true |
| 最终钱包 | available=0，frozen=0，demo=true |
| 本账号积分流水 | 3 条：adjustment (+10,0)、freeze (-10,+10)、consumed (0,-10) |
| 本账号任务 | 1 条；管理员详情 attempts=1 |
| 目标项目素材 | 1 条，实际 Demo PNG，1024×576 |
| 本账号充值订单 | 0 条 |
| 对应调账审计 | 1 条，amount=10、demo=true |

验收脚本对详情状态序列、受理后列表请求数=0、当前筛选、唯一任务/素材、流水/订单/审计数量、最终钱包和 pageerror 数进行了断言，均通过。注册期间一次自动化工具等待超时后重建了独立会话；先前未使用的空 QA 账号没有调账或提交生成，不影响上述账号的记录。

## 本机证据文件与边界

证据保存在已被 Git 忽略的本机目录 `D:\Code\Other\wsapi-video\.runtime\qa\r2\`，不包含密码、会话令牌或请求头；五个文件均已逐一移动并核对 SHA256，未覆盖其他文件：

- `r2-detail-initial.png`：空列表背景下的排队中详情。
- `r2-detail-final.png`：自动更新后的可下载、已消费状态与 Demo 作品。
- `r2-filter-empty-after-close.png`：Esc 关闭后仍为空的项目筛选列表。
- `r2-task-polling-network.json`：浏览器实际列表及详情响应的精简记录。
- `r2-task-polling-evidence.json`：审计、流水、任务/订单/素材数量、图像加载与断言结果。

本次采用服务端项目筛选使目标任务离开当前列表窗口，未创建 25+ 条生成任务，也未单独验证 offset>0 的历史页。此记录证明列表外详情的真实成功路径与终态停止；真实服务 503 重试、真实图片/视频模型、生产 HTTPS/反代以及跨设备环境不由本次 Demo 验收覆盖。既有响应夹具的错误重试证据应继续单独标注其性质。

## 补充：关闭和切换时的真实迟到响应

主任务于同日 17:59–18:00 使用 [回执恢复 QA](QA_SUBMISSION_RECOVERY_2026-10-17.md) 的独立账号及既有 3 个终态任务补测；没有增加任务、调账或订单。浏览器使用 `route.fetch()` 将详情 GET 发往真实 8091，取得真实 200 后保持原响应不变，待界面关闭/切换完成后再释放，并未伪造任务状态。

- 关闭分支：任务 `task_1531cc298c1f3a19d489e7775cec1328` 的详情回执仍在途时按 Esc，回到 `#/tasks`，然后释放回执。详情保持关闭，没有被迟到响应重新打开。
- 切换分支：同一任务的详情回执仍在途时，导航到 `task_a7d7cb7538ffca460c632f33d8df6a03`；收到后者真实 200 并显示该 ID 后，再释放前者回执。弹窗继续只显示新任务，没有被旧任务覆盖。
- Node strict assert 校验关闭/切换结果、钱包完整对象及任务/流水/订单 items 与测试前深相等：钱包 0/0、3 任务、8 流水、0 订单、pageerror=[]。分页 limit 不同，因此没有把整个分页响应元数据声明为相等。

证据为已忽略的 `.runtime/qa/r2-late-response-evidence.json` 与 `r2-switch-late-response.png`，截图已目视核对新任务 ID。测试后所有路由均移除。该补充证明真实详情请求的迟到回执处理；没有模拟进行中任务的自动轮询响应失败，也不能据此宣称真实 503 或外部模型恢复通过。
