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
		{"string group id", `{"group_id":"123","user_id":"456"}`, 123, 456, true},
		{"float id", `{"group_id":123.0}`, 123, 0, true},
		{"bool id unparseable", `{"group_id":true}`, 0, 0, true},
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
		{"string params id", `{"action":"send_group_msg","params":{"group_id":"123"}}`, 123, 0, true},
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
