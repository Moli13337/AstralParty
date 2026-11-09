package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	AuthDev    = 0
	AuthSteam  = 1
	AuthChina  = 2
	AuthAbroad = 3
)

type PlayerRow struct {
	ID          int64
	Name        string
	Level       int32
	Exp         int32
	Gold        int32
	CreatedAt   int64
	LastLoginAt int64
}

type Store struct {
	dataDir string
	index   *sql.DB
	players sync.Map
}

func Open(dataDir string, startUID int64) (*Store, error) {
	playersDir := filepath.Join(dataDir, "players")
	if err := os.MkdirAll(playersDir, 0o755); err != nil {
		return nil, err
	}
	idx, err := sql.Open("sqlite", filepath.Join(dataDir, "index.db"))
	if err != nil {
		return nil, err
	}
	if startUID <= 0 {
		startUID = 1
	}
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS account (
			auth_type  INTEGER NOT NULL,
			auth_id    TEXT    NOT NULL,
			player_id  INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (auth_type, auth_id)
		)`,
		`CREATE TABLE IF NOT EXISTS seq_player_id (id INTEGER NOT NULL)`,
		// startUID only seeds a fresh database; an existing sequence is kept.
		fmt.Sprintf(`INSERT INTO seq_player_id (id) SELECT %d WHERE NOT EXISTS (SELECT 1 FROM seq_player_id)`, startUID),
	} {
		if _, err := idx.Exec(stmt); err != nil {
			_ = idx.Close()
			return nil, err
		}
	}
	return &Store{dataDir: dataDir, index: idx}, nil
}

func (s *Store) Close() error {
	var firstErr error
	s.players.Range(func(_, v any) bool {
		if err := v.(*sql.DB).Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	if err := s.index.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func DevAuthKey(nick string) (int32, string) {
	return AuthDev, "dev:" + nick
}

// SteamAuthKey keys an account by the 64-bit SteamID the client forwards in
// ConnectC2S.steam.id (the same value the patch config injects).
func SteamAuthKey(id uint64) (int32, string) {
	return AuthSteam, fmt.Sprintf("steam:%d", id)
}

// AbroadAuthKey keys an account by the overseas SDK source and session id the
// client forwards in ConnectC2S.abroad (the INT client's login path).
func AbroadAuthKey(source int32, sid string) (int32, string) {
	return AuthAbroad, fmt.Sprintf("abroad:%d:%s", source, sid)
}

// ChinaAuthKey keys an account by the SDK game id and session id that the
// client forwards from the (patched) platform login.
func ChinaAuthKey(gameID string, sid string) (int32, string) {
	return AuthChina, "china:" + gameID + ":" + sid
}

func (s *Store) FindPlayer(authType int32, authID string) (int64, error) {
	var playerID int64
	err := s.index.QueryRow(
		`SELECT player_id FROM account WHERE auth_type = ? AND auth_id = ?`,
		authType, authID,
	).Scan(&playerID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return playerID, err
}

func (s *Store) CreatePlayer(authType int32, authID string, nick string) (*PlayerRow, error) {
	tx, err := s.index.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var playerID int64
	if err = tx.QueryRow(`SELECT id FROM seq_player_id`).Scan(&playerID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE seq_player_id SET id = id + 1`); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if _, err = tx.Exec(
		`INSERT INTO account (auth_type, auth_id, player_id, created_at) VALUES (?, ?, ?, ?)`,
		authType, authID, playerID, now,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	row := &PlayerRow{
		ID:          playerID,
		Name:        nick,
		Level:       1,
		CreatedAt:   now,
		LastLoginAt: now,
	}
	if err := s.savePlayerRow(row); err != nil {
		return nil, err
	}
	return row, nil
}

