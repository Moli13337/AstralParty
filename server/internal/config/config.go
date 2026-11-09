package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddr string
	DataDir    string
	CipherOn   bool
	MaxFrame   int32
	CipherKey  string
	CdnBaseURL string
}

func Default() *Config {
	dataDir := os.Getenv("AP_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	return &Config{
		ListenAddr: ":8800",
		DataDir:    dataDir,
		CipherOn:   false,
		MaxFrame:   1 << 22,
		CipherKey:  "test",
		CdnBaseURL: "http://127.0.0.1:8810",
	}
}

func (c *Config) PlayersDir() string {
	return filepath.Join(c.DataDir, "players")
}
