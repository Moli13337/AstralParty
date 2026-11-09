package handler

import (
	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/dispatch"
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	pbmodel "astral-party-server/internal/pb/model"
	"astral-party-server/internal/session"
)

const (
	CmdFriendListC2S         = 5163
	CmdFriendListS2C         = 5164
	CmdGetRecommendGuildsC2S = 9047
	CmdGetRecommendGuildsS2C = 9048
)

// FriendListS2C.isEnd must be true or the friend panel keeps polling for the
// next page of an empty list.
func friendList(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		req := &pb.FriendListC2S{}
		if err := proto.Unmarshal(f.Payload, req); err != nil {
			return err
		}
		return s.SendReply(CmdFriendListS2C, f.UpSN, int16(code.Code_Succ), &pb.FriendListS2C{
			Friends: []*pb.FriendInfo{},
			IsEnd:   true,
		})
	}
}

// GetRecommendGuildsS2C carries an empty guild list.
func getRecommendGuilds(deps *Deps) dispatch.Handler {
	return func(s *session.Session, f *frame.Frame) error {
		return s.SendReply(CmdGetRecommendGuildsS2C, f.UpSN, int16(code.Code_Succ), &pb.GetRecommendGuildsS2C{
			Guilds: []*pbmodel.Guild{},
		})
	}
}
