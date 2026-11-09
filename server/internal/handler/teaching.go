package handler

import (
	"astral-party-server/internal/frame"
	pb "astral-party-server/internal/pb"
	"astral-party-server/internal/pb/code"
	"astral-party-server/internal/session"
)

const (
	CmdTeachingC2S = 5121
	CmdTeachingS2C = 5122
)

// TutorialLogic.FinishTutorial fires TeachingC2S after the novice flow
// (trial-hero pick) completes and waits for the TeachingS2C callback; leaving
// it unanswered trips the 15s client RPC timeout and a ReLogin loop. The S2C
// message is empty, so an empty reply is the full protocol.
func teaching(s *session.Session, f *frame.Frame) error {
	return s.SendReply(CmdTeachingS2C, f.UpSN, int16(code.Code_Succ), &pb.TeachingS2C{})
}
