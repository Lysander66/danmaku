package bilibili

import (
	"fmt"
	"log/slog"
)

const (
	uid    = 0
	buvid  = "662135B5-0E5A"
	cookie = "buvid3=; SESSDATA="
)

func Run() {
	addr := "wss://zj-cn-live-comet.chat.bilibili.com:2245/sub"
	token := "QocYyWLA0Pr"

	roomId := 6
	client := NewEventHandler(roomId, uid, buvid, cookie, token)
	client.Connect(addr)

	select {}
}

func run() {
	roomId := 6
	info, err := GetDanmuInfo(roomId)
	if err != nil {
		slog.Error("getDanmuInfo", "err", err)
		return
	}

	var addr string
	for _, v := range info.Data.HostList {
		addr = fmt.Sprintf("wss://%s:%d/sub", v.Host, v.Port)
		break
	}

	client := NewEventHandler(roomId, uid, buvid, cookie, info.Data.Token)
	client.Connect(addr)
}
