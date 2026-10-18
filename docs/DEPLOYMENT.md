# 生产容器部署

默认使用根目录 `docker-compose.yml` 拉取 `ghcr.io/raingoal/wsapi-video` 镜像；`deploy/compose.yaml` 保留从源码构建的可选入口，两者共用 `deploy/.env.production.example`。它们提供单主机 PostgreSQL 与应用的配置，HTTPS 由宿主机已有反向代理提供，媒体放入独立私有 S3/R2。仓库主分支为 `master`，镜像发布方式见第 6 节；发布镜像不等同于已通过真实服务验收。

## 1. 独立环境与固定秘密

准备支持 Compose v2 的 Docker 主机、正式域名与证书、独立私有 Bucket 和 SMTP。使用独立生产数据库/卷，不能复用 Demo 库。生产部署配置单独保存在 `deploy/.env.production`，不读取开发用根目录 `.env`，真实配置文件已被 Git 忽略；Docker 构建上下文也排除各级 `.env*` 和 `.runtime`，不发送本机秘密、媒体或临时恢复工具。

在仓库根目录复制 `deploy/.env.production.example` 为 `deploy/.env.production`。例如 Windows PowerShell：

```powershell
Copy-Item -LiteralPath .\deploy\.env.production.example -Destination .\deploy\.env.production
```

Linux 服务器可以从指定主分支取得代码和模板：

```sh
git clone --branch master https://github.com/RainGoal/wsapi-video.git
cd wsapi-video
cp deploy/.env.production.example deploy/.env.production
```

填写以下字段：

| 字段                                                 | 要求                                                                                                                        |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `COMPOSE_PROJECT_NAME`                               | 使用稳定且独立的生产项目名，默认 `frame-studio-production`；名称变化会指向另一套卷，不能用改名代替升级                      |
| `FRAME_IMAGE`                                        | 使用可追溯的版本标签；发布时保存镜像 ID/digest，不把可变标签当不可变发布产物                                                |
| `PUBLIC_URL`                                         | 浏览器实际使用的唯一 HTTPS origin，无路径、查询或 fragment；与反向代理域名完全一致                                          |
| `TRUSTED_PROXY_CIDRS`                                | 逗号分隔的可信直连代理 CIDR；默认空、不信任转发头。只填写应用实际看到的代理来源，优先精确 `/32` 或 `/128`，详见反向代理说明 |
| `POSTGRES_PASSWORD`                                  | 独立随机 32 字节的十六进制表示，即 64 个 hex 字符；Compose 会把它插入内部数据库 URL，不能直接使用含 URL 保留字符的密码      |
| `APP_SECRET`                                         | 另行生成的至少 32 字符固定主密钥，建议也是随机 64 位 hex；配对备份数据库密文，不随部署重新生成                              |
| `BOOTSTRAP_ADMIN_EMAIL` / `BOOTSTRAP_ADMIN_PASSWORD` | 首装管理员邮箱和独立密码，密码至少 10 字符；只创建不存在的管理员，不提升已有普通用户或重置密码                              |

在有 Node.js 的可信终端中，每次运行下式生成一个独立随机值；分别用于数据库密码、主密钥和可选的初装密码，不重复使用：

```powershell
node -p "require('node:crypto').randomBytes(32).toString('hex')"
```

Compose 固定 `APP_ENV=production`、`DEMO_MODE=false`、`APP_SETUP_MODE=true`、`STORAGE_DRIVER=s3`，不会因开发 `.env` 改为 Demo 或本地存储。数据库 URL 固定为容器网络内的 `database:5432/studio`，不是容器自己的 `127.0.0.1`。该模板仅对同主机私有 Docker 网络内的 PostgreSQL 关闭数据库 TLS；外置数据库需要另行审查连接/TLS 配置，不能原样跨不可信网络使用。

SMTP/S3 字段可以留空，通过初装后台保存为数据库加密配置；也可在这个独立配置文件中提供首次导入值。生成网关和易支付从后台配置。密码若含 `$`、`#` 或空格，按 Compose dotenv 规则使用合适的引号；优先在后台填写外部秘密。不要运行会打印完整配置的普通 `docker compose config` 并分享输出，使用 `config --quiet`。

## 2. 初装启动与健康状态

填完部署字段后，在仓库根目录执行：

