# 后台操作、任务与分镜恢复验收（r8）

日期：2026-10-17。此记录覆盖当前源码和 `index-d1ue1syl.js`；后端 r8 已构建但未运行，浏览器使用既有 r3 / 8091。真实模型、商户、SMTP、私有 S3、Docker/HTTPS、完整恢复及目标设备容量仍未验收，不能据此批准上线。

## 本轮修复

- 商品/档位保存期间统一禁用关闭按钮、Escape 和遮罩关闭；完成回执按原编辑会话处理，失败保留输入。
- 设置页编辑或重载后不再显示旧连接成功结果。菜单、Logo、原生 hash、浏览器前进后退统一检查未保存配置；拒绝时以历史遍历回原位置，保留前进/后退记录。支付参数和验证令牌的 replaceState 清理保留历史标记。
- 人工调账在发送前按管理员 ID 保存并读回原目标、方向、数量、原因和 request_key；恢复后锁定原请求。存储不可用、损坏或已有不同请求均阻止新 POST。退出创作草稿清理不删除调账恢复记录。
- 调账仅首次收到 `422 invalid_input/invalid_json` 可清理为拒绝；已有未知结果的重试遇到任何 4xx 均保留。`200/code=ok` 还须有有效 Wallet。已确认结果但本地清理失败时，本页仅重试清理。localStorage 不是跨窗口原子锁；保留同键避免同一恢复请求重复入账。
- 任务详情按路由目标门控加载/错误/重试，切换时隐藏上一任务事实和操作；旧 GET 与取消回执均校验目标和更新时间，保留独立轮询。
- 分镜云端读取成功并完成本地草稿取舍前，禁止编辑、保存、导出和自动候选回填。恢复后追加新候选且保留原选定；显式使用云端版本恢复编辑。
- 已完成但交付 error/delivery_failed 且已核查的画布节点显示“交付异常”和信息图标。
- 新增标准库 slog JSON 日志：固定事件/代码、本地任务或订单 ID 及尝试次数。事务提交/有效租约更新确认后才报告成功，不写原始错误、URL、密钥、提示词、个人信息或人工核对依据。管理员人工操作仍由数据库审计记录。

## 自动化与产物

- 前端统一 `npm test` 57/57；全目录 Prettier、TypeScript、许可生成、Vite 构建通过。
- 全后端 `go test -race ./... -count=1 -timeout=180s` 一次通过，44.991 秒；使用真实 PostgreSQL 随机隔离 schema 和可用 ffprobe 9.0.2。commerce 29.063 秒、server 35.808 秒、observability 1.306 秒；vet、server/monitor 构建通过。新增日志六个定向用例另有真实 PG/race 验证。
- 前端 JS 467639 字节，SHA-256 `24d0a3dfcc5b6b471c589a17630b927930705e152336a9c205dceda03cd34ed4`；CSS `index-D3-xb9uJ.css` 99316 字节，`cd39e1ff59a055c98c1613863cb1b431d074a08ed4500a8686624080ea76ff0c`。
- `studio-release-20261017-r8.exe`：20301824 字节，SHA-256 `46a168e0e0ebe58f6c218a4d8da33d9ce6a85701dc717f06fbed5c664ccdaed2`。
- `monitor-release-20261017-r8.exe`：13974528 字节，SHA-256 `59d3897fceec02353ddcd5ccb9716edb57a07907f7956f2ccf7a389de97a6fe3`。
- 独立核对两个程序实际模块、19 模块并集、26 份声明及 Go LICENSE，源字节/复制/manifest 一致；003/004/005 冻结校验和未变。许可 manifest SHA-256 `badbe5e505f296a53861673f93bb8f572d8468d2749c4cd1c4137b35c623583d`。

机器记录位于 `.runtime/qa/backend-r8-validation.json`、`backend-r8-race-result.json`、`backend-r8-release-verification.json`；产物核对不等于运行或部署验收。

## 浏览器实际范围

持久证据为 `.runtime/qa/r8-recovery-browser-evidence-20261017.json`，74/74 个唯一检查，全部验收会话已退出并关闭；pageerror=0，未接管的业务写尝试=0。此计数包含界面行为、限定响应哈希与退出检查，不是 74 条端到端商业流程。

