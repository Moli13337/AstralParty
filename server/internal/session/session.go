package session

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/frame"
)

var ErrClosed = errors.New("session closed")

type Session struct {
	Conn      net.Conn
	SessionID int64
	PlayerID  int64
	Authed    bool

	// Client protocol version bytes, captured from the most recent inbound
	// frame for diagnostics. Replies always carry 0.0.0: the client
	// force-quits with an update prompt when a reply version byte exceeds its
	// own (GameSettings.VER1/2, sourced from the hot-update resource version
	// and possibly still zero during login), so 0.0.0 is the only safe value.
	Ver1 byte
	Ver2 byte
	Ver3 byte

	writeMu   sync.Mutex
	downSN    int64
	downSNMu  sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}
}

func New(conn net.Conn) *Session {
	return &Session{
		Conn:   conn,
		closed: make(chan struct{}),
	}
}

func NewSessionID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	v := int64(binary.BigEndian.Uint64(b[:]))
	return v & 0x7fffffffffffffff
}

func (s *Session) NextDownSN() int64 {
	s.downSNMu.Lock()
	defer s.downSNMu.Unlock()
	s.downSN++
	return s.downSN
}

// SendReply writes a response frame tied to a client request (echoes UpSN,
// DownSN stays 0).
func (s *Session) SendReply(cmdID int16, upSN int64, errCode int16, msg proto.Message) error {
	var payload []byte
	if msg != nil {
		var perr error
		payload, perr = proto.Marshal(msg)
		if perr != nil {
			return perr
		}
	}
	f := &frame.Frame{
		SessionID: s.SessionID,
		CmdID:     cmdID,
		UpSN:      upSN,
		Err:       errCode,
		Payload:   payload,
	}
	if frame.CipherEnabled {
		frame.XorPayload(f.Payload)
	}
	err := s.writeFrame(f)
	log.Printf("[tx reply] cmd=%d up=%d err=%d len=%d ok=%v", cmdID, upSN, errCode, len(payload), err == nil)
	return err
}

// SendPush writes a server-initiated frame (UpSN 0, DownSN increments).
func (s *Session) SendPush(cmdID int16, msg proto.Message) error {
	var payload []byte
	if msg != nil {
		var perr error
		payload, perr = proto.Marshal(msg)
		if perr != nil {
			return perr
		}
	}
	f := &frame.Frame{
		SessionID: s.SessionID,
		CmdID:     cmdID,
		DownSN:    s.NextDownSN(),
		Payload:   payload,
	}
	if frame.CipherEnabled {
		frame.XorPayload(f.Payload)
	}
	err := s.writeFrame(f)
	log.Printf("[tx push] cmd=%d down=%d len=%d ok=%v", cmdID, f.DownSN, len(payload), err == nil)
	return err
}

func (s *Session) writeFrame(f *frame.Frame) error {
	select {
	case <-s.closed:
		return ErrClosed
	default:
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.Conn.Write(f.Encode()); err != nil {
		return err
	}
	return nil
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.Conn.Close()
	})
}

func (s *Session) Done() <-chan struct{} {
	return s.closed
}

func (s *Session) Logf(format string, args ...any) {
	log.Printf("[sess %d player %d] %s", s.SessionID, s.PlayerID, fmt.Sprintf(format, args...))
}
