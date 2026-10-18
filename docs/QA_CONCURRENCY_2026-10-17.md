# E5：双会话画布、分镜冲突与慢响应验收

验收日期：2026-10-17（Asia/Shanghai，最终证据 17:48）。环境：`http://127.0.0.1:8091` 本机 Demo、Chromium、1440×1000。复用 R6 独立 QA 用户，建立两个隔离 browser context，分别登录同一用户；未操作其他测试页面、8092、真实模型或支付服务，未修改前后端生产代码。

**结果：本次真实浏览器场景通过，未发现缺陷。** 画布与分镜的真实版本冲突均返回 409；本地草稿、明确云端重载和另存副本符合各自界面说明。旧保存响应延迟期间继续输入、追加候选，没有覆盖新输入或已有 selected。任务、钱包、流水、订单的前后完整 JSON 快照相等。

## 追溯标识与素材

| 对象 | 标识 |
| --- | --- |
| QA 用户 | `usr_c48809b0d262739bdc542ed592bb756f` |
| 源项目 | `prj_d9332ac09e75265cd55c8b8c7209bfd0` |
| 画布冲突副本 | `prj_3cc8ce7430acfac95628bc90b5859275` |
| 分镜冲突副本 | `prj_d9bdd0873502a3aeda9da301b7b959df` |
| 源镜头 | `c650a65f-00c0-4164-b180-53090345c4ba` |
| 图片 A / B | `ast_4f71bf5cff45278fc4969b7f1c0c7588` / `ast_c57882c8458154fe4c792543bf725cdd` |
| 视频 A / B | `ast_c2d626430e30e8257cee8347e2473e2a` / `ast_aafac3f82ee0b3efeebffe301b6c0900` |

使用文件输入框上传 4 个受控本机文件：两份 PNG（每份 4,582 B）和两份 MP4（每份 9,916 B），同类 A/B 使用相同源字节、不同文件名与素材 ID。PNG 为本机 Demo 图片 fixture；MP4 为本机录制的 UI MEDIA FIXTURE，**不是模型生成视频**。本次没有新生成、报价确认、测试调账或充值。未在证据中记录密码、Cookie、CSRF。

## 画布冲突、草稿与副本

1. 通过界面将图片 A、视频 A 设为分镜候选、参考与选定成品，保存 shots revision=1；把两素材加入画布并建立笔记，形成 canvas revision=4、3 个节点。两个会话加载同一基线。
2. 会话 A 新增 `E5 cloud winner A`，保存 canvas revision=5；会话 B 仍基于 revision=4 新增 `E5 local B`，自动保存收到真实 HTTP 409。云端只含 A 的新增笔记，本地草稿只含 B 的新增笔记，公共图片、视频和初始笔记均在。
3. 刷新 B 后出现本地草稿提示；点击“恢复本地草稿”恢复 B 的内容并保持冲突，点击“另存画布副本”得到上述画布副本。副本画布节点与 B 草稿逐字段相等；分镜候选、参考和两个 selected 与源基线相等，镜头 ID 正确重新生成。副本素材列表含图片 A、视频 A。
4. 返回源项目，明确点击“使用云端版本”。再次让 A 保存新笔记到 revision=6、B 基于旧版本保存而收到 409，再点击“重载云端版本”；B 显示 A 的云端笔记，B 待丢弃笔记消失，本地画布草稿键为 null。这是用户明确丢弃草稿的行为。

## 画布旧响应延迟期间继续输入

测试路由仅拦截本项目的第一个 PUT，先 `route.fetch()` 请求真实后端并取得真实成功回执，再由测试门闩延迟 `route.fulfill()`；未伪造成功响应或修改请求体。延迟由操作完成后主动释放，没有用超时猜测竞态。

