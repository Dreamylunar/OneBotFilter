package onebotfilter

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
)

type OneBotMessage struct {
	Raw     []byte
	Partial OneBotMessagePartial
	Intact  map[string]json.RawMessage
}

func ParseOneBotMessage(Raw []byte) *OneBotMessage {
	oneBotMessage := &OneBotMessage{
		Raw: Raw,
	}
	if err := json.Unmarshal(Raw, &oneBotMessage.Intact); err != nil {
		return nil
	}
	if err := json.Unmarshal(Raw, &oneBotMessage.Partial); err != nil {
		return nil
	}
	switch oneBotMessage.Partial.MessageFormat {
	case MESSAGE_FORMAT_ARRAY:
		if err := json.Unmarshal(oneBotMessage.Partial.UnDecodedMessage, &oneBotMessage.Partial.MessageArray); err != nil {
			log.Printf("将%s解析为array失败\n", oneBotMessage.Partial.UnDecodedMessage)
			return nil
		}
	case MESSAGE_FORMAT_STRING:
		if err := json.Unmarshal(oneBotMessage.Partial.UnDecodedMessage, &oneBotMessage.Partial.MessageString); err != nil {
			log.Printf("将%s解析为string失败\n", oneBotMessage.Partial.UnDecodedMessage)
			return nil
		}
	default: //未知的format或没有format
		return nil
	}
	return oneBotMessage
}

type OneBotMessagePartial struct {
	MessageType      string           `json:"message_type"`
	MessageFormat    string           `json:"message_format"`
	UnDecodedMessage json.RawMessage  `json:"message"`
	MessageArray     []MessageContent `json:"-"`
	MessageString    string           `json:"-"`
	UserId           int64            `json:"user_id"`
	GroupId          int64            `json:"group_id"`
	RawMessage       string           `json:"raw_message"`
}
type MessageContent struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

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

// decodeId 解析单个id字段，兼容数字、浮点数与字符串形式的数字；无法解析时返回0。
func decodeId(raw json.RawMessage) int64 {
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		if i, err := num.Int64(); err == nil {
			return i
		}
		if f, err := num.Float64(); err == nil {
			return int64(f)
		}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f)
		}
	}
	return 0
}

// decodeIds 从 JSON 对象中解析 group_id / user_id，缺失或类型不符时为 0。
func decodeIds(obj map[string]json.RawMessage) (groupId, userId int64) {
	if v, exists := obj["group_id"]; exists {
		groupId = decodeId(v)
	}
	if v, exists := obj["user_id"]; exists {
		userId = decodeId(v)
	}
	return
}
