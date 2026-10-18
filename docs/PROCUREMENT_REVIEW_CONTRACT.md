# 采购人工核对记录

人工核对用于运营按本地任务、上游任务 ID 及原凭证引用，在线下核实供应端使用记录后留下结论与依据。它不查询供应账单，不把服务 Key 汇总余额差换算为单任务成本，不确认供应商退款，也不改变用户零售积分。

## 与异常处置分离

现有 generation_tasks.procurement_status 供生成异常处置使用，绑定原任务、释放冻结、重试归档和零售退款可改变它。它不能证明供应账单已匹配。新的 task_procurement_reviews 独立追加人工记录，四种状态为 pending（待核对）、matched（核对一致）、disputed（存在差异）、resolved（人工决议）；无记录相当于 revision=0、pending。

每次记录保存本地任务 ID、连续 revision、操作者 ID、状态、原因、纯文本依据与时间，并在同一事务追加 admin_audit。后续更正追加新版本，不能修改旧记录。UI 中现有状态标为“采购异常处置”，新入口为“采购人工核对”。

## 管理员 API

`GET /api/v1/admin/tasks/{id}/procurement-reviews?limit=25&offset=0`

要求管理员会话，沿用标准分页限制。响应 data 为 `{items,total,limit,offset,current}`；items 按 revision 倒序，current 始终表示最新记录，翻历史页不会倒退当前版本。管理员鉴权后，任务存在性、总数、current 和分页使用同一查询快照。未知任务 404，未登录 401，非管理员 403。

每条记录只含 `id,task_id,revision,admin_id,status,reason,evidence,created_at`；created_at 为 RFC3339。无记录时 current 的 revision 为 0、status 为 pending，其余字段为空字符串，items 为空。接口不自动附加任务提示词、服务 Key、完整签名 URL、供应商原始响应或财务信息；原因/依据是运营输入的纯文本，不承诺自动识别秘密，操作员不得在其中填写密钥或完整签名链接。

`POST /api/v1/admin/tasks/{id}/procurement-reviews`

```json
{
  "expected_revision": 0,
  "status": "matched",
  "reason": "已核对供应端对应任务记录",
  "evidence": "使用记录编号及核查说明，不填写密钥或完整签名链接"
}
```

expected_revision 必须显式提供非负整数，状态只接受上述四值。原因和依据均去首尾空白；原因必填，沿用既有原因最少三个 UTF-8 字节规则。原因和依据各最多 2000 个 Unicode 码点，依据可以为空。依据作为纯文本保存/展示，不作为可执行链接。无效业务字段返回 422，非法 JSON 沿用通用解码错误。

事务锁定本地任务并核对最新 revision，只有一致时追加下一版本和审计；成功返回 201 与新记录，过期版本返回 409。并发或重复提交相同期望版本只允许一条记录，失败审计回滚记录。接口不更新 generation_tasks，不改租约、attempt、交付/执行/结算状态、资产、订单、钱包或流水，也不调用网关。

读取失败不能显示为“尚未核对”；保存回执丢失不能假定未保存。前端保留输入，停止提交并要求先刷新最新记录，不自动重新 POST。409 同样先刷新并确认他人最新记录，再明确提交新的版本。

## 全局差异定位

运营异常快照及只读 monitor 追加 `procurement_review_disputed` 信号，仅取每任务最新 revision 为 disputed 的记录；不按所有历史差异累计，不依赖生成异常处置状态或零售结算，正常已消费任务同样可列出。最新核对改为 pending、matched 或 resolved 后退出信号。样本为本地任务 ID、当前核对 created_at 和固定 attempts=0，不读取原因、依据或操作者；总数、最早时间及五个样本使用同一查询快照。

管理员任务列表的 `status=procurement_review_disputed` 使用相同最新核对规则并保留搜索、媒体/用户/项目组合筛选与分页。原 `disputed`/`review_required` 筛选仍表示生成异常处置，不因人工核对变更。此信号只让已记录的人工差异可定位，不代表供应账单已自动查询或核销。监控角色仅需该表 task_id、revision、status、created_at 四列 SELECT，升级步骤见 [监控接入](MONITORING.md)。

## 发布与验收

005 仅新增独立记录表及约束，不修改已冻结的 003/004。回滚应用时保留新表和记录，不删除审计数据。单元/数据库测试和响应夹具只能证明对应实现；实际供应账单核查及运行服务中的端到端记录仍需分别验收。