```powershell
docker compose --env-file deploy/.env.production config --quiet
docker compose --env-file deploy/.env.production pull
docker compose --env-file deploy/.env.production up -d
docker compose --env-file deploy/.env.production ps
```

第一次拉取前确认第 6 节的 GHCR 访问权限。需要在服务器自行构建时，改用 `docker compose --env-file deploy/.env.production -f deploy/compose.yaml up --build -d`；这是可选方式，不是默认发布流程。

数据库只有命名持久卷，没有向宿主机发布端口。数据库健康检查使用 TCP `127.0.0.1`，避免把初始化过程中临时 Unix socket 当成启动完成；应用等待数据库 healthy 后自动执行带校验和的事务迁移，并创建初装管理员。初装缺少有效管理员时拒绝启动。镜像以 UID 10001 非 root 运行，含 CA、curl 和 ffprobe；Compose 限制额外权限，使用 init 管理子进程，停机预留 30 秒，应用内部 HTTP 关闭窗口为 20 秒。

应用只发布 `127.0.0.1:8090`，必须通过正确配置的 HTTPS 代理访问页面。生产 Cookie 带 Secure；直接用内部 HTTP 地址登录浏览器不能替代正式入口验证。

| 检查             | 初装期预期                                 | 用途                                                                      |
| ---------------- | ------------------------------------------ | ------------------------------------------------------------------------- |
| `/healthz`       | HTTP 200，容器可 healthy                   | 进程已经监听；镜像/Compose 健康检查保留 75 秒启动宽限                     |
| `/readyz`        | SMTP/S3 未配置时 HTTP 503                  | 数据库、运行配置与生产邮件/私有存储配置门槛；配置缺失不堵死管理员初装入口 |
| `/api/v1/system` | `demo=false`，依赖未齐时注册/支付/媒体关闭 | 对外能力状态，不代表供应商端到端测试通过                                  |

通过 HTTPS 登录管理员后，按邮件→私有存储→网关→模型商品→易支付的顺序配置。桶保持私有，实测上传、授权预览、签名参考链接与下载；SMTP 完成真实收件测试。缺必需配置时 `/readyz` 保持 503；配置齐备后应为 200，但它不测试真实邮件送达、对象写权限或付费模型/渠道可用性。商品和档位仍需独立验收后上架。

管理员创建后清空生产配置中的 bootstrap 邮箱、密码，并只重建应用容器：

```powershell
docker compose --env-file deploy/.env.production up -d --no-deps --force-recreate app
```

保持固定 `APP_SECRET` 和 `APP_SETUP_MODE=true`。该模式允许数据库管理外部配置，不会跳过生产 HTTPS、私有 S3 或就绪检查。后台保存过配置后，修改旧环境秘密不会覆盖数据库中的当前设置。

## 3. HTTPS 反向代理

`deploy/nginx.conf.example` 是宿主机已有 Nginx 的参考片段，不会自动申请证书、安装 Nginx 或开放端口。替换域名与有效证书路径，先执行该主机的 Nginx 配置检查，再按其既有流程加载。Nginx 若在另一个容器中，`127.0.0.1:8090` 不再指向应用，需独立审查网络连接，不能照抄宿主机示例。

代理应保留 Origin、Cookie、Range、If-Range，禁止缓存认证/API 响应；示例关闭代理缓存与媒体整包缓冲。默认 256 MiB 上传另留 1 MiB multipart 开销，因此示例请求体上限为 257 MiB；若修改应用限制，代理上限也需相应调整。

认证限流默认使用 TCP 直连地址。通过反向代理部署时，先在部署主机确认应用所见的实际代理 peer，再把它的精确 CIDR 写入 `TRUSTED_PROXY_CIDRS`，例如直连本机进程的 `127.0.0.1/32`、`::1/128`；宿主机代理经 Docker 端口映射时可能呈现桥接网关地址，不能直接假定是回环地址。保持应用端口仅对可信网络开放，不得填 `0.0.0.0/0`、`::/0` 或任意公网访客网段；共享 Docker 网络内也只应信任受控代理来源。地址变化时同步更新并重建应用容器。

