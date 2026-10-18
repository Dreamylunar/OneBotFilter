# bot 应用输出消息过滤器（out-message）— 设计文档

日期：2026-10-18
状态：已批准（待实现）

## 背景与问题

方向 B（bot 应用 → OneBot 客户端）目前只有账号 `group-id`/`user-id` 黑白名单过滤（见 `2026-10-18-event-id-filter-design.md`），**没有任何针对消息内容**的过滤。bot 应用发出的消息内容（出站 action 的 `params.message`）无法被中间层拦截或改写。

本设计新增一个「输出消息过滤器」，用于对 bot 应用发出的消息内容做正则过滤，并可选地给放行的消息加前缀。

## 术语

- **输入消息过滤器**：现有 `private-message` / `group-message`（及默认 `message`），作用于方向 A（OneBot 客户端 → bot 应用），见 `src/filters.go` 的 `Filter()`。
- **输出消息过滤器**：本设计新增的 `out-message`，作用于方向 B（bot 应用 → OneBot 客户端）。
- **ws 客户端 / bot 应用**：本程序拨号连接的一方（`WsClient`）。

## 目标

- 新增 per-bot-app 配置 `out-message`，与 `private-message`/`group-message` 并列，**单一配置，不区分私聊/群聊**。
- 支持四种模式 `on` / `off` / `whitelist` / `blacklist` 与正则表达式，语义与输入消息过滤器一致。
- 新增可选 `prefix`（**单个字符串**）：对**放行**的输出消息，在消息最前面拼接该前缀（与输入的 `prefix`/`prefix-replace` 语义不同——输入是「前缀绕过 + 替换」，此处是「拼接」）。
- 与账号黑白名单过滤**共存**：先查 id 黑白名单，再查消息内容。

## 非目标

- 不处理 `send_forward_msg` / `send_group_forward_msg` 的 `params.messages`（转发节点结构）。
- 不处理 bot 应用发来的 `post_type=message`/`notice` 等事件。
- `raw_message` 不参与匹配（OneBot v11 出站 action 的 `params` 中并无该字段；仅使用 `params.message`）。
- 不改变方向 A 的现有过滤逻辑。

## 详细设计

### 1. 配置（`src/config.go`、`config-example.yaml`）

新增类型：

```go
type OutMessageConfig struct {
	Mode    string   `mapstructure:"mode" yaml:"mode"`       // on / off / whitelist / blacklist；留空 = on
	Filters []string `mapstructure:"filters" yaml:"filters"` // 正则
	Prefix  string   `mapstructure:"prefix" yaml:"prefix"`   // 可选，加在放行消息最前面的前缀
}
```

`BotAppsConfig` 新增（与 `PrivateMessage` / `GroupMessage` 并列）：

```go
OutMessage OutMessageConfig `mapstructure:"out-message" yaml:"out-message"`
```

`(*BotAppsConfig).Check()` 新增校验：

```go
switch bac.OutMessage.Mode {
case "", ON, OFF, WHITELIST, BLACKLIST:
	// ok
default:
	return fmt.Errorf("%s.out-message.mode配置错误，只能是 on、off、whitelist 或 blacklist", bac.Name)
}
```

`config-example.yaml` 在 CASE 2 示例中补充（与 `private-message`/`group-message` 同级、4 空格缩进）：

```yaml
    out-message: # 对bot应用发往OneBot客户端的消息内容进行过滤
      mode: "blacklist" # 只能是on、off、whitelist或blacklist，留空等同于on
      filters: [ "广告", "\\d{6,}" ] # 使用正则匹配消息内容
      prefix: "【Bot】" # 可选。放行的输出消息会在最前面拼接此前缀（不是前缀替换）
```

### 2. 过滤器（`src/filters.go`）

新增结构与方法：

```go
type OutMessageFilter struct {
	OutMessageConfig
	Regexps []*regexp.Regexp // 编译后的正则
}

func (f *OutMessageFilter) Compile(cfg OutMessageConfig)
```

`Filter` 结构新增字段：`OutMessage OutMessageFilter`；`(*Filter).Compile` 中调用 `f.OutMessage.Compile(cfg.OutMessage)`。

**抽取共享正则判定函数**（避免与输入过滤器重复逻辑）。把现有 `(*MessageFilter).processFilter` 的正则匹配循环抽为自由函数，供输入/输出过滤器共用：

```go
// matchText 对单个文本段执行过滤器正则；返回 nil 表示未命中。
// 命中时：whitelist 返回 &TRUE，blacklist 返回 &FALSE。
func matchText(name, mode, rawMessage, text string, regexps []*regexp.Regexp) *bool
```

`(*MessageFilter).processFilter` 改为调用 `matchText`；`OutMessageFilter` 也使用它。