1. A 保存 `E5 canvas submitted OLD`；后端已经写入 canvas revision=7，但浏览器仍显示“正在保存到云端…”。
2. 回执尚未交付时，在同一文本框继续输入 `E5 canvas input NEW during old response`。API 确认云端仍为 OLD，本地 localStorage 草稿与文本框均为 NEW。
3. 释放旧回执，文本框保留 NEW，随后自动保存 canvas revision=8。API 中的新内容为 NEW，本地草稿已清除。旧响应没有回填 OLD 或吞掉后续更改。

## 分镜冲突、保留草稿重载与副本

1. 两会话加载 shots revision=1。A 修改标题并保存 revision=2；B 保留旧基线，修改为 `E5 shots LOCAL B with extra candidates`，通过“管理参考与候选”追加图片 B、视频 B 及其参考，保留选定 A，点击保存收到真实 HTTP 409。
2. 云端 revision=2 仍只有 A 候选与 A 的标题；B 本地 revision=1 草稿含本地标题、2 图片候选、2 视频候选、4 个参考，selected 仍为图片 A / 视频 A。
3. 点击“重载云端分镜（本地草稿继续保留）”，界面显示 A 的云端标题，本地草稿继续存在并可恢复。恢复旧草稿后保持冲突，点击“本地分镜另存副本”得到上述分镜副本。
4. 副本的镜头 ID 已重映射，其他镜头字段与 B 草稿逐字段相等；保存了云端画布的 6 个节点。副本素材列表含全部 4 个候选素材；逐一使用已登录会话请求 content Range `bytes=0-15`，均返回 HTTP 206、16 B。截图目视确认两类各 2 候选及各 1 个选定成品。

## 分镜旧响应期间输入与候选追加

同样先由真实后端完成保存，再延迟旧回执到达浏览器。

1. A 保存描述 `E5 shots submitted OLD` 到 shots revision=3。在该旧回执尚未返回时改为 `E5 shots NEW while old response pending`，并通过管理界面追加图片 B、视频 B 及其参考；没有点击新候选来改变选定结果。
2. 释放旧回执后，新描述与候选仍存在，“保存分镜更改”仍可用，localStorage 草稿升级到 revision=3 并保留全部新内容。没有把刚追加的候选替换为旧快照。
3. 再次显式保存成功，shots revision=4，本地草稿清除。刷新后新描述仍在，图片候选=2、视频候选=2、参考=4、selected 数量=2，两个 selected ID 仍为 A。完整 API 状态和 DOM 数量均已断言。

## 任务与账务核对

| 检查 | 测试前 | 测试后 |
| --- | --- | --- |
| 可用 / 冻结积分 | 0 / 0 | 0 / 0 |
| 任务 | 1 个既有 R6 任务 | 同一任务，完整快照不变 |
| 流水 | 3 条既有 R6 流水 | 同一批流水，完整快照不变 |
| 订单 | 0 | 0 |
| 浏览器 pageerror | — | `[]` |

Node strict assert 已校验前后 wallet/tasks/ledger/orders 完整 JSON `deepEqual`，并校验副本镜头字段、ID 重映射、画布节点、候选数量、selected、刷新结果和素材可读性。捕获到 2 次画布 409、1 次分镜 409；浏览器请求记录没有 quote/task/order 创建请求。

## 证据与边界

脱敏完整证据：[e5-concurrency-evidence.json](D:/Code/Other/wsapi-video/.runtime/qa/e5-concurrency-evidence.json)。截图位于 `D:/Code/Other/wsapi-video/.runtime/qa/`：`e5-canvas-conflict.png`、`e5-canvas-cloud-reloaded.png`、`e5-canvas-delayed-new-input.png`、`e5-shots-conflict-candidates.png`、`e5-shots-delayed-candidate-append.png`、`e5-shots-new-input-persisted.png`、`e5-shots-copy-media-preserved.png`。

本次验证本机双会话冲突、真实保存响应延迟与手动候选追加；未验证真实外部模型完成时的并发回调、生产 S3、跨真实设备或广域网断线。没有据此认定生产发布已验收。
