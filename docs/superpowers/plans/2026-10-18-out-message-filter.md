# bot 应用输出消息过滤器（out-message）— 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为方向 B（bot 应用 → OneBot 客户端）新增 `out-message` 内容过滤器：四种模式 + 正则，可选前缀拼接，与账号黑白名单过滤共存。

**Architecture:** 新增 `OutMessageConfig`/`OutMessageFilter`；把 `Filter.processFilter` 的正则匹配抽为共享函数 `matchText`；用 `Filter.ProcessFromBotApp(raw) (out, pass)` 统一方向 B 处理（id → 内容 → 前缀）；`readLoop` 改为调用它并复用 `blockedResponse`。

**Tech Stack:** Go 1.25.1，标准库 `encoding/json` + `testing`，`github.com/gorilla/websocket`，`github.com/dlclark/regexp2`。

## Global Constraints

- 模块名 `onebotfiler`，包名 `onebotfilter`（拼写差异保持不变）。
- 新增代码注释与日志文案使用中文，风格与既有代码一致。
- 测试 `go test ./...`；构建 `go build ./...`；静态检查 `go vet ./...`。
- **本项目不允许自行提交 git**：所有「提交」步骤改为「给出提交消息建议，由用户审查后手动提交」。不要执行 `git commit`。
- 仅改动 `src/` 与 `config-example.yaml`，不引入新依赖。

---

### Task 1: `out-message` 配置与校验

**Files:**
- Modify: `src/config.go`
- Modify: `config-example.yaml`
- Test: `src/config_test.go`

**Interfaces:**
- Produces: `type OutMessageConfig struct { Mode string; Filters []string; Prefix string }`；`BotAppsConfig.OutMessage OutMessageConfig`。

- [ ] **Step 1: 写失败测试**

在 `src/config_test.go` 末尾追加：

```go
func TestOutMessageModeCheck(t *testing.T) {
	for _, mode := range []string{"", ON, OFF, WHITELIST, BLACKLIST} {
		bac := BotAppsConfig{Name: "x", Uri: "ws://x"}
		bac.OutMessage.Mode = mode
		if err := bac.Check(); err != nil {
			t.Fatalf("mode=%q 应通过校验，得到 %v", mode, err)
		}
	}
	bac := BotAppsConfig{Name: "x", Uri: "ws://x"}
	bac.OutMessage.Mode = "nonsense"
	if err := bac.Check(); err == nil {
		t.Fatal("非法 out-message.mode 应报错")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run TestOutMessageModeCheck -v`
Expected: FAIL/编译错误，`bac.OutMessage` 未定义。

- [ ] **Step 3: 最小实现**

在 `src/config.go` 的 `MessageConfig` 定义之后新增：

```go
type OutMessageConfig struct {
	Mode    string   `mapstructure:"mode" yaml:"mode"`       // on / off / whitelist / blacklist；留空 = on
	Filters []string `mapstructure:"filters" yaml:"filters"` // 正则
	Prefix  string   `mapstructure:"prefix" yaml:"prefix"`   // 可选，加在放行消息最前面的前缀
}
```

在 `BotAppsConfig` 结构体中，`GroupMessage` 字段之后新增：

```go
	// 输出消息过滤器：对bot应用发往OneBot客户端的消息内容进行过滤（不区分私聊/群聊）
	OutMessage OutMessageConfig `mapstructure:"out-message" yaml:"out-message"`
```

在 `(*BotAppsConfig).Check()` 中，`PassNoIdEvent` 默认值处理之前新增：

```go
	switch bac.OutMessage.Mode {
	case "", ON, OFF, WHITELIST, BLACKLIST:
		// ok
	default:
		return fmt.Errorf("%s.out-message.mode配置错误，只能是 on、off、whitelist 或 blacklist", bac.Name)
	}
```

在 `config-example.yaml` 的 CASE 2（`bot2`）中，`group-message` 段之后补充：

