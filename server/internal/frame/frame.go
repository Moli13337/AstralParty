package frame

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const HeadLength = 35

var (
	ErrFrameTooLarge = errors.New("frame too large")
	ErrShortHeader   = errors.New("short header")
)

type Frame struct {
	Length    int32
	SessionID int64
	CmdID     int16
	Ver1      byte
	Ver2      byte
	Ver3      byte
	UpSN      int64
	DownSN    int64
	Err       int16
	Payload   []byte
}

func Decode(header []byte, payload []byte) (*Frame, error) {
	if len(header) < HeadLength {
		return nil, ErrShortHeader
	}
	f := &Frame{
		Length:    int32(binary.BigEndian.Uint32(header[0:4])),
		SessionID: int64(binary.BigEndian.Uint64(header[4:12])),
		CmdID:     int16(binary.BigEndian.Uint16(header[12:14])),
		Ver1:      header[14],
		Ver2:      header[15],
		Ver3:      header[16],
		UpSN:      int64(binary.BigEndian.Uint64(header[17:25])),
		DownSN:    int64(binary.BigEndian.Uint64(header[25:33])),
		Err:       int16(binary.BigEndian.Uint16(header[33:35])),
		Payload:   payload,
	}
	return f, nil
}

func (f *Frame) Encode() []byte {
	out := make([]byte, HeadLength+len(f.Payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(f.Payload)))
	binary.BigEndian.PutUint64(out[4:12], uint64(f.SessionID))
	binary.BigEndian.PutUint16(out[12:14], uint16(f.CmdID))
	out[14] = f.Ver1
	out[15] = f.Ver2
	out[16] = f.Ver3
	binary.BigEndian.PutUint64(out[17:25], uint64(f.UpSN))
	binary.BigEndian.PutUint64(out[25:33], uint64(f.DownSN))
	binary.BigEndian.PutUint16(out[33:35], uint16(f.Err))
	copy(out[HeadLength:], f.Payload)
	return out
}

func (f *Frame) String() string {
	return fmt.Sprintf("cmd=%d sess=%d up=%d down=%d err=%d len=%d",
		f.CmdID, f.SessionID, f.UpSN, f.DownSN, f.Err, len(f.Payload))
}
