# r14 发布候选与上线交付核查

日期：2026-10-18。本轮延续自由画布图片/视频创作范围，检查发布交付和实际可达用户路径，不增加产品功能。应用源码沿用 r13；只修复 CI smoke 日志契约及运维文档。

## 1. 修复与限定检查

`deploy/smoke.mjs` 曾要求缺管理员启动日志含 `production setup requires an active administrator`，而当前 `cmd/server` 只输出固定脱敏 JSON 事件，因此该断言必然不匹配。现解析每行 JSON，要求 `msg=platform_event`、`event=server_lifecycle`、`code=run_failed`；非零退出、随后同库 bootstrap 成功及移除 bootstrap 后重启登录的控制保持。没有恢复原始错误输出。

Node 语法与 Prettier 通过；3 项既有日志测试通过，0 fail/skip。直接执行当前断言块的 5 次无服务检查通过：固定事件和 CRLF 多事件接受；非失败 code、错误 event、仅旧原始文本拒绝。这些断言输入由夹具提供，没有执行 Docker 或完整 smoke。证据：[smoke 验证记录](../.runtime/qa/r14-smoke-validation.json)。

运维文档同步修正：监控中的 r3 改为有时间的历史观察；恢复核查使用画布节点/连线和独立图片/视频原文件，历史 shots 数据继续保留。

## 2. 匹配的构建候选

构建前后核对 r13 基线的 101 个后端源码/模块/迁移文件，bytes/SHA 全匹配；没有修改应用源码。当前工具链为 Go 1.24.2，`CGO_ENABLED=0`、`-trimpath -ldflags='-s -w'`，构建期间禁用模块与工具链下载。r13 旧测试记录没有记录工具链版本，本轮不反推它的 Go 版本。

| 目标          | 应用服务字节 | 监控程序字节 |  ZIP 字节 |
| ------------- | -----------: | -----------: | --------: |
| Windows amd64 |   14,223,872 |    9,678,848 | 8,824,613 |
| Linux amd64   |   13,836,472 |    9,384,120 | 8,577,460 |

两个 ZIP 分别包含 38 文件：服务与监控二进制、29 个后端许可文件、5 个配套 r13 前端/前端许可文件、构建说明和逐文件 manifest。每平台后端许可覆盖实际二进制中的 19 个依赖模块及 27 份声明，原始完整许可字节已核对。前端 5 文件与 r13 manifest 的 bytes/SHA 一致，包含 `index-BhkoTeSY.js` / `index-DFaUkSIm.css`。

已从每个 ZIP 实际读取全部文件的解压流，逐一验证名称、数量、字节数、SHA 与候选清单一致，拒绝额外、重复或缺失文件。打包清单只含二进制、静态前端、许可及构建说明，不包含环境配置、凭证、业务媒体或数据库。

- [Windows 候选 ZIP](../.runtime/releases/r13-20261018/frame-studio-r13-20261018-windows-amd64-candidate.zip)
- [Linux 候选 ZIP](../.runtime/releases/r13-20261018/frame-studio-r13-20261018-linux-amd64-candidate.zip)
- [构建与许可记录](../.runtime/qa/r14-build-validation.json)
- [ZIP 内容核对](../.runtime/qa/r14-candidate-content-validation.json)
- [发布候选说明](../.runtime/releases/r13-20261018/BUILD-CANDIDATE.md)

这是未经应用运行验收的构建候选。Linux 为交叉构建；server/monitor 在 Windows 和 Linux 均未执行。原生运行另需 PostgreSQL、ffprobe、生产环境变量及 HTTPS；ZIP 不包含这些依赖，也不是 Docker 镜像。正式部署基线保持 [容器部署说明](DEPLOYMENT.md)。

## 3. 需求核对与剩余验收

只读源码核对确认自由画布图片/视频入口、笔记与素材连线、显式报价生成、独立结果、云保存/草稿/409 副本、素材预览下载、任务快照轮询、钱包订单和后台配置入口均可达。未发现可定位的新首发功能缺口；源码存在不等同于生产环境可用。

[E1–E11](RELEASE_READINESS.md) 均保持待完成：真实模型/参考编辑、私有 S3、商户/SMTP 送达与回调；实际双会话冲突、目标设备容量；网关/Worker 故障；镜像/HTTPS 首装升级、恢复及保留卷回滚。E5/E11 部分内容可用本地应用验证，但当前启动限制下不能继续运行验收。本机没有 Docker，候选交叉构建和日志断言不能替代容器运行。

本轮没有覆盖 `web/dist`、启动或重启应用、向应用数据库应用迁移、部署或真实外部模型/支付调用。常驻服务最后确认于 `2026-10-17T14:00:46Z` 为 r3，之后未复核。此前应用更新及隔离恢复启动被自动审批拒绝，仅返回 `blocked by policy`，无具体理由；没有重试或绕过。目标仍为完整上线产品，未标记完成。

本轮完整索引为 [r14 manifest](../.runtime/qa/r14-validation-20261018.json)。r13 与更早 manifest 保留当时文档快照；后续文档更新不回写历史校验清单。
