package transport

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"astral-party-server/internal/config"
	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	"astral-party-server/internal/session"
)

type Server struct {
	cfg      *config.Config
	router   *dispatch.Router
	httpAPI  *httpAPI
	listener net.Listener
	sessions sync.Map
	running  atomic.Bool
}

func New(cfg *config.Config, router *dispatch.Router) *Server {
	srv := &Server{cfg: cfg, router: router}
	srv.httpAPI = &httpAPI{
		SourceURL: cfg.CdnBaseURL + "/INT_STEAM/3.3.0/026",
		ServerURL: trimHost(cfg.ListenAddr),
		NoticeURL: cfg.CdnBaseURL + "/notice",
		Version:   "3.3.0",
		SdkVer:    "1.0.0",
	}
	return srv
}

// trimHost converts ":8800" into a host:port usable from the client side.
func trimHost(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "127.0.0.1" + addr
	}
	return addr
}

func (srv *Server) ListenAndServe() error {
	l, err := net.Listen("tcp", srv.cfg.ListenAddr)
	if err != nil {
		return err
	}
	srv.listener = l
	srv.running.Store(true)
	log.Printf("listening on %s", srv.cfg.ListenAddr)
	for {
		conn, err := l.Accept()
		if err != nil {
			if !srv.running.Load() {
				return nil
			}
			return err
		}
		go srv.HandleConn(conn)
	}
}

func (srv *Server) Close() {
	srv.running.Store(false)
	if srv.listener != nil {
		_ = srv.listener.Close()
	}
	srv.sessions.Range(func(k, _ any) bool {
		k.(*session.Session).Close()
		return true
	})
}

// HandleConn serves a single connection; exported for tests and custom listeners.
func (srv *Server) HandleConn(conn net.Conn) {
	sess := session.New(conn)
	srv.sessions.Store(sess, struct{}{})
	defer func() {
		srv.sessions.Delete(sess)
		sess.Close()
	}()
	br := bufio.NewReader(conn)
	head, err := br.Peek(4)
	if err == nil && isHTTPHint(head) {
		srv.httpAPI.handleConn(br, conn)
		return
	}
	if err := srv.readLoop(sess, br); err != nil {
		sess.Logf("connection closed: %v", err)
	}
}

func (srv *Server) readLoop(sess *session.Session, br *bufio.Reader) error {
	head := make([]byte, frame.HeadLength)
	for {
		if _, err := io.ReadFull(br, head); err != nil {
			return err
		}
		length := int32(binary.BigEndian.Uint32(head[0:4]))
		if length < 0 || length > srv.cfg.MaxFrame {
			return io.ErrUnexpectedEOF
		}
		payload := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(br, payload); err != nil {
				return err
			}
		}
		if frame.CipherEnabled {
			frame.XorPayload(payload)
		}
		f, err := frame.Decode(head, payload)
		if err != nil {
			return err
		}
		if f.SessionID != 0 && sess.SessionID == 0 {
			// client stamps the assigned session id into later frames
			sess.SessionID = f.SessionID
		}
		// Echo the client's protocol version bytes back in every reply; a
		// higher version in a reply makes the client quit with a forced
		// update prompt.
		sess.Ver1, sess.Ver2, sess.Ver3 = f.Ver1, f.Ver2, f.Ver3
		log.Printf("[frame] cmd=%d ver=%d.%d.%d sess=%d up=%d down=%d err=%d len=%d",
			f.CmdID, f.Ver1, f.Ver2, f.Ver3, f.SessionID, f.UpSN, f.DownSN, f.Err, len(payload))
		if err := srv.dispatch(sess, f); err != nil {
			sess.Logf("dispatch error: %v", err)
		}
	}
}

func (srv *Server) dispatch(sess *session.Session, f *frame.Frame) error {
	h, ok := srv.router.Lookup(f.CmdID)
	if !ok {
		sess.Logf("no handler for %s", f)
		return nil
	}
	return h(sess, f)
}
