# FRAME STUDIO

独立图片与视频创作平台。采用石墨灰与暖琥珀的数字摄影工作室风格，围绕项目和自由画布组织创作，通过连线表达提示词、参考素材与生成结果的关系。

核心流程：**项目 → 创建并点击图片/视频节点 → 编辑提示词、参考素材与参数 → 当前节点生成与展示 → 再次生成或继续引用/下载。** 再次生成继续更新当前节点，历史任务、受理时输入/价格快照与归档素材均保留，已有作品可作为下一次创作的参考。

电商用户可从侧栏进入独立的「商品替换」工具：上传短视频与商品图，选择性抽帧识图，确认目标并报价，再通过现有 Seedance 生成、对比与下载。无需创建画布项目。管理员须显式启用已验证的 Seedance 商品；识图为同一 Sub2API 下单独配置的可选能力。接入方式与实际限制见 [商品替换说明](docs/PRODUCT_SWAP.md)。

画布默认拖动空白移动，节点可直接点击、拖动；按住 Ctrl 或空格拖动空白框选，Shift / Ctrl / ⌘ 点击多选。底栏可直接添加组，也可用 Ctrl / ⌘ G 打组、Ctrl / ⌘ Shift G 解组；支持组名编辑、成员计数、拖入高亮与吸附、调整组大小和整组移动。外观可切换浅/深色、点阵/线格/空白与图片信息，主题按个人偏好记忆，背景和信息开关随项目保存。视图区提供缩放滑杆、适合画布、小地图导航与操作帮助。

前端 React 19 / TypeScript / Vite；后端 Go 1.24 / PostgreSQL 16。媒体通过唯一的 Sub2API 连接器生成，支付通过易支付接入。账号、零售钱包、项目、素材和充值订单均由本平台独立管理。

