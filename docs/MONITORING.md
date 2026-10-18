# 只读异常快照与外部监控接入

此能力输出可定位的运营事实，不发送通知，不取消任务、释放冻结、退款、重发邮件或清理文件。外部调度、阈值、接收人与实际通知送达仍需在发布环境配置并演练。

## 后台排查

服务端 r8 源码已加入标准库 slog JSON 事件。输出固定 `msg=platform_event`、`event`、`code`，相关业务附本地 `task_id`/`order_id` 和正整数 `attempt`；字段与值均有限定，未知字段和旧标准日志原文会被过滤。任务创建/领取/提交、进入核查/重试、结算/取消、支付订单创建/到账及失败有对应事件；提交和有效租约写入成功后才记录成功，重复到账不再报一次成功。日志不保存原始错误、URL、上游响应、提示词、邮箱、密钥、签名链接或人工核对依据，业务详情仍回后台与数据库审计核查。

这是诊断事件流，不是完整审计、请求访问日志或通知渠道；不会给每个 HTTP 请求记录状态码/耗时，也没有接入日志收集与实际告警。`starting` 只表示进入监听调用，不能解释为 readiness 已通过；`run_failed` 也不包含原始启动错误。常驻服务最后确认于 `2026-10-17T14:00:46Z` 为 r3，之后未复核运行状态；r8 的日志改动当时只有源码、测试与构建证据，详见 [恢复与日志验收](QA_OPERATION_RECOVERY_2026-10-17.md)。当前交付状态见 [实施进度](IMPLEMENTATION_STATUS.md)。

运营概览的待处理信号从 `GET /api/v1/admin/operations/signals` 读取，要求管理员会话。每类包含准确总数、最早记录时间和最早五条本地对象样本；样本不是完整列表。同一任务可命中多类信号，邮件重试也是待发送的子集，不能将各类相加作为唯一异常对象数。

| code                          | 匹配事实                                                                                                                |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `task_submission_unknown`     | 仍冻结且执行状态为 submission_unknown                                                                                   |
| `task_frozen_overdue`         | 仍冻结且创建时间早于应用 TaskReviewAfter，当前默认 24 小时                                                              |
| `task_archive_failed`         | 仍冻结且生成完成，并处于归档重试/错误或 archive_review                                                                  |
| `task_lease_expired`          | 仍冻结且非空 Worker 租约已过期，不等于已证明 Worker 永久停止                                                            |
| `payment_review`              | 未到账且已付款、订单过期或查单次数至少 3 次，不等于付款失败                                                             |
| `mail_pending`                | 所有尚未发送的 outbox 记录，包含正常排队邮件                                                                            |
| `mail_retry`                  | 尚未发送且 attempts 大于零的邮件，是 mail_pending 的子集                                                                |
| `procurement_review_disputed` | 每个任务 revision 最大的采购人工核对记录仍为 disputed；与冻结、执行、交付和旧采购异常处置状态无关，已消费任务同样可计入 |

快照的计数、最早时间和样本来自一条 SQL 的同一时间点。`oldest_at`/样本 `occurred_at` 对过期租约使用 lease_until，人工采购差异使用当前核对记录的 created_at，其他使用对象 created_at；它们不是故障首次发生时间。采购差异的样本 ID 为本地任务 ID，attempts 固定为 0，不读取原因、依据或操作者。后续最新记录改为 pending、matched 或 resolved，该任务即退出此信号；历史 disputed 不继续计数。邮件只输出本地 outbox ID、时间与重试次数，不输出邮箱、主题、正文或验证/重置 ticket。快照也不含租约 token、密钥、签名链接或供应商错误响应。

后台任务列表支持 `status=frozen_overdue`、`lease_expired`、`archive_failed`、`procurement_review_disputed`，同时保留已有搜索和分页。人工采购差异筛选只查最新核对，不混同原有 `disputed`/`review_required` 的生成异常处置筛选。任务/订单样本可打开现有详情；邮件排查应核对 SMTP 配置、实际收件及重试进展，不在此手工修改 outbox 或重复发送。

## 给机器使用的只读账户

外部监控使用 `cmd/monitor`，不持有管理员会话、APP_SECRET 或应用数据库写权限。部署人员在目标数据库用管理账户创建**独立的新监控角色**，按实际数据库和 schema 替换以下 `studio`/`public`。这些是待部署的操作说明，本任务没有对生产数据库执行：