| 范围 | 已验证事实与边界 |
| --- | --- |
| 保存弹窗 | 旧 `index-Cf6_J2QA.js` 在延迟 PUT 时 Escape 可关闭，已受控复现。新商品及档位均验证 X/Escape/遮罩保护、503 保留草稿并解锁、成功结束当前编辑。PUT 全由夹具接管。 |
| 设置/导航 | 成功检测后编辑隐藏旧结果；新 revision 重载清结果；503 隐藏旧配置、重试恢复。菜单/Logo/hash/Back/Forward 取消保留 URL/输入且单次确认；允许 Forward 和 Logo 后实际目标页面出现。 |
| 概览 | 延迟刷新和 503 均隐藏旧指标/绿灯；仅配置读取失败时保留已取得的指标、隐藏旧 readiness，重试恢复。指标与绿灯来自明确 GET 夹具。 |
| 调账恢复 | 503 后导航、刷新、真实退出重登保留相同 payload/key；429、409、无效 200 均不清 key；另管理员身份夹具不能恢复该记录；成功后清理失败只重试本地清理；存储 setItem 故障零 POST。全部调账 POST 为夹具，无真实入账。 |
| 任务详情 | A→B 延迟/503 时无 A 详情，B 重试恢复，迟到 A 不覆盖 B。进行中任务夹具详情轮询间隔 4017ms；轮询取得 completed 后延迟取消 202 不倒退。取消 POST 由夹具接管。 |
| 分镜 | 云端 GET 延迟时添加禁用、原草稿字节不变；任务读取晚到且待决时不回填覆盖；恢复后标题/选定保留、候选补齐；保存载荷保持这些字段。明确选云端恢复原标题和编辑。保存 PUT 由夹具接管。 |
| 画布/支付 | 明确任务 GET 夹具验证交付异常中文、英文和信息图标。本人回跳订单夹具只自动查单一次，清理 out_trade_no 后历史返回不复活参数或重复查单；查单 POST 为夹具。 |
| 双语/布局 | 390px 调账恢复英文和分镜恢复无整页横向溢出；八张静态截图关闭动画后保存并目视。仅模拟视口，不等于真实手机验收。 |

浏览器共接管 16 个业务写请求（POST 9、PUT 7），含一次旧版复现和一次中途重载页面的延迟商品请求；不代表这些请求都返回成功。测试期间曾修正遮罩坐标、包含时间戳的文本选择器、实际状态文案，以及 REPL 跨单元原始值捕获；导航允许检查最终以目标 React 页面判定，未仅依赖临时 URL。没有内核重置或证据数组丢失。完整计数、修正及请求清单见 JSON。

前后各一次读取下列十份完整响应并比较 SHA-256：wallet；ledger/tasks/orders/projects/assets 各 limit=100；指定项目 canvas/shots；指定任务 A/B 详情。十份均一致，另实际管理员 settings 规范 JSON 哈希一致。它们只证明这些样本响应一致，不代表全历史、全数据库或期间持续不变；登录/退出会话确有变更。任务详情与项目的具体 ID 见机器记录。

截图：`r8-settings-read-failure.png`、`r8-task-target-error.png`、`r8-shots-pending-draft.png`、`r8-shots-restored-mobile.png`、`r8-canvas-delivery-error.png`、`r8-adjustment-recovered-desktop.png`、`r8-adjustment-recovered-mobile-en.png`、`r8-adjustment-recovered-mobile-en-detail.png`，均位于 `.runtime/qa/`。

## 运行与外部门槛

2026-10-17T14:00:46Z 只读核验 8090/8091/8092 仍为 r3，PID 分别 80640/79628/44704。此前服务更新和隔离恢复应用启动被自动审批拒绝，仅返回 `blocked by policy`；本轮未重试或换工具绕过，r8 未运行、005 未应用常驻服务。

2026-10-17T14:00:53Z 实际登录 8092 读取：revision=0、demo=false，media/payment/smtp/storage 均 false，读取后 logout=200。用户将通过新版后台填写配置。公网 HTTPS 指新平台访问网址，例如 `https://studio.example.com`；Sub2API 接口另配。独立 production 预发布、正式模型/预算、真实外部服务、外部告警和发布门槛 E1–E11 继续待验收。日志当前只有源码/测试/构建证据，没有运行日志采集或告警送达证据。
