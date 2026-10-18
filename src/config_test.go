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
