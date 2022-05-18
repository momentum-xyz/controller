package world

import (
	"errors"

	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const (
	selectWorldNameByIdQuery     = `SELECT name FROM spaces WHERE id = ? LIMIT 1;`
	selectAllSpacesQuery         = `SELECT id FROM spaces WHERE parentId = ? AND id != ?;`
	deleteOnlineUsersQuery       = `DELETE FROM online_users WHERE spaceId = ?;`
	deleteDynamicMembershipQuery = `DELETE FROM user_spaces_dynamic WHERE spaceId = ?;`
)

var log = logger.L()

type Storage interface {
	GetName(id uuid.UUID) (string, error)
	GetWorlds() []uuid.UUID
	CleanOnlineUsers(spaceID uuid.UUID) error
	CleanDynamicMembership(spaceID uuid.UUID) error
}

type storage struct {
	db sqlx.DB
}

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

func (s *storage) GetWorlds() []uuid.UUID {
	res, _ := s.db.Query(selectAllSpacesQuery, utils.BinId(uuid.Nil), utils.BinId(uuid.Nil))
	defer res.Close()
	worlds := make([]uuid.UUID, 0)
	for res.Next() {
		id := make([]byte, 16)
		if err := res.Scan(&id); err != nil {
			log.Error(err)
		}
		worldId, err := uuid.FromBytes(id)
		if err == nil {
			log.Info("Config Id:", worldId)
			worlds = append(worlds, worldId)
		}
	}
	return worlds
}

func (s *storage) GetName(id uuid.UUID) (string, error) {
	rows, err := s.db.Query(selectWorldNameByIdQuery, utils.BinId(id))
	if err != nil {
		return "", errors.New("GetWorldName: No world not found in DB")
	}
	defer rows.Close()
	var wName string
	if rows.Next() {
		err := rows.Scan(&wName)
		if err != nil {
			return "", err
		}
	}
	return wName, nil
}

func (s *storage) CleanOnlineUsers(spaceID uuid.UUID) error {
	res, err := s.db.Exec(deleteOnlineUsersQuery, utils.BinId(spaceID))
	if err != nil {
		log.Warnf("error: %+v", err)
	}

	var affected int64
	affected, err = res.RowsAffected()
	log.Debug("World storage: CleanOnlineUsers:", spaceID.String(), affected)

	return err
}

func (s *storage) CleanDynamicMembership(spaceID uuid.UUID) error {
	res, err := s.db.Exec(deleteDynamicMembershipQuery, utils.BinId(spaceID))
	if err != nil {
		log.Warnf("error: %+v", err)
	}

	var affected int64
	affected, err = res.RowsAffected()
	log.Debug("World storage: CleanDynamicMembership:", spaceID.String(), affected)

	return err
}
