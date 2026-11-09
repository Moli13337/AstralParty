package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"astral-party-server/internal/cdn"
	"astral-party-server/internal/config"
	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	"astral-party-server/internal/handler"
	"astral-party-server/internal/room"
	"astral-party-server/internal/session"
	"astral-party-server/internal/store"
	"astral-party-server/internal/transport"
)

func main() {
	addr := flag.String("addr", ":8800", "listen address")
	dataDir := flag.String("data", "data", "data directory")
	cipher := flag.Bool("cipher", false, "enable payload XOR (client ships it disabled)")
	cdnAddr := flag.String("cdn", ":8810", "hot-update bundle HTTP address (empty disables)")
	origin := flag.String("origin", "https://seresjp.feimogames.com/INT_STEAM/3.3.0/026", "official CDN base for on-miss origin fetch (empty disables)")
	startUID := flag.Int64("start-uid", 1, "first player id handed out by auto-registration (seeds a fresh index.db only)")
	skipTutorial := flag.Bool("skip-tutorial", false, "mark the novice tutorial achievement complete so clients skip it")
	flag.Parse()

	logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	}

	cfg := config.Default()
	cfg.ListenAddr = *addr
	cfg.DataDir = *dataDir
	frame.CipherEnabled = *cipher

	st, err := store.Open(cfg.DataDir, *startUID)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	router := dispatch.NewRouter()
	router.SetFallback(func(s *session.Session, f *frame.Frame) error {
		log.Printf("[fallback] cmd=%d up=%d len=%d -> empty reply", f.CmdID, f.UpSN, len(f.Payload))
		return s.SendReply(f.CmdID+1, f.UpSN, 0, nil)
	})
	handler.Register(router, &handler.Deps{
		Store:        st,
		Rooms:        room.New(),
		CipherKey:    cfg.CipherKey,
		SkipTutorial: *skipTutorial,
	})

	srv := transport.New(cfg, router)

	if *cdnAddr != "" {
		files := cdn.New(cfg.DataDir+"/cdn", *origin)
		files.WarmDir(cfg.DataDir + "/cdn")
		go func() {
			log.Printf("cdn listening on %s (root %s)", *cdnAddr, cfg.DataDir+"/cdn")
			if err := http.ListenAndServe(*cdnAddr, files.Handler()); err != nil {
				log.Printf("cdn stopped: %v", err)
			}
		}()
	}

	log.Fatal(srv.ListenAndServe())
}
