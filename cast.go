// Package cast implements (part of) the Google Cast (v2) protocol
package cast // import "luit.eu/cast"

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"luit.eu/cast/pb"
)

const (
	urnHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	urnConnection = "urn:x-cast:com.google.cast.tp.connection"
	// urnReceiver   = "urn:x-cast:com.google.cast.receiver"
	urnDeviceauth = "urn:x-cast:com.google.cast.tp.deviceauth"
)

// Message is a message to be sent to or received from a connection with a
// Cast device.
type Message struct {
	Namespace   string //
	Source      string // if not provided when sending, "sender-0" is used
	Destination string // if not provided when sending, "receiver-0" is used
	payload     []byte
}

func NewMessage(namespace, source, destination string, payload interface{}) (*Message, error) {
	v, err := json.Marshal(payload)
	msg := &Message{
		Namespace:   namespace,
		Source:      source,
		Destination: destination,
		payload:     v,
	}
	return msg, err
}

func (m *Message) SetPayload(v interface{}) error {
	var err error
	m.payload, err = json.Marshal(v)
	return err
}

func (m *Message) GetPayload(v interface{}) error {
	return json.Unmarshal(m.payload, v)
}

func (m *Message) RawPayload() string {
	return string(m.payload)
}

type Conn struct {
	c     *tls.Conn
	mu    sync.Mutex
	cond  *sync.Cond
	queue []*Message
	err   error
}

func Dial(network string, addr string) (*Conn, error) {
	conn, err := tls.Dial(network, addr, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, err
	}
	c := &Conn{
		c: conn,
	}
	c.cond = sync.NewCond(&c.mu)
	msg, _ := NewMessage(urnConnection, "", "", map[string]string{"type": "CONNECT"})
	err = c.Send(msg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	go c.heartbeat()
	go c.loop()
	return c, nil
}

func (c *Conn) Close() error {
	msg := Message{
		Namespace: urnConnection,
	}
	msg.SetPayload(map[string]string{"type": "CLOSE"})
	err := c.Send(&msg)
	if err != nil {
		_ = c.c.Close()
		return err
	}
	return c.c.Close()
}

func (c *Conn) Receive() (*Message, error) {
	var msg *Message
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.queue) < 1 && c.err == nil {
		c.cond.Wait()
	}
	if c.err != nil {
		return nil, c.err
	}
	msg = c.queue[0]
	c.queue = c.queue[1:]
	return msg, nil
}

func (c *Conn) Send(msg *Message) error {
	v := pb.CastMessage_CASTV2_1_0
	src := msg.Source
	if src == "" {
		src = "luit.eu/cast"
	}
	dest := msg.Destination
	if dest == "" {
		dest = "receiver-0"
	}
	ns := msg.Namespace
	payloadType := pb.CastMessage_STRING
	payload := string(msg.payload)
	buf, err := proto.Marshal(&pb.CastMessage{
		ProtocolVersion: &v,
		SourceId:        &src,
		DestinationId:   &dest,
		Namespace:       &ns,
		PayloadType:     &payloadType,
		PayloadUtf8:     &payload,
	})
	if err != nil {
		return err
	}
	buf = append(buf, 0, 0, 0, 0)
	copy(buf[4:], buf)
	binary.BigEndian.PutUint32(buf[:4], uint32(len(buf)-4))
	_, err = c.c.Write(buf)
	return err
}

func (c *Conn) heartbeat() {
	for {
		msg, err := NewMessage(urnHeartbeat, "", "", map[string]string{"type": "PING"})
		err = c.Send(msg)
		if err != nil {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (c *Conn) loop() {
	for {
		_ = c.c.SetReadDeadline(time.Now().Add(30 * time.Second))
		msg, err := c.recv()
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			c.cond.Broadcast()
			return
		}
		if msg.Namespace == urnHeartbeat {
			payload := struct {
				Type string `json:"type"`
			}{}
			err = msg.GetPayload(&payload)
			if err == nil && payload.Type == "PING" {
				pong := Message{
					Namespace:   urnHeartbeat,
					Source:      msg.Destination,
					Destination: msg.Source,
				}
				_ = pong.SetPayload(map[string]string{"type": "PONG"})
				_ = c.Send(&pong)
			}
		}
		c.mu.Lock()
		c.queue = append(c.queue, msg)
		c.mu.Unlock()
		c.cond.Broadcast()
	}
}

func (c *Conn) recv() (*Message, error) {
	var length uint32
	err := binary.Read(c.c, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	if _, err = io.ReadFull(c.c, buf); err != nil {
		return nil, err
	}
	var castMessage pb.CastMessage
	err = proto.Unmarshal(buf, &castMessage)
	if err != nil {
		return nil, err
	}
	msg := Message{
		Namespace:   castMessage.GetNamespace(),
		Source:      castMessage.GetSourceId(),
		Destination: castMessage.GetDestinationId(),
	}
	switch castMessage.GetPayloadType() {
	case pb.CastMessage_BINARY:
		msg.payload = castMessage.GetPayloadBinary()
	case pb.CastMessage_STRING:
		msg.payload = []byte(castMessage.GetPayloadUtf8())
	}
	return &msg, nil
}
