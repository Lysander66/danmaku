package main

import (
	"bufio"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Lysander66/danmaku/pkg/message"
	"github.com/Lysander66/zephyr/pkg/protocol"
	"github.com/lxzan/gws"
)

type ClientConfig struct {
	ServerURL      string
	ClientID       string
	ReconnectDelay time.Duration
	MaxReconnects  int
	PongWait       time.Duration
}

type Client struct {
	config         *ClientConfig
	conn           *gws.Conn
	isConnected    bool
	reconnectCount int
	stopChan       chan struct{}
}

type WebSocketClient struct {
	client *Client
}

func main() {
	// 配置
	config := &ClientConfig{
		ServerURL:      "ws://localhost:8080/ws?secret=your-secret-2025",
		ClientID:       "",               // 空字符串让服务端自动生成
		ReconnectDelay: 3 * time.Second,  // 重连间隔
		MaxReconnects:  -1,               // -1 表示无限重连
		PongWait:       45 * time.Second, // pong等待时间，与服务端超时匹配
	}

	// 创建客户端
	client := NewClient(config)

	// 处理系统信号
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动客户端
	go client.Start()

	// 处理用户输入
	go client.HandleUserInput()

	slog.Info("🚀 WebSocket 客户端启动")
	slog.Info("💡 输入命令")
	slog.Info("   broadcast <消息>  - 发送广播消息")
	slog.Info("   private <目标ID> <消息>  - 发送私聊消息")
	slog.Info("   quit  - 退出程序")
	slog.Info("---")

	// 等待退出信号
	<-signalChan
	slog.Info("👋 正在关闭客户端...")
	client.Stop()
}

func NewClient(config *ClientConfig) *Client {
	return &Client{
		config:   config,
		stopChan: make(chan struct{}),
	}
}

func (c *Client) Start() {
	for {
		select {
		case <-c.stopChan:
			return
		default:
			if err := c.connect(); err != nil {
				slog.Error("❌ 连接失败", "error", err)
				c.waitForReconnect()
				continue
			}

			// 连接成功，重置重连计数
			c.reconnectCount = 0
			c.isConnected = true

			// 注册客户端
			c.register()

			// 启动ReadLoop来处理接收到的消息
			go c.conn.ReadLoop()

			// 等待连接断开
			c.waitForDisconnection()
		}
	}
}

func (c *Client) connect() error {
	slog.Info("🔄 正在连接服务器", "url", c.config.ServerURL)

	// 创建WebSocket客户端事件处理器
	wsClient := &WebSocketClient{client: c}

	// 使用完整的URL，包括查询参数
	conn, _, err := gws.NewClient(wsClient, &gws.ClientOption{
		Addr: c.config.ServerURL,
		PermessageDeflate: gws.PermessageDeflate{
			Enabled:               true,
			ServerContextTakeover: true,
			ClientContextTakeover: true,
		},
	})
	if err != nil {
		return err
	}

	c.conn = conn
	slog.Info("✅ 连接成功!")
	return nil
}

func (c *Client) register() {
	if c.conn == nil {
		slog.Error("❌ 无法注册", "reason", "连接未建立")
		return
	}

	slog.Info("📝 正在注册客户端...")

	// 使用协议注册
	registerMsg := &message.RegisterMessage{
		BaseMessage: message.BaseMessage{
			ClientID: c.config.ClientID,
		},
	}

	body, err := registerMsg.Marshal()
	if err != nil {
		slog.Error("❌ 序列化注册消息失败", "error", err)
		return
	}

	pkt := protocol.NewPacket(message.OP_REGISTER, body)
	data, err := protocol.Pack(pkt)
	if err != nil {
		slog.Error("❌ 打包注册消息失败", "error", err)
		return
	}

	_ = c.conn.WriteMessage(gws.OpcodeBinary, data)
}

// handleMessage 处理协议消息
func (c *Client) handleMessage(pkt *protocol.Packet) {
	// 创建对应的消息对象
	msg := message.NewMessage(pkt.Header.Operation)
	if msg == nil {
		slog.Error("❌ 未知的操作类型", "operation", pkt.Header.Operation)
		return
	}

	// 解析消息体
	if err := msg.Unmarshal(pkt.Payload); err != nil {
		slog.Error("❌ 解析消息体失败", "error", err)
		return
	}

	// 根据操作类型处理消息
	switch pkt.Header.Operation {
	case message.OP_REGISTER_REPLY:
		c.handleRegisterReply(msg)
	case message.OP_BROADCAST:
		c.handleBroadcastMsg(msg)
	case message.OP_PRIVATE:
		c.handlePrivateMsg(msg)
	case message.OP_CLIENT_LIST:
		c.handleClientList(msg)
	case message.OP_CLIENT_ONLINE:
		c.handleClientOnline(msg)
	case message.OP_CLIENT_OFFLINE:
		c.handleClientOffline(msg)
	case message.OP_ERROR:
		c.handleError(msg)
	default:
		slog.Info("🔍 未知的操作类型", "operation", pkt.Header.Operation)
	}
}

