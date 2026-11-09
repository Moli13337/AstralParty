package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	"astral-party-server/internal/pb/model"
	"astral-party-server/internal/room"
	"astral-party-server/internal/session"
	"astral-party-server/internal/store"
)

const (
	CmdConnectC2S   = 5001
	CmdConnectS2C   = 5002
	CmdHeartbeatC2S = 5003
	CmdHeartbeatS2C = 5004
)

type Deps struct {
	Store        *store.Store
	Rooms        *room.Store
	CipherKey    string
	SkipTutorial bool
}

func Register(r *dispatch.Router, deps *Deps) {
	SkipTutorial = deps.SkipTutorial
	r.Register(CmdConnectC2S, connect(deps))
	r.Register(CmdHeartbeatC2S, heartbeat)
	r.Register(CmdTeachingC2S, teaching)
	registerRoom(r, deps)
	r.Register(CmdGetShowPlayerC2S, getShowPlayer(deps))
	r.Register(CmdSetShowPlayerC2S, setShowPlayer(deps))
	r.Register(CmdFriendListC2S, friendList(deps))
	r.Register(CmdGetRecommendGuildsC2S, getRecommendGuilds(deps))
}

func connect(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.ConnectC2S{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_InvalidParamErr), nil)
			return err
		}
		var authType int32
		var authID string
		var nick string
		uidLogin := int64(0)
		log.Printf("connect: auth=%d clientVer=%q publicKey=%q", req.Auth, req.ClientVer, req.PublicKey)
		switch req.Auth {
		case pb.AuthType_Dev:
			nick = req.GetDev().GetNick()
			// The transfer-code ("inheritance code") login sends the typed
			// code as DevInfo.nick with an unchecked password. A numeric code
			// selects the exact player id; anything else keys a named dev
			// account.
			if uid, err := strconv.ParseInt(nick, 10, 64); err == nil && uid > 0 {
				uidLogin = uid
				nick = fmt.Sprintf("Player%d", uid)
			} else {
				authType, authID = store.DevAuthKey(nick)
			}
		case pb.AuthType_Steam:
			steam := req.GetSteam()
			log.Printf("connect steam: id=%d ticketLen=%d identity=%q nick=%q",
				steam.GetId(), len(steam.GetTicket()), steam.GetIdentity(), steam.GetNick())
			if steam.GetId() == 0 {
				s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_AuthErr), nil)
				return nil
			}
			authType, authID = store.SteamAuthKey(steam.GetId())
			nick = steam.GetNick()
			if nick == "" {
				nick = fmt.Sprintf("Steam%d", steam.GetId()%100000)
			}
		case pb.AuthType_Abroad:
			abroad := req.GetAbroad()
			log.Printf("connect abroad: source=%d sid=%q deviceId=%q",
				abroad.GetSource(), abroad.GetSid(), abroad.GetDeviceId())
			if abroad.GetSid() == "" {
				s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_AuthErr), nil)
				return nil
			}
			authType, authID = store.AbroadAuthKey(abroad.GetSource(), abroad.GetSid())
			nick = "Player"
		case pb.AuthType_China:
			china := req.GetChina()
			if china.GetSid() == "" {
				s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_AuthErr), nil)
				return nil
			}
			authType, authID = store.ChinaAuthKey(china.GetGameId(), china.GetSid())
			nick = "Player"
		default:
			s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_AuthTypeNotExist), nil)
			return nil
		}
		if nick == "" {
			s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_InvalidParamErr), nil)
			return nil
		}

		playerID := int64(0)
		created := false
		if uidLogin > 0 {
			row, justCreated, err := deps.Store.GetOrCreatePlayerByID(uidLogin, nick)
			if err != nil {
				return err
			}
			playerID = row.ID
			created = justCreated
			if created {
				log.Printf("created player %d (uid login %d)", playerID, uidLogin)
			} else if err := deps.Store.TouchLogin(playerID); err != nil {
				return err
			}
		} else {
			var err error
			playerID, err = deps.Store.FindPlayer(authType, authID)
			if err != nil {
				return err
			}
			if playerID == 0 {
				row, err := deps.Store.CreatePlayer(authType, authID, nick)
				if err != nil {
					return err
				}
				playerID = row.ID
				created = true
				log.Printf("created player %d (%s)", playerID, nick)
			} else if err := deps.Store.TouchLogin(playerID); err != nil {
				return err
			}
		}

		player, err := deps.loadPlayerState(playerID, created)
		if err != nil {
			return err
		}

		s.SessionID = session.NewSessionID()
		s.PlayerID = playerID
		s.Authed = true

		plat := "dev"
		if req.Auth == pb.AuthType_Steam {
			plat = "steam"
		} else if req.Auth == pb.AuthType_Abroad {
			plat = "abroad"
		}
		account := &model.AccountInfo{
			AccountId: uint64(playerID),
			Nick:      player.Nick,
			PlayerId:  playerID,
			Token:     fmt.Sprintf("%s-token-%d", plat, playerID),
			Plat:      plat,
		}
		if req.Auth == pb.AuthType_Steam {
			account.SteamId = strconv.FormatUint(req.GetSteam().GetId(), 10)
		}
		resp := &pb.ConnectS2C{
			CipherKey: deps.CipherKey,
			SessionId: s.SessionID,
			Account:   account,
			Player:    player,
			QueueTime: 0,
			Data:      sdkDataJSON(playerID),
			NowTime:   nowUnix(),
		}
		return s.SendReply(CmdConnectS2C, f.UpSN, int16(code.Code_Succ), resp)
	}
}

// loadPlayerState returns the persisted Player payload, creating the default
// one for fresh accounts (or legacy rows with an empty blob).
func (deps *Deps) loadPlayerState(playerID int64, created bool) (*model.Player, error) {
	if !created {
		blob, err := deps.Store.LoadPlayerProto(playerID)
		if err != nil {
			return nil, err
		}
		if len(blob) > 0 {
			return unmarshalPlayer(blob)
		}
	}
	row, err := deps.Store.LoadPlayer(playerID)
	if err != nil {
		return nil, err
	}
	p := NewDefaultPlayer(row)
	blob, err := marshalPlayer(p)
	if err != nil {
		return nil, err
	}
	if err := deps.Store.SavePlayerProto(playerID, blob); err != nil {
		return nil, err
	}
	return p, nil
}

func heartbeat(s *session.Session, f *frame.Frame) error {
	req := &pb.HeartbeatC2S{}
	if err := proto.Unmarshal(f.Payload, req); err != nil {
		return err
	}
	resp := &pb.HeartbeatS2C{
		Client: req.Client,
		// The client does Convert.ToInt32(model.Server) - it expects unix
		// seconds. Milliseconds overflow Int32 and the resulting
		// OverflowException aborts the heartbeat callback, leaving the ping
		// watchdog stuck until its 15s timer kills the connection.
		Server: nowUnix(),
	}
	return s.SendReply(CmdHeartbeatS2C, f.UpSN, int16(code.Code_Succ), resp)
}

func playerModel(row *store.PlayerRow) *model.Player {
	return NewDefaultPlayer(row)
}

func sdkDataJSON(playerID int64) string {
	payload := map[string]any{
		"content": map[string]any{
			"data": map[string]any{
				"userId": fmt.Sprintf("%d", playerID),
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(b)
}
