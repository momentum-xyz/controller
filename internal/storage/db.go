package storage

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	getUserNameQuery                                = `SELECT name FROM users WHERE id = ?`
	queryWorldConfigQuery                           = `SELECT config FROM world_definition WHERE id = ?`
	getUserInfoQuery                                = `SELECT name, userTypeId FROM users WHERE id = ?;`
	getRandomWorldQuery                             = `SELECT id FROM spaces WHERE parentId = 0x00000000000000000000000000000000 AND id != 0x00000000000000000000000000000000;`
	getDefaultEntranceWorldQuery                    = `SELECT value FROM node_settings WHERE name = 'EntranceWorld';`
	getWorldByURLQuery                              = `SELECT worldId FROM url_mapping WHERE URL = ?;`
	getParentWorldQuery                             = `SELECT GetParentWorldByID(id) FROM spaces WHERE  id = ?;`
	getastUserWorldQuery                            = `SELECT worldId FROM user_lkp WHERE userId = ? ORDER BY updated_at DESC;`
	isSpacePresentQuery                             = `SELECT id FROM spaces WHERE id = ? LIMIT 1;`
	removeOnlineUserByIdAndSpaceIdQuery             = `DELETE FROM online_users WHERE userId = ? AND spaceId = ?;`
	insertOnlineUserByIdAndSpaceIdQuery             = `INSERT INTO online_users (userId,spaceId,updated_at) VALUES (?,?,NOW()) ON DUPLICATE KEY UPDATE updated_at=NOW();`
	removeDynamicWorldMembershipByIdAndWorldIdQuery = `DELETE FROM user_spaces_dynamic WHERE userId = ? AND spaceId = ?;`
	removeFromUsersQuery                            = `DELETE FROM users WHERE id = ?;`
	removeManyFromUsersQuery                        = `DELETE FROM users WHERE id IN(?);`
	getUserLastKnownPositionQuery                   = `SELECT spaceId,x,y,z FROM user_lkp WHERE  userId = ? AND worldId = ?;`
	getWorldDefaultSpawnPositionQuery               = `SELECT SpawnSpace,SpawnDislocation FROM world_definition WHERE id = ?;`
	getGuestUserTypeIDQuery                         = `SELECT id FROM user_types WHERE name = ?;`
	getUserIDsByTypeQuery                           = `SELECT id FROM users WHERE userTypeId = ?;`
	updateHighFivesQuery                            = `INSERT INTO high_fives (senderId,receiverId,created_at,updated_at,cnt) VALUES (?,?,NOW(),NOW(),1)
														ON DUPLICATE KEY UPDATE cnt = cnt+1, updated_at = NOW();`
	writeLastKnownPositionQuery = `INSERT INTO user_lkp (userId,worldId,spaceId,x,y,z,created_at,updated_at) 
														VALUES (?,?,?,?,?,?,NOW(),?) 
														ON DUPLICATE KEY UPDATE 
														worldId = ?, spaceId = ?, x = ?, y = ?, z = ?, updated_at = ?;`
)

const (
	mysqlDriverName    = "mysql"
	maxOpenConnections = 100
)

var log = logger.L()

type Database struct {
	*sqlx.DB
}

func OpenDB(cfg *config.MySQL) *Database {
	sqlconfig := cfg.GenConfig()
	DB, err := sqlx.Open(mysqlDriverName, sqlconfig.FormatDSN())
	if err != nil {
		log.Warnf("error: %+v", err)
	}
	DB.SetMaxOpenConns(maxOpenConnections)
	return &Database{DB: DB}
}

func (DB *Database) GetParentWorld(sid uuid.UUID) (uuid.UUID, error) {
	bid := make([]byte, 16)
	query := getParentWorldQuery

	rows, err := DB.Query(query, utils.BinId(sid))

	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&bid); err != nil {
				log.Error(err)
			}
			wid, err := uuid.FromBytes(bid)
			if err == nil {
				return wid, nil
			}
		}
	}
	return uuid.Nil, errors.New("DB query error for parent world")
}

func (DB *Database) isSpacePresent(id uuid.UUID) bool {
	query := isSpacePresentQuery
	rows1a, err := DB.Query(query, utils.BinId(id))
	//noinspection GoUnhandledErrorResult
	defer rows1a.Close()

	return err == nil && rows1a.Next()
}

