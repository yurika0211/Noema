# Noema

独立产品入口。Lucky Agent 只作为进程内执行引擎，不复用它的工作台界面。

运行时和 Android 客户端在同一个仓库。第一期样品证明：独立 HomeDir、固定会话、文本流、取消、capability 报告，事件日志落盘重放，以及 `noema.v1` WebSocket。人格包、关系状态、受控记忆和主动联系不在这个样品里。

## 现状

- 适配层：`internal/adapters/lucky`
- 业务看到的引擎接口：`internal/engine`
- 产品协议：`internal/protocol`（不是 Lucky Agent 的 WS 字段）
- 接入层：`internal/transport`
- 事件日志：`internal/eventlog`（落盘到 Noema 自己的状态目录，`seq` 跨重启单调）
- Android 文本壳：`apps/android`
- 角色原则：`characters/hiori/SOUL.md`（初始化写入独立 HomeDir，不是逐轮注入）
- Estuary 契约字段（只有 schema，未起服务）：`docs/estuary-event.schema.json`
- 当前 SDK pin **没有** 宿主上下文、审批回传、`ui_delta`。对应方法返回 `unsupported`。

相关记录：[Issue #1](https://github.com/yurika0211/Noema/issues/1)

## 运行后端

```bash
export NOEMA_API_KEY=...          # 模型供应商密钥
export NOEMA_BIND_KEY=...         # 必填。没有它 /v1/* 一律 401，loopback 也一样
export NOEMA_HOME=/var/lib/noema/owners/demo/characters/hiori/lucky-home
export NOEMA_STATE_DIR=/var/lib/noema/state   # 可选，默认 data/noema-state
GOWORK=off go run ./cmd/noema -listen 127.0.0.1:8787
```

`NOEMA_BIND_KEY` / `-api-key` 为空时 `-listen` 直接启动失败；运行时 `/v1/capabilities`、`/v1/ws` 返回 401，只有 `/healthz` 不鉴权。不要把 Origin 检查当鉴权，也不要为了本地方便留空 key。

模拟器默认连 `ws://10.0.2.2:8787/v1/ws`。真机用电脑的 LAN 地址。不要把 `NOEMA_HOME` 设成 `~/.luckyagent`，也不要把 `NOEMA_STATE_DIR` 放进 `NOEMA_HOME`（启动时会拒绝）。

只看能力面：

```bash
GOWORK=off go run ./cmd/noema -capabilities
```

## 事件日志

`internal/eventlog` 把可重放事件追加写到 `NOEMA_STATE_DIR/sessions/<session>.jsonl`：

- 每行一条记录，`seq` 在会话内单调递增，重启后从文件里的最大值继续，不从 0 开始。
- `owner + client_request_id` 的幂等记录一起落盘：相同输入复用同一 `run_id`，不同输入返回 `invalid request` 冲突。
- 重连只重放 `seq > last_seq` 的已持久事件，不会把同一句话打成新回合。
- `replay_complete` 是控制帧，不落盘、不占 `seq`：重连不会改写事件日志。
- 每条记录都是一个完整行；装载时末尾没有换行的崩溃残尾会被截掉，写入失败回退到上一个完整行且不推进 `seq`。
- 这是 Noema 自己的状态目录，不是 Lucky 会话 JSON，也不写进 Lucky HomeDir。

## Android

```bash
cd apps/android
cp local.properties.example local.properties   # 填 sdk.dir
./gradlew :app:testDebugUnitTest
```

客户端只消费 `noema.v1`。不接 `lh serve`，没有 Memory 图谱、Tasks、Skills、Cron。停止按钮才会取消当前回合。

`NOEMA_BIND_KEY` 存在 `EncryptedSharedPreferences`（`SettingsStore`），与后端是同一把 key；握手带 `Authorization: Bearer <key>`。

## 模拟器一轮（服务端与 Android 文本壳已过）

2026-11-04 已用真实 provider 和 Node WebSocket 对 `127.0.0.1:8788` 跑过 `noema.v1`（当时 `8787` 是别的服务）。同一天 Android 模拟器连上同一套协议，见下面「Android 模拟器」。

下面仍是再跑一遍时用的命令。

```bash
# 1. 后端（loopback 也要 key）
export NOEMA_API_KEY=...
export NOEMA_BIND_KEY=dev-loopback-key
export NOEMA_HOME=$PWD/data/lucky-home
export NOEMA_STATE_DIR=$PWD/data/noema-state
GOWORK=off go run ./cmd/noema -listen 127.0.0.1:8787

# 2. 探活与鉴权
curl -s localhost:8787/healthz                                       # ok
curl -s -o /dev/null -w '%{http_code}\n' localhost:8787/v1/capabilities                          # 401
curl -s -H "Authorization: Bearer $NOEMA_BIND_KEY" localhost:8787/v1/capabilities                # capability JSON

# 3. 模拟器
cd apps/android && ./gradlew :app:installDebug
# App 设置里填 ws://10.0.2.2:8787/v1/ws 和同一把 BIND_KEY
```

验收要点（服务端五条和 Android 一轮已记下；重启后续 `seq` 还没验）：

1. 发一条，正文流式出现。
2. 回合进行中 Stop，终态是 `cancelled`，不再补 `done`。
3. 杀 App / 断网重连后只重放已持久事件（`seq > last_seq`），同一句不会重跑。
4. 重启 `cmd/noema` 后重连同一个 session，`seq` 继续而不是归零。

### 已实测到哪一步（2026-11-04）

这一栏分开写，避免把没跑的当成跑过的。

**真跑过（真实二进制，不打 provider）**：

```bash
export NOEMA_HOME=/tmp/noema-probe/home
export NOEMA_STATE_DIR=/tmp/noema-probe/state
export NOEMA_BIND_KEY=probe-key-123
go build -mod=readonly -o /tmp/noema ./cmd/noema
/tmp/noema -listen 127.0.0.1:18787
```

| 请求 | 结果 |
| --- | --- |
| `GET /healthz`（无 key） | 200 |
| `GET /v1/capabilities`（无 key） | 401 |
| `GET /v1/capabilities`（错 key） | 401 |
| `GET /v1/capabilities`（正确 key） | 200 + capability JSON |
| `GET /v1/ws`（WebSocket 升级，无 key） | 401 |
| `-listen` 且未设 `NOEMA_BIND_KEY` | 启动即报错退出，不监听 |
| `NOEMA_STATE_DIR` | 生成 `sessions/` |

**真实 provider，Node WebSocket**（2026-11-04，`127.0.0.1:8788`。握手 `Authorization: Bearer`，与文档同一把 bind key；key 不写在这里）：

1. `GET /healthz` → 200 `ok`
2. `GET /v1/capabilities`：无 key、错 key → 401；对 key → 200。`text_stream` / `cancel_turn` / `isolated_home_dir` = true，其余 false
3. chat「用一两句话介绍你自己。不要用工具。」→ `content` #1–#13 后 `done` #14。自称灯織，安静、偏核对证据。`run=run_<session>_1`
4. 同一 session `replay last_seq=0` → 正文一致，`replay_complete` 不占 `seq`
5. 新 session「从 1 慢慢数到 80」，出现 `content` 后立刻 cancel → 含 `cancelled` #5，`done_count=0`，正文停在 1–7。这是回合进行中的 Stop，终态不是事后补上的 `cancelled`

**Android 模拟器**（2026-11-04，#8 关闭。`Medium_Phone_API_37.0`，adb `emulator-5554`，debug 包 `app.noema.android.debug`。后端仍是上面的 `127.0.0.1:8788`。App 填 `ws://10.0.2.2:8788/v1/ws`，BIND_KEY 用文档里的开发值，不写在这里）：

1. 空 key 连默认 `8787`：界面 `401 Unauthorized`，session 未分配。
2. 改成 8788 后点连接：`connected`，session `1793775645690726002`。
3. 发 `count slowly from 1 to 40`，出现「停止」就点。界面「已取消」，正文停在 28。
4. 这条 session 的状态日志：`content` #1–#15，随后 `cancelled` #16，`done_count=0`。几秒后仍没有事后 `done`。
5. 飞行模式断开再点「连接」：同一 session，日志仍是 1 个 run、16 条事件，没有新回合。

**还没跑**：重启 `cmd/noema` 后再连同一 session（`seq` 跨进程）。记忆、关系、写工具。

`internal/transport` 的 `TestRestartReplayAndIdempotentReconnect` 用 stub engine 走真实 TCP + 真实落盘，覆盖发送、`seq` 落盘、重启续接、重放与幂等。那条测试的 `cancel` 发在 `done` 之后。进行中取消的证据是上面第 5 条。

问题定位：

- 401：App 里的 key 与后端不一致，或后端没设 `NOEMA_BIND_KEY`。
- 连接被拒：模拟器用 `10.0.2.2`，真机用电脑 LAN 地址；真机场景后端要 `-listen 0.0.0.0:8787`，此时 key 更不能省。
- 重放为空：检查 `NOEMA_STATE_DIR/sessions/*.jsonl` 是否有内容，以及重连时 `last_seq` 是否已经是最新。
- 重启后续 `seq`、幂等、断线重放：看 `TestRestartReplayAndIdempotentReconnect`（stub）。2026-11-04 的真模型只覆盖了同一次进程里的正文流、`replay last_seq=0` 和进行中 cancel。

## CI

`.github/workflows/ci.yml`：

- Go 1.25、`GOWORK=off`、`-mod=readonly`：`go mod verify`、`go vet`、`go test`、`go test -race`、`go build`。
- Android：`./gradlew :app:testDebugUnitTest`（JVM unit test，无真机、无 instrumentation）。
- 两个 job 都不打真实模型，不需要 provider 或 `NOEMA_API_KEY`。

## 验证

```bash
GOWORK=off go test -mod=readonly ./...
GOWORK=off go test -mod=readonly -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -mod=readonly -o bin/noema ./cmd/noema

cd apps/android && ./gradlew :app:testDebugUnitTest
```

这些命令不调用真实模型。`-listen` 后的对话需要可用的 provider。