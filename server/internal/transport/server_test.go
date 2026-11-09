package transport

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/config"
	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	"astral-party-server/internal/handler"
	"astral-party-server/internal/store"
)

func startTestServer(t *testing.T) (addr string, st *store.Store, closeFn func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.ListenAddr = "127.0.0.1:0"

	st, err := store.Open(cfg.DataDir, 1)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	router := dispatch.NewRouter()
	handler.Register(router, &handler.Deps{Store: st, CipherKey: "test"})
	srv := New(cfg, router)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.HandleConn(conn)
		}
	}()

	return ln.Addr().String(), st, func() {
		_ = ln.Close()
		_ = st.Close()
	}
}

func sendFrame(t *testing.T, conn net.Conn, f *frame.Frame) {
	t.Helper()
	if _, err := conn.Write(f.Encode()); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func readFrame(t *testing.T, conn net.Conn) *frame.Frame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	head := make([]byte, frame.HeadLength)
	if _, err := readFull(conn, head); err != nil {
		t.Fatalf("read head: %v", err)
	}
	length := int32(binary.BigEndian.Uint32(head[0:4]))
	payload := make([]byte, length)
	if length > 0 {
		if _, err := readFull(conn, payload); err != nil {
			t.Fatalf("read payload: %v", err)
		}
	}
	f, err := frame.Decode(head, payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return f
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestConnectAndHeartbeat(t *testing.T) {
	addr, st, closeFn := startTestServer(t)
	defer closeFn()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := &pb.ConnectC2S{
		PublicKey: "t4UDM%2Q",
		ClientVer: "1.0.0",
		Auth:      pb.AuthType_Dev,
		AuthInfo:  &pb.ConnectC2S_Dev{Dev: &pb.DevInfo{Nick: "tester"}},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sendFrame(t, conn, &frame.Frame{
		SessionID: 0, CmdID: handler.CmdConnectC2S,
		Ver1: 1, UpSN: 100, Payload: payload,
	})

	resp := readFrame(t, conn)
	if resp.CmdID != handler.CmdConnectS2C {
		t.Fatalf("expected cmd 5002, got %d", resp.CmdID)
	}
	if resp.Err != int16(code.Code_Succ) {
		t.Fatalf("expected err 0, got %d", resp.Err)
	}
	if resp.UpSN != 100 {
		t.Fatalf("expected upsn 100 echoed, got %d", resp.UpSN)
	}
	s2c := &pb.ConnectS2C{}
	if err := proto.Unmarshal(resp.Payload, s2c); err != nil {
		t.Fatalf("unmarshal ConnectS2C: %v", err)
	}
	if s2c.SessionId == 0 {
		t.Fatal("expected non-zero sessionId")
	}
	if s2c.GetAccount().GetNick() != "tester" {
		t.Fatalf("expected nick tester, got %q", s2c.GetAccount().GetNick())
	}
	if s2c.GetPlayer().GetId() == 0 {
		t.Fatal("expected player id")
	}
	playerID := s2c.GetPlayer().GetId()

	// re-login with same nick must reuse the account
	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer func() { _ = conn2.Close() }()
	sendFrame(t, conn2, &frame.Frame{
		SessionID: 0, CmdID: handler.CmdConnectC2S,
		Ver1: 1, UpSN: 100, Payload: payload,
	})
	resp2 := readFrame(t, conn2)
	s2c2 := &pb.ConnectS2C{}
	if err := proto.Unmarshal(resp2.Payload, s2c2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s2c2.GetPlayer().GetId() != playerID {
		t.Fatalf("expected same player id %d, got %d", playerID, s2c2.GetPlayer().GetId())
	}

	// heartbeat on the first connection
	sendFrame(t, conn, &frame.Frame{
		SessionID: s2c.SessionId, CmdID: handler.CmdHeartbeatC2S,
		Ver1: 1, UpSN: 101, Payload: mustMarshal(t, &pb.HeartbeatC2S{Client: 12345}),
	})
	hb := readFrame(t, conn)
	if hb.CmdID != handler.CmdHeartbeatS2C {
		t.Fatalf("expected cmd 5004, got %d", hb.CmdID)
	}
	hbS2C := &pb.HeartbeatS2C{}
	if err := proto.Unmarshal(hb.Payload, hbS2C); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hbS2C.Client != 12345 {
		t.Fatalf("expected client echo 12345, got %d", hbS2C.Client)
	}

	// per-player db file exists
	if _, err := st.LoadPlayer(playerID); err != nil {
		t.Fatalf("load player: %v", err)
	}
}

func TestConnectChinaAuthAndFullPlayer(t *testing.T) {
	addr, _, closeFn := startTestServer(t)
	defer closeFn()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := &pb.ConnectC2S{
		PublicKey: "t4UDM%2Q",
		ClientVer: "1.0.0",
		Auth:      pb.AuthType_China,
		AuthInfo: &pb.ConnectC2S_China{China: &pb.ChinaInfo{
			GameId: "1001", ChannelId: "2", AppId: "app", Sid: "fake-steam-sid",
		}},
	}
	sendFrame(t, conn, &frame.Frame{
		SessionID: 0, CmdID: handler.CmdConnectC2S,
		Ver1: 1, UpSN: 100, Payload: mustMarshal(t, req),
	})
	resp := readFrame(t, conn)
	if resp.CmdID != handler.CmdConnectS2C || resp.Err != int16(code.Code_Succ) {
		t.Fatalf("connect failed: cmd=%d err=%d", resp.CmdID, resp.Err)
	}
	s2c := &pb.ConnectS2C{}
	if err := proto.Unmarshal(resp.Payload, s2c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	p := s2c.GetPlayer()
	if p.GetId() == 0 {
		t.Fatal("expected player id")
	}
	// GameLogicManager.InitAccount dereferences these directly (TaskLogic does
	// playerTask.WeekCondition.Count without a nil check), so they must be set.
	if p.GetTask() == nil || p.GetFriends() == nil || p.GetShopInfo() == nil ||
		p.GetBattlePass() == nil || p.GetClientData() == nil || p.GetGuildInfo() == nil {
		t.Fatal("nested player messages must be non-nil")
	}
	if s2c.GetAccount().GetNick() == "" {
		t.Fatal("expected non-empty nick")
	}
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
