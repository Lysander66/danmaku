package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lysander66/danmaku/pkg/message"
	"github.com/Lysander66/zephyr/pkg/protocol"
	"github.com/lxzan/gws"
)

const (
	WS_SECRET   = "your-secret-2025"
	SERVER_PORT = ":8080"
)

type ClientInfo struct {
	ID       string `json:"id"`
	IP       string `json:"ip"`
	JoinTime string `json:"join_time"`
	Session  *gws.Conn
}

type WebSocketHandler struct {
	clients         map[*gws.Conn]*ClientInfo
	clientsLock     sync.RWMutex
	clientIDCounter int
	clientIDLock    sync.Mutex
	pingTicker      *time.Ticker
}

var (
	handler *WebSocketHandler
)

func main() {
	handler = &WebSocketHandler{
		clients:         make(map[*gws.Conn]*ClientInfo),
		clientsLock:     sync.RWMutex{},
		clientIDCounter: 1,
		pingTicker:      time.NewTicker(30 * time.Second),
	}

	upgrader := gws.NewUpgrader(handler, &gws.ServerOption{
		ParallelEnabled:   true,                                 // Parallel message processing
		Recovery:          gws.Recovery,                         // Exception recovery
		PermessageDeflate: gws.PermessageDeflate{Enabled: true}, // Enable compression
		Authorize: func(r *http.Request, session gws.SessionStorage) bool {
			secret := r.URL.Query().Get("secret")
			if secret != WS_SECRET {
				return false
			}
			// 存储客户端IP到session中
			clientIP := getClientIP(r)
			session.Store("clientIP", clientIP)
			// 存储WebSocket key用于连接管理
			session.Store("websocketKey", r.Header.Get("Sec-WebSocket-Key"))
			return true
		},
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r)
		if err != nil {
			slog.Error("WebSocket升级失败", "error", err)
			return
		}
		go func() {
			socket.ReadLoop() // Blocking prevents the context from being GC.
		}()
	})

	go handler.startPingTicker()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// API 路由 - 获取客户端列表（需要鉴权）
	http.HandleFunc("/api/clients", func(w http.ResponseWriter, r *http.Request) {
		secret := r.URL.Query().Get("secret")
		slog.Debug("🔍 API调用", "secret", secret, "expected", WS_SECRET)
		if secret != WS_SECRET {
			slog.Warn("❌ 认证失败", "reason", "secret不匹配")
			w.WriteHeader(http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"error":"需要认证","code":401}`))
			return
		}
		slog.Debug("✅ 认证成功")

		handler.clientsLock.RLock()
		var clientList []ClientInfo
		for _, client := range handler.clients {
			clientList = append(clientList, *client)
		}
		handler.clientsLock.RUnlock()

		response := map[string]any{
			"clients": clientList,
			"count":   len(clientList),
		}

		responseJSON, err := json.Marshal(response)
		if err != nil {
			slog.Error("❌ 序列化客户端列表失败", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(responseJSON)
	})

	slog.Info("🚀 WebSocket 服务器启动", "port", SERVER_PORT)
	slog.Info("🌐 管理界面", "url", "http://localhost"+SERVER_PORT)

	if err := http.ListenAndServe(SERVER_PORT, nil); err != nil {
		slog.Error("服务器启动失败", "error", err)
		panic(err)
	}
}

func (h *WebSocketHandler) OnOpen(socket *gws.Conn) {
	clientIP := MustLoad[string](socket.Session(), "clientIP")

	slog.Info("🔌 新连接", "ip", clientIP, "status", "已认证")

	h.clientsLock.Lock()
	h.clients[socket] = &ClientInfo{
		IP:       clientIP,
		JoinTime: time.Now().Format("2006-01-02 15:04:05"),
		Session:  socket,
	}
	h.clientsLock.Unlock()

	// 设置心跳超时，给ping/pong留出缓冲时间
	_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
}

func (h *WebSocketHandler) OnClose(socket *gws.Conn, err error) {
	h.clientsLock.Lock()
	client, exists := h.clients[socket]
	if exists {
		delete(h.clients, socket)
	}
	h.clientsLock.Unlock()

	if exists && client.ID != "" {
		slog.Info("🔌 客户端断开", "id", client.ID, "ip", client.IP)

		// 广播客户端下线消息
		offlineMsg := &message.ClientOfflineMessage{
			BaseMessage: message.BaseMessage{
				ClientID: client.ID,
				Content: map[string]any{
					"message": "客户端 " + client.ID + " 已下线",
					"ip":      client.IP,
				},
			},
		}
		h.broadcastMessage(offlineMsg, message.OP_CLIENT_OFFLINE)
	}
}

func (h *WebSocketHandler) OnPing(socket *gws.Conn, payload []byte) {
	_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
	_ = socket.WritePong(payload)
	slog.Info("🏓 收到客户端ping", "action", "回复pong")
}

func (h *WebSocketHandler) OnPong(socket *gws.Conn, payload []byte) {
	// 收到pong响应，重置超时时间
	_ = socket.SetDeadline(time.Now().Add(45 * time.Second))

	// 查找客户端信息
	h.clientsLock.RLock()
	client, exists := h.clients[socket]
	h.clientsLock.RUnlock()

	if !exists {
		slog.Warn("💓 收到未知客户端pong响应")
		return
	}
	slog.Debug("💓 收到客户端pong响应", "id", client.ID, "ip", client.IP, "action", "重置超时时间")
}

func (h *WebSocketHandler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	h.handleMessage(socket, message.Bytes())
}

// handleMessage 处理协议消息
func (h *WebSocketHandler) handleMessage(socket *gws.Conn, data []byte) {
	h.clientsLock.RLock()
	client, exists := h.clients[socket]
	h.clientsLock.RUnlock()

	if !exists {
		return
	}

	pkt, err := protocol.Unpack(data)
	if err != nil {
		slog.Error("failed to unpack", "error", err)
		return
	}

	// 创建对应的消息
	msg := message.NewMessage(pkt.Header.Operation)
	if msg == nil {
		slog.Error("unknown operation", "operation", pkt.Header.Operation)
		return
	}

	if err = msg.Unmarshal(pkt.Payload); err != nil {
		slog.Error("failed to unmarshal", "error", err)
		return
	}

	switch pkt.Header.Operation {
	case message.OP_REGISTER:
		h.handleRegister(socket, msg, client, pkt.Header.SequenceID)
	case message.OP_BROADCAST:
		h.handleBroadcast(socket, msg, client, pkt.Header.SequenceID)
	case message.OP_PRIVATE:
		h.handlePrivate(socket, msg, client, pkt.Header.SequenceID)
	default:
		slog.Error("unknown operation", "operation", pkt.Header.Operation)
	}
}

func MustLoad[T any](session gws.SessionStorage, key string) (v T) {
	if value, exist := session.Load(key); exist {
		v, _ = value.(T)
	}
	return
}

func (h *WebSocketHandler) startPingTicker() {
	for range h.pingTicker.C {
		h.clientsLock.RLock()
		// 复制客户端列表，避免长时间持有锁
		sessions := make([]*gws.Conn, 0, len(h.clients))
		for session := range h.clients {
			sessions = append(sessions, session)
		}
		h.clientsLock.RUnlock()

		slog.Debug("🏓 定时器触发", "clients", len(sessions), "action", "发送ping")
		for _, session := range sessions {
			_ = session.WritePing(nil)
		}
	}
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// handleRegister 处理注册消息
func (h *WebSocketHandler) handleRegister(socket *gws.Conn, msg message.Message, client *ClientInfo, sequenceID uint32) {
	registerMsg, ok := msg.(*message.RegisterMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "RegisterMessage")
		return
	}

	clientID := registerMsg.ClientID
	if clientID == "" {
		h.clientIDLock.Lock()
		clientID = "client_" + strconv.Itoa(h.clientIDCounter)
		h.clientIDCounter++
		h.clientIDLock.Unlock()
	}

	h.clientsLock.Lock()
	client.ID = clientID
	h.clientsLock.Unlock()

	slog.Info("📝 客户端注册", "id", clientID, "ip", client.IP)

	// 确保客户端信息已更新到map中
	h.clientsLock.Lock()
	if existingClient, exists := h.clients[socket]; exists {
		existingClient.ID = clientID
	}
	h.clientsLock.Unlock()

	// 发送注册成功消息
	response := &message.RegisterReplyMessage{
		BaseMessage: message.BaseMessage{
			ClientID: clientID,
			Content: map[string]any{
				"message": "注册成功",
				"ip":      client.IP,
			},
		},
		Success: true,
		Message: "注册成功",
	}

	h.sendMessage(socket, response, message.OP_REGISTER_REPLY)

	// 广播新客户端上线
	onlineMsg := &message.ClientOnlineMessage{
		BaseMessage: message.BaseMessage{
			ClientID: clientID,
			Content: map[string]any{
				"message": "客户端 " + clientID + " 已上线",
				"ip":      client.IP,
			},
		},
	}

	h.broadcastMessage(onlineMsg, message.OP_CLIENT_ONLINE)

	// 发送当前客户端列表
	h.sendClientList(socket)
}

// handleBroadcast 处理广播消息
func (h *WebSocketHandler) handleBroadcast(socket *gws.Conn, msg message.Message, client *ClientInfo, sequenceID uint32) {
	broadcastMsg, ok := msg.(*message.BroadcastMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "BroadcastMessage")
		return
	}

	slog.Info("📻 广播消息", "id", client.ID, "ip", client.IP, "content", broadcastMsg.Content)

	// 构造广播消息
	response := &message.BroadcastMessage{
		BaseMessage: message.BaseMessage{
			ClientID: client.ID,
			Content: map[string]any{
				"message": broadcastMsg.Content,
				"ip":      client.IP,
			},
		},
	}

	h.broadcastMessage(response, message.OP_BROADCAST)
}

// handlePrivate 处理私聊消息
func (h *WebSocketHandler) handlePrivate(socket *gws.Conn, msg message.Message, client *ClientInfo, sequenceID uint32) {
	privateMsg, ok := msg.(*message.PrivateMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "PrivateMessage")
		return
	}

	targetID := privateMsg.Target
	if targetID == "" {
		return
	}

	slog.Info("💬 私聊消息", "from", client.ID, "to", targetID, "content", privateMsg.Content)

	// 查找目标客户端
	h.clientsLock.RLock()
	var targetSession *gws.Conn
	for session, c := range h.clients {
		if c.ID == targetID {
			targetSession = session
			break
		}
	}
	h.clientsLock.RUnlock()

	response := &message.PrivateMessage{
		BaseMessage: message.BaseMessage{
			ClientID: client.ID,
			Content: map[string]any{
				"message": privateMsg.Content,
				"ip":      client.IP,
			},
		},
	}

	if targetSession != nil {
		h.sendMessage(targetSession, response, message.OP_PRIVATE)
	} else {
		// 目标不存在，发送错误消息
		errorMsg := &message.ErrorMessage{
			BaseMessage: message.BaseMessage{},
			Error:       "目标客户端 " + targetID + " 不存在或未在线",
		}
		h.sendMessage(socket, errorMsg, message.OP_ERROR)
	}
}

// sendMessage 发送消息
func (h *WebSocketHandler) sendMessage(socket *gws.Conn, msg message.Message, operation uint16) error {
	body, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	pkt := protocol.NewPacket(operation, body)
	data, err := protocol.Pack(pkt)
	if err != nil {
		return fmt.Errorf("failed to pack: %w", err)
	}

	return socket.WriteMessage(gws.OpcodeBinary, data)
}

// broadcastMessage 广播消息
func (h *WebSocketHandler) broadcastMessage(msg message.Message, operation uint16) error {
	body, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	pkt := protocol.NewPacket(operation, body)
	data, err := protocol.Pack(pkt)
	if err != nil {
		return fmt.Errorf("failed to pack: %w", err)
	}

	// 先复制客户端列表，避免长时间持有锁
	h.clientsLock.RLock()
	sessions := make([]*gws.Conn, 0, len(h.clients))
	for session := range h.clients {
		sessions = append(sessions, session)
	}
	h.clientsLock.RUnlock()

	for _, session := range sessions {
		_ = session.WriteMessage(gws.OpcodeBinary, data)
	}

	return nil
}

// sendClientList 发送客户端列表
func (h *WebSocketHandler) sendClientList(socket *gws.Conn) {
	h.clientsLock.RLock()
	var clientList []message.ClientInfo
	for _, client := range h.clients {
		clientList = append(clientList, message.ClientInfo{
			ID:       client.ID,
			IP:       client.IP,
			JoinTime: client.JoinTime,
		})
	}
	h.clientsLock.RUnlock()

	listMsg := &message.ClientListMessage{
		BaseMessage: message.BaseMessage{},
		Clients:     clientList,
	}

	h.sendMessage(socket, listMsg, message.OP_CLIENT_LIST)
}
