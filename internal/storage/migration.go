package storage

import (
	"os"

	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
)

func MigrateDb(DB *Database, cfg *config.MySQL) {
	log.Info("Check for DB migrations...")
	migrateDb202(DB)
	migrateDbAssetsToUuid(DB, cfg)

}

func migrateDbAssetsToUuid(DB *Database, cfg *config.MySQL) {
	log.Info("Check for DB migration: assets to binary...")
	query := `select data_type from information_schema.columns where table_schema = ? and table_name = 'space_types' and column_name = 'asset';`
	needUpgrade := false
	res, err := DB.Query(query, cfg.DATABASE)
	if err == nil {
		defer res.Close()
		if res.Next() {
			var atype string
			if err := res.Scan(&atype); err != nil {
				log.Error(err)
			}
			if atype != "binary" {
				needUpgrade = true
			}
		}
	}
	if needUpgrade {
		log.Info("Need assets migration to UUID")
		rows, err := DB.Query(`select id,asset from space_types;`)
		if err == nil {
			defer rows.Close()
			if _, err = DB.Exec(`ALTER TABLE space_types ADD column asset_uuid binary(16) after asset;`); err != nil {
				log.Warnf("error: %+v", err)
			}

			for rows.Next() {
				var asset string
				id := make([]byte, 16)
				if err := rows.Scan(&id, &asset); err != nil {
					log.Error(err)
				}
				basset, err := uuid.Parse(asset)
				if err != nil {
					if err.Error() != "invalid UUID length: 0" {
						log.Info("Cancelling migration0")
						return
					}
					_, err = DB.Exec(`update space_types set asset_uuid=NULL where id =?;`, id)
				} else {
					_, err = DB.Exec(`update space_types set asset_uuid=? where id =?;`, utils.BinId(basset), id)
				}

				if err != nil {
					log.Warnf("Cancelling migration1: %+v", err)
					return
				}
			}
			log.Info("Data is migrated")
			if _, err = DB.Exec(`ALTER TABLE space_types drop COLUMN asset;`); err != nil {
				log.Warnf("error: %+v", err)
			}
			if _, err = DB.Exec(`ALTER TABLE space_types RENAME COLUMN asset_uuid TO asset;`); err != nil {
				log.Warnf("error: %+v", err)
			}
			if _, err = DB.Exec(`alter table space_types add child_placement json default (json_array()) null;`); err != nil {
				log.Warnf("error: %+v", err)
			}
			if _, err = DB.Exec(`alter table spaces add child_placement json default (json_array()) null;`); err != nil {
				log.Warnf("error: %+v", err)
			}
		}
	}
}

func migrateDb202(DB *Database) {
	log.Info("Check for DB migration to 202")
	if rows, err := DB.Query(`SELECT  worldId from user_lkp;`); err == nil {
		defer rows.Close()
		rows.Next()
		log.Info("No need DB migration v202")
		return
	}

	log.Info("Need DB migration v202")
	var err error
	if _, err = DB.Exec(`ALTER TABLE user_lkp ADD worldId binary(16) NOT NULL;`); err != nil {
		os.Exit(1)
	}
	if _, err = DB.Exec(`ALTER TABLE user_lkp ADD KEY FK_845 (worldId);`); err != nil {
		log.Errorf("error: %+v", err)
	}
	if _, err = DB.Exec(`ALTER TABLE user_lkp ADD CONSTRAINT FK_843 FOREIGN KEY (worldId) REFERENCES spaces (id);`); err != nil {
		log.Errorf("error: %+v", err)
	}

	query := `SELECT userId,spaceId FROM user_lkp;`

	rows, err := DB.Query(query)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			buid := make([]byte, 16)
			bsid := make([]byte, 16)
			if err := rows.Scan(&buid, &bsid); err != nil {
				log.Error(err)
			}
			userId, err1 := uuid.FromBytes(buid)
			spaceId, err2 := uuid.FromBytes(bsid)

			if err1 == nil && err2 == nil {
				func() {
					querybase := `select GetParentWorldByID(?);`
					rows, err := DB.Query(querybase, utils.BinId(spaceId))
					if err == nil {
						defer rows.Close()
						if rows.Next() {
							bwid := make([]byte, 16)
							if err := rows.Scan(&bwid); err != nil {
								log.Error(err)
							}
							worldId, err := uuid.FromBytes(bwid)
							if err == nil {
								querybase := `update user_lkp set worldId=? where userId=? and spaceId=?;`
								_, err := DB.Exec(querybase, utils.BinId(worldId), utils.BinId(userId), utils.BinId(spaceId))
								if err != nil {
									log.Warnf("error: %+v", err)
								}
							}
						}
					}
				}()
			}
		}
	}

	_, err = DB.Exec(`ALTER TABLE user_lkp DROP PRIMARY KEY;`)
	if err != nil {
		log.Warnf("error: %+v", err)
	}
	_, err = DB.Exec(`ALTER TABLE user_lkp ADD PRIMARY KEY (userId,worldId);`)

	if err != nil {
		log.Errorf("Problem with migration")
		return
	}
	log.Info("Migrated!")
}
