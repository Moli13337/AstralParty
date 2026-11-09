package handler

import "time"

func nowUnix() int64 {
	return time.Now().Unix()
}

func nowUnixMilli() int64 {
	return time.Now().UnixMilli()
}
