package message

import (
	"encoding/json"
)

// Operation types for WebSocket messages
const (
	OP_REGISTER       uint16 = 100 // Client registration
	OP_REGISTER_REPLY uint16 = 101 // Registration reply
	OP_BROADCAST      uint16 = 102 // Broadcast message
	OP_PRIVATE        uint16 = 103 // Private message
	OP_CLIENT_LIST    uint16 = 104 // Client list
	OP_CLIENT_ONLINE  uint16 = 105 // Client online
	OP_CLIENT_OFFLINE uint16 = 106 // Client offline
	OP_ERROR          uint16 = 107 // Error message
)

// RegisterMessage 注册消息
type RegisterMessage struct {
	BaseMessage
}

func (m *RegisterMessage) Type() uint16 {
	return OP_REGISTER
}

func (m *RegisterMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *RegisterMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// RegisterReplyMessage 注册回复消息
type RegisterReplyMessage struct {
	BaseMessage
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (m *RegisterReplyMessage) Type() uint16 {
	return OP_REGISTER_REPLY
}

func (m *RegisterReplyMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *RegisterReplyMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// BroadcastMessage 广播消息
type BroadcastMessage struct {
	BaseMessage
}

func (m *BroadcastMessage) Type() uint16 {
	return OP_BROADCAST
}

func (m *BroadcastMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *BroadcastMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// PrivateMessage 私聊消息
type PrivateMessage struct {
	BaseMessage
}

func (m *PrivateMessage) Type() uint16 {
	return OP_PRIVATE
}

func (m *PrivateMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *PrivateMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// ClientListMessage 客户端列表消息
type ClientListMessage struct {
	BaseMessage
	Clients []ClientInfo `json:"clients"`
}

func (m *ClientListMessage) Type() uint16 {
	return OP_CLIENT_LIST
}

func (m *ClientListMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *ClientListMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// ClientOnlineMessage 客户端上线消息
type ClientOnlineMessage struct {
	BaseMessage
}

func (m *ClientOnlineMessage) Type() uint16 {
	return OP_CLIENT_ONLINE
}

func (m *ClientOnlineMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *ClientOnlineMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// ClientOfflineMessage 客户端下线消息
type ClientOfflineMessage struct {
	BaseMessage
}

func (m *ClientOfflineMessage) Type() uint16 {
	return OP_CLIENT_OFFLINE
}

func (m *ClientOfflineMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *ClientOfflineMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// ErrorMessage 错误消息
type ErrorMessage struct {
	BaseMessage
	Error string `json:"error"`
}

func (m *ErrorMessage) Type() uint16 {
	return OP_ERROR
}

func (m *ErrorMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *ErrorMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// NewMessage 根据操作类型创建对应的消息
func NewMessage(operation uint16) Message {
	switch operation {
	case OP_REGISTER:
		return &RegisterMessage{}
	case OP_REGISTER_REPLY:
		return &RegisterReplyMessage{}
	case OP_BROADCAST:
		return &BroadcastMessage{}
	case OP_PRIVATE:
		return &PrivateMessage{}
	case OP_CLIENT_LIST:
		return &ClientListMessage{}
	case OP_CLIENT_ONLINE:
		return &ClientOnlineMessage{}
	case OP_CLIENT_OFFLINE:
		return &ClientOfflineMessage{}
	case OP_ERROR:
		return &ErrorMessage{}
	default:
		return nil
	}
}
