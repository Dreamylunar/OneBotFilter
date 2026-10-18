# 事件与 ws 客户端数据的账号黑白名单过滤 — 设计文档

日期：2026-10-18
状态：已批准（待实现）

## 背景与问题

当前程序只对「消息事件」套用账号黑白名单与内容过滤器，存在两处漏放：

1. **方向 A（OneBot 客户端 → bot 应用）**
   `src/client.go` 的 `writeLoop` 中，只有能被 `ParseOneBotMessage` 解析、且 `raw_message` 非空的消息事件会经过 `Filter()`。
   其余数据（`notice`/`request`/`meta_event` 等非消息事件，以及解析失败/`raw_message` 为空的消息）在 `src/client.go:149` 被「直接放行」，导致来自黑名单群/白名单外群聊、以及黑名单/白名单外私聊用户的事件仍会转发给 bot 应用。

2. **方向 B（bot 应用 → OneBot 客户端）**
   `src/client.go` 的 `readLoop` 对 bot 应用发出的数据**完全不过滤**，直接写入 OneBot 客户端。bot 应用发往黑名单群/白名单外群聊、或黑名单/白名单外私聊用户的数据（尤其是 `send_group_msg` 等 API action）不会被拦截。

## 术语

- **OneBot 客户端**：通过反向 ws 连接本程序的一方（`WsServer.Conn`）。
- **ws 客户端 / bot 应用**：本程序作为客户端拨号连接的一方（`WsClient`）。

## 目标

- 方向 A：非消息事件也要经过 `group-id` / `user-id` 账号黑白名单过滤。
- 方向 B：bot 应用发出的数据也要经过 `group-id` / `user-id` 账号黑白名单过滤，包含 API action 的目标（`params.group_id` / `params.user_id`）。
- 新增 per-bot-app 配置项 `pass-no-id-event`，控制方向 A 中「不含 id 信息的数据」是否通过，默认 `true`。

## 非目标

- 方向 B 不套用消息内容过滤器（`filters` / `prefix` / `mode`）—— bot 应用的出站数据只做账号黑白名单检查。
- 不新增除 `pass-no-id-event` 外的配置。
- 不改动消息内容过滤器（`Filter()`）的行为。

## 详细设计

### 1. 配置（`src/config.go`、`config-example.yaml`）

`BotAppsConfig` 新增字段：

```go
PassNoIdEvent *bool `mapstructure:"pass-no-id-event" yaml:"pass-no-id-event"`
```

- 使用 `*bool` 以区分「未配置」与「显式 false」。
- 在 `(*BotAppsConfig).Check()` 中：若为 `nil`，默认置为 `true`。
- 该开关**仅作用于方向 A**。

`config-example.yaml` 在 `bot-apps` 的示例中补充说明性注释项，例如：

```yaml
pass-no-id-event: true # 是否通过不含group_id/user_id的数据（如meta_event、action响应），默认true
```

### 2. ID 提取（`src/onebotMessage.go`）

```go
// ParseIds 解析顶层 group_id / user_id。ok=false 表示 raw 不是 JSON 对象。
func ParseIds(raw []byte) (groupId, userId int64, ok bool)

// ParseOutboundIds 解析顶层 group_id / user_id，若存在 params 则额外解析 params.group_id / params.user_id
// （顶层优先）。ok=false 表示 raw 不是 JSON 对象。
func ParseOutboundIds(raw []byte) (groupId, userId int64, ok bool)
```

实现要点：

- 先用 `json.Unmarshal` 到 `map[string]json.RawMessage`，失败（数组/标量/非 JSON）时返回 `ok=false`。
- 顶层字段解析失败或不存在时视为 0。
- `ParseOutboundIds` 中 `params` 解析失败时忽略 `params`。

### 3. 过滤器（`src/filters.go`）

`Filter` 新增字段并在 `Compile` 中赋值：

```go
type Filter struct {
    // ...现有字段...
    PassNoIdEvent bool
}

// Compile 内：
f.PassNoIdEvent = true
if cfg.PassNoIdEvent != nil {
    f.PassNoIdEvent = *cfg.PassNoIdEvent
}
```

新增方法：

