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
