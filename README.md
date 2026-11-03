# Noema

独立产品入口。Lucky Agent 只作为进程内执行引擎，不复用它的工作台界面。

第一期样品只证明：独立 HomeDir、固定会话、文本流、取消、以及 capability 报告。人格包、关系状态、受控记忆、主动联系和 Android 壳不在这个样品里。

## 现状

- 适配层：`internal/adapters/lucky`
- 业务看到的引擎接口：`internal/engine`
- 角色原则：`characters/hiori/SOUL.md`（初始化写入独立 HomeDir，不是逐轮注入）
- 当前 SDK pin **没有** 宿主上下文、审批回传、`ui_delta`。对应方法返回 `unsupported`，capability 为 false。

相关记录：[Issue #1](https://github.com/yurika0211/Noema/issues/1)

## 运行

```bash
export NOEMA_API_KEY=...
export NOEMA_HOME=/var/lib/noema/owners/demo/characters/hiori/lucky-home
GOWORK=off go run ./cmd/noema -message "你好"
```

只看能力面：

```bash
GOWORK=off go run ./cmd/noema -capabilities
```

不要把 `NOEMA_HOME` 设成 `~/.luckyagent`。

## 验证

```bash
GOWORK=off go test -mod=readonly ./...
GOWORK=off go test -mod=readonly -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -mod=readonly -o bin/noema ./cmd/noema
```

这些命令不调用真实模型。带 `-message` 的运行需要可用的 provider。
