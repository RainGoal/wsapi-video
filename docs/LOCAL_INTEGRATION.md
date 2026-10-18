# 本机真实服务联调

当前本机入口：<http://127.0.0.1:8092/#/admin/settings>。

此入口使用独立 `frame_integration` 数据库，`APP_ENV=development`、`DEMO_MODE=false`，没有导入演示钱包、素材或订单。它仅用于本地配置与受控开发验证，不代表已经完成 HTTPS 生产部署；不要将此开发服务直接开放到公网。

## 登录与配置

管理员登录信息在被 Git 忽略的 `.runtime/dev-integration.json`；基础环境在 `.runtime/.env.integration`。不要将这些文件或服务密钥提交到仓库、贴到聊天记录中。

在系统设置依次配置生成网关、易支付、邮件服务和私有存储，每次保存填写操作原因。密钥留空表示保留原值；清除需明确勾选。保存后填写的明文不会回传到页面。接口约定见 [后台配置契约](ADMIN_SETTINGS_CONTRACT.md)。

易支付通知与返回地址必须严格匹配部署环境 `PUBLIC_URL`：分别为 `PUBLIC_URL/api/v1/payment/notify/easypay` 和 `PUBLIC_URL/wallet`。公开域名不能在后台修改。本机 `localhost/127.0.0.1` 地址无法接收支付方的公网通知。

真实支付、邮件和公网验收应按 [生产部署说明](DEPLOYMENT.md) 建立独立的 production 预发布环境：`APP_ENV=production`、`DEMO_MODE=false`、`APP_SETUP_MODE=true`，配置 HTTPS、固定主密钥、独立数据库与私有 S3。将该环境的 `PUBLIC_URL` 设为公网 HTTPS origin（不含路径），从该地址登录新版后台，保存真实服务配置及匹配的通知、返回地址。本地配置不会自动复制到新环境，须在目标后台重新填写；不要通过导入开发或 Demo 数据库建立预发布资金库。

仅把本机 `.runtime/.env.integration` 的 `PUBLIC_URL` 改成公网域名不会切换运行模式。development 会返回开发用验证/重置链接，Cookie 等安全行为也与 production 不同，不能用于公网身份或邮件验收。仅修改后台回调地址会因与部署地址不匹配而被拒绝。

联调环境初始存储为未配置的 S3。先设置私有桶并检查连接，再上传或生成；首次对象操作会锁定存储位置，以后可轮换同一位置的凭证。不要用演示库测试过的本地存储替代这个桶。

只读连接检测不代表模型生成、邮件送达、S3 读写和支付回调已经通过。商品与充值档位需要分别配置；真实付费生成和支付按明确的测试预算进行，测试结果需记入 [上线验收清单](RELEASE_READINESS.md)。

## 本地开发服务重启

本机 PostgreSQL 在 `127.0.0.1:55432`。若数据库已停止，先按 README 的本地数据库步骤启动，保留现有 `.runtime/postgres/data`。

停止原联调服务后，在 PowerShell 执行：

```powershell
& 'D:\Code\Other\wsapi-video\scripts\Start-Integration.ps1'
```

脚本读取已有环境文件并构建、启动服务，不生成新密钥或重建数据库。修改前端后先在 `web` 目录执行 `npm run build`，再刷新浏览器。

后台配置保存在数据库密文中；重启不会清空。备份时必须配对保留数据库、`APP_SECRET` 和私有对象。丢失主密钥无法解密历史服务凭证，不能通过重新生成主密钥修复。完整说明见 [运行手册](OPERATIONS.md)。
