package space

import (
	"database/sql"

	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const (
	selectSpaceById               = `SELECT * FROM spaces WHERE id = ?`
	selectSpaceTypeByUUID         = `SELECT * FROM space_types WHERE id = ?`
	selectSpaceByUUID             = `SELECT * FROM spaces WHERE spaceTypeId = ? AND child_placement IS NOT NULL`
	selectTilesQuery              = `SELECT permanentType,hash FROM tiles WHERE spaceId = ? AND render = 1 AND permanentType IS NOT NULL`
	selectChildrenByParentIdQuery = `SELECT spaces.id,spaces.visible,space_types.visible,spaces.position FROM spaces join space_types WHERE space_types.id = spaces.spaceTypeId and spaces.parentId =?;`
	selectSpaceAttributesById     = `select a.name, flag, value from space_attributes inner join attributes a where a.id = space_attributes.attributeId and spaceId = ?`
)

var log = logger.L()

type Storage interface {
	QuerySingleSpaceTypeByUUID(id uuid.UUID) (map[string]interface{}, error)
	QuerySingleSpaceBySpaceTypeId(id uuid.UUID) (map[string]interface{}, error)
	QuerySingleSpaceById(id uuid.UUID) (map[string]interface{}, error)
	LoadSpaceTileTextures(id uuid.UUID) map[string]string
	SelectChildrenSpacesByParentId(id []byte) ([]childrenSpace, error)
	SelectChildrenEntriesByParentId(id []byte) (map[uuid.UUID]map[string]interface{}, error)
	QuerySpaceAttributesById(id uuid.UUID) ([]spaceAttribute, error)
}

func NewStorage(db sqlx.DB) Storage {
	return &storage{
		db: db,
	}
}

type storage struct {
	db sqlx.DB
}

func (s *storage) QuerySpaceAttributesById(id uuid.UUID) ([]spaceAttribute, error) {
	rows, err := s.db.Query(selectSpaceAttributesById, utils.BinId(id))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer rows.Close()

	results := make([]spaceAttribute, 0)
	for rows.Next() {
		sa := spaceAttribute{}
		if err := rows.Scan(&sa.Name, &sa.Flag, &sa.Value); err == nil {
			results = append(results, sa)
		} else {
			log.Debug(err)
		}
	}

	return results, nil
}

func (s *storage) SelectChildrenSpacesByParentId(id []byte) ([]childrenSpace, error) {
	res, err := s.db.Queryx(selectChildrenByParentIdQuery, id)
	defer res.Close()
	if err != nil {
		log.Info(err)
		return nil, err
	}
	var result []childrenSpace
	for res.Next() {
		var child childrenSpace
		_ = res.StructScan(&child)
		result = append(result, child)
	}
	return result, nil
}

func (s *storage) SelectChildrenEntriesByParentId(id []byte) (map[uuid.UUID]map[string]interface{}, error) {
	rows, err := s.db.Queryx(
		`SELECT spaces.*,s.visible as st_visible FROM spaces join space_types s WHERE s.id = spaces.spaceTypeId and parentId =?`,
		id,
	)
	defer rows.Close()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	result := make(map[uuid.UUID]map[string]interface{})

	flag := true
	var columns []string
	for rows.Next() {
		if flag {
			columns, err = rows.Columns()
			flag = false
			if err != nil {
				return nil, err
			}
		}
		count := len(columns)
		values := make([]interface{}, count)
		valuePtrs := make([]interface{}, count)

		for i := 0; i < count; i++ {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			log.Error(err)
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
		id := utils.DbToUuid(entry["id"])
		result[id] = entry
	}
	return result, nil
}

func (s *storage) LoadSpaceTileTextures(id uuid.UUID) map[string]string {
	textures := make(map[string]string)
	res, err := s.db.Query(selectTilesQuery, utils.BinId(id))
	defer res.Close()

	if err != nil {
		log.Error(err)
	}

	for res.Next() {
		var permanentType string
		var hash sql.NullString
		if err := res.Scan(&permanentType, &hash); err != nil {
			log.Error(err)
		}
		log.Debug("found tile", permanentType, hash)

		if hash.Valid {
			textures[permanentType] = hash.String
		}
	}
	return textures
}

func (s *storage) QuerySingleSpaceById(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceById, utils.BinId(id))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer rows.Close()
	retval := utils.LoadRow(rows)
	return retval, nil
}

func (s *storage) QuerySingleSpaceTypeByUUID(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceTypeByUUID, utils.BinId(id))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer rows.Close()
	retval := utils.LoadRow(rows)
	return retval, nil
}

func (s *storage) QuerySingleSpaceBySpaceTypeId(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.Query(selectSpaceByUUID, utils.BinId(id))
	if err != nil {
		log.Error(err)
		return nil, err
	}

	defer rows.Close()
	retval := utils.LoadRow(rows)

	return retval, nil
}
