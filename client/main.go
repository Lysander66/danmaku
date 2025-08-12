package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lxzan/gws"
)

type Message struct {
	Type     string `json:"type"`
	ClientID string `json:"client_id"`
	Content  any    `json:"content"`
	Target   string `json:"target,omitempty"`
}

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
	messageChan    chan Message
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
		config:      config,
		stopChan:    make(chan struct{}),
		messageChan: make(chan Message, 100),
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

			// 启动消息处理协程
			go c.writeMessages()

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

	registerMsg := Message{
		Type:     "register",
		ClientID: c.config.ClientID,
	}

	if data, err := json.Marshal(registerMsg); err == nil {
		_ = c.conn.WriteMessage(gws.OpcodeText, data)
	}
}

func (c *Client) writeMessages() {
	for {
		select {
		case message := <-c.messageChan:
			// 检查连接状态
			if c.conn != nil && c.isConnected {
				if data, err := json.Marshal(message); err == nil {
					_ = c.conn.WriteMessage(gws.OpcodeText, data)
				}
			}

		case <-c.stopChan:
			if c.conn != nil {
				c.conn.WriteClose(1000, []byte("normal closure"))
			}
			return
		}
	}
}

func (c *Client) handleMessage(message Message) {
	switch message.Type {
	case "register_success":
		if content, ok := message.Content.(map[string]any); ok {
			if ip, exists := content["ip"]; exists {
				slog.Info("🎉 注册成功", "id", message.ClientID, "ip", ip)
			} else {
				slog.Info("🎉 注册成功", "id", message.ClientID)
			}
			c.config.ClientID = message.ClientID
		}

	case "client_online":
		if content, ok := message.Content.(map[string]any); ok {
			if msg, exists := content["message"]; exists {
				slog.Info("📢 客户端上线", "message", msg)
			}
		}

	case "client_offline":
		if content, ok := message.Content.(map[string]any); ok {
			if msg, exists := content["message"]; exists {
				slog.Info("📢 客户端下线", "message", msg)
			}
		}

	case "broadcast":
		if content, ok := message.Content.(map[string]any); ok {
			msg := content["message"]
			ip := content["ip"]
			slog.Info("📻 广播消息", "id", message.ClientID, "ip", ip, "message", msg)
		}

	case "private":
		if content, ok := message.Content.(map[string]any); ok {
			msg := content["message"]
			ip := content["ip"]
			slog.Info("💬 私聊消息", "id", message.ClientID, "ip", ip, "message", msg)
		}

	case "client_list":
		slog.Info("👥 当前在线客户端")
		if clients, ok := message.Content.([]any); ok {
			for _, client := range clients {
				if clientMap, ok := client.(map[string]any); ok {
					id := clientMap["id"]
					ip := clientMap["ip"]
					joinTime := clientMap["join_time"]
					slog.Info("   - 客户端信息", "id", id, "ip", ip, "joinTime", joinTime)
				}
			}
		}

	case "error":
		slog.Error("❌ 错误", "content", message.Content)

	default:
		slog.Info("🔍 未知消息类型", "type", message.Type, "content", message.Content)
	}
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

// 发送广播消息
func (c *Client) sendBroadcast(content string) {
	if !c.isConnected {
		slog.Error("❌ 未连接到服务器")
		return
	}

	select {
	case c.messageChan <- Message{
		Type:    "broadcast",
		Content: content,
	}:
		slog.Info("📻 广播消息已发送", "content", content)
	default:
		slog.Error("❌ 消息队列已满")
	}
}

// 发送私聊消息
func (c *Client) sendPrivate(target, content string) {
	if !c.isConnected {
		slog.Error("❌ 未连接到服务器")
		return
	}

	select {
	case c.messageChan <- Message{
		Type:    "private",
		Target:  target,
		Content: content,
	}:
		slog.Info("💬 私聊消息已发送", "target", target, "content", content)
	default:
		slog.Error("❌ 消息队列已满")
	}
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

	var msg Message
	if err := json.Unmarshal(message.Bytes(), &msg); err != nil {
		slog.Error("❌ 解析消息失败", "error", err)
		return
	}

	w.client.handleMessage(msg)
}