只有直连 peer 命中配置 CIDR 时，应用才使用单个合法 IP 格式的 `X-Real-IP` 作为身份接口限流地址；缺失、重复、IP 链、带端口或 zone 的值均退回直连 peer，`X-Forwarded-For` 不参与该判断。Nginx 必须如示例使用 `proxy_set_header X-Real-IP $remote_addr` 覆盖客户端输入，不能原样传递用户提供的该头。若前面另有 CDN/负载均衡，应先限定 Nginx 的真实 IP 信任边界，并独立验证最终 `$remote_addr`，不能扩大应用信任范围代替配置。错误 CIDR 会使应用启动失败；空配置仍可启动，但经同一代理的访客共用该代理的限流桶，正式开放前须确认不同客户端获得独立限流、伪造头不能绕过。

应用现有 `WriteTimeout` 为五分钟，示例代理等待为 310 秒，不等于无限时大媒体支持。原生 ZIP 流式下载减少浏览器整包内存，但不解除服务器、代理、磁盘暂存、网络和客户端限制。大包、并发媒体、慢网、中断恢复及所支持容量尚需真实设备测试。

## 4. 升级、密码与备份

升级前备份数据库、私有对象、部署配置和对应 `APP_SECRET`，记录上一镜像 ID/digest。保持 `COMPOSE_PROJECT_NAME` 与数据库卷名称，使用新版本镜像重建应用；已有事务迁移不可编辑，失败先保留卷与日志，回退前确认旧应用兼容已应用 schema。不要通过删卷或重建空库修复升级问题。

在 `deploy/.env.production` 中将 `FRAME_IMAGE` 设置为完整镜像引用：`ghcr.io/raingoal/wsapi-video:v0.1.4`、`ghcr.io/raingoal/wsapi-video:sha-<完整提交 SHA>`，或 `ghcr.io/raingoal/wsapi-video@sha256:<digest>`。要跟随最新已通过检查的 master 构建，可使用 `ghcr.io/raingoal/wsapi-video:latest`。然后只拉取并重建应用：

```sh
docker compose --env-file deploy/.env.production pull app
docker compose --env-file deploy/.env.production up -d --no-deps app
docker compose --env-file deploy/.env.production ps
```

`v0.1.0` 仅支持 `linux/amd64`；从 `v0.1.1` 起同时支持 `linux/amd64`、`linux/arm64`。在 ARM64 服务器上从旧版升级时，先将现有配置中的 `FRAME_IMAGE` 改为 `ghcr.io/raingoal/wsapi-video:v0.1.1` 或新版多架构 index digest，再执行上述命令。旧 Release 模板固定了旧镜像 digest，仅执行 pull 不会切到新版。保留现有密码、`APP_SECRET`、项目名及数据库卷；无需添加固定 `platform`，Docker 会自动选择原生架构。

推送代码只会自动构建和发布镜像，不会自动修改你的服务器容器。升级命令保留现有数据库卷；不要添加 `down -v`。应用仍会自动执行新增迁移，因此回退镜像前必须确认 schema 兼容性。

从 `v0.1.2` 起，易支付网关基础地址支持 `/epay/` 等路径前缀。升级后在后台保留服务商提供的完整基础地址重新保存；无需追加 `submit.php`，已有根域名配置继续可用。

`v0.1.2` 还补齐模型商品和充值档位删除，并修复已配置生成凭证却因存储未就绪而无法上架商品的问题。首次启动自动增加目录软删除字段；删除目录项不会移除已有业务记录。