```sql
CREATE ROLE frame_monitor LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION;
GRANT CONNECT ON DATABASE studio TO frame_monitor;
GRANT USAGE ON SCHEMA public TO frame_monitor;
GRANT SELECT (id, created_at, attempts, lease_until, execution_status,
  settlement_status, delivery_status, error_code)
  ON public.generation_tasks TO frame_monitor;
GRANT SELECT (id, created_at, check_attempts, credit_status, payment_status, expires_at)
  ON public.payment_orders TO frame_monitor;
GRANT SELECT (id, created_at, attempts, sent_at)
  ON public.auth_mail_outbox TO frame_monitor;
GRANT SELECT (task_id, revision, status, created_at)
  ON public.task_procurement_reviews TO frame_monitor;
ALTER ROLE frame_monitor SET default_transaction_read_only = on;
ALTER ROLE frame_monitor IN DATABASE studio SET search_path = public;
```

查询表名没有限定 schema；最后一行的 `public` 必须与上述四表实际所在 schema 一致，也可在专用 MONITOR_DATABASE_URL 中显式设置同一个 search_path。仅替换 GRANT 的 schema 而不配置连接搜索路径，会导致找不到表。不要在监控 DSN 中覆盖成其他搜索路径。先由应用正常执行包括 005 在内的迁移，再部署新版 monitor；已部署的监控角色只追加上面的四列授权，不授予核对原因、依据、操作者或表级 SELECT。表或列授权缺失会使读取失败，不能解释为无采购差异。

在可信交互式 psql 中用 `\password frame_monitor` 设置独立密码，经秘密管理器提供连接字符串；不要在聊天、命令日志或仓库里写实际密码。不授予其他角色成员资格、全表 SELECT、写权限或数据库管理员权限；迁移新增表不会自动扩大该角色授权。结合数据库既有 TLS、网络访问及认证策略，仅允许监控主机连接。外置 PostgreSQL 不得沿用同主机容器模板的关闭 TLS 设置。

## 构建与单次读取

本机 Windows 构建示例：

```powershell
Push-Location -LiteralPath 'D:\Code\Other\wsapi-video\backend'
try {
    go build -o 'D:\Code\Other\wsapi-video\.runtime\frame-monitor.exe' ./cmd/monitor
    if ($LASTEXITCODE -ne 0) { throw 'Monitor build failed' }
} finally { Pop-Location }
```

先通过部署秘密管理器将专用只读连接注入 `MONITOR_DATABASE_URL`，然后单次执行：

```powershell
& 'D:\Code\Other\wsapi-video\.runtime\frame-monitor.exe' -timeout 10s -review-after 24h
```

CLI 不回退到 DATABASE_URL，也不读取后台凭证；连接会话强制并校验 default_transaction_read_only=on。连接和查询共用总超时，失败以非零状态退出，标准错误不输出连接字符串。成功时标准输出只有一份 JSON 快照；退出 0 仅表示成功读取，不能解释为零异常。

生产镜像构建配置也将同一程序放在 `/app/monitor`，可在受控监控容器或主机上运行。实际镜像/容器运行仍需目标主机验收；文件中的构建配置不是已运行证明。

外部监控系统应解析每项 code/count/oldest_at，按确认后的阈值、持续时间和去重规则发通知，同时对 CLI 超时/失败告警；不得把读取失败当成零异常。当前应用冻结阈值固定为 24 小时，若以后修改应用 TaskReviewAfter，监控的 review-after 必须同步。邮件待发需要结合最早记录年龄与重试次数判断，不能把正常排队直接当作失败。此文档没有创建定时任务或通知渠道。

Sub2API 服务账户余额和精确采购账单尚无经核实的公共接口，继续保持不可用与人工对账，不能推算成已核销。新增信号仅汇总运营已经记录的人工差异，不证明供应端金额、退款或账单状态，也不自动处置任务。S3 实际占用、主机暂存空间、数据库健康与容量、HTTPS/API 错误率另由基础设施监控覆盖；后台素材逻辑字节数不能替代它们。

数据库权限语法依据：[PostgreSQL 16 GRANT](https://www.postgresql.org/docs/16/sql-grant.html)、[ALTER ROLE](https://www.postgresql.org/docs/16/sql-alterrole.html)。
