package handler

import (
	"log"

	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	pbmodel "astral-party-server/internal/pb/model"
	"astral-party-server/internal/session"
)

const (
	CmdCreateRoomC2S     = 5005
	CmdCreateRoomS2C     = 5006
	CmdSyncRoomC2S       = 5007
	CmdSyncRoomS2C       = 5008
	CmdStartGameC2S      = 5019
	CmdStartGameS2C      = 5020
	CmdChoiceHeroC2S2    = 5085
	CmdChoiceHeroS2C2    = 5086
	CmdAffirmHeroC2S     = 5087
	CmdAffirmHeroS2C     = 5088
	CmdSingleCampaignC2S = 5229
	CmdSingleCampaignS2C = 5230
)

func registerRoom(r *dispatch.Router, deps *Deps) {
	r.Register(CmdCreateRoomC2S, createRoom(deps))
	r.Register(CmdSyncRoomC2S, syncRoom(deps))
	r.Register(CmdStartGameC2S, startGame(deps))
	r.Register(CmdChoiceHeroC2S2, choiceHero(deps))
	r.Register(CmdAffirmHeroC2S, affirmHero(deps))
	r.Register(CmdSingleCampaignC2S, singleCampaign(deps))
}

// roomPlayer loads the player's persisted Player snapshot for room listings.
func roomPlayer(deps *Deps, s *session.Session) *pbmodel.Player {
	p, err := deps.loadPlayerState(s.PlayerID, false)
	if err != nil {
		return &pbmodel.Player{Id: s.PlayerID}
	}
	return p
}

func noRoom(s *session.Session, f *frame.Frame) error {
	log.Printf("[room] no room for player %d (cmd %d)", s.PlayerID, f.CmdID)
	return s.SendReply(f.CmdID+1, f.UpSN, int16(code.Code_RoomNotExist), nil)
}

func createSoloRoom(deps *Deps, s *session.Session, mapID int32, state pbmodel.Room_State, name string) *pbmodel.Room {
	player := roomPlayer(deps, s)
	room := &pbmodel.Room{
		Name:       name,
		MasterId:   player.Id,
		Players:    []*pbmodel.Player{player},
		State:      state,
		PlayerIdx:  0,
		CreateTime: nowUnix(),
		UpdateTime: nowUnix(),
		MapId:      mapID,
		LevelId:    mapID,
	}
	created := deps.Rooms.Create(room)
	log.Printf("[room] created id=%d state=%d map=%d name=%q player=%d", created.Id, created.State, mapID, name, player.Id)
	return created
}

func singleCampaign(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.SingleCampaignC2S{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			return err
		}
		room := createSoloRoom(deps, s, req.GetLevelId(), pbmodel.Room_choice_hero, "campaign")
		return s.SendReply(CmdSingleCampaignS2C, f.UpSN, int16(code.Code_Succ), &pb.SingleCampaignS2C{Room: room})
	}
}

func createRoom(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.CreateRoomC2S{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			return err
		}
		room := createSoloRoom(deps, s, req.GetMapId(), pbmodel.Room_wait, "room")
		return s.SendReply(CmdCreateRoomS2C, f.UpSN, int16(code.Code_Succ), &pb.CreateRoomS2C{Room: room})
	}
}

func syncRoom(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		room := deps.Rooms.GetByPlayer(s.PlayerID)
		if room == nil {
			return noRoom(s, f)
		}
		return s.SendReply(CmdSyncRoomS2C, f.UpSN, int16(code.Code_Succ), &pb.SyncRoomS2C{Room: room})
	}
}

func choiceHero(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.ChoiceHeroC2S2{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			return err
		}
		room := deps.Rooms.GetByPlayer(s.PlayerID)
		if room == nil {
			return noRoom(s, f)
		}
		deps.Rooms.SetHero(s.PlayerID, req.GetHeroId())
		log.Printf("[room] id=%d choice hero=%d hasHero=%v player=%d", room.Id, req.GetHeroId(), req.GetHasHero(), s.PlayerID)
		return s.SendReply(CmdChoiceHeroS2C2, f.UpSN, int16(code.Code_Succ), &pb.ChoiceHeroS2C2{})
	}
}

func affirmHero(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		room := deps.Rooms.GetByPlayer(s.PlayerID)
		if room == nil {
			return noRoom(s, f)
		}
		heroID := deps.Rooms.Hero(s.PlayerID)
		deps.Rooms.Mutate(s.PlayerID, func(r *pbmodel.Room) {
			r.State = pbmodel.Room_ready1
			r.UpdateTime = nowUnix()
		})
		log.Printf("[room] id=%d affirm hero=%d -> ready1 player=%d", room.Id, heroID, s.PlayerID)
		return s.SendReply(CmdAffirmHeroS2C, f.UpSN, int16(code.Code_Succ), &pb.AffirmHeroS2C{
			HeroId:    heroID,
			HasChoice: true,
			PlayerId:  s.PlayerID,
			UseAdorn:  0,
		})
	}
}

func startGame(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		room := deps.Rooms.GetByPlayer(s.PlayerID)
		if room == nil {
			return noRoom(s, f)
		}
		deps.Rooms.Mutate(s.PlayerID, func(r *pbmodel.Room) {
			r.State = pbmodel.Room_running
			r.Round = 1
			r.StartTime = nowUnix()
			r.UpdateTime = nowUnix()
		})
		log.Printf("[room] id=%d -> running player=%d", room.Id, s.PlayerID)
		return s.SendReply(CmdStartGameS2C, f.UpSN, int16(code.Code_Succ), &pb.StartGameS2C{Room: room})
	}
}
