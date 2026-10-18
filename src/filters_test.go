package onebotfilter

import (
	"encoding/json"
	"testing"
)

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
	var strObj struct {
		Params struct {
			Message string `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out, &strObj); err != nil {
		t.Fatalf("输出不是合法JSON：%v", err)
	}
	if strObj.Params.Message != "【Bot】你好" {
		t.Fatalf("字符串形式的 params.message 应为拼接前缀后的值，得到 %s", out)
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

func TestOutMessageFilterAllow(t *testing.T) {
	f := &OutMessageFilter{}
	f.Compile(OutMessageConfig{Mode: BLACKLIST, Filters: []string{"广告"}})
	if f.allowOutMessage("t", "这是广告", []string{"这是广告"}) {
		t.Fatal("blacklist命中应拦截")
	}
	if !f.allowOutMessage("t", "正常", []string{"正常"}) {
		t.Fatal("blacklist未命中应放行")
	}
	if !f.allowOutMessage("t", "image", nil) {
		t.Fatal("blacklist下无text段应放行")
	}
	f.Compile(OutMessageConfig{Mode: WHITELIST, Filters: []string{"允许"}})
	if !f.allowOutMessage("t", "允许内容", []string{"允许内容"}) {
		t.Fatal("whitelist命中应放行")
	}
	if f.allowOutMessage("t", "其他", []string{"其他"}) {
		t.Fatal("whitelist未命中应拦截")
	}
	if f.allowOutMessage("t", "image", nil) {
		t.Fatal("whitelist下无text段应拦截")
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

func TestProcessFromBotAppOutMessageBranches(t *testing.T) {
	f := &Filter{Name: "t"}

	// 非法正则应被跳过（覆盖 compileRegexps 的 continue 分支），合法正则仍应正常生效
	f.OutMessage.Compile(OutMessageConfig{Mode: BLACKLIST, Filters: []string{"(", "广告"}})
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"这是广告"}}`)); pass {
		t.Fatal("非法正则应跳过，合法正则仍应命中并拦截")
	}
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"正常"}}`)); !pass {
		t.Fatal("未命中黑名单的输出消息应放行")
	}

	// 无 text 段的数组消息：白名单应拦截，黑名单应放行
	imageOnly := []byte(`{"action":"send_group_msg","params":{"group_id":1,"message":[{"type":"image","data":{"file":"x.jpg"}}]}}`)
	f.OutMessage.Compile(OutMessageConfig{Mode: WHITELIST, Filters: []string{"允许"}})
	if _, pass := f.ProcessFromBotApp(imageOnly); pass {
		t.Fatal("白名单下无 text 段的消息应被拦截")
	}
	f.OutMessage.Compile(OutMessageConfig{Mode: BLACKLIST, Filters: []string{"广告"}})
	if _, pass := f.ProcessFromBotApp(imageOnly); !pass {
		t.Fatal("黑名单下无 text 段的消息应放行")
	}

	// off 模式带 params.message 应拦截
	f.OutMessage.Compile(OutMessageConfig{Mode: OFF})
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"任意内容"}}`)); pass {
		t.Fatal("off 模式的输出消息应被拦截")
	}
	// 白名单未命中也应拦截
	f.OutMessage.Compile(OutMessageConfig{Mode: WHITELIST, Filters: []string{"允许"}})
	if _, pass := f.ProcessFromBotApp([]byte(`{"action":"send_group_msg","params":{"group_id":1,"message":"其他"}}`)); pass {
		t.Fatal("白名单未命中的输出消息应被拦截")
	}
}
