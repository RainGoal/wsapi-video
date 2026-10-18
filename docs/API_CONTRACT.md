# 平台业务接口契约

状态：2026-10-17 实施基线。所有路径属于新平台，不是 Sub2API 公开 API。

## 公共约定

- 根路径 `/api/v1`，同源 Cookie 会话；后端监听 `127.0.0.1:8090`，开发前端 `127.0.0.1:5176` 代理 `/api`。
- 成功响应 `{ "code":"ok", "message":"", "data": T }`；失败使用非 2xx HTTP 与 `{ "code":"稳定原因", "message":"用户可读说明" }`。
- 业务对象 ID 是不透明字符串；时间使用 RFC3339；金额使用整数分，积分使用整数。
- 受保护的写操作带 `X-CSRF-Token`，值来自登录及 `/me`；公开注册/登录/验证/找回与支付通知使用各自校验。
- 列表统一 `{items:[]}`，无数据返回空数组；错误不暴露原 Key、供应端地址或签名 URL。
- 不存在或非本人资源返回 404；401 未登录；403 无权限；409 版本/幂等冲突；422 输入错误；429 限流；503 尚未配置服务。

## 系统和身份

`GET /system`：公开配置，包含 `brand/version/demo/registration_enabled/storage_driver/payment_enabled/media_configured`，不返回秘密。

User：`id/email/display_name/role/status/email_verified/created_at`，role 为 user/admin。

- `POST /auth/register`：邮箱、密码、显示名，创建独立账户；开发环境允许响应验证链接，生产只能发送邮件。
- `POST /auth/login`：返回 `{user,csrf_token}` 并设置安全 Cookie。
- `GET /me`：返回 `{user,csrf_token}`。
- `POST /auth/logout`、`GET /auth/sessions`、`DELETE /auth/sessions/{id}`：退出、会话查询/撤销。
- 验证邮箱、重发验证、忘记/重置/修改密码按 auth 模块路由实现并由前端共用。

Cookie、过期和 token 的服务端记录才是登录事实，前端不能仅根据本地状态判断管理员身份。

## 项目和画布

Project：`id/user_id/name/description/status/cover_asset_id/revision/shots_revision/created_at/updated_at`，status 为 active/archived，删除为软删除。

- `GET /projects`：本人项目列表，支持搜索与状态。
- `POST /projects`：`{name,description?}`，返回 Project。
- `GET/PATCH/DELETE /projects/{id}`：详情、修改和受控软删除。
- `GET/PUT /projects/{id}/canvas`：`{graph_schema_version:1,revision,graph}`。
- graph：`{nodes:[],edges:[],viewport:{x,y,zoom}}`；节点通过 `asset_id/task_id` 引用服务端事实。
- PUT 包含读取时的 revision，原子保存后返回递增 revision；旧版本返回 409，不静默覆盖。
- 项目资料的 `Project.revision`、画布文档的 `Canvas.revision` 和分镜的 `shots_revision` 独立递增，不能互相代用。画布保存只更新项目最近编辑时间，不改变项目资料 revision。
- `POST /projects/{id}/duplicate` 接受 `{name?,graph?,shots?}`，在事务中另存冲突草稿并保留素材引用、候选和选定结果；重映射镜头 ID，不生成或扣费。

## 分镜

- `GET/PUT /projects/{id}/shots`：`{revision,shots:[]}`；批量保存镜头、排序与选定关系，旧版本 409。
- 镜头包含 id、描述、提示词、比例、时长、参考素材、候选和独立选定图片/视频。
- 可选 `excluded_candidate_ids` 记录用户主动移除的候选素材 ID，旧文档缺失时视为空；最多 1000 项，ID 非空且不超过 100 字节。移除决定随分镜保存、重载和项目副本保留，自动追加新交付时跳过这些 ID，重新加入候选时清除对应记录。排除项不能同时出现在活动候选或选定成品中，也不构成素材的活引用。
- 项目、画布、分镜、任务使用一致的资源归属；具体 shots 字段以模块导出类型为准，变更须同步前端。
- 重生成不替换已选成品，删除节点/镜头不改变历史任务与账务。

## 素材和成品

- `GET /assets?project_id=...&kind=image|video|audio`：列表 `{items:Asset[]}`。
- `POST /assets`：multipart `file`、`project_id`，返回 Asset。
- `GET /assets/{id}/references`：本人素材的只读引用位置，含项目、画布/镜头/任务/封面和具体对象 ID，使用标准分页；详见 [素材来源与引用](ASSET_REFERENCES_CONTRACT.md)。
- `GET /assets/{id}/content`：本人预览，支持本地/私有存储读取或受控签名跳转。
- `GET /assets/{id}/download`：本人下载；`DELETE /assets/{id}` 检查引用及在途任务。
- Asset：`id/user_id/project_id/task_id/kind/file_name/content_type/object_key/size_bytes/width/height/duration_seconds/created_at`；面向前端提供可访问预览/下载路径，object key 不作为授权依据。
- 选定成品包从已授权、就绪资产创建，失败重试不生成新媒体或记账。

## 商品、报价、任务、钱包和支付