`v0.1.3` 增加视频按次、按请求秒数及分辨率覆盖定价。启动时自动增加可空定价配置列；旧商品继续固定按次，已有报价、任务和账本保留原总额。升级后可直接在后台配置，无需修改环境文件中的外部服务凭证。规则与示例见[视频零售定价](OPERATIONS.md#视频零售定价)。

`v0.1.4` 修复存储未配置时的误导提示：创作页明确提示完成私有存储配置，不再显示为没有可用商品。生产生成仍需私有存储保存成品；存储配置继续在后台填写，已上架商品无需重建。本版本无新增数据库迁移。

`POSTGRES_PASSWORD` 是 PostgreSQL 镜像首次创建数据目录时的初始密码，修改环境文件不会修改已有卷里的数据库账户密码。轮换须按数据库运维流程协调数据库账户与应用连接；本模板没有自动轮换或重置数据操作。`APP_SECRET` 更换须离线重加密，丢失原密钥无法仅凭数据库密文恢复。外部媒体/支付密钥在后台轮换并保留历史凭证版本，不能删旧业务引用。

对象存储的位置首次操作前会被持久锁定。迁桶需要停机、离线复制及一致性核对，不能在后台或数据库删锁绕过。容器可写层用于媒体临时文件，不承载私有媒体的持久副本；需结合实际并发和媒体上限监控主机暂存空间。完整恢复、回滚和告警要求见 [运行手册](OPERATIONS.md)。

## 5. 发布包中的第三方许可

前端构建先从锁定的生产依赖生成 `web/public/third-party-notices.txt`，Vite 将其放入 dist；账户关于区链接同源 `/third-party-notices.txt`。文件包含 React、React DOM、scheduler 与固定画布的版本、版权和完整许可，不以代码压缩后的注释代替随包文本。

后端构建完成后，`cmd/licenses` 从 server/monitor 二进制的实际构建信息核对模块版本、校验和及对应源码目录，递归复制完整 LICENSE/NOTICE 等许可文件并保留相对路径，同时包含同一 Go 工具链的运行时许可。AWS SDK/Smithy 的嵌套 singleflight 许可也随模块保留，不用根 Apache 许可替代。最终镜像通过 Dockerfile 将结果放在 `/app/licenses`，manifest.json 记录两个二进制的 SHA-256、Go 版本和所含模块；缺根许可、空许可、符号链接、替换模块或来源版本不符会使收集失败。该目录不含业务配置和凭证。Debian/ffmpeg 等系统包许可仍保留在镜像自身的 `/usr/share/doc`。

构建和静态文件验证不能证明最终容器已包含这些文件；实际发布时仍须检查目标镜像中的 `/app/licenses/manifest.json`、对应完整文本，以及 HTTPS 入口下的前端清单。现有作者页内署名保留。

## 6. 自动镜像发布与隔离首装 smoke

[GitHub Actions](https://github.com/RainGoal/wsapi-video/actions/workflows/ci.yml) 在分支推送、`v*` 标签推送和 PR 时执行检查。`master` 的 push 或在 `master` 上手动运行时发布开发镜像；版本标签触发版本镜像和 GitHub Release。两条路径都先完成 Go race/vet/build 和前端测试/格式/构建，再在 `ubuntu-24.04`、`ubuntu-24.04-arm` 原生 runner 分别校验两份 Compose、构建并加载本地 `frame-studio:ci`，核对架构、预拉 PostgreSQL 并执行 `deploy/smoke.mjs`。通过 smoke 的各架构镜像以本次 run/attempt 唯一临时标签上传，再通过独立 digest artifact 交给 publish job。只有两种架构都成功后，才按这两个受测 digest 合并并发布正式 manifest index，不重新构建；合并后还会检查平台与 digest 映射。任一架构失败均不更新正式版本、`sha-` 或 `latest` 标签。

从 `v0.1.1` 起，下列镜像引用支持 `linux/amd64` 和 `linux/arm64`；Compose 无需指定平台，Docker 自动选择匹配镜像。固定部署使用多架构 index digest，同一引用可用于两种服务器：

- `ghcr.io/raingoal/wsapi-video:sha-<完整提交 SHA>`：按提交定位的构建，另在 Actions 摘要中提供 digest，固定部署优先使用 digest。
- `ghcr.io/raingoal/wsapi-video:latest`：当前 master 的成功构建；master 运行串行，发布前再次核对远端 HEAD，过时提交不更新此标签。
- `ghcr.io/raingoal/wsapi-video:vMAJOR.MINOR.PATCH`：对应 [GitHub Release](https://github.com/RainGoal/wsapi-video/releases) 的版本镜像；版本标签构建不修改 `latest` 或 `sha-` 标签，各次构建的固定 digest 记录在对应发布结果中。

创建正式版本时，使用严格的 `vMAJOR.MINOR.PATCH` 格式（例如 `v0.1.2`，数字无前导零），标签必须指向已进入 `master` 历史的提交。发布前会再次核对远端标签所指提交。示例：

```sh
git switch master
git pull --ff-only origin master
git tag -a v0.1.2 -m "Release v0.1.2"
git push origin v0.1.2
```

只有镜像发布成功后才创建 GitHub Release。Release 包含版本镜像、源码提交、固定 digest、自动变更记录，以及 Compose 部署压缩包、`image-digest.txt` 和 `SHA256SUMS`。部署包内配置模板固定到该次镜像 digest，保持根目录 Compose 和 `deploy/` 布局；解压后复制模板、填写基础配置，再按第 2 节启动。附件上传完整后才公开 Release。已经存在 Release 的标签不会再次发布正式版本；禁止移动已发布标签或用旧版本号覆盖，后续变更应使用新的版本号。若只有 Release job 失败，可以在 Actions 重跑该失败 job，继续使用前一步已发布的镜像摘要。

发布使用 GitHub 自动提供的 `GITHUB_TOKEN`，仅 container 和 publish job 有 `packages: write`，仅 release job 有 `contents: write`；不需要额外 Docker Hub 或供应商密钥。仓库管理员需允许 GitHub Actions 运行及其包写入；如果账号/组织策略禁止，需在 GitHub 侧调整。流程使用分架构的 Buildx GHA 缓存，并附仓库、提交的 OCI 标签。它发布镜像和版本文件，不连接或重启部署服务器。

**首次创建的 GHCR 包默认可能是 private，即使代码仓库公开。** 如需服务器匿名拉取，在 GitHub 的 Packages → `wsapi-video` → Package settings 中确认可见性为 public；否则在服务器执行 `docker login ghcr.io --username RainGoal`，按提示提供具有 `read:packages` 权限的访问令牌。不要把令牌写入 Compose、env 模板或提交仓库。公开包也需要等首个成功发布完成后才可拉取。

手动执行本地镜像检查需要 Docker daemon 和 Node.js 22：

```powershell
docker build --tag frame-studio:ci .
docker pull postgres:16.4-bookworm
node deploy/smoke.mjs frame-studio:ci
```

镜像构建、依赖下载与预拉镜像需要普通构建网络。smoke 运行时使用随机名称的 `--internal` Docker 网络，不发布宿主机端口，PostgreSQL 数据只放其自有容器 tmpfs；无模型、SMTP、S3 或支付调用。脚本验证全新库缺管理员拒绝启动、生产初装 `/healthz=200` 与 `/readyz=503` 分离、静态页面、能力关闭、管理员 Secure/HttpOnly/SameSite Cookie、设置可读，以及移除 bootstrap 后用同一数据库/固定主密钥重启。清理只删除携带本次随机所有者标签的容器和网络，不使用生产 Compose 项目或持久卷。

本机没有 Docker CLI；本地完成的 YAML、工作流和脚本静态检查不代表镜像构建或容器 smoke 已通过。构建、smoke 和推送结果以 Actions 对应提交的运行记录为准。即使 smoke 通过，也不证明真实私有 S3、SMTP、模型、商户、正式 HTTPS、容量或恢复已通过。

配置语法依据：[Compose 服务](https://docs.docker.com/reference/compose-file/services/)、[变量插值](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)、[Docker HEALTHCHECK](https://docs.docker.com/reference/dockerfile/#healthcheck)、[内部网络](https://docs.docker.com/reference/cli/docker/network/create/)、[Nginx 代理](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)。

发布配置依据：[向 GitHub Container Registry 发布](https://docs.github.com/en/actions/use-cases-and-examples/publishing-packages/publishing-docker-images)、[GHCR 认证与包可见性](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[多架构矩阵发布](https://docs.docker.com/build/ci/github-actions/multi-platform/)、[Buildx imagetools create](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/)、[GitHub 原生 ARM runner](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)、[GitHub CLI 创建 Release](https://cli.github.com/manual/gh_release_create)。

## 7. 本地发布候选边界

r14 核查修复了首装 smoke 仍匹配原始启动错误、与当前脱敏 JSON 日志不一致的问题；现检查非零退出及固定 `server_lifecycle/run_failed` 事件，随后同库 bootstrap/去 bootstrap 重启对照保持。语法、格式、日志测试与断言块检查已过；本机仍无 Docker，实际镜像、容器首装及重启均未验收。

本机另有 `.runtime/releases/r13-20261018` 的 Windows/Linux amd64 候选 ZIP，包含两个应用二进制、配套 r13 前端与完整许可，逐文件 bytes/SHA 已核对。它们不是镜像或生产安装器，未包含 ffprobe、PostgreSQL、环境配置或秘密；Linux 是交叉构建，所有应用二进制均未执行。正式部署使用前述 CI 发布的镜像与根 Compose 文件；历史 ZIP 不进入 Git 仓库，也不代表后续提交的镜像已验收。完整历史记录与候选链接见 [发布候选验收](QA_RELEASE_CANDIDATE_2026-10-18.md)。
