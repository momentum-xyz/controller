package user

import (
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/momentum-xyz/controller/utils"
	"github.com/pkg/errors"
)

const (
	getUserCountQuery = `SELECT count(*) FROM users;`
	getUserNameQuery  = `SELECT name FROM users WHERE id = ?;`
	getUserIDsQuery   = `SELECT id FROM users;`
	getOnlineUserIDs  = `SELECT DISTINCT userId FROM online_users`
)

type Storage interface {
	GetUserName(id uuid.UUID) (string, error)
	GetUserCount() (int, error)
	GetUserIDs() ([]uuid.UUID, error)
	GetOnlineUserIDs() ([]uuid.UUID, error)
}

type storage struct {
	db sqlx.DB
}

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

func (s *storage) GetOnlineUserIDs() ([]uuid.UUID, error) {
	rows, err := s.db.Query(getOnlineUserIDs)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	bid := make([]byte, 16)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		if err := rows.Scan(&bid); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		id, err := uuid.FromBytes(bid)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to parse id")
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func (s *storage) GetUserIDs() ([]uuid.UUID, error) {
	rows, err := s.db.Query(getUserIDsQuery)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	bid := make([]byte, 16)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		if err := rows.Scan(&bid); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		id, err := uuid.FromBytes(bid)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to parse id")
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func (s *storage) GetUserCount() (int, error) {
	rows, err := s.db.Query(getUserCountQuery)
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