输出过滤器的模式判定（无「前缀绕过」，前缀只做拼接）：

```go
// allowOutMessage 依据模式与正则判断一组文本是否放行。
func (f *OutMessageFilter) allowOutMessage(name, rawMessage string, texts []string) bool
```

逻辑：

- `""` / `on` → 放行。
- `off` → 拦截。
- `whitelist` → 任一文本段命中正则 → 放行，否则拦截。
- `blacklist` → 任一文本段命中正则 → 拦截，否则放行。
- 其它 → 放行（配置异常时宽松处理，与现有风格一致）。

### 3. 方向 B 处理入口（`src/filters.go` + `src/client.go`）

用单一入口替换现有 `FilterFromBotApp`：

```go
// ProcessFromBotApp 处理bot应用发往OneBot客户端的数据。
// 返回处理后的完整数据与是否放行；不放行时 out 为 nil。
func (f *Filter) ProcessFromBotApp(raw []byte) (out []byte, pass bool)
```

逻辑：

1. 解析顶层 + `params` 的 `group_id`/`user_id`（`ParseOutboundIds`）；`ok && !f.AllowIds(g,u)` → `return nil, false`。
2. 非 JSON 对象（`!ok`）→ `return raw, true`。
3. 取 `params.message`；无 `params` / 无 `message` → `return raw, true`（如查询类 action）。
4. 解析 `params.message` 格式：
   - 字符串：`texts = []string{message}`。
   - 数组：`texts` 为其中 `type=="text"` 段的 `data.text`。
   - 其它/无法解析 → `return raw, true`（不处理）。
5. 内容过滤：`f.OutMessage.allowOutMessage(...)` 为 false → `return nil, false`。
6. 放行且 `OutMessage.Prefix != ""` → 拼接前缀：
   - 字符串形式：`message = Prefix + message`。
   - 数组形式：在最前面插入 `MessageContent{Type: "text", Data: {"text": Prefix}}`。
   - 回写 `params["message"]`、`obj["params"]`，`json.Marshal(obj)` 得到 `out`。
   - 无前缀 → `out = raw`。
7. `return out, true`。

`(*WsClient).readLoop`（`src/client.go`）改为：

```go
case msg := <-wc.readChan:
	if msg.MsgType == websocket.TextMessage {
		out, pass := wc.filter.ProcessFromBotApp(msg.MsgData)
		if !pass {
			// 被拦截：回传带原echo的失败响应
			if resp, ok := blockedResponse(msg.MsgData); ok {
				wc.write(websocket.TextMessage, resp)
			}
			continue
		}
		if err := wss.WriteMessage(msg.MsgType, out); err != nil {
			log.Println("写入到OneBot客户端出错：", err)
		}
		continue
	}
	if err := wss.WriteMessage(msg.MsgType, msg.MsgData); err != nil {
		log.Println("写入到OneBot客户端出错：", err)
	}
```

现有 `FilterFromBotApp` 方法及其测试 `TestFilterFromBotApp` 迁移为 `ProcessFromBotApp` / `TestProcessFromBotApp`（断言 `pass`，并在需要时检查 `out`）。

### 4. 日志（`(*Filter).String`）

在 `String()` 中补充 `out-message` 一行（模式、正则、前缀），便于重载时查看。

## 边界与行为

| 情况 | 行为 |
| --- | --- |
| 出站帧命中 id 黑名单/白名单外 | 拦截（失败响应） |
| `params.message` 命中内容过滤 | 拦截（失败响应） |
| 放行且有 `prefix` | 拼接前缀后转发 |
| 查询类 action（无 `params.message`） | 放行（仅受 id 约束），不加前缀 |
| 非 JSON 帧 | 放行 |
| `params.message` 既非 string 也非 array | 放行，不加前缀 |

## 测试计划（`src/*_test.go`，`go test ./...`）

- `ProcessFromBotApp`：
  - id 黑名单群/用户 → `pass=false`（含 `params` 内目标）。
  - `out-message.mode=off` → 拦截。
  - `whitelist`：命中放行、未命中拦截。
  - `blacklist`：命中拦截、未命中放行。
  - `prefix`（string 形式）：放行消息前缀被拼接。
  - `prefix`（array 形式）：最前面插入 text 前缀段。
  - 非 JSON、无 `params.message` → 放行且 `out==raw`。
- `Check()`：`out-message.mode` 非法值报错、合法值通过。
- 现有测试保持通过。

## 影响面

- 行为变更：方向 B 出站消息新增内容过滤与可选前缀拼接（默认 `mode` 为空 = 不过滤，行为与现状一致，向后兼容）。
- 新增可选取配置项 `out-message`，未配置时不影响既有功能。
- 复用已实现的失败响应机制（`blockedResponse` / `RETCODE_BLOCKED`）。
