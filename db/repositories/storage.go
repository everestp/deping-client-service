package repositories

import "database/sql"

// Storage is the top-level dependency container for all repositories.
// Wire it once at startup and inject into services.
type Storage struct {
	Users    UserRepository
	Telegram TelegramRepository
}

func NewStorage(db *sql.DB) *Storage {
	return &Storage{
		Users:    NewUserRepository(db),
		Telegram: NewTelegramRepository(db),
	}
}