func (DB *Database) GetWorldByURL(URL *url.URL) (uuid.UUID, bool) {
	query := getWorldByURLQuery
	domain := URL.String()
	log.Info("Quering domain:", domain)
	rows, err := DB.Query(query, domain)
	log.Info("Spawn flow: G1")
	if err == nil {
		log.Info("Spawn flow: G2")
		//noinspection GoUnhandledErrorResult
		defer rows.Close()
		log.Info("Spawn flow: G3")
		if rows.Next() {
			log.Info("Spawn flow: G4")
			bindata := make([]byte, 16)
			log.Info("Spawn flow: G5")
			if err := rows.Scan(&bindata); err != nil {
				log.Error(err)
			}
			worldId, err := uuid.FromBytes(bindata)
			log.Info("Spawn flow: G6")
			if err == nil && DB.isSpacePresent(worldId) {
				log.Info("Spawn world it forced by URL map to:", worldId)
				return worldId, true
			}
			log.Info("Spawn flow: G7")
		}
	}
	log.Info("Spawn flow: G8")
	return uuid.Nil, false
}

func (DB *Database) GetastUserWorld(uid uuid.UUID) (uuid.UUID, bool) {
	query := getastUserWorldQuery
	rows, err := DB.Query(query, utils.BinId(uid))
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows.Close()
		if rows.Next() {
			bindata := make([]byte, 16)
			if err := rows.Scan(&bindata); err != nil {
				log.Error(err)
			}
			worldId, err := uuid.FromBytes(bindata)
			if err == nil && DB.isSpacePresent(worldId) {
				log.Info("Last visited world for user", uid, "is", worldId)
				return worldId, true
			}
		}
	}
	return uuid.Nil, false
}

func (DB *Database) GetDefaultEntranceWorld() (uuid.UUID, bool) {
	rows1, err := DB.Query(getDefaultEntranceWorldQuery)
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows1.Close()
		if rows1.Next() {
			var worldString string
			if err := rows1.Scan(&worldString); err != nil {
				log.Error(err)
			}
			worldId, err := uuid.Parse(worldString)
			if err == nil && DB.isSpacePresent(worldId) {
				log.Infof("Using Entrance Config %s", worldId)
				return worldId, true
			}
		}
	}
	return uuid.Nil, false
}

func (DB *Database) GetRandomWorld() (uuid.UUID, bool) {
	rows2, err := DB.Query(getRandomWorldQuery)
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows2.Close()
		if rows2.Next() {
			var bindata []byte
			if err := rows2.Scan(&bindata); err != nil {
				log.Error(err)
			}

			worldId, err := uuid.FromBytes(bindata[:])
			if err == nil && DB.isSpacePresent(worldId) {
				return worldId, true
			}
		}
	}
	return uuid.Nil, false
}

func (DB *Database) GetUserLastKnownPosition(UserId, WorldId uuid.UUID) (uuid.UUID, cmath.Vec3, bool) {
	rows, err := DB.Query(getUserLastKnownPositionQuery, utils.BinId(UserId), utils.BinId(WorldId))
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows.Close()
		if rows.Next() {
			bindata := make([]byte, 16)
			var v cmath.Vec3
			if err := rows.Scan(&bindata, &(v.X), &(v.Y), &(v.Z)); err != nil {
				log.Error(err)
			}
			spaceId, err := uuid.FromBytes(bindata)
			if err == nil && DB.isSpacePresent(spaceId) {
				log.Info(
					"User "+UserId.String()+" last known space on world "+WorldId.String()+" is "+spaceId.String(), v,
				)
				return spaceId, v, true
			}
		}
	}

	return uuid.Nil, cmath.Vec3{}, false
}

func (DB *Database) GetWorldDefauleSpawnPositon(WorldId uuid.UUID) (uuid.UUID, cmath.Vec3, bool) {
	rows3, err := DB.Query(getWorldDefaultSpawnPositionQuery, utils.BinId(WorldId))
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer rows3.Close()
		if rows3.Next() {
			var posData []byte
			var spaceData []byte
			if err := rows3.Scan(&spaceData, &posData); err != nil {
				log.Error(err)
			}
			var pos cmath.Vec3
			var spaceId uuid.UUID
			var err error
			err = json.Unmarshal(posData, &pos)
			if err == nil {
				spaceId, err = uuid.FromBytes(spaceData)
				if err == nil && DB.isSpacePresent(spaceId) {
					return spaceId, pos, true
				}
			}
		}
	}

	return uuid.Nil, cmath.Vec3{}, false
}

