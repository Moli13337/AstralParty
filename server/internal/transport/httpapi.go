package transport

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

// The official 8800 port multiplexes HTTP config APIs and the protobuf TCP
// protocol on one listener. The client starts by sending
// GET /api/hotaddressExtend/get?route=..&version=.. (hot-update config) and
// GET /api/hotaddressServer/get (game server address) before switching to
// raw TCP frames, so peek the first bytes of every connection and route.
func isHTTPHint(head []byte) bool {
	return bytes.HasPrefix(head, []byte("GET ")) ||
		bytes.HasPrefix(head, []byte("POST")) ||
		bytes.HasPrefix(head, []byte("HEAD ")) ||
		bytes.HasPrefix(head, []byte("PUT ")) ||
		bytes.HasPrefix(head, []byte("OPTIO"))
}

type httpAPI struct {
	// SourceURL is the hot-update bundle root returned as
	// HotUpdateConfigData.sourceUrl. ServerURL is the game TCP endpoint
	// returned inside RemoteServerConfigData.serverUrl.
	SourceURL string
	ServerURL string
	NoticeURL string
	Version   string
	SdkVer    string
}

func (a *httpAPI) handleConn(br *bufio.Reader, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	// drain headers until empty line
	for {
		line, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	parts := strings.Fields(strings.TrimSpace(reqLine))
	if len(parts) < 2 {
		return
	}
	path := parts[1]
	if q := strings.IndexByte(path, '?'); q >= 0 {
		path = path[:q]
	}
	var body string
	switch path {
	case "/api/hotaddressExtend/get":
		body = a.hotUpdateConfig()
	case "/api/hotaddressServer/get":
		body = a.remoteServerConfig()
	default:
		log.Printf("[http] 404 %s", path)
		writeHTTP(conn, 404, "{}")
		return
	}
	log.Printf("[http] 200 %s -> %s", path, body)
	writeHTTP(conn, 200, body)
}

// Responses are dual-format: flat fields for JsonUtility.FromJson<T> and a
// content.data wrapper for SimpleJSON-style lookups; each parser picks the
// keys it knows and ignores the rest.
func (a *httpAPI) hotUpdateConfig() string {
	flat := fmt.Sprintf(
		`{"version":%q,"sourceUrl":%q,"sdkVersion":%q,"code":0,"content":{"data":{"version":%q,"sourceUrl":%q,"sdkVersion":%q}}}`,
		a.Version, a.SourceURL, a.SdkVer, a.Version, a.SourceURL, a.SdkVer)
	return flat
}

func (a *httpAPI) remoteServerConfig() string {
	flat := fmt.Sprintf(
		`{"version":%q,"noticeUrl":%q,"serverUrl":%q,"isAuditMode":false,"isAngelMode":false,"code":0,"content":{"data":{"version":%q,"noticeUrl":%q,"serverUrl":%q,"isAuditMode":false,"isAngelMode":false}}}`,
		a.Version, a.NoticeURL, a.ServerURL, a.Version, a.NoticeURL, a.ServerURL)
	return flat
}

func writeHTTP(conn net.Conn, code int, body string) {
	status := "200 OK"
	if code == 404 {
		status = "404 Not Found"
	}
	resp := fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, len(body), body)
	_, _ = io.WriteString(conn, resp)
}
