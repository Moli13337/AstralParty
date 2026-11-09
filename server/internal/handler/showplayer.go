package handler

import (
	"log"

	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	"astral-party-server/internal/session"
)

const (
	CmdGetShowPlayerC2S = 5153
	CmdGetShowPlayerS2C = 5154
	CmdSetShowPlayerC2S = 5151
	CmdSetShowPlayerS2C = 5152
)

// GetShowPlayerS2C.ShowData is a nested message: PlayerCareer.UpdateData
// dereferences it unguarded, so the reply must always carry one.
func getShowPlayer(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.GetShowPlayerC2S{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			return err
		}
		playerID := req.GetPlayerId()
		if playerID == 0 {
			playerID = s.PlayerID
		}
		log.Printf("[showplayer] get player=%d (requested %d)", playerID, req.GetPlayerId())
		return s.SendReply(CmdGetShowPlayerS2C, f.UpSN, int16(code.Code_Succ), &pb.GetShowPlayerS2C{
			ShowData: &pb.ShowPlayer{
				PlayerId: playerID,
				// AccountInfoWindow.RefreshStatisticalData dereferences
				// Statistics unguarded; a zeroed message renders as 0s.
				Statistics: &pb.ShowPlayerStatistics{},
			},
		})
	}
}

// SetShowPlayerS2C is an empty message; the fallback would handle it, but a
// dedicated handler keeps profile saves off the [fallback] log.
func setShowPlayer(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		return s.SendReply(CmdSetShowPlayerS2C, f.UpSN, int16(code.Code_Succ), &pb.SetShowPlayerS2C{})
	}
}