```go
// AllowIds 检查群/用户黑白名单；id 为 0 时跳过该项检查。
func (f *Filter) AllowIds(groupId, userId int64) bool

// FilterEventFromOneBot 处理方向 A 中的非消息数据，返回是否放行。
func (f *Filter) FilterEventFromOneBot(raw []byte) bool

// FilterFromBotApp 处理方向 B 中 bot 应用发出的数据，返回是否放行。
func (f *Filter) FilterFromBotApp(raw []byte) bool
```

`FilterEventFromOneBot` 逻辑：

1. `groupId, userId, ok := ParseIds(raw)`
2. `!ok` → 放行（无法判断，保持现有宽松行为）。
3. `groupId == 0 && userId == 0` → 返回 `f.PassNoIdEvent`。
4. 否则 → 返回 `f.AllowIds(groupId, userId)`。（私聊事件只有 `user_id`，因此同样套用 `user-id` 过滤器。）

`FilterFromBotApp` 逻辑：

1. `groupId, userId, ok := ParseOutboundIds(raw)`
2. `!ok` → 放行。
3. 返回 `f.AllowIds(groupId, userId)`（二者皆为 0 时自然放行，符合「方向 B 无 id 数据一律放行」）。

`AllowIds` 逻辑：`groupId != 0 && !f.GroupId.Filter(groupId)` → false；`userId != 0 && !f.UserId.Filter(userId)` → false；否则 true。Debug 模式下打印被拦截的原因。

### 4. 收发循环（`src/client.go`）

**方向 A — `writeLoop`**

```go
case msg := <-wc.writeChan:
    if msg.MsgType == websocket.TextMessage {
        // 消息事件：完整过滤器（账号黑白名单 + 内容过滤）
        if m := ParseOneBotMessage(msg.MsgData); m != nil && m.Partial.RawMessage != "" {
            if wc.filter.Filter(m) {
                wc.conn.WriteJSON(m.Intact)
            }
            continue
        }
        // 非消息事件 / 解析失败：仅账号黑白名单
        if !wc.filter.FilterEventFromOneBot(msg.MsgData) {
            continue
        }
        wc.conn.WriteMessage(msg.MsgType, msg.MsgData)
        continue
    }
    // 非文本消息直接放行
    wc.conn.WriteMessage(msg.MsgType, msg.MsgData)
```

**方向 B — `readLoop`**

```go
case msg := <-wc.readChan:
    if msg.MsgType == websocket.TextMessage && !wc.filter.FilterFromBotApp(msg.MsgData) {
        continue
    }
    wss.WriteMessage(msg.MsgType, msg.MsgData)
```

> 注意：`readLoop` 在 `client.go` 中已能访问 `wc.filter`（同为 `WsClient` 方法）。

## 边界与行为

| 情况 | 方向 A | 方向 B |
| --- | --- | --- |
| 消息事件（可解析、有 `raw_message`） | 完整 `Filter()`（id + 内容） | 仅 id |
| `notice`/`request` 等事件（含 id） | `AllowIds` | 仅 id |
| 私聊事件（仅 `user_id`） | `user-id` 过滤器 | 仅 id |
| `meta_event` / action 响应等无 id 数据 | 由 `pass-no-id-event` 决定 | 放行 |
| 非 JSON 文本帧 | 放行 | 放行 |

## 测试计划（`src/*_test.go`，`go test ./src/...`）

- `ParseIds` / `ParseOutboundIds`：顶层、`params` 内、缺失、非 JSON、数组。
- `AllowIds`：黑名单命中、白名单未命中、id 为 0 跳过。
- `FilterEventFromOneBot`：群事件被拦截、私聊事件按 `user-id` 拦截、无 id 数据受 `PassNoIdEvent` 控制。
- `FilterFromBotApp`：`send_group_msg`（`params.group_id`）被拦截、无 id action 放行。
- 可选：`Config.Check()` 对 `pass-no-id-event` 的默认值行为。

## 影响面

- 行为变更：方向 B 由「完全不过滤」变为「按账号黑白名单过滤」；方向 A 非消息事件由「直接放行」变为「按账号黑白名单过滤」。
- 向后兼容：`pass-no-id-event` 默认 `true`，未配置的既有配置文件行为尽量保持不变（仅不再放行命中黑名单/白名单外群或用户的事件/出站数据）。
