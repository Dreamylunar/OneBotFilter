package onebotfilter

import (
	"encoding/json"
	"testing"
)

func TestBlockedResponse(t *testing.T) {
	resp, ok := blockedResponse([]byte(`{"action":"send_group_msg","params":{"group_id":123},"echo":"e1"}`))
	if !ok {
		t.Fatal("带echo的action应生成失败响应")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(resp, &m); err != nil {
		t.Fatalf("响应不是合法JSON：%v", err)
	}
	if m["status"] != "failed" {
		t.Fatalf("status应为failed，得到%v", m["status"])
	}
	if m["echo"] != "e1" {
		t.Fatalf("echo应原样回传，得到%v", m["echo"])
	}
	if _, ok := blockedResponse([]byte(`{"action":"get_login_info"}`)); ok {
		t.Fatal("无echo的action不应生成响应")
	}
	if _, ok := blockedResponse([]byte(`{"action":"x","echo":null}`)); ok {
		t.Fatal("echo为null不应生成响应")
	}
	if _, ok := blockedResponse([]byte(`not json`)); ok {
		t.Fatal("非JSON不应生成响应")
	}
}
