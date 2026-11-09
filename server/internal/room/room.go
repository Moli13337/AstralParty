package room

import (
	"sync"

	pbmodel "astral-party-server/internal/pb/model"
)

// Store keeps volatile room state. Rooms live in memory only; player data
// stays in the per-player SQLite databases.
type Store struct {
	mu       sync.Mutex
	nextID   int64
	rooms    map[int64]*pbmodel.Room
	byPlayer map[int64]int64 // playerID -> roomID
	heroes   map[int64]int32 // playerID -> chosen hero id (per room lifetime)
}

func New() *Store {
	return &Store{
		nextID:   1,
		rooms:    make(map[int64]*pbmodel.Room),
		byPlayer: make(map[int64]int64),
		heroes:   make(map[int64]int32),
	}
}

// Create inserts a fully built room and binds its members. The caller
// populates players/state; every player in the room is bound byPlayer.
func (s *Store) Create(r *pbmodel.Room) *pbmodel.Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Id = s.nextID
	s.nextID++
	s.rooms[r.Id] = r
	for _, p := range r.Players {
		s.byPlayer[p.Id] = r.Id
	}
	return r
}

// GetByPlayer returns the room the player is currently in (nil if none).
func (s *Store) GetByPlayer(playerID int64) *pbmodel.Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byPlayer[playerID]
	if !ok {
		return nil
	}
	return s.rooms[id]
}

// Remove deletes a room and unbinds all its members.
func (s *Store) Remove(roomID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[roomID]
	if !ok {
		return
	}
	delete(s.rooms, roomID)
	for _, p := range r.Players {
		delete(s.byPlayer, p.Id)
	}
}

// SetHero records the hero a player picked during choice_hero.
func (s *Store) SetHero(playerID int64, heroID int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heroes[playerID] = heroID
}

// Hero returns the last recorded hero choice (0 if none).
func (s *Store) Hero(playerID int64) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heroes[playerID]
}

// Mutate runs fn against the player's room under the store lock.
func (s *Store) Mutate(playerID int64, fn func(r *pbmodel.Room)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byPlayer[playerID]
	if !ok {
		return
	}
	fn(s.rooms[id])
}
