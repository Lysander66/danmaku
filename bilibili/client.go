package bilibili

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lxzan/gws"
	"github.com/tidwall/gjson"
)

type DanmuInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Ttl     int    `json:"ttl"`
	Data    struct {
		Group            string  `json:"group"`
		BusinessId       int     `json:"business_id"`
		RefreshRowFactor float64 `json:"refresh_row_factor"`
		RefreshRate      int     `json:"refresh_rate"`
		MaxDelay         int     `json:"max_delay"`
		Token            string  `json:"token"`
		HostList         []struct {
			Host    string `json:"host"`
			Port    int    `json:"port"`
			WssPort int    `json:"wss_port"`
			WsPort  int    `json:"ws_port"`
		} `json:"host_list"`
	} `json:"data"`
}

func GetDanmuInfo(roomID int) (*DanmuInfo, error) {
	rawURL := fmt.Sprintf("https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d&type=0", roomID)
	response, err := http.Get(rawURL)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	info := &DanmuInfo{}
	if err = json.Unmarshal(data, info); err != nil {
		return nil, err
	}

	return info, nil
}

type EventHandler struct {
	roomid int
	uid    int
	buvid  string
	cookie string
	token  string
}

func NewEventHandler(roomid, uid int, buvid, cookie, token string) *EventHandler {
	return &EventHandler{
		roomid: roomid,
		uid:    uid,
		buvid:  buvid,
		cookie: cookie,
		token:  token,
	}
}

func (e *EventHandler) Connect(addr string) {
	option := &gws.ClientOption{
		Addr:          addr,
		RequestHeader: http.Header{},
		PermessageDeflate: gws.PermessageDeflate{
			Enabled:               true,
			ServerContextTakeover: true,
			ClientContextTakeover: true,
		},
	}
	option.RequestHeader.Set("Cookie", e.cookie)

	socket, _, err := gws.NewClient(e, option)
	if err != nil {
		slog.Error("NewClient", "err", err)
		return
	}

	go socket.ReadLoop()

	go func() {
		for {
			time.Sleep(30 * time.Second)
			_ = socket.WriteMessage(gws.OpcodeBinary, encodeMessage(&Message{}, OP_HEARTBEAT))
		}
	}()
}

func (e *EventHandler) OnOpen(socket *gws.Conn) {
	body, _ := json.Marshal(map[string]any{
		"uid":      e.uid,
		"roomid":   e.roomid,
		"protover": 3,
		"buvid":    e.buvid,
		"platform": "web",
		"type":     2,
		"key":      e.token,
	})
	msg := &Message{Body: body}
	err := socket.WriteMessage(gws.OpcodeBinary, encodeMessage(msg, OP_AUTH))
	if err != nil {
		slog.Error("发送AUTH包", "err", err)
	}
}

func (e *EventHandler) OnClose(socket *gws.Conn, err error) {
	slog.Error("OnClose", "err", err)
}

func (e *EventHandler) OnPing(socket *gws.Conn, payload []byte) {
	_ = socket.WritePong(payload)
}

func (e *EventHandler) OnPong(socket *gws.Conn, payload []byte) {
}

func (e *EventHandler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	msg := decodeMessage(message.Bytes())
	switch msg.Header.Operation {
	case OP_HEARTBEAT_REPLY:
		slog.Debug("心跳包回复", "len", len(message.Bytes()))
	case OP_SEND_SMS_REPLY:
		data := msg.decompress()
		for _, body := range data {
			parseCmd(body)
		}
		slog.Debug("弹幕消息", "len(data)", len(data))
	case OP_AUTH_REPLY:
		slog.Debug("鉴权包回复", "len", len(message.Bytes()))
	default:
		slog.Warn("未知", "OP", msg.Header.Operation)
	}
}

const (
	CMD_DANMU_MSG                     = "DANMU_MSG"
	CMD_LIKE_INFO_V3_CLICK            = "LIKE_INFO_V3_CLICK"
	CMD_LIKE_INFO_V3_UPDATE           = "LIKE_INFO_V3_UPDATE"
	CMD_ROOM_REAL_TIME_MESSAGE_UPDATE = "ROOM_REAL_TIME_MESSAGE_UPDATE"
	CMD_SEND_GIFT                     = "SEND_GIFT"
	CMD_WATCHED_CHANGE                = "WATCHED_CHANGE"
)

// body的内容一般是json格式，里面一条广播消息称为cmd
func parseCmd(body []byte) {
	cmd := gjson.GetBytes(body, "cmd").String()
	switch cmd {
	case CMD_DANMU_MSG:
		// TODO
		var userId int64
		var userName string
		if arr := gjson.GetBytes(body, "info").Array(); len(arr) >= 18 {
			content := arr[1].String()
			if arr2 := arr[2].Array(); len(arr2) >= 8 {
				userId = arr2[0].Int()
				userName = arr2[1].String()
			}
			slog.Info("弹幕", "ID", userId, "name", userName, "content", content)
		}
	case CMD_LIKE_INFO_V3_CLICK:
		count := gjson.GetBytes(body, "data.click_count").Int()
		slog.Debug("点赞数", "count", count)
	case CMD_LIKE_INFO_V3_UPDATE:
		text := gjson.GetBytes(body, "data.uname").String() + gjson.GetBytes(body, "data.like_text").String()
		slog.Debug(text)
	case CMD_ROOM_REAL_TIME_MESSAGE_UPDATE:
		count := gjson.GetBytes(body, "data.fans").Int()
		slog.Debug("fans", "fans", count)
	case CMD_WATCHED_CHANGE:
		text := gjson.GetBytes(body, "data.text_large").String()
		slog.Debug(text)
	case CMD_SEND_GIFT:
		data := gjson.GetBytes(body, "data")
		text := data.Get("uname").String() + " " + data.Get("action").String() + " " + data.Get("giftName").String()
		slog.Info(text)
	default:
		slog.Info("其它", "CMD", cmd)
	}
}
