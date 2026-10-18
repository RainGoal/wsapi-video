# 管理后台运行配置契约

所有接口要求管理员会话；PUT/POST 要求 `X-CSRF-Token`。数据库、公开域名、主密钥、监听地址及本地存储根目录由部署环境提供，不能通过浏览器修改。

`GET /api/v1/admin/settings` 返回统一 envelope `{code:"ok",data:{revision,updated_at,settings,readiness}}`，下方示例是 data 字段。

```json
{
  "revision": 0,
  "updated_at": null,
  "settings": {
    "site": {"brand":"FRAME STUDIO","registration_enabled":true,"support_email":"","retention_policy":"","storage_quota_policy":"","cleanup_policy":"","submission_recovery_policy":"","refund_policy":""},
    "media": {"origin":"","output_origins":[],"image_key_configured":false,"video_key_configured":false},
    "payment": {"enabled":false,"origin":"","pid":"","notify_url":"https://studio.example/api/v1/payment/notify/easypay","return_url":"https://studio.example/wallet","key_configured":false},
    "smtp": {"host":"","port":587,"username":"","from":"","password_configured":false},
    "storage": {"driver":"s3","endpoint":"","region":"auto","bucket":"","access_key_configured":false,"secret_key_configured":false,"location_locked":false}
  },
  "readiness": {"media":false,"payment":false,"smtp":false,"storage":false,"registration":false,"demo":false}
}
```

`PUT /api/v1/admin/settings` 接收 `{revision,reason,settings,clear_secrets?:string[]}`。`settings` 包含全部分区的非秘密字段。秘密字段为 `media.image_key` / `media.video_key` / `payment.key` / `smtp.password` / `storage.access_key` / `storage.secret_key`，省略或空字符串保留旧值；非空替换；仅 `clear_secrets` 列出的字段会删除。禁止把掩码作为秘密回传。响应与 GET 相同，不返回任何秘密或其末位。

保存原子生效并记录只含变更字段名称的管理员审计。过期 revision 返回 HTTP 409 `{code:"settings_conflict",message:"..."}`。字段验证返回 HTTP 422 `{code:"invalid_settings",message:"...",data:{fields:{"media.origin":"必须使用固定 HTTPS Origin"}}}`。存储已有资产后不允许切换 driver/endpoint/bucket/region；同位置凭证可以轮换。已有媒体任务和支付订单保留原配置快照，不会改用新商户/网关或密钥。

图片结果下载的 `media.output_origins` 使用当前管理员配置，独立于原任务的网关地址和 Key 快照。补充允许域名可使旧任务重试保存；撤销域名会使后续下载重试拒绝访问该外部域名。原任务网关 Origin 仍按既有规则隐式允许；域名修改不取消已发出的下载、不替换历史凭证，也不会重新生成。仅接受精确 HTTPS Origin，不允许通配符，图片下载不携带网关 Bearer。

站点政策为 `site.retention_policy`（素材保留期限）、`site.storage_quota_policy`（容量说明）、`site.cleanup_policy`（清理规则）、`site.submission_recovery_policy`（提交未知与长期冻结处理期限）、`site.refund_policy`（退款及迟到成品政策）。各项为最多 2000 个 Unicode 码点的纯文本，保存时裁去首尾空白；默认空，旧配置缺字段时也按空处理。不替运营预设天数、额度或资金承诺。政策内容是对外声明，不能用配置文字代替配额、定时清理、自动退款或具体人工处置的执行证据。

`POST /api/v1/admin/settings/test/{media|payment|smtp|storage}` 接收 `{}`，只检测已保存配置。返回 HTTP 200 `{ok:boolean,message:string,checked_at:string,scope:string}`。媒体只读查询模型列表；支付只检测 HTTPS 接入，不创建订单、不声称验签已联调；SMTP 连接、TLS、认证后 QUIT，不发送邮件；存储 HeadBucket，不写入/删除对象。错误信息经过脱敏，不输出 URL 查询、供应商响应体或凭证。测试结果不是正式模型/支付/存储权限的完整验收证明。

`GET /api/v1/system` 无需登录，使用最新站点名、支持邮箱、上述五项政策（顶级同名字段）、`development` 实际开发模式布尔值及注册/媒体/支付可用状态。后台将运行模式与 Demo 标记分别解释，不将非 Demo 开发环境标作已上线；生产模式也不代表发布门槛已通过。只返回显式允许公开的字段，不返回运行配置对象或凭证。用户可在登录前后查看政策，空项显示待运营公布；正式开放前仍须填写并核对实际执行流程。生产 `APP_SETUP_MODE=true` 只允许管理员先配置缺失的外部依赖；公开注册在 SMTP/私有存储就绪前关闭。无数据库或密文解密失败均 fail closed，不使用旧环境回退继续接单。APP_SECRET 必须随数据库密文备份；更换主密钥需要离线重加密。

首次生产部署仍需配置 `APP_ENV=production`、`PUBLIC_URL=https://...`、`DATABASE_URL`、随机 `APP_SECRET`、`STORAGE_DRIVER=s3`、`APP_SETUP_MODE=true` 和 bootstrap 管理员。启动后仅管理员能填写缺少的外部配置。使用数据库管理 SMTP/S3 后，后续启动保留 `APP_SETUP_MODE=true`，但已有生效配置照常使用；它不会绕过就绪检查、HTTPS、生产禁止 Demo 或私有存储要求。未发现有效管理员的生产初装直接拒绝启动。`/readyz` 在 SMTP/存储缺失时返回 503，以便部署系统保持未就绪。

存储位置在首次对象操作前持久锁定，并与设置保存串行，避免第一份文件仍在上传时切换桶造成数据丢失；首次失败的上传也可能保守地锁定位置。删除全部素材不解除锁定。切换位置需要离线迁移和运维处理，不能从后台直接跳过。
