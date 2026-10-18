# 运营异常快照与只读监控验收

日期：2026-10-17。依据开发计划的异常定位与监控要求，补齐只读信号、样本详情入口、任务筛选和机器读取能力。外部告警的调度、接收人与真实送达仍未配置。

## 实现与版本

- 管理员接口 `/api/v1/admin/operations/signals` 使用同一 SQL 快照统计七类信号，每类返回准确总数、最早时间和最早五条样本。类别允许重叠，邮件待投递包含正常排队及重试，不应加总为唯一故障数。
- 租约时间为 lease_until，其他时间为 created_at，均不声称是首次异常时间。接口不输出邮件收件人、正文、ticket、租约 token、密钥或签名链接，不执行处置。
- 后台提供任务/订单精确详情深链及类别入口，任务列表新增 frozen_overdue、lease_expired、archive_failed。原 submission_unknown 列表语义保留，可能包含已结算记录，类别入口使用“进入对应列表排查”。
- `cmd/monitor` 只读取专用 MONITOR_DATABASE_URL，不读取 APP_SECRET、不回退应用 DATABASE_URL；强制并校验只读连接，默认总超时 10 秒。stdout 成功仅一份 JSON，失败非零且错误脱敏；exit 0 不等于零异常。
- `/system` 新增明确的 development 布尔值，后台区分开发、生产、演示及旧后端的环境未确认；生产模式标识不代表完成发布验收。
- 已构建后端 `studio-release-20261017-r4.exe`，SHA-256 `a81bb09c4789b7a81a7679d0f7667c60cf24454d6b2caadf55e23d72e51db520`。
- 已构建监控 `monitor-release-20261017.exe`，SHA-256 `eda5f46a81e78a4c36f67fe0931ff1c6d618bd448963d2b1a1bcf710c91cbd4a`。
- 前端 `index-QLiG3XWs.js`、CSS `index-ZwSgo3Qj.css`，由既有服务静态目录读取。

## 真实数据库与构建

使用本机 PostgreSQL 的随机隔离 schema、实际 ffprobe：

`go test -race ./... -count=1 -timeout=180s` 全部通过，墙钟 52.31 秒；commerce 30.937 秒、server 38.376 秒、operations 4.505 秒、monitor 4.141 秒。`go vet ./...`、server/monitor 构建通过。前端 30/30、全目录格式、类型检查及生产构建通过。

新增数据库测试验证七类计数、阈值/回落、最早时间、稳定排序与样本上限；401/403/200 权限、取消后的 503、敏感值不泄漏，以及任务/支付/邮件/钱包/账本/审计读取前后指纹不变。任务列表三个新别名验证新鲜记录不匹配、旧冻结/过期租约/归档异常匹配、组合搜索与分页、已释放记录排除。

CLI 的真实临时限列角色仅获三表所需列 SELECT 即可读快照；六类私密列均拒绝为 42501，所有者通过只读监控连接写入为 25006。75ms 建连停滞与 1s 锁阻塞查询超时测试通过；临时角色和 schema 已清理。未修改活业务配置；CLI 本身不执行迁移，未调用外部服务。

003/004 迁移保持冻结校验和；Dockerfile/CI 已纳入 monitor 构建，但本机无 Docker，镜像、容器 smoke 和远端 CI 未执行。

## 浏览器范围与运行限制

三个既有本地服务的 r3→r4 更新被自动审批拒绝，工具仅返回 `blocked by policy`，未给具体原因；命令未执行，未换工具重试。8090/8091/8092 仍运行 r3，8092 新接口实际返回 404，后台如实显示接口错误和“环境未确认”。因此本记录不声称新接口已在运行服务联调通过。

在当前 r3 服务托管的新前端上，使用独立 Chromium 上下文与明确响应夹具，共 21 项检查通过：

- 七类卡片、1,234,567 大计数、每类最多五样本；1440 与 390 宽度无整页横向溢出。
- 503 时隐藏旧卡片并能重试，零计数/空分类/null 快照不冒称服务健康。
- 任务/订单样本打开指定本地 ID，任务深链刷新保留详情；三个新类别筛选入口正确。
- 详情 404 不保留旧内容，关闭后迟到响应不重开；邮件仅去设置，不把 outbox ID 当作任务或订单。
- 中英文文案、英文窄屏，以及开发/生产/演示标识的响应夹具检查通过。
- 实际后台配置完整前后相同、revision=0；media/payment/smtp/storage readiness 均为 false，未保存配置或调用生成、支付、发信、对象写入。

pageerror 为空，交互阶段无非 GET 请求，最后仅退出独立测试会话（200）并关闭浏览器。证据为被忽略的 `.runtime/qa/operations-signals-browser-evidence.json`、`operations-signals-desktop.png` 和 `operations-signals-mobile.png`。这些截图与响应夹具不证明真实异常已产生、真实外部通知或 r4 运行验收。

此前隔离恢复应用 8103 的启动也曾被自动审批拒绝，仍未启动，见 [恢复前检查](QA_RECOVERY_APP_PREFLIGHT_2026-10-17.md)。完整发布门槛继续保留于 [上线就绪审查](RELEASE_READINESS.md)。
