# 素材来源、引用位置与发布许可验收

日期：2026-10-17。依据原始 PRD 的素材来源、引用位置及删除说明，以及开发计划 M6 的发布包开源清单要求。本文区分真实数据库/HTTP 测试、实际运行页面和明确响应夹具，不代表生产上线验收。

## 素材实现与接口

素材卡片和预览根据真实 Asset.task_id 区分上传文件与生成结果，生成结果可打开来源任务。新增只读 `GET /api/v1/assets/{id}/references`，七字段响应、25 条分页、同一 SQL 快照的权限/总数/分页和稳定排序见 [接口契约](ASSET_REFERENCES_CONTRACT.md)。画布/封面的位置 ID 为项目 ID，分镜为镜头 ID，任务为历史输入或生成来源；没有虚构节点级定位或自动解绑。

页面按需展开，折叠不请求，支持分页、刷新、失败重试和迟到响应隔离；错误不会显示为空引用。归档项目提供恢复提示，软删除项目禁用项目跳转，历史任务仍可查看。同项目视图 query 切换保留画布状态和未保存分镜，镜头链接只滚动/聚焦；真正删除仍由服务端事务守卫决定。

真实 PostgreSQL 隔离 schema 测试覆盖 36 引用的 25+11 分页、四种位置、跨项目/归档/软删除、跨账号恶意关联、401/404/422、字段白名单，以及读取前后业务与会话指纹不变。组合 HTTP 测试验证实际 Demo 归档结果被画布、镜头和来源任务引用，匿名拒绝、其他账号 404，钱包/画布/分镜完整 JSON 不变；定向 race 通过（server 8.455 秒）。

## 浏览器证据与限制

受测前端为 `index-D1ElAnE3.js` / `index-Dj75XTl9.css`。运行服务仍为 r3，真实引用接口返回 404，页面如实显示“接口不存在”；新接口未在这些常驻服务上联调成功。

独立 Chromium 上下文共 **26/26** 项限定检查通过：

- 真实五个素材的来源标签、生成来源任务深链、折叠零请求、旧后端 404 错误提示。
- 明确响应夹具的四类引用、26 个位置的 25+1 分页、归档/软删除/历史任务、空结果；503 重试、短页和重复位置拒绝，刷新清除旧行，关闭后迟到响应不污染另一素材。
- 中英文保留用户项目/镜头名称，390px 与长标题无页面/引用行横向溢出；桌面和移动端截图已目视检查。
- 夹具链接进入真实项目画布、素材及具体镜头；镜头取得焦点。编辑标题但不保存，经素材→引用画布→分镜切换后仍保留；同项目 query 导航未重新读取画布或分镜。
- 实际钱包、完整流水/任务/订单、画布、分镜六个响应前后相等；交互阶段没有业务写请求，pageerror 为空，最后退出独立会话为 200。
- 关于区许可链接存在，实际 GET `/third-party-notices.txt` 返回 200、5340 字节，与源文件逐字节相同。

证据在被忽略的 `.runtime/qa/asset-references-browser-evidence.json`，截图为 `asset-references-desktop.png`、`asset-references-mobile.png`、`asset-references-mobile-long-title.png`。夹具引用不代表这些关系已写入运行数据库；标题更改仅在独立浏览器本地草稿中，未保存到云端。

## 发布许可与最终产物

前端构建前生成完整许可清单，按锁文件核对 React 19.2.5、React DOM 19.2.5、scheduler 0.27.0 的实际安装版本，附固定画布许可；缺失/空许可或版本不符会失败，关于区及原有作者署名均保留。前端测试 **38/38**、格式、TypeScript 与 Vite 构建通过。

Go collector 读取 server/monitor 的实际二进制构建信息，再核对模块锁定版本/校验和/源码。递归保留许可相对路径，包含实际发行依赖 AWS SDK/Smithy 的两份嵌套 singleflight BSD LICENSE；模块根许可不能替代它们。缺根许可、空/不可读文档、符号链接、replacement 或工具链不符均拒绝；源码/配置文件不会作为许可误收。Dockerfile 加入 `/app/licenses`，CI 核对前端清单字节，容器 smoke 增加随包二进制与许可 SHA 验证。Docker 本身未执行。

统一后端验证使用真实本机 PostgreSQL 和 ffprobe：`go test -race ./... -count=1 -timeout=180s` 全过，墙钟45.99秒，assets 3.385秒、commerce 24.017秒、server 32.605秒；`go vet ./...` 通过。此轮全量测试之后，collector 为嵌套许可做最后修改，最终另有定向 **4 PASS、1 SKIP** 和 vet 通过；SKIP 是 Windows 不允许创建 symlink，不能称该主机场景已实测。业务源码及下列二进制未受 collector 最后修改影响，无需以旧全量结果冒称新 collector 已跑全量 race。

| 已构建、未运行的产物 | SHA-256 |
| --- | --- |
| studio-release-20261017-r5.exe | 283a51cc7b7f0f612ad8dee27cef858fd652aef5a953b93344e900fe86cd87e1 |
| monitor-release-20261017-r5.exe | 59f9bf34d28cfd25140a6272b304523e53613cf1354a9279f43b42be22102417 |

Windows 最终许可目录为 `.runtime/licenses-windows-r5-complete-20261017`，manifest SHA-256 为 `fffcfd39c3970b9852a04667b2a12609665f8c0ce5b790d30f57f3d62a1c587d`。另按 Docker 的 Linux amd64 / CGO_ENABLED=0 / trimpath / stripped 参数交叉编译并只读取产物，最终目录 `.runtime/licenses-linux-complete-20261017`，manifest SHA-256 为 `47770fad05cb0fcd2f27c1b7da2c7f17c7756c736eb586c8d0858f3678b56b70`。

两种平台的最终清单各覆盖两份实际二进制的模块并集：19 模块、26 份模块许可文件及 Go LICENSE；独立核对 build info、模块 version/sum、源文本/复制件完整字节和 manifest SHA 均一致。较早24文件及中间结果保留为历史，不能作为最终许可包。Linux交叉构建没有执行目标二进制，也不证明最终容器文件、容器启动或HTTPS发布已通过。003/004迁移校验和保持冻结值。

## 配置与运行边界

2026-10-17T12:03:29Z 只读复核：非 Demo 的 8092 管理员登录/设置读取/退出均为 200，配置 revision=0，media/payment/smtp/storage readiness 均为 false；新 references/signals 接口均为 404。证据 `.runtime/qa/integration-config-final-20261017.json` 只记录脱敏状态，不含秘密。

此前恢复应用启动和 r3→r4 服务更新均被自动审批拒绝，仅返回 `blocked by policy`，未给具体原因，未绕过重试。当前 r5 构建也不代表运行服务已更新。真实模型、易支付、SMTP、私有 S3、Docker/HTTPS、完整应用恢复及真实设备大媒体验收继续保留于 [上线审查](RELEASE_READINESS.md)。
