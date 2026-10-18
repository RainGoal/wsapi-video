# 用户历史列表与搜索

下列接口均要求本站账号会话，始终按当前账号隔离。响应为统一 envelope `{"code":"ok","data":{"items":[],"total":0,"limit":25,"offset":0}}`。

所有接口支持 `limit`（默认 25，范围 1–100）及 `offset`（默认 0，范围 0–10000000）。非法、重复、超限分页参数返回 422。支持的筛选字段为单值，重复、含 NUL、超过长度上限或非法枚举也返回 422；搜索最多 200 UTF-8 字节，匹配大小写不敏感的字面子串。

| 接口 | 搜索及筛选 |
| --- | --- |
| `GET /api/v1/projects` | `q` 搜索名称/描述；`status=active`（默认）/`archived`/`all`，不包含软删除项目 |
| `GET /api/v1/assets` | `q` 搜索文件名；`project_id`、`kind=image/video/audio`；保留项目共享引用素材，不包含软删除素材 |
| `GET /api/v1/assets/{id}/references` | 本人未删除素材的引用位置；无搜索/类型筛选，按 `project_id,owner_type,owner_id` 升序稳定分页；见 [素材引用契约](ASSET_REFERENCES_CONTRACT.md) |
| `GET /api/v1/tasks` | `q` 搜索任务 ID/模型/提示词；`project_id`、`shot_id`、`kind=image/video`、`status` |
| `GET /api/v1/wallet/ledger` | `q` 搜索流水 ID/原因/任务 ID/订单 ID；`event_type=adjustment/freeze/consumed/released/refund/recharge` |
| `GET /api/v1/payment/orders` | `q` 搜索订单 ID/本站交易号/渠道交易号；`status=pending/expired/paid/credited`；`out_trade_no` 精确匹配本人交易号 |

任务 `status` 可为空或 `all`，或 `queued/sending/submission_unknown/generating/completed/failed/canceled/pending/archiving/archive_retry/ready/error/frozen/consumed/released/refunded/disputed/resolved/review_required`。它匹配执行、交付或结算状态；`disputed/resolved` 匹配采购核查状态，`review_required` 表示冻结且提交未知或有争议。订单状态匹配支付或入账状态；例如 `pending` 包含仍未入账的过期订单。订单 `status=all` 等同不筛选。

项目排序为 `updated_at DESC,id DESC`；其他列表为 `created_at DESC,id DESC`。ID 是相同时间戳时的稳定次序。`total` 是当前筛选条件下的记录数，访问超出末页仍返回相同 total 和空 items；数据实时变化时，客户端刷新应重新开始分页，合并结果按 ID 去重。

工作台依赖项目全部素材/任务恢复节点和分镜候选，应按项目加载所有分页，不能把默认 25 条当成完整项目。支付返回页应使用 `out_trade_no` 的精确查询获取本人订单内部 ID 后调用原查单接口；此列表查询无查单、支付或入账副作用，不信任前端金额/成功标记。
