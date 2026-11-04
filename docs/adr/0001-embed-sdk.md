# ADR 0001：嵌入 luckyagent-app/sdk，不起 Estuary 服务

状态：接受。日期：2026-11-04。对应 #17 的 M0-01 其余。

## 决定

Noema 进程内调用 `luckyagent-app/sdk`。唯一 Agent Loop 留在 Lucky Agent。Noema 只做产品协议 `noema.v1`、事件日志、鉴权和 Android 文本壳。

Estuary 只保留字段契约 `docs/estuary-event.schema.json`。不另起一个 Estuary 微服务。schema 的 `$id` 路径和文件位置不一致，等真有 schema 目录时再挪，这次不动。

## 为什么不是再写一套 Loop

模型选择、工具执行、会话和 Compact 已经在锁定的 SDK 里。再写一套会和上游分叉，Noema 还要自己补取消、重放和 capability。适配层 `internal/adapters/lucky` 是唯一碰到 SDK 类型的地方。SDK 没有的能力（宿主上下文、审批回传、`ui_delta`、受控记忆）报告为 false，方法返回 `unsupported`。

## 为什么不是再起一个服务

第一期是一个 owner、一个角色、一个 HomeDir。HTTP 只暴露 Noema 自己的 `/healthz`、`/v1/capabilities`、`/v1/ws`。客户端不直连 `lh serve`。再套一层 Estuary 进程不会让契约更真，只会多一个要一起启动的东西。

## 没选的做法

- 改 SOUL 或拼接用户消息，顶替逐轮宿主上下文。
- 把 `NOEMA_HOME` 指到 `~/.luckyagent`，或把事件日志放进 HomeDir。
- 在普通日志里写对话正文。观测只记协议版本、session、run、client_request_id、状态、耗时、首段时间和字节数。

## 后果

上游补齐宿主上下文、审批和 UI 事件之前，关系、记忆、写工具和表情都不做。一键启动是 `scripts/noema-up.sh`，它只是带上这些约束去跑已有的 `go run ./cmd/noema -listen`。
