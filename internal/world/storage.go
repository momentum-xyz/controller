package world

import (
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	selectWorldNameByIdQuery         = `SELECT name FROM spaces WHERE id = ? LIMIT 1;`
	selectAllSpacesQuery             = `SELECT id FROM spaces WHERE parentId = ? AND id != ?;`
	removeWorldOnlineUsersQuery      = `DELETE FROM online_users WHERE userId = ? AND GetParentWorldByID(spaceId) = ?;`
	removeUserDynamicMembershipQuery = `DELETE FROM user_spaces_dynamic WHERE userId = ?;`
)

type Storage interface {
	GetName(id uuid.UUID) (string, error)
	GetWorlds() ([]uuid.UUID, error)
	RemoveWorldOnlineUser(worldID, userID uuid.UUID) error
	RemoveUserDynamicMembership(userID uuid.UUID) error
}

type storage struct {
	db sqlx.DB
}

var log = logger.L()

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

func (s *storage) GetWorlds() ([]uuid.UUID, error) {
	res, err := s.db.Query(selectAllSpacesQuery, utils.BinId(uuid.Nil), utils.BinId(uuid.Nil))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer res.Close()

	worlds := make([]uuid.UUID, 0)
	for res.Next() {
		id := make([]byte, 16)
		if err := res.Scan(&id); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		worldId, err := uuid.FromBytes(id)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to parse world id")
		}
		log.Info("Config Id:", worldId)
		worlds = append(worlds, worldId)
	}

	return worlds, nil
}

func (s *storage) GetName(id uuid.UUID) (string, error) {
	rows, err := s.db.Query(selectWorldNameByIdQuery, utils.BinId(id))
	if err != nil {
		return "", errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	var wName string
	if rows.Next() {
		if err := rows.Scan(&wName); err != nil {
			return "", errors.WithMessage(err, "failed to scan rows")
		}
	}

	return wName, nil
}

func (s *storage) RemoveWorldOnlineUser(userID, worldID uuid.UUID) error {
	res, err := s.db.Exec(removeWorldOnlineUsersQuery, utils.BinId(userID), utils.BinId(worldID))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
	}

	log.Debugf("World storage: RemoveWorldOnlineUser: %s, %s, %d", userID, worldID, affected)
	return nil
}

func (s *storage) RemoveUserDynamicMembership(userID uuid.UUID) error {
	res, err := s.db.Exec(removeUserDynamicMembershipQuery, utils.BinId(userID))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
	}

	log.Debugf("World storage: RemoveUserDynamicMembership: %s, %d", userID.String(), affected)
	return nil
}
