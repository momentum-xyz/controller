package user

import (
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/momentum-xyz/controller/utils"
	"github.com/pkg/errors"
)

const (
	selectUserCountQuery = `SELECT count(*) FROM users;`
	getUserNameQuery     = `SELECT name FROM users WHERE id = ?;`
)

type Storage interface {
	GetUserName(id uuid.UUID) (string, error)
	SelectUserCount() (int, error)
}

type storage struct {
	db sqlx.DB
}

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

func (s *storage) SelectUserCount() (int, error) {
	rows, err := s.db.Query(selectUserCountQuery)
	if err != nil {
		return 0, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	numUsers := 0
	if rows.Next() {
		if err := rows.Scan(&numUsers); err != nil {
			return 0, errors.WithMessage(err, "failed to scan rows")
		}
	}

	return numUsers, nil
}

func (s *storage) GetUserName(id uuid.UUID) (string, error) {
	row := *s.db.QueryRow(getUserNameQuery, utils.BinId(id))
	if row.Err() != nil {
		return "", errors.WithMessage(row.Err(), "failed to query db")
	}
	var name string
	if err := row.Scan(&name); err != nil {
		return "", errors.WithMessage(err, "failed to scan row")
	}
	return name, nil
}