// GetOrCreatePlayerByID logs into the exact player id (transfer-code login:
// the client sends the typed code as DevInfo.nick and no password is checked).
// Missing ids are created on the spot and the auto-increment sequence is kept
// past them so later self-registered accounts never collide.
func (s *Store) GetOrCreatePlayerByID(playerID int64, nick string) (*PlayerRow, bool, error) {
	pdb, err := s.playerDB(playerID)
	if err != nil {
		return nil, false, err
	}
	row := &PlayerRow{ID: playerID}
	err = pdb.QueryRow(
		`SELECT name, level, exp, gold, created_at, last_login_at FROM player WHERE id = ?`,
		playerID,
	).Scan(&row.Name, &row.Level, &row.Exp, &row.Gold, &row.CreatedAt, &row.LastLoginAt)
	if err == nil {
		return row, false, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	tx, err := s.index.Begin()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	if _, err = tx.Exec(
		`INSERT OR IGNORE INTO account (auth_type, auth_id, player_id, created_at) VALUES (?, ?, ?, ?)`,
		AuthDev, fmt.Sprintf("uid:%d", playerID), playerID, now,
	); err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(
		`UPDATE seq_player_id SET id = ? WHERE id < ?`,
		playerID+1, playerID+1,
	); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	row = &PlayerRow{ID: playerID, Name: nick, Level: 1, CreatedAt: now, LastLoginAt: now}
	if err := s.savePlayerRow(row); err != nil {
		return nil, false, err
	}
	return row, true, nil
}

func (s *Store) LoadPlayer(playerID int64) (*PlayerRow, error) {
	pdb, err := s.playerDB(playerID)
	if err != nil {
		return nil, err
	}
	row := &PlayerRow{ID: playerID}
	err = pdb.QueryRow(
		`SELECT name, level, exp, gold, created_at, last_login_at FROM player WHERE id = ?`,
		playerID,
	).Scan(&row.Name, &row.Level, &row.Exp, &row.Gold, &row.CreatedAt, &row.LastLoginAt)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) TouchLogin(playerID int64) error {
	pdb, err := s.playerDB(playerID)
	if err != nil {
		return err
	}
	_, err = pdb.Exec(`UPDATE player SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), playerID)
	return err
}

// LoadPlayerProto returns the raw serialized model.Player blob (nil if unset).
func (s *Store) LoadPlayerProto(playerID int64) ([]byte, error) {
	pdb, err := s.playerDB(playerID)
	if err != nil {
		return nil, err
	}
	var blob []byte
	err = pdb.QueryRow(`SELECT proto FROM player WHERE id = ?`, playerID).Scan(&blob)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

// SavePlayerProto persists the serialized Player payload.
func (s *Store) SavePlayerProto(playerID int64, blob []byte) error {
	pdb, err := s.playerDB(playerID)
	if err != nil {
		return err
	}
	_, err = pdb.Exec(`UPDATE player SET proto = ? WHERE id = ?`, blob, playerID)
	return err
}

func (s *Store) savePlayerRow(row *PlayerRow) error {
	pdb, err := s.playerDB(row.ID)
	if err != nil {
		return err
	}
	_, err = pdb.Exec(
		`INSERT INTO player (id, name, level, exp, gold, created_at, last_login_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name, last_login_at = excluded.last_login_at`,
		row.ID, row.Name, row.Level, row.Exp, row.Gold, row.CreatedAt, row.LastLoginAt,
	)
	return err
}

func (s *Store) playerDB(playerID int64) (*sql.DB, error) {
	if v, ok := s.players.Load(playerID); ok {
		return v.(*sql.DB), nil
	}
	path := filepath.Join(s.dataDir, "players", fmt.Sprintf("%d.db", playerID))
	pdb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS player (
			id            INTEGER PRIMARY KEY,
			name          TEXT    NOT NULL,
			level         INTEGER NOT NULL DEFAULT 1,
			exp           INTEGER NOT NULL DEFAULT 0,
			gold          INTEGER NOT NULL DEFAULT 0,
			created_at    INTEGER NOT NULL,
			last_login_at INTEGER NOT NULL,
			proto         BLOB
		)`,
	} {
		if _, err := pdb.Exec(stmt); err != nil {
			_ = pdb.Close()
			return nil, err
		}
	}
	actual, _ := s.players.LoadOrStore(playerID, pdb)
	if actual != pdb {
		_ = pdb.Close()
	}
	return actual.(*sql.DB), nil
}
