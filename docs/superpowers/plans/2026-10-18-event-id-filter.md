# 事件与 ws 客户端数据的账号黑白名单过滤 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让方向 A（OneBot 客户端 → bot 应用）的非消息事件、方向 B（bot 应用 → OneBot 客户端）的全部出站数据都经过 `group-id`/`user-id` 账号黑白名单过滤，并新增 `pass-no-id-event` 配置。

**Architecture:** 在 `Filter` 上新增账号黑白名单通用方法与两个方向的事件过滤入口；从 JSON 文本帧中提取 `group_id`/`user_id`（方向 B 额外解析 `params`）；在 `client.go` 的两条收发循环里调用。所有改动限定在 `src/`，不改变现有消息内容过滤器逻辑。

**Tech Stack:** Go 1.25.1，标准库 `encoding/json` + `testing`，`github.com/gorilla/websocket`。

## Global Constraints

- 模块名 `onebotfiler`，包名 `onebotfilter`（注意拼写差异，保持不变）。
- 新增代码注释与日志文案使用中文，风格与既有代码一致。
- 测试命令：`go test ./...`；构建命令：`go build ./...`。
- **本项目不允许自行提交 git**：所有「提交」步骤改为「给出提交消息建议，由用户审查后手动提交」。不要执行 `git commit`。
- 仅改动 `src/` 与 `config-example.yaml`，不引入新依赖。

---

### Task 1: ID 提取工具

**Files:**
- Modify: `src/onebotMessage.go`
- Test: `src/onebotMessage_test.go`（新建）

**Interfaces:**
- Produces:
  - `func ParseIds(raw []byte) (groupId, userId int64, ok bool)` — 顶层提取。
  - `func ParseOutboundIds(raw []byte) (groupId, userId int64, ok bool)` — 顶层 + `params` 内提取。

- [ ] **Step 1: 写失败测试**

新建 `src/onebotMessage_test.go`：

```go
package onebotfilter

import "testing"

func TestParseIds(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		groupId int64
		userId  int64
		ok      bool
	}{
		{"group event", `{"group_id":123,"user_id":456}`, 123, 456, true},
		{"private event", `{"user_id":456}`, 0, 456, true},
		{"meta event no id", `{"post_type":"meta_event"}`, 0, 0, true},
		{"empty object", `{}`, 0, 0, true},
		{"array", `[1,2,3]`, 0, 0, false},
		{"not json", `hello`, 0, 0, false},
		{"null", `null`, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, u, ok := ParseIds([]byte(c.raw))
			if g != c.groupId || u != c.userId || ok != c.ok {
				t.Fatalf("ParseIds(%s) = (%d,%d,%v), want (%d,%d,%v)", c.raw, g, u, ok, c.groupId, c.userId, c.ok)
			}
		})
	}
}

func TestParseOutboundIds(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		groupId int64
		userId  int64
		ok      bool
	}{
		{"action params group", `{"action":"send_group_msg","params":{"group_id":123}}`, 123, 0, true},
		{"action params user", `{"action":"send_private_msg","params":{"user_id":456}}`, 0, 456, true},
		{"top level wins", `{"group_id":1,"params":{"group_id":2}}`, 1, 0, true},
		{"no id action", `{"action":"get_login_info","params":{}}`, 0, 0, true},
		{"not json", `xx`, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, u, ok := ParseOutboundIds([]byte(c.raw))
			if g != c.groupId || u != c.userId || ok != c.ok {
				t.Fatalf("ParseOutboundIds(%s) = (%d,%d,%v), want (%d,%d,%v)", c.raw, g, u, ok, c.groupId, c.userId, c.ok)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run 'TestParseIds|TestParseOutboundIds' -v`
Expected: FAIL/编译错误，`undefined: ParseIds`。

- [ ] **Step 3: 最小实现**

在 `src/onebotMessage.go` 末尾追加：

