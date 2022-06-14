package storage

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	queryWorldConfigQuery                           = `SELECT config FROM world_definition WHERE id = ?;`
	getUserInfoQuery                                = `SELECT name, userTypeId FROM users WHERE id = ?;`
	getRandomWorldQuery                             = `SELECT id FROM spaces WHERE parentId = 0x00000000000000000000000000000000 AND id != 0x00000000000000000000000000000000;`
	getDefaultEntranceWorldQuery                    = `SELECT value FROM node_settings WHERE name = 'EntranceWorld';`
	getWorldByURLQuery                              = `SELECT worldId FROM url_mapping WHERE URL = ?;`
	getParentWorldQuery                             = `SELECT GetParentWorldByID(id) FROM spaces WHERE id = ?;`
	getastUserWorldQuery                            = `SELECT worldId FROM user_lkp WHERE userId = ? ORDER BY updated_at DESC;`
	isSpacePresentQuery                             = `SELECT id FROM spaces WHERE id = ? LIMIT 1;`
	removeOnlineUserByIdAndSpaceIdQuery             = `DELETE FROM online_users WHERE userId = ? AND spaceId = ?;`
	insertOnlineUserByIdAndSpaceIdQuery             = `INSERT INTO online_users (userId,spaceId,updated_at) VALUES (?,?,NOW()) ON DUPLICATE KEY UPDATE updated_at=NOW();`
	removeDynamicWorldMembershipByIdAndWorldIdQuery = `DELETE FROM user_spaces_dynamic WHERE userId = ? AND spaceId = ?;`
	removeFromUsersQuery                            = `DELETE FROM users WHERE id = ?;`
	removeManyFromUsersQuery                        = `DELETE FROM users WHERE id IN(?);`
	removeAllOnlineUsersQuery                       = `TRUNCATE online_users;`
	removeAllDynamicMembershipQuery                 = `TRUNCATE user_spaces_dynamic;`
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

func OpenDB(cfg *config.MySQL) (*Database, error) {
	sqlconfig := cfg.GenConfig()
	DB, err := sqlx.Open(mysqlDriverName, sqlconfig.FormatDSN())
	if err != nil {
		return nil, errors.WithMessage(err, "failed to open db")
	}
	DB.SetMaxOpenConns(maxOpenConnections)
	return &Database{DB: DB}, nil
}

func (DB *Database) GetParentWorld(sid uuid.UUID) (uuid.UUID, error) {
	rows, err := DB.Query(getParentWorldQuery, utils.BinId(sid))
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	bid := make([]byte, 16)
	if rows.Next() {
		if err := rows.Scan(&bid); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to scan rows")
		}
		wid, err := uuid.FromBytes(bid)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse wid")
		}
		return wid, nil
	}

	return uuid.Nil, utils.ErrNotFound
}

func (DB *Database) isSpacePresent(id uuid.UUID) (bool, error) {
	rows, err := DB.Query(isSpacePresentQuery, utils.BinId(id))
	if err != nil {
		return false, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	return rows.Next(), nil
}

func (DB *Database) GetWorldByURL(URL *url.URL) (uuid.UUID, error) {
	domain := URL.String()
	log.Info("Quering domain:", domain)
	rows, err := DB.Query(getWorldByURLQuery, domain)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	log.Info("Spawn flow: G1")
	if rows.Next() {
		log.Info("Spawn flow: G2")
		bindata := make([]byte, 16)
		log.Info("Spawn flow: G3")
		if err := rows.Scan(&bindata); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to scan rows")
		}
		worldId, err := uuid.FromBytes(bindata)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse world id")
		}
		log.Info("Spawn flow: G4")
		present, err := DB.isSpacePresent(worldId)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to check isSpacePresent")
		}
		if present {
			log.Info("Spawn world it forced by URL map to:", worldId)
			return worldId, nil
		}
		log.Info("Spawn flow: G5")
	}
	log.Info("Spawn flow: G6")

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, utils.ErrNotFound
}

func (DB *Database) GetastUserWorld(uid uuid.UUID) (uuid.UUID, error) {
	rows, err := DB.Query(getastUserWorldQuery, utils.BinId(uid))
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		bindata := make([]byte, 16)
		if err := rows.Scan(&bindata); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to scan rows")
		}
		worldId, err := uuid.FromBytes(bindata)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse world id")
		}
		present, err := DB.isSpacePresent(worldId)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to check isSpacePresent")
		}
		if present {
			log.Info("Last visited world for user", uid, "is", worldId)
			return worldId, nil
		}
	}

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, utils.ErrNotFound
}

func (DB *Database) GetDefaultEntranceWorld() (uuid.UUID, error) {
	rows, err := DB.Query(getDefaultEntranceWorldQuery)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		var worldString string
		if err := rows.Scan(&worldString); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to scan rows")
		}
		worldId, err := uuid.Parse(worldString)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse world id")
		}
		present, err := DB.isSpacePresent(worldId)
		if present {
			log.Infof("Using Entrance Config %s", worldId)
			return worldId, nil
		}
	}

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, utils.ErrNotFound
}

