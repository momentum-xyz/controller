package user

import (
	"github.com/jmoiron/sqlx"
)

const (
	selectUserCountQuery = `select count(*) from users;`
)

type Storage interface {
	SelectUserCount() int
}

type storage struct {
	db sqlx.DB
}

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

func (s *storage) SelectUserCount() int {
	rows, err := s.db.Query(selectUserCountQuery)
	if err != nil {
		return 0
	}
	numUsers := 0
	defer rows.Close()
	if rows.Next() {
		rows.Scan(&numUsers)
	}
	return numUsers
}
