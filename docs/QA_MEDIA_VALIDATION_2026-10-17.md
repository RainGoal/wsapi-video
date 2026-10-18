# 媒体载荷、结算与后台配置专项

日期：2026-10-17。范围为本机真实 PostgreSQL、Chromium、私有本地文件与实际 ffprobe，不代表外部模型、商户、S3 或生产反代已通过。

## 修复与验证

- PNG/JPEG/GIF 原来只读尺寸配置，现先限制尺寸和文件大小，再解码首张图像；只有头部的文件拒绝。
- MP4 原来只扫描容器元数据，WebP 原来只解析短头；现在与 WebM/压缩音频一起要求 ffprobe 返回实际解码帧且没有错误诊断。只头 MP4、元数据仍有效但载荷清零的 MP4、截短 WebP 均拒绝。
- ffprobe 使用本地 file 协议、64,000,000 像素、单解码线程、15 秒执行及 1 MiB 输出上限；缺失解码器时拒绝相应格式，不降级为只读头部。修复了 Go MIME 探测下 Ogg/FLAC 分支不可达，并接受对应的常见声明别名。
- 既有归档恢复在长度/SHA 一致后，也重新检查实际媒体并核对记录的类型和尺寸；不修改原对象。只头 PNG 即使数据库中的长度及 SHA 与其一致，也保持 `archive_retry/frozen`，不消费、不取回上游。
- 新归档及既有归档统一核对 `asset.Kind == task.Kind`。错误类型标记 `delivery=error`、`procurement=disputed`、`archive_kind_mismatch`，保留冻结积分；运营重试仍经过检查。
- 真实 PostgreSQL 的 8 组矩阵覆盖新归档/恢复、图片/视频任务与图片/视频实物。修复前四个类型不符场景会变成 ready/consumed；修复后四个均不结算，正确类型仍只结算一次，任务/对象/流水及上游提交次数无重复。上游为本机协议服务器，不是真实供应商。

使用 FFmpeg 9.0.2 生成的原始小样本见 `backend/internal/assets/testdata/README.md`。本机工具来自 [Gyan FFmpeg builds](https://www.gyan.dev/ffmpeg/builds/)，下载 ZIP 的 SHA-256 与发布的校验文件相符：`60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba`。工具 ZIP/可执行文件仅在被忽略的 `.runtime/media-tools/`；CI 增加安装 ffmpeg 后运行实际解码测试。命令依据为 [ffprobe 官方文档](https://ffmpeg.org/ffprobe.html) 及本机 `-h full` 的 `count_frames`、`max_pixels`、`threads`、`protocol_whitelist` 签名。

## 最终构建与浏览器

后端 `studio-release-20261017-r2.exe` SHA-256：`b4d67de88061eb2571daec82af21691265c04b8da2fcb12c33949850d21107bf`。前端 `index-0Bj-SZrT.js`，CSS `index-CqnL0m-j.css`。8090/8091/8092 已更新；本机 `/readyz=200` 仅为 development 检查，8092 的外部服务配置仍 revision 0。

使用既有独立 QA 账号在 8091 新建项目 `prj_bcbe1932b775a6f8dfe8fb6eb90c3640`，真实 UI 文件输入的结果：

| 文件                               | 状态              | 实际结果                 |
| ---------------------------------- | ----------------- | ------------------------ |
| 只有 IHDR 的 PNG                   | 422 invalid_media | 没有素材记录             |
| 保留容器元数据、像素载荷清零的 MP4 | 422 invalid_media | 没有素材记录             |
| 截短至 30 字节的 WebP              | 422 invalid_media | 没有素材记录             |
| 浏览器 Canvas 创建的 PNG           | 201               | 16×12，115 字节          |
| 本机 FFmpeg H.264 MP4              | 201               | 64×36，0.4 秒，1680 字节 |
| 本机 FFmpeg WebP                   | 201               | 64×36，84 字节           |

最终只有三份有效素材。图像均实际解码；视频预览有原生 controls，调用播放至结束后定位 0.2 秒，`media.error=null`。三份鉴权原文件下载均 200，逐份 SHA 与归档记录相同；MP4 Range 返回 206、`bytes 0-63/1680`、64 字节。画布 revision=0/节点0；钱包 0/0，任务3、订单0；wallet/tasks/ledger/orders 完整 JSON 前后相等，pageerror 为空。没有提交生成或支付；完成后正式退出并关闭独立会话。

证据：`.runtime/qa/media-payload-browser-evidence.json`。

## 后台与反代修补

支付启用开关现在展示 `payment.enabled` 字段错误。最终前端在独立管理员页面勾选启用、保留空商户并提交，真实返回 422，直接显示“启用支付前须填写接口、商户 ID 和商户密钥”，`aria-invalid=true`。服务器完整配置前后相同、revision=0、pageerror 为空。390px 页面宽度390，字段错误可见；该检查未保存配置，也未请求外部支付服务。证据为 `.runtime/qa/settings-payment-validation-evidence.json` 及同目录 mobile 截图。

页面与本机联调文档说明 notify/return 必须匹配部署 `PUBLIC_URL` 固定路径；本机地址不能接收真实商户公网回调。后续需求覆盖审查修正了指引：公网验收须按部署手册建立独立的 production 预发布环境，不能仅把当前 development 服务的 `PUBLIC_URL` 改成公网域名。未修改当前实际 URL 或运行模式。

认证限流新增显式 `TRUSTED_PROXY_CIDRS`；仅可信直连 peer 可提供唯一合法 `X-Real-IP`，其余使用直连地址，忽略 XFF。17 个地址场景、8 个配置场景及生产配置检查通过；模板传递配置并要求 Nginx 覆盖真实 IP。这里只证明代码与配置解析，尚未实际部署 Nginx/Docker 或验证 CDN。

## 统一验证及边界

本机 PostgreSQL 随机隔离 schema、真实 ffprobe 下 `go test -race ./... -count=1 -timeout=180s` 全部通过，assets 2.450s、auth 10.469s、commerce 24.905s、projects 7.313s、server 30.087s；`go vet ./...`、Go 构建通过。前端 23/23、全目录格式、TypeScript、Vite build，以及 Compose/CI/smoke 格式和 `git diff --check` 通过。003/004 迁移校验和未变。CI 任务没有在本机执行，Docker 仍不可用。

GIF 目前只验证首帧；WAV 检查容器与数据长度。本轮视频只有 0.4 秒、没有音轨，小样本解码和播放不证明所有供应商编码、动画后续帧、长视频吞吐、浏览器兼容或私有 S3。完整解码增加 CPU 和临时磁盘成本，15 秒预算内可支持的真实视频须在目标硬件验证，E1–E11 的外部及容量门槛继续保留。