代码仓库：[RainGoal/wsapi-video](https://github.com/RainGoal/wsapi-video)，主分支为 `master`。推送到 `master` 后，GitHub Actions 完成代码检查，在 x86 和 ARM 原生 runner 分别构建镜像并执行隔离首装 smoke，再把两份受测镜像合并发布到 `ghcr.io/raingoal/wsapi-video`，提供 `latest` 和 `sha-<完整提交 SHA>` 标签。推送 `vMAJOR.MINOR.PATCH` 版本标签后，执行同样的检查并发布对应版本镜像及 [GitHub Release](https://github.com/RainGoal/wsapi-video/releases)，附带固定镜像摘要的 Compose 部署包与校验和。从 `v0.1.1` 起，镜像原生支持 `linux/amd64` 和 `linux/arm64`，Docker 自动选择宿主机架构。服务器使用根目录 [docker-compose.yml](docker-compose.yml) 或 Release 部署包拉取镜像部署。

核心创作、账号账务和运营后台已有实现及本地验证；真实模型、支付、SMTP、私有云存储和生产运行验收仍需在目标环境完成。具体进度与证据见 [实施状态](docs/IMPLEMENTATION_STATUS.md) 和[上线待验清单](docs/RELEASE_READINESS.md)。

## 本地启动（Windows）

需要 Go 1.24+、Node.js 22 和 PostgreSQL 16。`tools/devdb` 可提供仅监听本机的开发数据库；首次启动会下载 PostgreSQL 二进制，数据保留在 `.runtime/postgres/data`。

1. 在项目根目录创建配置：

```powershell
.\scripts\Setup-Dev.ps1 -Demo
```

`-Demo` 显式开启带水印的本地图像演示。省略则只开放已配置并验证的真实商品。不要用演示数据库承载真实余额。

2. 在一个终端启动数据库并保持运行：

```powershell
Set-Location D:\Code\Other\wsapi-video\tools\devdb
go run .
```

默认端口 55432、用户 studio、数据库 postgres。默认密码仅供本机开发；`DEV_DATABASE_PASSWORD` 可在首次初始化前覆盖。已有集群的密码不会由环境变量自动修改。

3. 在第二个终端启动 API：

```powershell
Set-Location D:\Code\Other\wsapi-video
. .\scripts\Use-Env.ps1
Set-Location .\backend
go run ./cmd/server
```

4. 在第三个终端启动前端：

```powershell
Set-Location D:\Code\Other\wsapi-video\web
npm ci
npm run dev
```

打开 <http://127.0.0.1:5176>。开发环境注册会显示邮件验证链接；验证后登录。新账号余额为零，管理员可通过有审计记录的积分调整为开发账号添加测试积分。演示不连接真实模型，不提供假视频，不创建支付成功记录。

管理员需显式设置 `BOOTSTRAP_ADMIN_EMAIL` 和至少 10 字符的 `BOOTSTRAP_ADMIN_PASSWORD` 后启动后端。仅创建不存在的管理员，不提升已有普通账号、不覆盖密码。首次创建后可移除这两项配置。本次本机管理员保存在被 Git 忽略的 `.runtime/dev-admin.json`，带历史项目与素材的体验账号在 `.runtime/dev-creator.json`。

## 日常开发与验证

```powershell
Set-Location D:\Code\Other\wsapi-video\backend
$env:TEST_DATABASE_URL='postgres://studio:studio-local-development@127.0.0.1:55432/postgres?sslmode=disable'
go test -race ./... -count=1
go vet ./...
Set-Location ..\web
npm test
npm run format:check
npm run build
```

数据库集成测试为每次运行创建并清理随机 schema，需要创建 schema 的权限；未设置 `TEST_DATABASE_URL` 时会明确跳过。CI 使用真实 PostgreSQL 并执行 Go race 检查。浏览器验收记录见 `docs/IMPLEMENTATION_STATUS.md`。

PNG、JPEG、GIF 先检查尺寸上限，再实际解码首张图像；MP4、WebM、WebP、MP3、OGG、FLAC 需要可用 `FFPROBE_PATH` 并检查实际解码帧，只有文件头或损坏的载荷不能作为成品归档。WAV 校验容器和数据长度。容器内已安装 ffprobe；Windows 本机需安装 FFmpeg 并将 `FFPROBE_PATH` 指向 `ffprobe.exe`。每次探测最多 15 秒，上传/归档有大小、尺寸和时长限制；长视频须在目标硬件验证，不以元数据可读代替可播放交付。

## 接入真实服务

外部服务的主要配置入口是管理员登录后的 **运营后台 → 系统设置**，包含站点与注册、生成网关、易支付、邮件服务和私有存储。模型商品、价格、能力与充值档位在各自管理分区配置；用户、任务、订单、积分调整和审计均有独立页面。

首次生产部署先配置以下部署参数，使用全新的生产数据库和对象存储，关闭 `DEMO_MODE`：

```dotenv
APP_ENV=production
APP_SETUP_MODE=true
DEMO_MODE=false
PUBLIC_URL=https://studio.example.com
LISTEN_ADDR=0.0.0.0:8090
DATABASE_URL=<独立生产PostgreSQL连接串>
APP_SECRET=<随机生成且至少32字符的固定主密钥>
STORAGE_DRIVER=s3
BOOTSTRAP_ADMIN_EMAIL=<初装管理员邮箱>
BOOTSTRAP_ADMIN_PASSWORD=<独立且至少10字符的初装密码>
```

`PUBLIC_URL`、数据库、主密钥、监听地址和本地存储目录由部署环境管理，不能从浏览器修改。生产初装必须有有效管理员，否则拒绝启动。管理员创建成功后移除 bootstrap 邮箱和密码；它们不会提升已有普通账户或重置已有密码。

`APP_SETUP_MODE=true` 允许管理员在 SMTP/S3 尚未填写时进入后台初装。缺失必需配置时 `/readyz` 返回 503，公开注册与真实生成受就绪条件限制，初装模式不绕过 HTTPS、生产禁止 Demo 或私有 S3 要求。使用数据库内的运行配置后，后续启动继续保留此变量；不必把后台秘密重新复制回环境变量。

建议按以下顺序设置：

1. 邮件服务：配置 SMTP 主机、端口、用户名、授权码和发件地址，检测连接；再用测试账户完成真实验证邮件与密码重置收件测试。
2. 私有存储：填写 S3/R2 Endpoint、区域、私有 Bucket 与访问凭证。检测通过后实测上传、预览、签名参考地址与下载。首次对象操作后位置锁定，已有素材不可直接切桶。
3. 生成网关：填写固定 HTTPS Sub2API origin、独立图片/视频 Key，以及实际结果下载域名白名单。每行一个确切 HTTPS origin，不包含 `/v1` 路径或通配符。
4. 模型商品：先创建下架商品，填写确切 model ID、零售积分和真实能力。视频支持按次或按请求秒数收费，并为不同分辨率设置覆盖价；图片保持固定单次积分。使用明确预算完成生成与归档验收后标记已验证并上架。一任务生成一张图或一条视频，详见[视频零售定价](docs/OPERATIONS.md#视频零售定价)。
5. 易支付：填写服务商提供的 HTTPS 网关基础地址（支持并保留 `/epay/` 等路径前缀，无需追加 `submit.php` 或查询参数）、PID、签名密钥及公网 notify/return URL。完成真实商户回调、主动查单及唯一到账验收后，再上架充值档位。支付返回页面只查单，不凭前端参数入账。

后台只返回凭证的“已配置”状态。密码框留空保留原值，填写新值则替换，清除必须勾选明确操作。媒体和支付配置变更会保存加密的凭证版本，历史任务与订单继续按原引用处理。运行配置保存后实时生效并记录审计；首次后台保存前可从环境变量导入初始值，已有数据库配置不会因修改旧环境变量而回退。

**`APP_SECRET` 与数据库密文必须配对保管和恢复。** 数据库中包含当前配置与历史凭证版本；更换主密钥需要离线重加密，不能通过重新生成密钥来修复启动问题。

“检测连接”不会生成、支付或发送邮件：网关只读查询模型，易支付只检测 HTTPS 可达，SMTP 检测 TLS/认证，S3 只读检测 Bucket。通过检测不代表真实模型能力、商户密钥/到账或对象写权限已验收。

Sub2API 契约与对接注意事项见 `docs/API_CONTRACT.md`；详细发布及异常处理见 `docs/OPERATIONS.md`。真实商户、SMTP、S3 与付费媒体需要在你自己的服务环境完成验收。

## 构建与部署

`npm run build` 后，Go 服务可直接托管 `web/dist`，此时 `PUBLIC_URL` 应设为统一入口，例如 `http://127.0.0.1:8090`。反向代理需保留 Origin、Cookie 和 Range，提供 HTTPS，并允许上传大小和长媒体下载。

服务器默认使用根目录 `docker-compose.yml`，拉取 GitHub Actions 发布的镜像；`deploy/compose.yaml` 保留可选的源码构建入口。两者共用 `deploy/.env.production.example`，生产配置不读取本地开发 `.env`。先复制模板，填写自己的 HTTPS `PUBLIC_URL`、分别生成的数据库密码和固定主密钥，以及初装管理员：

```powershell
Copy-Item -LiteralPath .\deploy\.env.production.example -Destination .\deploy\.env.production
# Edit deploy/.env.production before continuing; never commit this file.
docker compose --env-file deploy/.env.production config --quiet
docker compose --env-file deploy/.env.production pull
docker compose --env-file deploy/.env.production up -d
```

Linux 服务器可用 `cp deploy/.env.production.example deploy/.env.production` 复制模板。Compose 强制生产模式、关闭 Demo、启用管理员初装，并将数据库连接指向容器内的 PostgreSQL；网关、商户、SMTP 和 S3 凭证可以随后在后台配置。

模板要求数据库密码使用随机 64 位十六进制，避免插入数据库 URL 时出现保留字符；`APP_SECRET` 单独生成、保持固定并随数据库密文配对备份。应用仅发布到宿主机 `127.0.0.1:8090`，由现有 HTTPS 反向代理对外。首次 GHCR 包如需认证，按部署说明登录或设置包可见性。固定版本、升级命令、自动构建方式、反向代理和恢复边界见 [生产部署说明](docs/DEPLOYMENT.md)。

应用启动自动执行带校验和的事务迁移。已应用 SQL 禁止修改。健康检查使用 `/healthz`；SMTP/S3 未配置时仍可让管理员进入初装，此时 `/readyz` 应返回 503。CI 的通过状态以仓库 Actions 实际运行结果为准，容器 smoke 也不代替真实服务及生产验收。

## 目录与边界

- `web/`：账户、项目、自由画布、素材、任务、钱包和运营后台。
- `backend/`：身份、项目/素材、报价/账务、媒体 Worker、支付与迁移。
- `docs/`：计划、接口、实施状态和运行说明。
- `tools/devdb/`：可选本机开发数据库工具，不进入发布镜像。

不包含团队成员、实时协作、整图自动执行、剪辑时间线、会员订阅或对外售卖 API。画布操作只改变布局与引用，不自动生成、扣费、取消或退款。限流、队列唤醒与租约均使用 PostgreSQL，当前运行无需 Redis。

手动分镜、镜头排序和选定成品导出不属于当前产品范围。历史后端 shots 数据、兼容接口及已应用迁移保留，不作为用户功能宣传；旧验收记录保留其实际版本和边界。

画布交互参考并提取自 [infinite-canvas 固定提交 `3a6b64b602ad062b29fac66902ed18a7ef5fd9dc`](https://github.com/basketikun/infinite-canvas/tree/3a6b64b602ad062b29fac66902ed18a7ef5fd9dc)（MIT）；许可证与来源见 `web/THIRD_PARTY_NOTICES.md` 和应用账户页。