```yaml
    out-message: # 对bot应用发往OneBot客户端的消息内容进行过滤
      mode: "blacklist" # 只能是on、off、whitelist或blacklist，留空等同于on
      filters: [ "广告", "\\d{6,}" ] # 使用正则匹配消息内容
      prefix: "【Bot】" # 可选。放行的输出消息会在最前面拼接此前缀（不是前缀替换）
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run TestOutMessageModeCheck -v`
Expected: PASS。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: 新增out-message配置与校验`（由用户手动提交）。

---

### Task 2: 抽取 `matchText` 并实现 `OutMessageFilter`

**Files:**
- Modify: `src/filters.go`
- Test: `src/filters_test.go`

**Interfaces:**
- Consumes: `OutMessageConfig`（Task 1）。
- Produces:
  - `func matchText(name, mode, rawMessage, text string, regexps []*regexp.Regexp) *bool`
  - `type OutMessageFilter struct { OutMessageConfig; Regexps []*regexp.Regexp }`
  - `func (f *OutMessageFilter) Compile(cfg OutMessageConfig)`
  - `func (f *OutMessageFilter) allowOutMessage(name, rawMessage string, texts []string) bool`
  - `Filter.OutMessage OutMessageFilter`

- [ ] **Step 1: 写失败测试**

在 `src/filters_test.go` 末尾追加（若文件未导入 `regexp2` 则无需，直接使用）：

```go
func TestOutMessageFilterAllow(t *testing.T) {
	f := &OutMessageFilter{}
	f.Compile(OutMessageConfig{Mode: BLACKLIST, Filters: []string{"广告"}})
	if f.allowOutMessage("t", "这是广告", []string{"这是广告"}) {
		t.Fatal("blacklist命中应拦截")
	}
	if !f.allowOutMessage("t", "正常", []string{"正常"}) {
		t.Fatal("blacklist未命中应放行")
	}
	f.Compile(OutMessageConfig{Mode: WHITELIST, Filters: []string{"允许"}})
	if !f.allowOutMessage("t", "允许内容", []string{"允许内容"}) {
		t.Fatal("whitelist命中应放行")
	}
	if f.allowOutMessage("t", "其他", []string{"其他"}) {
		t.Fatal("whitelist未命中应拦截")
	}
	f.Compile(OutMessageConfig{Mode: OFF})
	if f.allowOutMessage("t", "x", []string{"x"}) {
		t.Fatal("off应拦截")
	}
	f.Compile(OutMessageConfig{Mode: ON})
	if !f.allowOutMessage("t", "x", []string{"x"}) {
		t.Fatal("on应放行")
	}
	f.Compile(OutMessageConfig{})
	if !f.allowOutMessage("t", "x", []string{"x"}) {
		t.Fatal("空mode应放行")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run TestOutMessageFilterAllow -v`
Expected: FAIL/编译错误，`OutMessageFilter` 未定义。

- [ ] **Step 3: 最小实现**

在 `src/filters.go` 中：给 `Filter` 结构体新增字段：

```go
	OutMessage OutMessageFilter
```

新增 `OutMessageFilter` 类型与 `Compile`：

```go
// 输出消息过滤器
type OutMessageFilter struct {
	OutMessageConfig
	Regexps []*regexp.Regexp //编译后的正则表达式
}

func (f *OutMessageFilter) Compile(cfg OutMessageConfig) {
	f.OutMessageConfig = cfg
	f.Regexps = nil
	for _, filter := range cfg.Filters {
		pattern, err := regexp.Compile(filter, regexp.None)
		if err != nil {
			log.Printf("编译正则表达式：%s，出错：%v\n", filter, err)
			continue
		}
		f.Regexps = append(f.Regexps, pattern)
	}
}
```

把现有 `(*MessageFilter).processFilter` 的正则循环抽为共享自由函数，并改写 `processFilter`：

```go
// matchText 对单个文本段执行过滤器正则；返回 nil 表示未命中。
// 命中时：whitelist 返回 &TRUE，blacklist 返回 &FALSE。
func matchText(name, mode, rawMessage, text string, regexps []*regexp.Regexp) *bool {
	for _, pattern := range regexps {
		if ok, err := pattern.MatchString(text); ok {
			switch mode {
			case WHITELIST:
				log.Printf("%s：白名单的消息：%s\n", name, rawMessage)
				return &TRUE
			case BLACKLIST:
				log.Printf("%s：黑名单的消息：%s\n", name, rawMessage)
				return &FALSE
			}
		} else if err != nil {
			log.Printf("过滤器%s正则匹配出错的消息：%s\n", pattern.String(), rawMessage)
		}
	}
	return nil
}

// 处理对单个text消息的正则匹配
func (mf *MessageFilter) processFilter(Name, Text, RawMessage string) *bool {
	if mf == nil {
		return nil
	}
	return matchText(Name, mf.Mode, RawMessage, Text, mf.Regexps)
}
```

新增 `allowOutMessage`：

```go
// allowOutMessage 依据模式与正则判断输出消息的一组文本段是否放行。
func (f *OutMessageFilter) allowOutMessage(name, rawMessage string, texts []string) bool {
	switch f.Mode {
	case "", ON:
		return true
	case OFF:
		return false
	case WHITELIST:
		for _, text := range texts {
			if r := matchText(name, f.Mode, rawMessage, text, f.Regexps); r != nil && *r {
				return true
			}
		}
		return false
	case BLACKLIST:
		for _, text := range texts {
			if r := matchText(name, f.Mode, rawMessage, text, f.Regexps); r != nil && !*r {
				return false
			}
		}
		return true
	}
	log.Printf("%s的out-message.mode配置异常，必须为on、off、whitelist或blacklist\n", name)
	return true
}
```

在 `(*Filter).Compile` 中新增 `f.OutMessage.Compile(cfg.OutMessage)`。

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run 'TestOutMessageFilterAllow|TestFilter' -v`
Expected: PASS（原有 `TestFilter*` 亦不回归）。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: 抽取matchText并实现OutMessageFilter`（由用户手动提交）。

---

### Task 3: `ProcessFromBotApp` 统一方向 B 处理

**Files:**
- Modify: `src/filters.go`
- Test: `src/filters_test.go`

**Interfaces:**
- Consumes: `OutMessageFilter.allowOutMessage`（Task 2）、`ParseOutboundIds`/`AllowIds`（已存在）。
- Produces: `func (f *Filter) ProcessFromBotApp(raw []byte) (out []byte, pass bool)`；移除 `FilterFromBotApp`。

- [ ] **Step 1: 写失败测试**

在 `src/filters_test.go` 中：把现有 `TestFilterFromBotApp` 整体替换为 `TestProcessFromBotApp`，并新增 `TestProcessFromBotAppOutMessage`。文件顶部导入需包含 `encoding/json` 与 `strings`。

```go
func TestProcessFromBotApp(t *testing.T) {
	f := &Filter{
		Name:          "t",
		GroupId:       IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{123}}},
		UserId:        IdFilter{IdConfig{Mode: BLACKLIST, Ids: []int64{456}}},
		PassNoIdEvent: true,
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":123}}`)); pass {
		t.Fatal("发往黑名单群的action应被拦截")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_private_msg","params":{"user_id":456}}`)); pass {
		t.Fatal("发往黑名单用户的action应被拦截")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":999}}`)); !pass {
		t.Fatal("发往合法群的action应放行")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"get_login_info"}`)); !pass {
		t.Fatal("无id的action应放行")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`not json`)); !pass {
		t.Fatal("非JSON应放行")
	}
}

func TestProcessFromBotAppOutMessage(t *testing.T) {
	f := &Filter{Name: "t"}

	f.OutMessage.Compile(OutMessageConfig{Mode: BLACKLIST, Filters: []string{"广告"}})
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"这是广告"}}`)); pass {
		t.Fatal("命中黑名单的输出消息应被拦截")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"正常"}}`)); !pass {
		t.Fatal("未命中黑名单的输出消息应放行")
	}

	f.OutMessage.Compile(OutMessageConfig{Mode: ON, Prefix: "【Bot】"})
	out, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"你好"}}`))
	if !pass {
		t.Fatal("on模式应放行")
	}
	if !strings.Contains(string(out), "【Bot】你好") {
		t.Fatalf("字符串形式应拼接前缀，得到 %s", out)
	}

	out, pass = f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":[{"type":"text","data":{"text":"你好"}}]}}`))
	if !pass {
		t.Fatal("数组形式应放行")
	}
	var obj struct {
		Params struct {
			Message []MessageContent `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("输出不是合法JSON：%v", err)
	}
	if len(obj.Params.Message) != 2 || obj.Params.Message[0].Data["text"] != "【Bot】" {
		t.Fatalf("数组形式应在最前插入前缀段，得到 %s", out)
	}

	f.OutMessage.Compile(OutMessageConfig{Mode: OFF})
	out, pass = f.ProcessFromBotApp([]byte(`{"action":"get_login_info"}`))
	if !pass {
		t.Fatal("无 params.message 的 action 不受内容过滤影响，应放行")
	}
	if string(out) != `{"action":"get_login_info"}` {
		t.Fatalf("无 message 时应原样放行，得到 %s", out)
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./src/ -run 'TestProcessFromBotApp' -v`
Expected: FAIL/编译错误，`ProcessFromBotApp` 未定义。

- [ ] **Step 3: 最小实现**

在 `src/filters.go` 中删除现有 `FilterFromBotApp` 方法，新增以下方法：

```go
// ProcessFromBotApp 处理bot应用发往OneBot客户端的数据。
// 返回处理后的完整数据与是否放行；不放行时 out 为 nil。
func (f *Filter) ProcessFromBotApp(raw []byte) (out []byte, pass bool) {
	groupId, userId, ok := ParseOutboundIds(raw)
	if ok && !f.AllowIds(groupId, userId) {
		return nil, false
	}
	if !ok {
		return raw, true
	}
	return f.applyOutMessage(raw)
}

// applyOutMessage 对出站帧的 params.message 做内容过滤与前缀拼接。
func (f *Filter) applyOutMessage(raw []byte) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, true
	}
	paramsRaw, exists := obj["params"]
	if !exists {
		return raw, true
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(paramsRaw, &params); err != nil {
		return raw, true
	}
	msgRaw, exists := params["message"]
	if !exists {
		return raw, true
	}
	// 字符串形式
	var messageString string
	if err := json.Unmarshal(msgRaw, &messageString); err == nil {
		if !f.OutMessage.allowOutMessage(f.Name, messageString, []string{messageString}) {
			return nil, false
		}
		if f.OutMessage.Prefix == "" {
			return raw, true
		}
		newRaw, err := json.Marshal(f.OutMessage.Prefix + messageString)
		if err != nil {
			log.Printf("序列化%s的输出消息出错：%v\n", f.Name, err)
			return raw, true
		}
		params["message"] = newRaw
		return f.rebuildFrame(raw, obj, params)
	}
	// 数组形式
	var messageArray []MessageContent
	if err := json.Unmarshal(msgRaw, &messageArray); err == nil {
		texts := make([]string, 0, len(messageArray))
		for _, seg := range messageArray {
			if seg.Type == MESSAGE_TYPE_TEXT {
				if text, ok := seg.Data["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		if !f.OutMessage.allowOutMessage(f.Name, string(msgRaw), texts) {
			return nil, false
		}
		if f.OutMessage.Prefix == "" {
			return raw, true
		}
		prefixSeg := MessageContent{Type: MESSAGE_TYPE_TEXT, Data: map[string]interface{}{"text": f.OutMessage.Prefix}}
		messageArray = append([]MessageContent{prefixSeg}, messageArray...)
		newRaw, err := json.Marshal(messageArray)
		if err != nil {
			log.Printf("序列化%s的输出消息出错：%v\n", f.Name, err)
			return raw, true
		}
		params["message"] = newRaw
		return f.rebuildFrame(raw, obj, params)
	}
	// 既非字符串也非数组：不处理
	return raw, true
}

// rebuildFrame 将修改后的 params 写回整帧；出错时回退为原始数据并放行。
func (f *Filter) rebuildFrame(raw []byte, obj, params map[string]json.RawMessage) ([]byte, bool) {
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		log.Printf("序列化%s的输出params出错：%v\n", f.Name, err)
		return raw, true
	}
	obj["params"] = paramsRaw
	out, err := json.Marshal(obj)
	if err != nil {
		log.Printf("序列化%s的输出消息出错：%v\n", f.Name, err)
		return raw, true
	}
	return out, true
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./src/ -run 'TestProcessFromBotApp' -v`
Expected: PASS。

- [ ] **Step 5: 提交（手动）**

建议提交消息：`feat: 实现ProcessFromBotApp统一方向B处理`（由用户手动提交）。

---

### Task 4: 接入 `readLoop` 并在 `String()` 中展示

**Files:**
- Modify: `src/client.go`
- Modify: `src/filters.go`

**Interfaces:**
- Consumes: `Filter.ProcessFromBotApp`（Task 3）、`blockedResponse`（已存在）。

- [ ] **Step 1: 改写 `(*WsClient).readLoop`**

用以下实现整体替换 `src/client.go` 中的 `readLoop`：

```go
// 处理从bot应用端读取的消息
func (wc *WsClient) readLoop(ctx context.Context, wss *WsServer) {
	for {
		select {
		case msg := <-wc.readChan:
			if msg.MsgType == websocket.TextMessage {
				// bot应用发往OneBot客户端的数据要经过账号黑白名单与输出消息过滤器
				out, pass := wc.filter.ProcessFromBotApp(msg.MsgData)
				if !pass {
					// 被拦截的action回一个带原echo的失败响应，避免bot应用一直等待
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

- [ ] **Step 2: `(*Filter).String` 增加 out-message 行**

在 `src/filters.go` 的 `(*Filter).String` 返回的模板末尾（`group-message` 的 `prefix ... replace` 之后）追加：

```
out-message: %s
	filters: [ %s ]
	prefix: %s
```

并在对应实参末尾追加：

```go
		f.OutMessage.Mode,
		strings.Join(f.OutMessage.Filters, ", "),
		f.OutMessage.Prefix,
```

- [ ] **Step 3: 构建与全量测试**

Run: `go build ./...`
Expected: 无输出（成功）。

Run: `go vet ./...`
Expected: 无输出（成功）。

Run: `go test ./...`
Expected: PASS（Task 1~3 全部测试）。

- [ ] **Step 4: 提交（手动）**

建议提交消息：`feat: readLoop接入输出消息过滤器并在String中展示`（由用户手动提交）。

---

## Self-Review

- **Spec coverage:**
  - 配置 `out-message`（单字符串 prefix、不分私聊/群聊）→ Task 1。
  - 四模式 + 正则语义（复用/抽取 `matchText`）→ Task 2。
  - 仅用 `params.message`（string/array）→ Task 3 `applyOutMessage`。
  - 前缀「拼接」（string 拼接 / array 插段）→ Task 3。
  - 与账号黑白名单共存（先 id 后内容）→ Task 3 `ProcessFromBotApp`。
  - 拦截复用失败响应 → Task 4。
  - 非目标（`params.messages`、`post_type` 事件、`raw_message`）→ 未实现，符合 spec。
- **Placeholder scan:** 无 TODO/TBD；所有步骤含可执行代码与命令。
- **Type consistency:** `OutMessageConfig`/`OutMessageFilter`/`allowOutMessage`/`ProcessFromBotApp`/`matchText` 的签名在 Task 1~4 间一致；`FilterFromBotApp` 被 `ProcessFromBotApp` 取代，测试同步迁移。
