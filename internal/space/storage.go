package space

import (
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"

	"database/sql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	selectSpaceById                      = `SELECT * FROM spaces WHERE id = ?;`
	selectSpaceTypeByUUID                = `SELECT * FROM space_types WHERE id = ?;`
	selectSpaceByUUID                    = `SELECT * FROM spaces WHERE spaceTypeId = ? AND child_placement IS NOT NULL;`
	selectTilesQuery                     = `SELECT permanentType,hash FROM tiles WHERE spaceId = ? AND render = 1 AND permanentType IS NOT NULL;`
	selectChildrenByParentIdQuery        = `SELECT spaces.id,spaces.visible,space_types.visible,spaces.position FROM spaces JOIN space_types WHERE space_types.id = spaces.spaceTypeId AND spaces.parentId =?;`
	selectSpaceAttributesById            = `SELECT a.name, flag, value FROM space_attributes INNER JOIN attributes a WHERE a.id = space_attributes.attributeId AND spaceId = ?;`
	selectChildrenEntriesByParentIDQuery = `SELECT spaces.*,s.visible AS st_visible FROM spaces JOIN space_types s WHERE s.id = spaces.spaceTypeId AND parentId =?;`
	selectOnlineSpaceIDsQuery            = `SELECT DISTINCT spaceId FROM online_users;`
	selectCheckOnlineSpaceByIDQuery      = `SELECT EXISTS(SELECT 1 FROM online_users WHERE spaceId = ? LIMIT 1);`
	selectStageModeUsersSpacesQuery      = `SELECT userId, spaceId FROM space_integration_users
											WHERE flag = 1
											AND integrationTypeId = (SELECT id
																		FROM integration_types
																		WHERE name = 'stage_mode'
																		LIMIT 1);`
	disableStageModeForUserInSpaceQuery = `UPDATE space_integration_users
											SET flag = 0
											WHERE userId = ?
											AND spaceId = ?
											AND integrationTypeId = (SELECT id
																		FROM integration_types
																		WHERE name = 'stage_mode'
																		LIMIT 1);`
)

type Storage interface {
	QuerySingleSpaceTypeByUUID(id uuid.UUID) (map[string]interface{}, error)
	QuerySingleSpaceBySpaceTypeId(id uuid.UUID) (map[string]interface{}, error)
	QuerySingleSpaceById(id uuid.UUID) (map[string]interface{}, error)
	LoadSpaceTileTextures(id uuid.UUID) (map[string]string, error)
	SelectChildrenSpacesByParentId(id []byte) ([]childrenSpace, error)
	SelectChildrenEntriesByParentId(id []byte) (map[uuid.UUID]map[string]interface{}, error)
	QuerySpaceAttributesById(id uuid.UUID) ([]spaceAttribute, error)
	GetStageModeUsersSpaces() ([]UserSpace, error)
	DisableStageModeForUserInSpace(userID, spaceID uuid.UUID) error
	GetOnlineSpaceIDs() ([]uuid.UUID, error)
	CheckOnlineSpaceByID(id uuid.UUID) (bool, error)
}

type UserSpace struct {
	UserID  uuid.UUID
	SpaceID uuid.UUID
}

var log = logger.L()

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

type storage struct {
	db sqlx.DB
}

func (s *storage) CheckOnlineSpaceByID(id uuid.UUID) (bool, error) {
	row, err := s.db.Query(selectCheckOnlineSpaceByIDQuery, utils.BinId(id))
	if err != nil {
		return false, errors.WithMessage(err, "failed to query db")
	}
	row.Close()

	if row.Next() {
		var exists int64
		if err := row.Scan(&exists); err != nil {
			return false, errors.WithMessage(err, "failed to scan row")
		}
		return exists == 1, nil
	}

	return false, utils.ErrNotFound
}

func (s *storage) GetOnlineSpaceIDs() ([]uuid.UUID, error) {
	rows, err := s.db.Query(selectOnlineSpaceIDsQuery)
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

func (s *storage) GetStageModeUsersSpaces() ([]UserSpace, error) {
	rows, err := s.db.Query(selectStageModeUsersSpacesQuery)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	ubid := make([]byte, 16)
	sbid := make([]byte, 16)
	res := make([]UserSpace, 0)
	for rows.Next() {
		if err := rows.Scan(&ubid, &sbid); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		uid, err := uuid.FromBytes(ubid)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to parse user id")
		}
		sid, err := uuid.FromBytes(sbid)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to parse space id")
		}
		res = append(res, UserSpace{
			UserID: uid, SpaceID: sid,
		})
	}

	return res, nil
}

func (s *storage) DisableStageModeForUserInSpace(userID, spaceID uuid.UUID) error {
	res, err := s.db.Exec(disableStageModeForUserInSpaceQuery, utils.BinId(userID), utils.BinId(spaceID))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return nil
	}

	log.Debugf("Space storage: DisableStageModeForUserInSpace: %s, %s, %d", userID, spaceID, affected)
	return nil
}

func (s *storage) QuerySpaceAttributesById(id uuid.UUID) ([]spaceAttribute, error) {
	rows, err := s.db.Query(selectSpaceAttributesById, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	results := make([]spaceAttribute, 0)
	for rows.Next() {
		sa := spaceAttribute{}
		if err := rows.Scan(&sa.Name, &sa.Flag, &sa.Value); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		results = append(results, sa)
	}

	return results, nil
}

func (s *storage) SelectChildrenSpacesByParentId(id []byte) ([]childrenSpace, error) {
	res, err := s.db.Queryx(selectChildrenByParentIdQuery, id)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer res.Close()

	var result []childrenSpace
	for res.Next() {
		var child childrenSpace
		if err := res.StructScan(&child); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		result = append(result, child)
	}

	return result, nil
}

func (s *storage) SelectChildrenEntriesByParentId(id []byte) (map[uuid.UUID]map[string]interface{}, error) {
	rows, err := s.db.Queryx(selectChildrenEntriesByParentIDQuery, id)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	result := make(map[uuid.UUID]map[string]interface{})
	flag := true
	var columns []string
	for rows.Next() {
		if flag {
			columns, err = rows.Columns()
			if err != nil {
				return nil, errors.WithMessage(err, "failed to get columns")
			}
			flag = false
		}
		count := len(columns)
		values := make([]interface{}, count)
		valuePtrs := make([]interface{}, count)

		for i := 0; i < count; i++ {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}

		entry := make(map[string]interface{})
		for i, col := range columns {
			var v interface{}
			val := values[i]
			b, ok := val.([]byte)
			if ok {
				v = string(b)
			} else {
				v = val
			}
			entry[col] = v
		}
		id, err := utils.DbToUuid(entry["id"])
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get id")
		}
		result[id] = entry
	}

	return result, nil
}

func (s *storage) LoadSpaceTileTextures(id uuid.UUID) (map[string]string, error) {
	res, err := s.db.Query(selectTilesQuery, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer res.Close()

	textures := make(map[string]string)
	for res.Next() {
		var permanentType string
		var hash sql.NullString
		if err := res.Scan(&permanentType, &hash); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		log.Debug("found tile", permanentType, hash)

		if hash.Valid {
			textures[permanentType] = hash.String
		}
	}

	return textures, nil
}

func (s *storage) QuerySingleSpaceById(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceById, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	return utils.LoadRow(rows)
}

func (s *storage) QuerySingleSpaceTypeByUUID(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceTypeByUUID, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	return utils.LoadRow(rows)
}

func (s *storage) QuerySingleSpaceBySpaceTypeId(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceByUUID, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	return utils.LoadRow(rows)
}