```go
// ParseIds 解析顶层 group_id / user_id。ok=false 表示 raw 不是 JSON 对象。
func ParseIds(raw []byte) (groupId, userId int64, ok bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return 0, 0, false
	}
	groupId, userId = decodeIds(obj)
	return groupId, userId, true
}

// ParseOutboundIds 解析顶层 group_id / user_id，若存在 params 则额外解析 params 内的同名字段（顶层优先）。
func ParseOutboundIds(raw []byte) (groupId, userId int64, ok bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return 0, 0, false
	}
	groupId, userId = decodeIds(obj)
	if paramsRaw, exists := obj["params"]; exists {
		var params map[string]json.RawMessage
		if json.Unmarshal(paramsRaw, &params) == nil {
			g, u := decodeIds(params)
			if groupId == 0 {
				groupId = g
			}
			if userId == 0 {
				userId = u
			}
		}
	}
	return groupId, userId, true
}

// decodeIds 从 JSON 对象中解析 group_id / user_id，缺失或类型不符时为 0。
func decodeIds(obj map[string]json.RawMessage) (groupId, userId int64) {
	if v, exists := obj["group_id"]; exists {
		_ = json.Unmarshal(v, &groupId)
	}
	if v, exists := obj["user_id"]; exists {
		_ = json.Unmarshal(v, &userId)
	}
	return
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run 'TestParseIds|TestParseOutboundIds' -v`
Expected: PASS。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: 添加从JSON帧提取group_id/user_id的工具函数`（由用户手动提交）。

---

### Task 2: 新增 `pass-no-id-event` 配置项

**Files:**
- Modify: `src/config.go`（`BotAppsConfig` 结构体，及 `(*BotAppsConfig).Check()`）
- Modify: `config-example.yaml`
- Test: `src/config_test.go`（新建）

**Interfaces:**
- Produces: `BotAppsConfig.PassNoIdEvent *bool`，`Check()` 后保证非 nil。

- [ ] **Step 1: 写失败测试**

新建 `src/config_test.go`：

```go
package onebotfilter

import "testing"

func TestPassNoIdEventDefault(t *testing.T) {
	bac := BotAppsConfig{Name: "x", Uri: "ws://x"}
	if err := bac.Check(); err != nil {
		t.Fatal(err)
	}
	if bac.PassNoIdEvent == nil || !*bac.PassNoIdEvent {
		t.Fatalf("未配置时 pass-no-id-event 默认应为 true，得到 %v", bac.PassNoIdEvent)
	}

	f := false
	bac2 := BotAppsConfig{Name: "y", Uri: "ws://y", PassNoIdEvent: &f}
	if err := bac2.Check(); err != nil {
		t.Fatal(err)
	}
	if bac2.PassNoIdEvent == nil || *bac2.PassNoIdEvent {
		t.Fatalf("显式配置 false 应保留，得到 %v", bac2.PassNoIdEvent)
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run TestPassNoIdEventDefault -v`
Expected: FAIL/编译错误，`unknown field PassNoIdEvent`。

- [ ] **Step 3: 最小实现**

在 `src/config.go` 的 `BotAppsConfig` 结构中（`Message` 字段之后）新增：

```go
	// 是否通过不含group_id/user_id的数据（如meta_event、action响应）。
	// 仅作用于 OneBot客户端→bot应用 方向；未配置时默认为 true。
	PassNoIdEvent *bool `mapstructure:"pass-no-id-event" yaml:"pass-no-id-event"`
```

在 `(*BotAppsConfig).Check()` 的 `return nil` 之前新增：

```go
	if bac.PassNoIdEvent == nil {
		pass := true
		bac.PassNoIdEvent = &pass
	}
```

在 `config-example.yaml` 的 CASE 2（`bot2`）中，`group-id` 段之后补充：

```yaml
    pass-no-id-event: true # 是否通过不含group_id/user_id的数据（如meta_event、action响应），默认true
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run TestPassNoIdEventDefault -v`
Expected: PASS。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: 新增pass-no-id-event配置项，默认通过无id数据`（由用户手动提交）。

---

### Task 3: Filter 账号黑白名单方法与 Compile 接入

**Files:**
- Modify: `src/filters.go`（`Filter` 结构体、`Compile`、新增方法）
- Test: `src/filters_test.go`（新建）

**Interfaces:**
- Consumes: `ParseIds`、`ParseOutboundIds`（Task 1）；`BotAppsConfig.PassNoIdEvent`（Task 2）。
- Produces:
  - `Filter.PassNoIdEvent bool`
  - `func (f *Filter) AllowIds(groupId, userId int64) bool`
  - `func (f *Filter) FilterEventFromOneBot(raw []byte) bool`
  - `func (f *Filter) FilterFromBotApp(raw []byte) bool`

- [ ] **Step 1: 写失败测试**

新建 `src/filters_test.go`：

```go
package onebotfilter

import "testing"

func TestFilterEventFromOneBot(t *testing.T) {
	f := &Filter{
		Name:          "t",
		GroupId:       IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{123}}},
		UserId:        IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{456}}},
		PassNoIdEvent: true,
	}
	if f.FilterEventFromOneBot([]byte(`{"group_id":123,"user_id":999}`)) {
		t.Fatal("黑名单群的事件应被拦截")
	}
	if f.FilterEventFromOneBot([]byte(`{"user_id":456}`)) {
		t.Fatal("黑名单用户的私聊事件应被拦截")
	}
	if !f.FilterEventFromOneBot([]byte(`{"group_id":999,"user_id":998}`)) {
		t.Fatal("通过黑白名单的事件应放行")
	}
	if !f.FilterEventFromOneBot([]byte(`{"post_type":"meta_event"}`)) {
		t.Fatal("pass-no-id-event=true 时无id事件应放行")
	}
	f.PassNoIdEvent = false
	if f.FilterEventFromOneBot([]byte(`{"post_type":"meta_event"}`)) {
		t.Fatal("pass-no-id-event=false 时无id事件应被拦截")
	}
	if !f.FilterEventFromOneBot([]byte(`not json`)) {
		t.Fatal("非JSON数据应放行")
	}
}

func TestFilterWhitelist(t *testing.T) {
	f := &Filter{
		Name:          "w",
		GroupId:       IdFilter{IdConfig{Mode: WHITELIST, Ids: []int64{123}}},
		UserId:        IdFilter{IdConfig{Mode: WHITELIST, Ids: []int64{456}}},
		PassNoIdEvent: true,
	}
	if f.FilterEventFromOneBot([]byte(`{"group_id":999}`)) {
		t.Fatal("白名单外的群事件应被拦截")
	}
	if !f.FilterEventFromOneBot([]byte(`{"group_id":123}`)) {
		t.Fatal("白名单内的群事件应放行")
	}
}

func TestFilterFromBotApp(t *testing.T) {
	f := &Filter{
		Name:          "t",
		GroupId:       IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{123}}},
		UserId:        IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{456}}},
		PassNoIdEvent: true,
	}
	if f.FilterFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":123}}`)) {
		t.Fatal("发往黑名单群的action应被拦截")
	}
	if f.FilterFromBotApp([]byte(`{"action":"send_private_msg","params":{"user_id":456}}`)) {
		t.Fatal("发往黑名单用户的action应被拦截")
	}
	if !f.FilterFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":999}}`)) {
		t.Fatal("发往合法群的action应放行")
	}
	if !f.FilterFromBotApp([]byte(`{"action":"get_login_info"}`)) {
		t.Fatal("无id的action应放行")
	}
	if !f.FilterFromBotApp([]byte(`not json`)) {
		t.Fatal("非JSON数据应放行")
	}
}

