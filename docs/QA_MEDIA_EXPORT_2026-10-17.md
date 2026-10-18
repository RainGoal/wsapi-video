# 大媒体原生导出专项验收

执行日期：2026-10-17。版本为本地工作区本轮导出修复后的代码，尚非生产发布记录。所有请求使用受控本机 HTTP 服务、PostgreSQL 随机隔离 schema 与真实本地磁盘对象；测试结束清理 schema 和临时文件。未请求真实模型、支付、SMTP 或 S3 服务，未调整 8092 配置。

## 修复与证据范围

- ZIP 输出前仍先打开所有已授权对象；缺对象返回正常 503，尚未发送 ZIP 附件头。
- 输出后发生读取错误、实际长度与数据库快照不符、ZIP 最终写入失败，使用 `http.ErrAbortHandler` 终止传输，不把不完整 ZIP 以正常 HTTP 响应结束。`secure` 中间件透传该哨兵，由 `net/http` 关闭连接。
- 每个对象最多读取记录长度加一个字节，并复用 64 KiB 缓冲区；避免实际超长对象绕过导出容量限制。
- 请求取消会关闭已打开的对象流；复制过程中检查上下文，结束后关闭所有流。

本轮文件为重复的确定性媒体测试数据，落盘后逐字节经 HTTP 传送。数据库素材类型用于覆盖视频素材的导出分支，但这些 `.bin` 对象不是已验证的可播放视频；本记录不证明大视频编码、解码或目标设备播放器兼容。

## 256 MiB 流式下载、取消与重试

`TestPostgresExportLargeNativeDownloadCancellationAndRetry` 使用项目正式 prepare/download handler、签名 ID 快照、真实磁盘对象与 HTTP/1.1 GET。该测试在 handler 层注入已知账号，账号 Cookie/CSRF 的完整代码链另见下一节。客户端下载直接流向磁盘，不把 ZIP 收成内存 Blob。

| 对象大小 | 实际 ZIP 字节 | 实际源读取字节 | 采样峰值堆增长 | 期间累计分配 | 本次下载写盘耗时 |
| --- | ---: | ---: | ---: | ---: | --- |
| 32 MiB | 33,554,584 | 33,554,432 | 212,872 B | 213,400 B | 186.4567 ms |
| 256 MiB | 268,435,608 | 268,435,456 | 479,736 B | 480,264 B | 1.0132751 s |

对象体积增加 8 倍，实测 Go 堆峰值增长约 208 KiB → 469 KiB；没有随对象体积线性增长。测试在每跨过 4 MiB 源读取时采样 `runtime.MemStats.HeapAlloc`，比较请求前 GC 后基线，设置大对象堆增长不得超过 32 MiB、不得超过小对象加 16 MiB 的回归门槛。采样包括同一进程中的服务与 HTTP 客户端；不是操作系统 RSS、浏览器内存或严格连续峰值测量。耗时仅描述这次本机受控传输，不作为容量或并发性能承诺。

两个完整 ZIP 均通过目录解析、单一条目数量、精确未压缩长度、ZIP CRC 与源对象 SHA256 校验。重复传入同一 asset ID，签名快照与最终 ZIP 均只保留一个条目。

客户端收到 1,048,576 B 后取消，源读取停在 1,114,112 B，所有打开的素材流已关闭；handler 在 3 秒测试期限内结束。同一签名链接重试成功得到 268,435,608 B，CRC 与原 SHA256 一致。准备、下载、取消、重试前后钱包可用/冻结积分、流水数、生成任务数、支付订单数、素材数完全相同。

## 完整 server / secure / Cookie / CSRF 代码链

`TestExportProductionHandlerAbortsTruncatedLargeObject` 使用 `server.New` 的完整 HTTP handler、真实注册登录 Cookie 与 CSRF prepare 请求。运行配置采用隔离测试用的开发 HTTP 设置；它验证正式代码路径，不等同于正式 HTTPS 部署验收。

实际 256 MiB 对象开始下载，客户端接收 1 MiB 后将正在读取的本地源文件截短。观测结果：

- 响应头已经是 200；收到 **1,179,701 B** 后客户端明确报告 **`unexpected EOF`**，不完整 ZIP 无法打开。200 响应头本身不被用作下载成功证据。
- 证明导出发出的终止哨兵穿过 `secure` 后确实中断 HTTP 流，没有被普通 panic 恢复逻辑吞掉或追加 JSON 后正常结束。
- 恢复原对象后，同一链接重试收到 **268,435,616 B**，单一 256 MiB 条目、ZIP CRC 与原 SHA256 全部通过。
- 钱包可用/冻结、流水、任务、订单及素材行数保持不变。

## 读取失败与缺对象

`TestPostgresExportReadFailuresAbortHTTPAndZIP` 在实际磁盘读取上注入确定性故障，均验证最终客户端错误、ZIP 拒绝以及所有流关闭：

| 故障 | 已发送头 | 本次接收字节 | 最终结果 |
| --- | --- | ---: | --- |
| 读取 1 MiB 后返回存储错误 | 200 ZIP | 1,048,627 | `unexpected EOF`，ZIP 拒绝 |
| 读取 1 MiB 后提前正常 EOF | 200 ZIP | 1,048,627 | 长度不符触发中断，`unexpected EOF`，ZIP 拒绝 |
| 实际对象长于记录的 4 MiB | 200 ZIP | 4,194,355 | 有界读取后中断，`unexpected EOF`，ZIP 拒绝 |
| 第二个对象在打开时缺失 | 503 JSON | 不生成 ZIP | 无附件头，先前打开的对象关闭 |

恢复读取条件后同一资产可再次完整导出；故障与重试均不改变资金、任务或资产记录。`TestExportZIPFinalWriteFailureAbortsHandler` 另以 ZIP 中央目录写入错误验证最终 `zip.Close` 失败同样触发终止，且关闭对象流。

## 执行命令与限制

预先设置指向本机测试 PostgreSQL 的 `TEST_DATABASE_URL`，凭证不写入本记录：

```text
go test -race ./internal/projects -run 'Test(ExportZIPFinalWriteFailureAbortsHandler|PostgresExport)' -count=1 -timeout=120s -v
go test -race ./internal/server -run TestExportProductionHandlerAbortsTruncatedLargeObject -count=1 -timeout=90s -v
go test -race ./... -count=1 -timeout=120s
go vet ./...
git diff --check
```

以上专项均通过；projects 用时 7.486s，完整 server 链专项 6.769s。真实 PostgreSQL 全量 race 命令退出码为 0，所有包通过，其中 projects 8.269s、server 31.473s、settings 1.346s、storage 2.076s、migrations 2.341s。`go vet ./...` 与最终 `git diff --check` 均通过。

本记录覆盖单个 256 MiB 真实磁盘对象的本机传输、取消、重试与受控失败。未证明 2 GiB 上限、200 个大对象并发流、HTTP/2、正式反向代理超时、私有 S3/R2、真实大视频播放与 Range、浏览器/手机实际内存及弱网下载体验；这些证据不得由本机 HTTP 客户端测试代替。
