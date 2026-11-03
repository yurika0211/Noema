# Noema

独立产品入口。Lucky Agent 只作为进程内执行引擎，不复用它的工作台界面。

运行时和 Android 客户端在同一个仓库。第一期样品证明：独立 HomeDir、固定会话、文本流、取消、capability 报告，以及 `noema.v1` WebSocket。人格包、关系状态、受控记忆和主动联系不在这个样品里。

## 现状

- 适配层：`internal/adapters/lucky`
- 业务看到的引擎接口：`internal/engine`
- 产品协议：`internal/protocol`（不是 Lucky Agent 的 WS 字段）
- 接入层：`internal/transport`
- Android 文本壳：`apps/android`
- 角色原则：`characters/hiori/SOUL.md`（初始化写入独立 HomeDir，不是逐轮注入）
- 当前 SDK pin **没有** 宿主上下文、审批回传、`ui_delta`。对应方法返回 `unsupported`。

相关记录：[Issue #1](https://github.com/yurika0211/Noema/issues/1)

## 运行后端

```bash
export NOEMA_API_KEY=...          # 模型供应商密钥
export NOEMA_BIND_KEY=...         # 可选，Android 接入鉴权
export NOEMA_HOME=/var/lib/noema/owners/demo/characters/hiori/lucky-home
GOWORK=off go run ./cmd/noema -listen 127.0.0.1:8787
```

模拟器默认连 `ws://10.0.2.2:8787/v1/ws`。真机用电脑的 LAN 地址。不要把 `NOEMA_HOME` 设成 `~/.luckyagent`。

只看能力面：

```bash
GOWORK=off go run ./cmd/noema -capabilities
```

## Android

```bash
cd apps/android
cp local.properties.example local.properties   # 填 sdk.dir
./gradlew :app:testDebugUnitTest
```

客户端只消费 `noema.v1`。不接 `lh serve`，没有 Memory 图谱、Tasks、Skills、Cron。停止按钮才会取消当前回合。

## 验证

```bash
GOWORK=off go test -mod=readonly ./...
GOWORK=off go test -mod=readonly -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -mod=readonly -o bin/noema ./cmd/noema
```

这些命令不调用真实模型。`-listen` 后的对话需要可用的 provider。