func (DB *Database) GetRandomWorld() (uuid.UUID, error) {
	rows, err := DB.Query(getRandomWorldQuery)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		var bindata []byte
		if err := rows.Scan(&bindata); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse rows")
		}
		worldId, err := uuid.FromBytes(bindata)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse world id")
		}
		present, err := DB.isSpacePresent(worldId)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to check isSpacePresent")
		}
		if present {
			return worldId, nil
		}
	}

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, utils.ErrNotFound
}

func (DB *Database) GetUserLastKnownPosition(UserId, WorldId uuid.UUID) (uuid.UUID, cmath.Vec3, error) {
	rows, err := DB.Query(getUserLastKnownPositionQuery, utils.BinId(UserId), utils.BinId(WorldId))
	if err != nil {
		return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		bindata := make([]byte, 16)
		var v cmath.Vec3
		if err := rows.Scan(&bindata, &(v.X), &(v.Y), &(v.Z)); err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to scan rows")
		}
		spaceId, err := uuid.FromBytes(bindata)
		if err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to parse space id")
		}
		present, err := DB.isSpacePresent(spaceId)
		if err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to check isSpacePresent")
		}
		if present {
			log.Info(
				"User "+UserId.String()+" last known space on world "+WorldId.String()+" is "+spaceId.String(), v,
			)
			return spaceId, v, nil
		}
	}

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, cmath.MNan32Vec3(), utils.ErrNotFound
}

func (DB *Database) GetWorldDefauleSpawnPositon(WorldId uuid.UUID) (uuid.UUID, cmath.Vec3, error) {
	rows, err := DB.Query(getWorldDefaultSpawnPositionQuery, utils.BinId(WorldId))
	if err != nil {
		return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		var posData []byte
		var spaceData []byte
		if err := rows.Scan(&spaceData, &posData); err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to scan rows")
		}
		var pos cmath.Vec3
		var spaceId uuid.UUID
		if err := json.Unmarshal(posData, &pos); err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to unmarshal pos data")
		}
		spaceId, err = uuid.FromBytes(spaceData)
		if err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to parse space id")
		}
		present, err := DB.isSpacePresent(spaceId)
		if err != nil {
			return uuid.Nil, cmath.MNan32Vec3(), errors.WithMessage(err, "failed to check isSpacePresent")
		}
		if present {
			return spaceId, pos, nil
		}
	}

	// TODO: maybe wee need to return nil error here?
	return uuid.Nil, cmath.MNan32Vec3(), utils.ErrNotFound
}

func (DB *Database) GetUserSpawnPositionInWorld(uid, worldId uuid.UUID) (uuid.UUID, cmath.Vec3) {
	spaceId, v, err := DB.GetUserLastKnownPosition(uid, worldId)
	if err != nil {
		spaceId, v, err = DB.GetWorldDefauleSpawnPositon(worldId)
		if err != nil {
			return worldId, cmath.DefaultPosition()
		}
	}
	return spaceId, v
}

func (DB *Database) GetUserSpawnPosition(uid uuid.UUID, URL *url.URL) (uuid.UUID, uuid.UUID, cmath.Vec3, error) {
	// Look for world to spawn user in
	var err error
	var worldId uuid.UUID
	log.Info("Spawn flow: R1", uid)
	if worldId, err = DB.GetWorldByURL(URL); err != nil {
		log.Info("Spawn flow: R2", uid)
		if worldId, err = DB.GetastUserWorld(uid); err != nil {
			log.Info("Spawn flow: R3", uid)
			// logger.Logln(1, "User "+uid.String()+" is new ")
			if worldId, err = DB.GetDefaultEntranceWorld(); err != nil {
				log.Info("Spawn flow: R4", uid)
				if worldId, err = DB.GetRandomWorld(); err != nil {
					log.Info("Spawn flow: R5", uid)
					return uuid.Nil, uuid.Nil, cmath.MNan32Vec3(), errors.New("no place to spawn")
				}
			}
		}
	}
	log.Info("Spawn flow: R6", uid)
	spaceId, v := DB.GetUserSpawnPositionInWorld(uid, worldId)
	log.Info("Spawn flow: R7", uid)
	return worldId, spaceId, v, nil
}

func (DB *Database) GetUserInfo(id uuid.UUID) (string, uuid.UUID, error) {
	row := DB.QueryRow(getUserInfoQuery, utils.BinId(id))
	if row.Err() != nil {
		return "", uuid.Nil, errors.WithMessage(row.Err(), "failed to query db")
	}
	var name string
	bid := make([]byte, 16)
	if err := row.Scan(&name, &bid); err != nil {
		return "", uuid.Nil, errors.WithMessage(err, "failed to scan row")
	}
	tid, err := uuid.FromBytes(bid)
	if err != nil {
		return "", uuid.Nil, errors.WithMessage(err, "failed to parse tid")
	}
	return name, tid, nil
}