// handleRegisterReply 处理注册回复
func (c *Client) handleRegisterReply(msg message.Message) {
	registerReply, ok := msg.(*message.RegisterReplyMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "RegisterReplyMessage")
		return
	}

	if registerReply.Success {
		if content, ok := registerReply.Content.(map[string]any); ok {
			if ip, exists := content["ip"]; exists {
				slog.Info("🎉 注册成功", "id", registerReply.ClientID, "ip", ip)
			} else {
				slog.Info("🎉 注册成功", "id", registerReply.ClientID)
			}
		}
		c.config.ClientID = registerReply.ClientID
	} else {
		slog.Error("❌ 注册失败", "message", registerReply.Message)
	}
}

// handleBroadcastMsg 处理广播消息
func (c *Client) handleBroadcastMsg(msg message.Message) {
	broadcastMsg, ok := msg.(*message.BroadcastMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "BroadcastMessage")
		return
	}

	if content, ok := broadcastMsg.Content.(map[string]any); ok {
		message := content["message"]
		ip := content["ip"]
		slog.Info("📻 广播消息", "id", broadcastMsg.ClientID, "ip", ip, "message", message)
	}
}

// handlePrivateMsg 处理私聊消息
func (c *Client) handlePrivateMsg(msg message.Message) {
	privateMsg, ok := msg.(*message.PrivateMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "PrivateMessage")
		return
	}

	if content, ok := privateMsg.Content.(map[string]any); ok {
		message := content["message"]
		ip := content["ip"]
		slog.Info("💬 私聊消息", "id", privateMsg.ClientID, "ip", ip, "message", message)
	}
}

// handleClientList 处理客户端列表
func (c *Client) handleClientList(msg message.Message) {
	clientListMsg, ok := msg.(*message.ClientListMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "ClientListMessage")
		return
	}

	slog.Info("👥 客户端列表")
	for _, client := range clientListMsg.Clients {
		slog.Info("   - 客户端信息", "id", client.ID, "ip", client.IP, "joinTime", client.JoinTime)
	}
}

// handleClientOnline 处理客户端上线
func (c *Client) handleClientOnline(msg message.Message) {
	onlineMsg, ok := msg.(*message.ClientOnlineMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "ClientOnlineMessage")
		return
	}

	if content, ok := onlineMsg.Content.(map[string]any); ok {
		if message, exists := content["message"]; exists {
			slog.Info("📢 客户端上线", "message", message)
		}
	}
}

// handleClientOffline 处理客户端下线
func (c *Client) handleClientOffline(msg message.Message) {
	offlineMsg, ok := msg.(*message.ClientOfflineMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "ClientOfflineMessage")
		return
	}

	if content, ok := offlineMsg.Content.(map[string]any); ok {
		if message, exists := content["message"]; exists {
			slog.Info("📢 客户端下线", "message", message)
		}
	}
}

// handleError 处理错误消息
func (c *Client) handleError(msg message.Message) {
	errorMsg, ok := msg.(*message.ErrorMessage)
	if !ok {
		slog.Error("❌ 消息类型转换失败", "expected", "ErrorMessage")
		return
	}

	slog.Error("❌ 错误", "error", errorMsg.Error)
}

func (c *Client) HandleUserInput() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])

		switch command {
		case "quit", "q":
			slog.Info("👋 正在退出...")
			close(c.stopChan)
			return

		case "broadcast", "b":
			if len(parts) < 2 {
				slog.Error("❌ 用法", "command", "broadcast <消息>")
				continue
			}
			content := strings.Join(parts[1:], " ")
			c.sendBroadcast(content)

		case "private", "p":
			if len(parts) < 3 {
				slog.Error("❌ 用法", "command", "private <目标ID> <消息>")
				continue
			}
			target := parts[1]
			content := strings.Join(parts[2:], " ")
			c.sendPrivate(target, content)

		case "status", "s":
			c.showStatus()

		case "help", "h":
			c.printHelp()

		default:
			slog.Error("❌ 未知命令", "command", command, "help", "输入 help 查看帮助")
		}
	}
}

