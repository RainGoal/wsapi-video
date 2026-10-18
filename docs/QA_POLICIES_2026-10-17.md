# 运营政策配置与公开入口验收

日期：2026-10-17。依据原始 PRD 第 52、122 行，补齐保留期限、容量、清理规则、提交未知处理期限和退款政策的配置与公开展示能力。默认不设置任何运营承诺，不新增自动配额、清理或退款。

## 版本与改动

- 后端 `studio-release-20261017-r3.exe`，SHA-256 `e69440ff6c0d4adfa6c1d29f0daf780e78d988153100b19095867723651a5b87`。
- 前端 `index-D9X0QoYN.js`，CSS `index-CUlRdqk7.css`。
- 后台站点设置增加五项纯文本政策，每项最多 2000 个 Unicode 码点，保存时 TrimSpace；沿用配置 revision、管理员权限、CSRF 和字段名审计。
- 匿名 `/api/v1/system` 仅显式输出允许公开的字段。登录页底部及登录后侧栏均可打开政策；空项显示待运营公布，每次打开重新读取，读取失败时不展示旧政策，可重试。
- 公网联调指引改为独立 production 预发布环境，明确仅修改 development 的 PUBLIC_URL 不会启用生产安全行为。当前服务运行模式与地址未改。

## 数据库与构建

真实 PostgreSQL 使用随机隔离 schema，不修改运营配置，不调用外部供应商：

`go test -race ./internal/settings ./internal/server -count=1`

settings 1.594 秒、server 29.999 秒，含编译墙钟 36.38 秒，通过。新增测试覆盖旧密文缺字段、默认空、首尾空白、2000 个含非 BMP 字符的码点边界、五项逐字段 422、失败/冲突不推进 revision 或审计、成功审计只列字段名、空白清空、匿名白名单及秘密不泄露。

`go vet ./...`、Go 构建通过。前端 26/26、全目录格式、TypeScript 和生产构建通过。003/004 迁移校验和保持原值。上轮全后端 race 的媒体/账务结果仍属 r2 记录，本轮只对修改的 settings/server 包完整重跑 race，不冒称 r3 全量重跑。

## 浏览器实际操作

使用本机 Chromium 与非 Demo 的 development 8092：

- 实际匿名 `/system` 的五项政策和支持邮箱为空；登录前、独立管理员登录后均可查看，显示六处待运营公布。
- 后台五项输入可用；2001 个电影表情在客户端拒绝，尚无配置 PUT。2000 个表情可通过客户端边界校验。
- 为验证服务端字段提示，将唯一一次浏览器配置 PUT 的退款字段在传输阶段改为 2001 字，真实服务返回 `422 invalid_settings`，字段 `site.refund_policy` 显示“政策说明最多 2000 字”。完整配置前后相同、revision=0，没有保存测试政策或覆盖用户配置。
- 手机视口 390×844：后台页面宽 390；公开弹窗宽 362、高 816，clientWidth/scrollWidth 同为 360；2000 个无断点英文字符未造成横向溢出。桌面视口 1440，弹窗宽 850、clientWidth/scrollWidth 同为 848。稳定截图已目视检查。
- 使用明确的 `/system` 展示响应夹具验证：文字含 img/script 标记仍按原文展示，弹窗中实际标签数为 0；换行为 pre-wrap；重新打开取得新版本；503 隐藏旧内容，点击“重新加载”恢复；旧响应省略五字段时正常显示待公布；英文标签切换不翻译运营填写的原文；Esc 关闭后焦点回到入口。
- `pageerror=[]`；没有生成、支付、发信、S3 写入或成功配置保存。独立管理员会话最后退出 200，测试浏览器和上下文关闭。

完整成功保存和审计的证据来自隔离 PostgreSQL HTTP 测试；填充内容、503 与旧字段展示来自浏览器响应夹具，不冒称已在 8092 发布真实政策。

浏览器初次因重试按钮名称及跨调用夹具状态处理而等待超时；依据实际 DOM 修正测试后通过，未据此修改产品代码。首次动画中截图已由禁用动画后的稳定截图替换。

证据为被忽略的 `.runtime/qa/policy-browser-evidence.json`、`policies-public-desktop.png`、`policies-public-mobile.png`、`policies-public-mobile-english.png`、`policies-admin-validation-mobile.png`。

## 发布边界

三个本机 API 已更新至 r3，`/readyz=200` 仍仅代表 development 检查。8092 仍 revision=0，媒体、支付、SMTP 和私有存储未配置，政策未公布。入口实现不等于容量、清理或退款承诺已经确定或执行；真实服务、商户、production/HTTPS、Docker、完整恢复与目标设备容量门槛继续保留，正式上线未通过。
