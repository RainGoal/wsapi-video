# E11：200 节点、150 连线、50 镜头本机容量验收

日期：2026-10-17，证据冻结于 18:15（Asia/Shanghai）。环境：`http://127.0.0.1:8091` 本机 Demo，Windows x64，Headless Chromium **151.0.7922.34**；桌面 1440×1000，触屏模拟 390×844、DPR=1、`isMobile/hasTouch=true`。浏览器报告 hardwareConcurrency=16、deviceMemory=32，未做 CPU 或网络限速。这是桌面浏览器模拟触屏视口，不是真实手机。

**结果：该样本的画布与分镜操作通过。测试发现的撤销草稿残留、可信触屏指针异常均已修复并定向回归。** 最终前端包为 `/assets/index-DuDH2Rtd.js`，最终 canvas revision=19、shots revision=3；任务和账务完整快照前后相等。没有操作 8092、sub2api、真实模型或支付服务。

## 样本与正常接口

用户：`usr_c48809b0d262739bdc542ed592bb756f`；独立项目：`prj_ada0aee77ef74711dd6cbba05b017b7f`。通过正常 POST `/projects`、PUT `/projects/{id}/canvas`、PUT `/projects/{id}/shots` 建立数据，未直接改数据库或绕过鉴权。

| 内容 | 实际样本 |
| --- | --- |
| 节点 | 200：50 图片、100 文字笔记、50 生成构想 |
| 连线 | 150 条内部引用，50 个构想各接 1 图片与 2 笔记 |
| 镜头 | 50 个，每个 2 图片候选、2 参考、1 选定成品 |
| 素材 | 复用本人已授权的 2 个 PNG 素材 ID；每份 4,582 B、1024×576，同源字节 |
| DOM | 画布 200 节点 / 150 连线；分镜 50 卡片 / 100 候选 / 50 selected |
| 初始 JSON | graph 56,554 B，shots 数组 24,923 B；不含 HTTP 封装 |

种子两个 PUT 串行往返共 47ms，初始 canvas/shots revision 均为 1。图片在视口外采用现有 lazy loading；未强制解码全部远处图片。该样本没有视频节点、视频候选或新生成。

## 测量方法与实测

加载从导航开始，到第 200 个节点已出现并核对 150 条连线；刷新和最终包额外等待两个 requestAnimationFrame。浏览器 `performance.now()` 提供 readyMs，Node 墙钟包括自动化命令往返。操作测量从执行真实 click/tap/fill/键盘或 CDP 触屏序列前开始，到相应 DOM 状态成立、再等待两个 rAF。不是纯输入延迟、FPS、百分位或性能承诺；每项为一次观测，不扣除自动化和帧等待开销。

| 加载观测 | 浏览器 readyMs | 墙钟 ms |
| --- | ---: | ---: |
| 初始桌面打开 | 181.7 | 184 |
| 初始桌面刷新 | 103.0 | 106 |
| 初始 390px 打开 | 196.3 | 198 |
| 最终包桌面刷新，200 / 150 | 121.4 | 124 |
| 最终包 390px 刷新，200 / 150 | 113.7 | 116 |

下表为本轮发现缺陷前的交互观测，初始包 `/assets/index-Bkzbj50-.js`；分镜触屏切换行来自中间修复包 `/assets/index-DL5l68OX.js`。最终包另做下文定向回归，没有重复完整性能测试。

| 操作 | 桌面 ms | 390px 触屏 ms |
| --- | ---: | ---: |
| 放大 / 缩小 | 55.3 / 65.0 | 76.2 / 81.8 |
| 平移，8 个移动步骤 | 182.0（空格拖动） | 274.5（单指） |
| 双指缩放，6 个移动步骤 | — | 182.1 |
| 选中笔记 / 编辑文本 | 60.1 / 31.9 | 85.5 / 32.6 |
| 选中连线 / 删除 / 撤销 | 56.8 / 65.7 / 64.0 | 97.3 / 82.6 / 65.7 |
| 可见端口新建连线 / 撤销 | 123.8 / 66.8 | — |
| 删除节点及其线 / 撤销 | 68.8 / 65.4 | — |
| 切换到 50 镜头 / 跳至第 50 镜头 | 144.7 / 55.8 | 146.4 / 65.3 |
| 第 50 镜头切换候选 | 82.1 | 81.5 |

