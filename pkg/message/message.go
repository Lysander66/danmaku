package message

type Codec interface {
	Marshal() ([]byte, error)
	Unmarshal(data []byte) error
}

type Message interface {
	Codec
	Type() uint16
}

type BaseMessage struct {
	ClientID string `json:"client_id"`
	Content  any    `json:"content"`
	Target   string `json:"target,omitempty"`
}

type ClientInfo struct {
	ID       string `json:"id"`
	IP       string `json:"ip"`
	JoinTime string `json:"join_time"`
}
