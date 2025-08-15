package message

// Message 基础消息接口
type Message interface {
	Type() uint16
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

// BaseMessage 基础消息结构
type BaseMessage struct {
	ClientID string `json:"client_id"`
	Content  any    `json:"content"`
	Target   string `json:"target,omitempty"`
}

// ClientInfo 客户端信息
type ClientInfo struct {
	ID       string `json:"id"`
	IP       string `json:"ip"`
	JoinTime string `json:"join_time"`
}