最终包 390px 切换到 50 镜头另测 **115.2ms**，仍为 100 候选、50 selected。桌面将第 50 镜头切到图片 B 并保存 shots revision=2；触屏切回图片 A 并保存 revision=3；刷新后最后镜头仍选 A。390px 的 documentWidth=390，没有横向溢出。Long Tasks API 确认可用，上述已记录加载和操作窗口没有 ≥50ms 的 longtask 条目；该观察不代表所有帧都达标。

## 发现、修复与最终包回归

1. **撤销回已保存图残留删除草稿。** 云端 revision=3 为 200/150，立即删除再撤销，DOM 回到 200/150，但状态仍 pending、localStorage 留下 199/149；刷新出现错误的恢复草稿提示。根因是图等于已保存快照时 effect 过早返回，未清理 active 草稿和状态。由主代理修复 Workspace 的保存收敛与在途确认分支。
2. 中间修复包验证立即删除→撤销，1.3s 内无额外 PUT、revision=8 不变、saved/draft=null，刷新没有草稿提示。另让真实删除 PUT 先落库为 revision=9/199/149，再延迟回执并撤销：在途 DOM 与 localStorage 均为 200/150；释放回执后第二个 PUT 基于 revision=9 恢复到 revision=10/200/150，最终 saved/draft=null。最终包再次验证立即删撤：revision=19 不变、0 额外 PUT，刷新 200/150、无恢复提示。
3. **可信触屏结束 ID 残留。** 原轮捕获 2 条 NotFoundError，干净复现再捕获 1 条；stack 指向 InfiniteCanvas 的 touchStart 对失效旧 ID 执行 setPointerCapture。所有触屏来自 Playwright tap 或 Chromium CDP Input.dispatchTouchEvent；document 指针日志 `isTrusted=true`，没有构造 PointerEvent 或使用 dispatchEvent。窗口 pointerup 的旧订阅依赖变化回调，节点选中/属性栏渲染时可能失去结束清理。
4. 本代理获授权仅修改 `infinite-canvas.tsx`：用最新回调 ref 稳定订阅，在 window 捕获阶段处理 pointerup/cancel，确保先于节点结束时的渲染清理指针；未增加异常吞噬。类型检查、目标 Prettier、现有 canvas 测试 5/5 通过，生产包由主代理统一构建。
5. 最终包执行可信原序列 **1 次加 3 次重复，共 4 轮**：平移工具→8 步单指平移→6 步双指缩放→选中笔记→填入同值→关闭属性→再次快速单指平移→选择工具→触屏点击连线。每轮 36 条指针事件全部 trusted；缩放实际 **0.7→0.82**，200 个节点世界坐标逐项不变，连线属性正常打开，**新增 pageerror=[]**。原 3 条错误独立保留，未清掉历史来宣称通过。

## 任务、账务与证据边界

最终 wallet/tasks/ledger/orders 的完整 JSON 与测试前 `deepEqual`：可用/冻结积分始终 0/0，同一 1 个既有任务、同一 3 条既有流水、0 订单。捕获的浏览器写请求没有 quote/task/order 创建；最终全部 200 节点 ID 和坐标与种子相等。未新生成、调账、充值或产生费用。

脱敏原始数据：[e11-capacity-evidence.json](D:/Code/Other/wsapi-video/.runtime/qa/e11-capacity-evidence.json)，含计时、请求、版本、最终包、源码 SHA256、前后财务快照、原缺陷与修复后断言。原可信异常独立证据：[e11-trusted-pointer-error.json](D:/Code/Other/wsapi-video/.runtime/qa/e11-trusted-pointer-error.json)。截图均在 `D:/Code/Other/wsapi-video/.runtime/qa/`，包括 `e11-desktop-200-nodes-overview.png`、`e11-final-mobile-trusted-gesture.png`、`e11-final-mobile-shot-50.png`、`e11-fixed-undo-refresh-clean.png`、`e11-fixed-undo-inflight-draft.png`；原缺陷截图也保留。已目视桌面概览与移动分镜截图。

这些结果只证明当前本机浏览器、200/150/50、重复的小 PNG 与本地网络场景。不能外推真实目标手机、弱网、生产 S3、不同大图片、视频解码、多视频同时播放、长时间操作或更大数据量；没有据此认定生产容量或发布验收完成。