// sendBroadcast 发送广播消息
func (c *Client) sendBroadcast(content any) {
	if c.conn == nil || !c.isConnected {
		slog.Error("❌ 未连接到服务器")
		return
	}

	broadcastMsg := &message.BroadcastMessage{
		BaseMessage: message.BaseMessage{
			Content: content,
		},
	}

	body, err := broadcastMsg.Marshal()
	if err != nil {
		slog.Error("❌ 序列化广播消息失败", "error", err)
		return
	}

	pkt := protocol.NewPacket(message.OP_BROADCAST, body)
	data, err := protocol.Pack(pkt)
	if err != nil {
		slog.Error("❌ 打包广播消息失败", "error", err)
		return
	}

	_ = c.conn.WriteMessage(gws.OpcodeBinary, data)
	slog.Info("📻 广播消息已发送", "content", content)
}

// sendPrivate 发送私聊消息
func (c *Client) sendPrivate(target string, content any) {
	if c.conn == nil || !c.isConnected {
		slog.Error("❌ 未连接到服务器")
		return
	}

	privateMsg := &message.PrivateMessage{
		BaseMessage: message.BaseMessage{
			Target:  target,
			Content: content,
		},
	}

	body, err := privateMsg.Marshal()
	if err != nil {
		slog.Error("❌ 序列化私聊消息失败", "error", err)
		return
	}

	pkt := protocol.NewPacket(message.OP_PRIVATE, body)
	data, err := protocol.Pack(pkt)
	if err != nil {
		slog.Error("❌ 打包私聊消息失败", "error", err)
		return
	}

	_ = c.conn.WriteMessage(gws.OpcodeBinary, data)
	slog.Info("💬 私聊消息已发送", "target", target, "content", content)
}

// 显示状态
func (c *Client) showStatus() {
	status := "未连接"
	if c.isConnected {
		status = "已连接"
	}

	slog.Info("📊 客户端状态")
	slog.Info("   ID", "value", c.config.ClientID)
	slog.Info("   服务器", "value", c.config.ServerURL)
	slog.Info("   连接状态", "value", status)
	slog.Info("   重连次数", "value", c.reconnectCount)
}

// 打印帮助
func (c *Client) printHelp() {
	slog.Info("📖 可用命令")
	slog.Info("   broadcast <消息>        - 发送广播消息 (简写: b)")
	slog.Info("   private <目标ID> <消息>  - 发送私聊消息 (简写: p)")
	slog.Info("   status                  - 查看连接状态 (简写: s)")
	slog.Info("   help                    - 显示帮助信息 (简写: h)")
	slog.Info("   quit                    - 退出程序 (简写: q)")
}

// 等待重连
func (c *Client) waitForReconnect() {
	if c.config.MaxReconnects != -1 && c.reconnectCount >= c.config.MaxReconnects {
		slog.Error("❌ 达到最大重连次数", "max", c.config.MaxReconnects, "action", "停止重连")
		close(c.stopChan)
		return
	}

	c.reconnectCount++
	slog.Info("⏳ 准备重连", "delay", int(c.config.ReconnectDelay.Seconds()), "count", c.reconnectCount)

	select {
	case <-time.After(c.config.ReconnectDelay):
		// 继续重连
	case <-c.stopChan:
		return
	}
}

// 等待连接断开
func (c *Client) waitForDisconnection() {
	// 这里可以添加连接监控逻辑
	// 当连接断开时，OnClose会触发
	for c.isConnected && c.conn != nil {
		select {
		case <-c.stopChan:
			return
		case <-time.After(1 * time.Second):
			// 定期检查连接状态
		}
	}

	slog.Info("🔌 连接已断开")
}

func (c *Client) Stop() {
	close(c.stopChan)
	if c.conn != nil {
		c.conn.WriteClose(1000, []byte("normal closure"))
	}
	slog.Info("✅ 客户端已停止")
}

func (w *WebSocketClient) OnOpen(socket *gws.Conn) {
	slog.Info("🔌 WebSocket连接已建立")
}

func (w *WebSocketClient) OnClose(socket *gws.Conn, err error) {
	w.client.isConnected = false
	w.client.conn = nil // 重置连接引用

	if err != nil {
		slog.Error("🔌 连接关闭", "error", err)
	} else {
		slog.Info("🔌 连接正常关闭")
	}
}

func (w *WebSocketClient) OnPing(socket *gws.Conn, payload []byte) {
	// 收到服务端ping，自动回复pong并重置超时
	_ = socket.WritePong(payload)
	_ = socket.SetDeadline(time.Now().Add(w.client.config.PongWait))
	slog.Debug("💓 收到服务端ping", "action", "自动回复pong，重置超时时间")
}

func (w *WebSocketClient) OnPong(socket *gws.Conn, payload []byte) {
	slog.Info("💓 收到pong响应")
}

func (w *WebSocketClient) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	// 解析协议
	pkt, err := protocol.Unpack(message.Bytes())
	if err != nil {
		slog.Error("❌ 解析消息失败", "error", err)
		return
	}

	w.client.handleMessage(pkt)
}
