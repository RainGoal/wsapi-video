# 商品替换

独立入口 `/product-swap`，沿用已确认的石墨灰 / 暖琥珀设计。素材、报价、任务、私有归档和账务复用现有服务；工具工作区不出现在画布项目列表，不创建画布节点。

实施范围：上传短视频与同款商品多角度图片；可选真实抽帧识图；手动确认目标与要求；按已验证 Seedance 商品规格报价；幂等生成、状态恢复、历史、同步对比和下载。参考视频生成不能保证逐像素保留人物、动作或背景。首版处理完整短片段，用户上传已剪好的素材。

管理员需对 Seedance 商品显式启用 `supports_product_swap`，并按现有商品验证流程重新验证上架。必须支持至少 1 张图片、1 段视频，且配置时长、分辨率和画幅。分析在同一 Sub2API origin 的 `/v1/chat/completions` 使用独立的 `analysis_model` / `analysis_key`；需由部署方确认兼容协议与图像模型可用。没有默认模型或伪分析回退。协议参考：https://developers.openai.com/api/docs/guides/images-vision 。真实服务验收前不视为能力已验证。

报价保存替换目标、图片角度、视频 ID、音轨选项、分析 ID 与生成提示词的不可变快照；任务提交继续使用原有 quote_id/request_key 恢复协议。识图为可选步骤，不收取平台生成积分，可能产生管理员网关费用；每账号限制调用频率。图像与帧中的文字均当作素材数据。

媒体处理需要 `FFMPEG_PATH`（默认 ffmpeg）与现有 `FFPROBE_PATH`。项目 Docker 镜像已通过 Debian 的 `ffmpeg` 软件包内置两者；Dockerfile 和两份 Compose 分别固定为 `/usr/bin/ffmpeg`、`/usr/bin/ffprobe`，无需在宿主机安装或另起媒体容器。只读取鉴权后的平台资产，禁止用户传入外部 URL 或存储路径。抽帧有时长、大小、进程时间和输出大小限制。

接口：

- `POST /api/v1/product-swap/workspace {}`：幂等创建当前账号工具工作区，返回 `project_id`、`analysis_available`、`media_processing_available`、`max_duration_seconds`。
- `POST /api/v1/product-swap/analyze {project_id,source_video_id,images:[{asset_id,angle}]}`：从原片段的 0%、25%、50%、75%、95% 位置抽样，结合商品图返回目标、动作、遮挡、外观、风险、缺图建议与抽样时间。每账号每 10 分钟最多 6 次。服务不可用或数据不完整时返回错误，允许用户手动填写。
- `POST /api/v1/quotes` 扩展 `product_swap:{source_video_id,images,target,instructions,preserve_scene,audio_mode,analysis_id?}`。与普通 `prompt/reference_asset_ids/audio/start_frame/end_frame/shot_id` 互斥；由服务端构造引用与提示词，响应附带最终 `prompt`。`analysis_id` 必须属于当前账号，并与工作区、源视频、商品图及其顺序和角度严格匹配。
- `POST /api/v1/tasks {quote_id,request_key}` 与原协议一致。任务 `input.product_swap` 保留完整设置；历史通过工具工作区 `project_id` 分页获取。

首版上传完整的 30 秒内片段，商品图 1–6 张且受所选模型上限约束，不提供自动剪片、目标跟踪或蒙版编辑。音轨支持 `original`（生成后合成原声）、`muted`（生成后移除音轨）、`generated`（使用模型输出）。保留原声要求请求时长与原片段相差不超过 0.25 秒，归档前再次检查实际生成视频时长；不拉伸或循环音轨。模型画面动作仍可能偏移，不能据此保证口型与动作同步。若原片段本身无音轨则保留为静音；音轨处理异常保留原任务与冻结金额供人工核查，不重新调用生成。

数据库迁移 `008_product_swap.sql` 在后端启动时按现有迁移机制执行。工作区、分析元数据持久化在 PostgreSQL，浏览器草稿与提交恢复记录按账号隔离；最近查看任务可刷新恢复。页面对比使用同步静音预览，下载才包含最终音轨。

## 启用顺序

1. 部署带有迁移的后端。Docker 使用新版本镜像及匹配的 Compose 即已包含媒体工具；原生部署需配置可执行的 `FFMPEG_PATH` 与 `FFPROBE_PATH`。
2. 在运营后台为兼容的 Seedance 视频商品启用“商品替换”，填写实际可用的视频 / 图片数量、时长、画幅和分辨率，重新完成验证并上架。
3. 如需素材分析，在媒体设置中填写支持图像输入的 `analysis_model` 和独立 `analysis_key`。不配置时用户仍可手动填写替换对象。
4. 从侧栏“商品替换”上传真实的短片段和商品图片，确认报价后进行真实服务验收。模型是否能保留人物、动作及包装文字，需要以实际成片评估。

## 开发验证（2026-10-24）

- 在隔离 PostgreSQL 中运行 `go test -race ./... -count=1` 与 `go vet ./...`，均通过；覆盖工作区唯一性、跨账号隔离、报价快照、幂等冻结及独立识图密钥的保存 / 脱敏。
- 使用真实 ffmpeg / ffprobe 验证抽帧、原声合成、静音输出与临时文件清理。
- 前端 `npm test` 共 222 项全部通过，`npm run format:check`、类型检查与 `npm run build` 通过。构建仍提示现有单包超过 500 kB。
- 浏览器使用独立的接口夹具与本地测试媒体，验证提交回执丢失后的同请求恢复、刷新恢复结果、参数变化使报价失效、无识图服务时手动操作、无可用商品时阻止提交、中英文及 320–1440 px 布局。
- 对比预览验证两段视频实际推进、同步暂停、进度拖动、原视频独立播放、快速暂停及结束后重播。

本轮没有调用付费 Seedance 或识图服务；浏览器测试素材与返回值不代表真实商品替换效果。尚未部署生产环境。