func TestCompilePassNoIdEvent(t *testing.T) {
	tr := true
	f := (&Filter{Name: "c"}).Compile(BotAppsConfig{Name: "c", PassNoIdEvent: &tr})
	if !f.PassNoIdEvent {
		t.Fatal("Compile 应使用配置的 pass-no-id-event")
	}
	f2 := (&Filter{Name: "c2"}).Compile(BotAppsConfig{Name: "c2"})
	if !f2.PassNoIdEvent {
		t.Fatal("Compile 在配置为 nil 时应默认 true")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run 'TestFilter|TestCompilePassNoIdEvent' -v`
Expected: FAIL/编译错误，`unknown field PassNoIdEvent` / `undefined: FilterEventFromOneBot`。

- [ ] **Step 3: 最小实现**

在 `src/filters.go` 的 `Filter` 结构中新增字段：

```go
	PassNoIdEvent bool // 是否通过不含id的数据（仅方向A）
```

修改 `(*Filter).Compile` 为：

```go
func (f *Filter) Compile(cfg BotAppsConfig) *Filter {
	f.Name = cfg.Name
	f.UserId = IdFilter{cfg.UserId}
	f.GroupId = IdFilter{cfg.GroupId}
	f.PrivateMessage.Compile(cfg.PrivateMessage)
	f.GroupMessage.Compile(cfg.GroupMessage)
	f.PassNoIdEvent = true
	if cfg.PassNoIdEvent != nil {
		f.PassNoIdEvent = *cfg.PassNoIdEvent
	}
	return f
}
```

在 `src/filters.go` 中新增方法：

```go
// AllowIds 检查群/用户黑白名单；id 为 0 时跳过该项检查。
func (f *Filter) AllowIds(groupId, userId int64) bool {
	if groupId != 0 && !f.GroupId.Filter(groupId) {
		if CONFIG.Server.Debug {
			log.Printf("%s：群 %d 被账号黑白名单拦截\n", f.Name, groupId)
		}
		return false
	}
	if userId != 0 && !f.UserId.Filter(userId) {
		if CONFIG.Server.Debug {
			log.Printf("%s：QQ %d 被账号黑白名单拦截\n", f.Name, userId)
		}
		return false
	}
	return true
}

// FilterEventFromOneBot 处理方向A中的非消息数据，返回是否放行。
func (f *Filter) FilterEventFromOneBot(raw []byte) bool {
	groupId, userId, ok := ParseIds(raw)
	if !ok {
		return true
	}
	if groupId == 0 && userId == 0 {
		return f.PassNoIdEvent
	}
	return f.AllowIds(groupId, userId)
}

// FilterFromBotApp 处理方向B中bot应用发出的数据，返回是否放行。
func (f *Filter) FilterFromBotApp(raw []byte) bool {
	groupId, userId, ok := ParseOutboundIds(raw)
	if !ok {
		return true
	}
	return f.AllowIds(groupId, userId)
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run 'TestFilter|TestCompilePassNoIdEvent' -v`
Expected: PASS。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: Filter新增账号黑白名单方法并接入pass-no-id-event`（由用户手动提交）。

---

### Task 4: 接入收发循环

**Files:**
- Modify: `src/client.go`（`writeLoop` 与 `readLoop`）

**Interfaces:**
- Consumes: `Filter.FilterEventFromOneBot`、`Filter.FilterFromBotApp`（Task 3）。

- [ ] **Step 1: 改写方向 A —— `(*WsClient).writeLoop`**

用以下实现整体替换 `src/client.go` 中的 `writeLoop` 函数体：

```go
// 处理发送给bot应用端的消息
func (wc *WsClient) writeLoop(ctx context.Context) {
	for {
		select {
		case msg := <-wc.writeChan:
			if msg.MsgType == websocket.TextMessage {
				// 能解析为消息事件且有 raw_message：走完整过滤器（账号黑白名单 + 内容过滤）
				if onebotMessage := ParseOneBotMessage(msg.MsgData); onebotMessage != nil && onebotMessage.Partial.RawMessage != "" {
					if wc.filter.Filter(onebotMessage) {
						if err := wc.conn.WriteJSON(onebotMessage.Intact); err != nil {
							log.Printf("向%s发送消息出错：%v\n", wc.Name, err)
						}
					}
					continue
				}
				// 非消息事件或解析失败：仅走账号黑白名单过滤
				if !wc.filter.FilterEventFromOneBot(msg.MsgData) {
					continue
				}
				if err := wc.conn.WriteMessage(msg.MsgType, msg.MsgData); err != nil {
					log.Printf("向%s发送消息出错：%v\n", wc.Name, err)
				}
				continue
			}
			// 非文本消息直接放行
			if err := wc.conn.WriteMessage(msg.MsgType, msg.MsgData); err != nil {
				log.Printf("向%s发送消息出错：%v\n", wc.Name, err)
			}
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 2: 改写方向 B —— `(*WsClient).readLoop`**

用以下实现整体替换 `src/client.go` 中的 `readLoop` 函数体：

```go
// 处理从bot应用端读取的消息
func (wc *WsClient) readLoop(ctx context.Context, wss *WsServer) {
	for {
		select {
		case msg := <-wc.readChan:
			// bot应用发往OneBot客户端的数据也要经过账号黑白名单过滤
			if msg.MsgType == websocket.TextMessage && !wc.filter.FilterFromBotApp(msg.MsgData) {
				continue
			}
			//转发给OneBot客户端
			if err := wss.WriteMessage(msg.MsgType, msg.MsgData); err != nil {
				log.Println("写入到OneBot客户端出错：", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 3: 构建与全量测试**

Run: `go build ./...`
Expected: 无输出（成功）。

Run: `go test ./...`
Expected: PASS（Task 1~3 的全部测试）。

- [ ] **Step 4: 提交（手动）**

建议提交消息：`feat: 收发循环接入事件与出站数据的账号黑白名单过滤`（由用户手动提交）。

---

## Self-Review

- **Spec coverage:** 方向 A 非消息事件过滤 → Task 3 `FilterEventFromOneBot` + Task 4 Step 1；私聊事件走 user-id → `AllowIds` 同时检查 user；方向 B（含 action 的 `params` 目标）→ Task 1 `ParseOutboundIds` + Task 3 `FilterFromBotApp` + Task 4 Step 2；`pass-no-id-event` 配置（仅方向 A、默认 true）→ Task 2 + Task 3 `Compile`。全部覆盖。
- **Placeholder scan:** 无 TODO/TBD，所有步骤含可执行代码与命令。
- **Type consistency:** `ParseIds`/`ParseOutboundIds` 签名、`AllowIds`/`FilterEventFromOneBot`/`FilterFromBotApp` 名称、`PassNoIdEvent *bool`/`bool`（配置为指针、Filter 为值）在 Task 1~4 中一致。
