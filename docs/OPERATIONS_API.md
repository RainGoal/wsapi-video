# 只读运营概览

`GET /api/v1/admin/overview` 使用平台管理员会话，响应沿用 `{code,message,data}`。

| data 字段 | 含义 |
| --- | --- |
| `generated_at` | 数据库快照时间 |
| `users.total/active/disabled/unverified` | 用户总数、启用、停用、未验证邮箱数 |
| `projects.active/archived/deleted` | 活跃、归档及软删除项目数 |
| `assets.active_count/active_bytes` | 尚未软删除素材的记录数和字节数 |
| `assets.retained_count/retained_bytes` | 包含软删除素材的留存记录数和字节数 |
| `assets.capacity_basis` | 固定为 `logical_database_records`，这是数据库逻辑容量，不代表对象存储实际账单或磁盘可用空间 |
| `tasks.review_required` | 仍冻结且提交待确认或采购状态 disputed 的任务数 |
| `tasks.frozen_overdue` | 冻结且创建时间早于 `TaskReviewAfter` 的任务数；默认阈值 24 小时 |
| `tasks.archive_failed` | 冻结且生成完成、归档重试/错误或等待归档核查的任务数 |
| `tasks.frozen_count` | 全部仍冻结任务数 |
| `wallet.available_total/frozen_total` | 全平台可用与冻结整数积分总额 |
| `payments.pending_count` | 未完成积分到账的充值订单数 |
| `payments.review_required` | 未到账且已付款、已过本地订单有效期、或查单次数达到 3 次的订单数；不代表付款失败 |
| `procurement_balance.status/reason` | 当前为 unavailable 及原因，尚无经核实的 Sub2API 服务账户余额公开接口 |

同一 SQL 语句读取一致快照。该接口不触发生成、查单、删除、取消、退款、积分释放或自动补单。待核查计数允许重叠，不能相加当作唯一异常任务数。

## 可定位的异常快照

概览中的采购异常计数来自任务自动处置状态，不能代表供应账单已匹配。独立的采购人工核对记录由管理员按任务读取/追加，见 [采购核对契约](PROCUREMENT_REVIEW_CONTRACT.md)；核对记录不修改任务状态、积分或上述概览的任务异常计数，也不调用供应账单接口。

`GET /api/v1/admin/operations/signals` 同样要求管理员会话，返回 data `{generated_at,review_after_seconds,sample_limit:5,signals:[{code,count,oldest_at,samples:[{id,occurred_at,attempts}]}]}`。保留原七类信号，追加 `procurement_review_disputed`：每任务最新 revision 的人工核对为 disputed 时计入，已 consumed/released/refunded 同样可计；旧的 disputed 被最新 pending/matched/resolved 替换后不计。样本只含任务 ID、当前核对 created_at 和固定 attempts=0，不含原因、依据及操作者。八类信号、时间语义、样本限制及专用只读机器消费方式见 [监控接入](MONITORING.md)。不返回邮件收件人/正文/ticket、租约 token 或凭证，不执行任何处置。后台 API 使用管理员权限，机器监控改用只读数据库角色和 monitor CLI，无需管理员写权限。

`GET /api/v1/admin/tasks?status=procurement_review_disputed` 按同一最新核对条件返回完整可分页列表，支持既有 q/kind/user_id/project_id 组合筛选及 created_at DESC、id DESC 排序。它独立于旧 `status=disputed` 和 `review_required` 的生成异常处置筛选；信号总数与后续列表是不同请求的读取快照，期间新核对可能改变数量。
