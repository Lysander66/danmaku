package bilibili

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"log/slog"

	"github.com/andybalholm/brotli"
)

const (
	OP_HEARTBEAT       = 2 //客户端发送的心跳包(30秒发送一次)
	OP_HEARTBEAT_REPLY = 3 //服务器收到心跳包的回复
	OP_SEND_SMS_REPLY  = 5 //服务器推送的弹幕消息包
	OP_AUTH            = 7 //客户端发送的鉴权包(客户端发送的第一个包)
	OP_AUTH_REPLY      = 8 //服务器收到鉴权包后的回复
)

// FixedLengthHeader
// https://open-live.bilibili.com/document/657d8e34-f926-a133-16c0-300c1afc6e6b
type FixedLengthHeader struct {
	PacketLength uint32 //整个Packet的长度，包含Header
	HeaderLength uint16 //Header的长度，固定为16
	Version      uint16 //压缩 2 zlib, 3 brotli
	Operation    uint32 //消息的类型
	SequenceID   uint32 //保留字段
}

type Message struct {
	Header FixedLengthHeader //所有字段 大端 对齐
	Body   []byte            //消息体，客户端解析Body之前请先解析Version字段
}

func (h FixedLengthHeader) encode() []byte {
	//buf := make([]byte, binary.Size(h))
	//binary.BigEndian.PutUint32(buf[0:], h.PacketLength)
	//binary.BigEndian.PutUint16(buf[4:], h.HeaderLength)
	//binary.BigEndian.PutUint16(buf[6:], h.Version)
	//binary.BigEndian.PutUint32(buf[8:], h.Operation)
	//binary.BigEndian.PutUint32(buf[12:], h.SequenceID)

	buf := []byte{0, 0, 0, 0, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	binary.BigEndian.PutUint32(buf, h.PacketLength)
	binary.BigEndian.PutUint16(buf[6:], h.Version)
	binary.BigEndian.PutUint32(buf[8:], h.Operation)
	return buf
}

func encodeMessage(msg *Message, operation uint32) []byte {
	headSize := binary.Size(FixedLengthHeader{})
	header := FixedLengthHeader{
		PacketLength: uint32(headSize + len(msg.Body)),
		HeaderLength: uint16(headSize),
		Version:      1,
		Operation:    operation,
		SequenceID:   1,
	}
	return append(header.encode(), msg.Body...)
}

func decodeMessage(data []byte) *Message {
	packetLength := binary.BigEndian.Uint32(data[0:4])
	if int(packetLength) != len(data) {
		slog.Error("Invalid packet length")
		return nil
	}

	msg := &Message{Body: data[16:packetLength]}
	msg.Header.Version = binary.BigEndian.Uint16(data[6:8])
	msg.Header.Operation = binary.BigEndian.Uint32(data[8:12])
	return msg
}

func (m *Message) decompress() [][]byte {
	switch m.Header.Version {
	case 3:
		b, err := brotliDecompress(m.Body)
		if err != nil {
			slog.Error("brotli解压", "err", err)
			return nil
		}
		return slice(b)
	case 2:
		b, err := zlibDecompress(m.Body)
		if err != nil {
			slog.Error("zlib解压", "err", err)
			return nil
		}
		return slice(b)
	case 1:
		fallthrough
	case 0:
		return [][]byte{m.Body}
	}

	return nil
}

// 压缩后的body格式可能包含多个完整的proto包（可以理解为递归）
func slice(data []byte) [][]byte {
	var packets [][]byte
	total := len(data)
	cursor := 0
	for cursor < total {
		packetLength := int(binary.BigEndian.Uint32(data[cursor : cursor+4]))
		packets = append(packets, data[cursor+16:cursor+packetLength])
		cursor += packetLength
	}
	return packets
}

func brotliDecompress(b []byte) ([]byte, error) {
	r := brotli.NewReader(bytes.NewReader(b))
	return io.ReadAll(r)
}

func zlibDecompress(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