- `GET /model-products`：`{items:ModelProduct[],demo:boolean}`；商品明确 `kind/model/capabilities/credits/pricing/version/active/verified/demo`，实际字段以 commerce 导出类型为准。
- 生产环境未配置私有存储时，`GET /model-products`、`POST /quotes` 及尚未受理的新 `POST /tasks` 返回 `503 storage_unavailable`，明确提示管理员完成私有存储配置，不再返回空商品列表。拒绝不会冻结积分；已受理任务的同键重试继续返回原任务。
- 视频可选 `pricing:{billing_unit:"per_video"|"per_second",resolution_credits:{"720p":5,"1080p":8}}`；`credits` 为默认单位整数积分，分辨率覆盖价优先（忽略大小写与两端空格），未覆盖使用默认价。缺失/null 的 `pricing` 保持旧固定单次积分；图片不接受此配置。按秒使用规范化后的请求时长，单位积分与总额均须在 1–1,000,000,000 范围内。
- `POST /quotes`：项目、商品、提示词、参数与参考 asset ID；返回报价 ID、固定总积分和 expires_at。视频定价明细 `pricing` 包含 `billing_unit/unit_credits/quantity/resolution/total_credits`，按次数量为 1，按秒数量为请求时长。服务端保存输入、完整商品/价格快照与总额；后台后续调价不重算该报价。
- `POST /tasks`：`{quote_id,request_key}`；同键同内容返回原任务，同键不同内容 409；事务内创建任务并冻结积分。
- `409 quote_expired`：持有报价锁且确认该报价没有已受理任务后，才返回此明确拒绝；前端可清理原恢复记录并重新报价。通用 `409 conflict` 可能表示并发已受理，不能按状态码清除恢复信息。
- `409 product_unavailable`：同样在持有报价锁且确认没有已受理任务后返回，表示商品已删除；可清理原恢复记录并选择其他商品重新报价。已受理任务的同键重试继续返回原任务。
- `GET /tasks`、`GET /tasks/{id}`：只返回本人任务，支持项目/类型/状态过滤。
- `POST /tasks/{id}/cancel`：按真实能力执行；未确认取消不释放。
- `GET /wallet`：可用/冻结积分；`GET /wallet/ledger`：不可变流水列表。
- `GET /payment/tiers`：可购买充值档位；`GET/POST /payment/orders`、`GET /payment/orders/{id}`：订单/收银台与到账状态。
- `GET/POST /payment/notify/easypay`：公开验签入口，核验商户/订单/金额并主动查单确认，再唯一入账。
- 支付返回页只查单。Demo 商品只能在显式开发环境运行，演示积分不与真实支付成功状态混用。

创建结果不明确时保存 submission_unknown，不自动再次 POST。执行、交付、零售结算、采购核查状态独立保存。生成成功且平台归档成功才结算。

浏览器提交前按账号/项目保存并读回原 `quote_id/request_key`，使用同名 Web Lock 串行化同源标签中的恢复记录写入和清理；网络请求不持有此锁。不支持 Web Locks 或无法可靠保存记录时不发送生成请求。历史不确定提交遇到鉴权、限流或通用冲突仍保留原键；只有有效任务回执或明确 `quote_expired` / `product_unavailable` 才结束恢复。回执已确认但本地清理失败时，本页只重试清理。

## 基础后台

所有 `/admin/*` 复用认证与管理员鉴权，调价、停用、补单、退款/调账须有审计。

- 用户列表/状态修改/会话撤销。
- 商品列表/上架/能力和零售价配置，充值档位配置。商品上架读取后台当前对应图片或视频凭证；用户可售列表、报价及生成继续检查生产存储是否就绪。
- `DELETE /admin/model-products/{id}`、`DELETE /admin/payment/tiers/{id}`：请求 `{version,reason}`，成功返回 `{ok:true,id,version}`。版本落后返回 409，缺失版本或原因无效返回 422，不存在或已删除返回 404。删除与审计在同一事务完成，自动下架并从目录列表移除，保留已有任务、订单、账本与快照；删除的 ID 不可复用。
- 订单列表/查单补偿、异常任务列表/决议、任务退款、积分调整。
- 每任务采购人工核对记录：管理员读取/追加结论、原因与纯文本依据，独立于异常处置和零售账务，见 [完整契约](PROCUREMENT_REVIEW_CONTRACT.md)。
- 审计与存储/异常概览。

准确路由与请求类型由各模块维护并在实现说明中同步；UI 不得伪造后台操作成功。

## 媒体调用边界

唯一固定 Sub2API HTTPS origin。图片采用 `/v1/images/generations/async`、`/v1/images/edits/async`、`/v1/images/tasks/{id}`；视频采用 `/v1/videos/generations`、`/v1/videos/{id}`、`/v1/videos/{id}/content` 与按能力开放的取消路径。

图片/视频分别使用服务凭证，任务保留原凭证引用；凭证轮换须同时保留仍在途任务的旧凭证。平台私有素材生成临时 HTTPS 地址后提交，结果及时归档到平台存储。

当前 Sub2API 工作区存在另一个任务的未提交媒体改造。新平台不得修改这些文件，不把工作区中的新路由当作已经发布可用的生产接口。