func (DB *Database) GetUserSpawnPositionInWorld(uid, worldId uuid.UUID) (uuid.UUID, cmath.Vec3) {
	spaceId, v, ok := DB.GetUserLastKnownPosition(uid, worldId)
	if !ok {
		spaceId, v, ok = DB.GetWorldDefauleSpawnPositon(worldId)
		if !ok {
			return worldId, cmath.DefaultPosition()
		}
	}
	return spaceId, v
}

func (DB *Database) GetUserSpawnPosition(uid uuid.UUID, URL *url.URL) (uuid.UUID, uuid.UUID, cmath.Vec3, error) {
	// Look for world to spawn user in
	var worldId uuid.UUID
	var ok bool
	log.Info("Spawn flow: R1", uid)
	if worldId, ok = DB.GetWorldByURL(URL); !ok {
		log.Info("Spawn flow: R2", uid)
		if worldId, ok = DB.GetastUserWorld(uid); !ok {
			log.Info("Spawn flow: R3", uid)
			// logger.Logln(1, "User "+uid.String()+" is new ")
			if worldId, ok = DB.GetDefaultEntranceWorld(); !ok {
				log.Info("Spawn flow: R4", uid)
				if worldId, ok = DB.GetRandomWorld(); !ok {
					log.Info("Spawn flow: R5", uid)
					return uuid.Nil, uuid.Nil, cmath.DefaultPosition(), errors.New("no place to spawn")
				}
			}
		}
	}
	log.Info("Spawn flow: R6", uid)
	spaceId, v := DB.GetUserSpawnPositionInWorld(uid, worldId)
	log.Info("Spawn flow: R7", uid)
	return worldId, spaceId, v, nil
}

func (DB *Database) GetUserInfo(id uuid.UUID) (string, uuid.UUID) {
	query := getUserInfoQuery
	row := DB.QueryRow(query, utils.BinId(id))
	var name string
	bid := make([]byte, 16)
	err := row.Scan(&name, &bid)
	if err != nil {
		log.Errorf("error: %+v", err)
	}
	tid, err := uuid.FromBytes(bid)
	if err != nil {
		log.Errorf("error: %+v", err)
	}
	return name, tid
}

func (DB *Database) GetGuestUserTypeId(typename string) uuid.UUID {
	query := getGuestUserTypeIDQuery
	row := DB.QueryRow(query, typename)
	bid := make([]byte, 16)
	err := row.Scan(&bid)
	if err != nil {
		log.Errorf("error: %+v", err)
	}
	tid, err := uuid.FromBytes(bid)
	if err != nil {
		log.Errorf("error: %+v", err)
	}
	return tid
}