func (DB *Database) GetGuestUserTypeId(typename string) (uuid.UUID, error) {
	row := DB.QueryRow(getGuestUserTypeIDQuery, typename)
	if row.Err() != nil {
		return uuid.Nil, errors.WithMessage(row.Err(), "failed to query db")
	}
	bid := make([]byte, 16)
	if err := row.Scan(&bid); err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to scan row")
	}
	tid, err := uuid.FromBytes(bid)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to parse tid")
	}
	return tid, nil
}

func (DB *Database) GetUsersIDsByType(typeid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := DB.Query(getUserIDsByTypeQuery, utils.BinId(typeid))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	bid := make([]byte, 16)
	var ids []uuid.UUID
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

func (DB *Database) QuerySingleByField(table string, field string, ref interface{}) (map[string]interface{}, error) {
	query := `SELECT * FROM ` + table + ` WHERE  ` + field + ` = ?;`
	rows, err := DB.Query(query, ref)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	return utils.LoadRow(rows)
}

func (DB *Database) QuerySingleAuxById(tables []string, id []byte) (map[string]interface{}, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	query := `SELECT * FROM ` + strings.Join(tables, ",") + ` WHERE spaceId = ?;`
	rows, err := DB.Query(query, id)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	return utils.LoadRow(rows)
}

func (DB *Database) QuerySingleByBinId(table string, id []byte) (map[string]interface{}, error) {
	query := `SELECT * FROM ` + table + ` WHERE  id = ?;`
	rows, err := DB.Query(query, id)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	return utils.LoadRow(rows)
}

func (DB *Database) QuerySingleByUUID(table string, id uuid.UUID) (map[string]interface{}, error) {
	return DB.QuerySingleByBinId(table, utils.BinId(id))
}

func (DB *Database) WriteLastKnownPosition(
	userId, worldId, anchorId uuid.UUID, vector *cmath.Vec3, timeOffset time.Duration,
) error {
	tm := time.Now().Add(time.Second * timeOffset)

	_, err := DB.Exec(
		writeLastKnownPositionQuery, utils.BinId(userId), utils.BinId(worldId), utils.BinId(anchorId),
		vector.X, vector.Y, vector.Z, tm, utils.BinId(worldId), utils.BinId(anchorId), vector.X, vector.Y, vector.Z, tm,
	)
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	log.Info("Saved pos for " + userId.String())
	return nil
}

func (DB *Database) RemoveAllOnlineUsers() error {
	res, err := DB.Exec(removeAllOnlineUsersQuery)
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return nil
	}

	log.Debugf("Storage: RemoveAllOnlineUsers: %d", affected)
	return nil
}

func (DB *Database) RemoveAllDynamicMembership() error {
	res, err := DB.Exec(removeAllDynamicMembershipQuery)
	if err != nil {
		return errors.WithMessage(err, "failed to query db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		return nil
	}

	log.Debugf("Storage: RemoveAllDynamicMembership: %d", affected)
	return nil
}

func (DB *Database) RemoveOnline(userId, worldId uuid.UUID) error {
	res, err := DB.Exec(removeOnlineUserByIdAndSpaceIdQuery, utils.BinId(userId), utils.BinId(worldId))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
	}

	log.Debug("Storage: RemoveOnline:", userId.String(), worldId.String(), affected)
	return nil
}

func (DB *Database) RemoveFromUsers(userId uuid.UUID) error {
	res, err := DB.Exec(removeFromUsersQuery, utils.BinId(userId))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
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
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
	}

	log.Debug("Storage: RemoveManyFromUsers: ", affected)
	return nil
}

func (DB *Database) InsertOnline(userId, spaceId uuid.UUID) error {
	_, err := DB.Exec(insertOnlineUserByIdAndSpaceIdQuery, utils.BinId(userId), utils.BinId(spaceId))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}
	return nil
}

func (DB *Database) RemoveDynamicWorldMembership(userId, worldId uuid.UUID) error {
	res, err := DB.Exec(removeDynamicWorldMembershipByIdAndWorldIdQuery, utils.BinId(userId), utils.BinId(worldId))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}

	var affected int64
	affected, err = res.RowsAffected()
	if err != nil {
		// TODO: maybe return error here?
		return nil
	}

	log.Debug("Storage: RemoveDynamicWorldMembership:", userId.String(), worldId.String(), affected)
	return nil
}

func (DB *Database) QueryWorldConfig(id uuid.UUID) (map[string]interface{}, error) {
	rows, err := DB.Query(queryWorldConfigQuery, utils.BinId(id))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to query db")
	}
	//noinspection GoUnhandledErrorResult
	defer rows.Close()

	if rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, errors.WithMessage(err, "failed to scan rows")
		}
		var r map[string]interface{}
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, errors.WithMessage(err, "failed to unmarshal data")
		}
		return r, err
	}

	return nil, utils.ErrNotFound
}

func (DB *Database) UpdateHighFives(sender, target uuid.UUID) error {
	_, err := DB.Exec(updateHighFivesQuery, utils.BinId(sender), utils.BinId(target))
	if err != nil {
		return errors.WithMessage(err, "failed to exec db")
	}
	return nil
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