func (DB *Database) GetUsersIDsByType(typeid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := DB.Query(getUserIDsByTypeQuery, utils.BinId(typeid))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bid := make([]byte, 16)
	var ids []uuid.UUID
	for rows.Next() {
		if err := rows.Scan(&bid); err != nil {
			return nil, err
		}
		id, err := uuid.FromBytes(bid)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (DB *Database) QuerySingleByField(table string, field string, ref interface{}) (map[string]interface{}, error) {
	querybase := `SELECT * FROM ` + table + ` WHERE  ` + field + ` = ?;`
	rows, err := DB.Query(querybase, ref)

	if err != nil {
		log.Error(err)
		return nil, err
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()
	retval := utils.LoadRow(rows)
	return retval, nil
}

func (DB *Database) QuerySingleAuxById(tables []string, id []byte) (map[string]interface{}, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	querybase := `SELECT * FROM ` + strings.Join(tables[:], ",") + ` WHERE spaceId = ?;`
	rows, err := DB.Query(querybase, id)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()
	retval := utils.LoadRow(rows)
	return retval, nil
}

func (DB *Database) QuerySingleByBinId(table string, id []byte) (map[string]interface{}, error) {
	querybase := `SELECT * FROM ` + table + ` WHERE  id = ?;`
	rows, err := DB.Query(querybase, id)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()
	retval := utils.LoadRow(rows)
	return retval, nil
}

func (DB *Database) QuerySingleByUUID(table string, id uuid.UUID) (map[string]interface{}, error) {
	return DB.QuerySingleByBinId(table, utils.BinId(id))
}

func (DB *Database) WriteLastKnownPosition(
	userId, worldId, anchorId uuid.UUID, vector *cmath.Vec3, timeOffset time.Duration,
) {
	querybase := writeLastKnownPositionQuery

	tm := time.Now().Add(time.Second * timeOffset)
	_, err := DB.Exec(
		querybase, utils.BinId(userId), utils.BinId(worldId), utils.BinId(anchorId), vector.X, vector.Y, vector.Z, tm,
		utils.BinId(worldId), utils.BinId(anchorId), vector.X, vector.Y, vector.Z, tm,
	)

	if err != nil {
		return
	}

	log.Info("Saved pos for " + userId.String())
}

func (DB *Database) RemoveOnline(userId, worldId uuid.UUID) error {
	res, err := DB.Exec(removeOnlineUserByIdAndSpaceIdQuery, utils.BinId(userId), utils.BinId(worldId))
	if err != nil {
		return err
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return err
	}

	log.Debug("Storage: RemoveOnline:", userId.String(), worldId.String(), affected)
	return nil
}

func (DB *Database) RemoveFromUsers(userId uuid.UUID) error {
	res, err := DB.Exec(removeFromUsersQuery, utils.BinId(userId))
	if err != nil {
		return err
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return err
	}

	log.Debug("Storage: RemoveFromUsers:", userId.String(), affected)
	return nil
}

func (DB *Database) RemoveManyFromUsers(ids []uuid.UUID) error {
	var bids [][]byte
	for i := range ids {
		bids = append(bids, utils.BinId(ids[i]))
	}

	res, err := DB.Exec(removeManyFromUsersQuery, bids)
	if err != nil {
		return err
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return err
	}

	log.Debug("Storage: RemoveManyFromUsers: ", affected)
	return nil
}

func (DB *Database) InsertOnline(userId, spaceId uuid.UUID) {
	_, err := DB.Exec(insertOnlineUserByIdAndSpaceIdQuery, utils.BinId(userId), utils.BinId(spaceId))
	if err != nil {
		log.Warnf("error: %+v", err)
	}
}

func (DB *Database) RemoveDynamicWorldMembership(userId, worldId uuid.UUID) error {
	res, err := DB.Exec(removeDynamicWorldMembershipByIdAndWorldIdQuery, utils.BinId(userId), utils.BinId(worldId))
	if err != nil {
		return err
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return err
	}

	log.Debug("Storage: RemoveDynamicWorldMembership:", userId.String(), worldId.String(), affected)
	return nil
}

func (DB *Database) QueryWorldConfig(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := DB.Query(queryWorldConfigQuery, utils.BinId(id))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		var data []byte
		err := rows.Scan(&data)
		if err != nil {
			log.Error(err)
			return nil, err
		}
		var r map[string]interface{}
		err = json.Unmarshal(data, &r)
		return r, err
	}

	return nil, err
}

func (DB *Database) UpdateHighFives(sender, target uuid.UUID) {
	_, err := DB.Exec(updateHighFivesQuery, utils.BinId(sender), utils.BinId(target))
	if err != nil {
		log.Warn(errors.WithMessage(err, "UpdateHighFives: failed to exec"))
	}
}

func (DB *Database) GetUserName(id uuid.UUID) string {
	row := DB.QueryRow(getUserNameQuery, utils.BinId(id))
	var name string
	err := row.Scan(&name)
	if err != nil {
		log.Warn(errors.WithMessage(err, "GetUserName: failed to scan"))
	}
	return name
}

/*
// TODO: make better, make tests
func (DB *database) QuerySingleSpaceTypeByUUIDV2(id uuid.UUID) (*SpaceType, error) {
	var spaceType SpaceType
	query := `SELECT * FROM space_types WHERE  id = ?;`
	err := DB.QueryRowx(query, util.BinId(id)).StructScan(&spaceType)
	if err != nil {
		logger.Logf(0, "error getting space types: %v", err)
		return nil, err
	}
	return &spaceType, nil
}

type SpaceType struct {
	Id                        []byte `db:"id"`
	Name                      string `db:"name"`
	Asset                     []byte `db:"asset"`
	AuxiliaryTables           string `db:"auxiliary_tables"`
	Description               string `db:"description"`
	TypeParameters            string `db:"type_parameters"`
	DefaultInstanceParameters string `db:"default_instance_parameters"`
	AssetTypes                string `db:"asset_types"`
	TypeParameters2d          string `db:"type_parameters_2D"`
	TypeParameters3d          string `db:"type_parameters_3D"`
	AllowedSubspaces          string `db:"allowed_subspaces"`
	DefaultTiles              string `db:"default_tiles"`
	FrameTemplates            string `db:"frame_templates"`
	ChildPlacement            string `db:"child_placement"`
}
*/
