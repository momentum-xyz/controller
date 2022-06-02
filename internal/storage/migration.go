package storage

import (
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

func MigrateDb(DB *Database, cfg *config.MySQL) error {
	log.Info("Check for DB migrations...")
	if err := migrateDb202(DB); err != nil {
		return errors.WithMessage(err, "failed to migrate db 202")
	}
	return migrateDbAssetsToUuid(DB, cfg)
}

func migrateDbAssetsToUuid(DB *Database, cfg *config.MySQL) error {
	log.Info("Check for DB migration: assets to binary...")
	query := `SELECT data_type FROM information_schema.columns WHERE table_schema = ? AND table_name = 'space_types' AND column_name = 'asset';`
	needUpgrade := false
	res, err := DB.Query(query, cfg.DATABASE)
	if err != nil {
		return errors.WithMessage(err, "failed to query data type from db")
	}
	defer res.Close()

	if res.Next() {
		var atype string
		if err := res.Scan(&atype); err != nil {
			return errors.WithMessage(err, "failed to scan data type")
		}
		if atype != "binary" {
			needUpgrade = true
		}
	}

	if needUpgrade {
		log.Info("Need assets migration to UUID")
		rows, err := DB.Query(`SELECT id,asset FROM space_types;`)
		if err != nil {
			return errors.WithMessage(err, "failed to query space types")
		}
		defer rows.Close()

		if _, err := DB.Exec(`ALTER TABLE space_types ADD COLUMN asset_uuid binary(16) AFTER asset;`); err != nil {
			return errors.WithMessage(err, "failed to add asset uuid column")
		}

		for rows.Next() {
			var asset string
			id := make([]byte, 16)
			if err := rows.Scan(&id, &asset); err != nil {
				return errors.WithMessage(err, "failed to scan asset")
			}
			basset, err := uuid.Parse(asset)
			if err != nil {
				if err.Error() != "invalid UUID length: 0" {
					return errors.WithMessage(err, "failed to parse asset id")
				}
				_, err = DB.Exec(`UPDATE space_types SET asset_uuid=NULL WHERE id =?;`, id)
			} else {
				_, err = DB.Exec(`UPDATE space_types SET asset_uuid=? WHERE id =?;`, utils.BinId(basset), id)
			}
			if err != nil {
				return errors.WithMessage(err, "failed to update asset uuid")
			}
		}

		log.Info("Data is migrated")
		if _, err := DB.Exec(`ALTER TABLE space_types DROP COLUMN asset;`); err != nil {
			return errors.WithMessage(err, "failed to drop asset column")
		}
		if _, err := DB.Exec(`ALTER TABLE space_types RENAME COLUMN asset_uuid TO asset;`); err != nil {
			return errors.WithMessage(err, "failed to rename asset column")
		}
		if _, err := DB.Exec(`ALTER TABLE space_types ADD child_placement JSON DEFAULT (json_array()) NULL;`); err != nil {
			return errors.WithMessage(err, "failed to add child placement to space types")
		}
		if _, err := DB.Exec(`ALTER TABLE spaces ADD child_placement JSON DEFAULT (json_array()) NULL;`); err != nil {
			return errors.WithMessage(err, "failed to add child placement to spaces")
		}
	}

	return nil
}

func migrateDb202(DB *Database) error {
	log.Info("Check for DB migration to 202")

	if rows, err := DB.Query(`SELECT  worldId FROM user_lkp;`); err == nil {
		defer rows.Close()

		rows.Next()
		log.Info("No need DB migration v202")
		return nil
	}

	log.Info("Need DB migration v202")
	if _, err := DB.Exec(`ALTER TABLE user_lkp ADD worldId binary(16) NOT NULL;`); err != nil {
		return errors.WithMessage(err, "failed to add world id to user lkp")
	}
	if _, err := DB.Exec(`ALTER TABLE user_lkp ADD KEY FK_845 (worldId);`); err != nil {
		return errors.WithMessage(err, "failed to add key to user lkp")
	}
	if _, err := DB.Exec(`ALTER TABLE user_lkp ADD CONSTRAINT FK_843 FOREIGN KEY (worldId) REFERENCES spaces (id);`); err != nil {
		return errors.WithMessage(err, "failed to add constraint to user lkp")
	}

	rows, err := DB.Query(`SELECT userId,spaceId FROM user_lkp;`)
	if err != nil {
		return errors.WithMessage(err, "failed to get data from user lkp")
	}
	defer rows.Close()

	for rows.Next() {
		buid := make([]byte, 16)
		bsid := make([]byte, 16)
		if err := rows.Scan(&buid, &bsid); err != nil {
			return errors.WithMessage(err, "failed to scan user and space ids")
		}
		userId, err := uuid.FromBytes(buid)
		if err != nil {
			return errors.WithMessage(err, "failed to parse user id")
		}
		spaceId, err := uuid.FromBytes(bsid)
		if err != nil {
			return errors.WithMessage(err, "failed to parse space id")
		}

		if err := func() error {
			rows, err := DB.Query(`SELECT GetParentWorldByID(?);`, utils.BinId(spaceId))
			if err != nil {
				return errors.WithMessage(err, "failed to get parent world by id")
			}
			defer rows.Close()

			if rows.Next() {
				bwid := make([]byte, 16)
				if err := rows.Scan(&bwid); err != nil {
					return errors.WithMessage(err, "failed to scan world id")
				}
				worldId, err := uuid.FromBytes(bwid)
				if err != nil {
					return errors.WithMessage(err, "failed to parse world id")
				}
				if _, err := DB.Exec(`UPDATE user_lkp SET worldId=? WHERE userId=? AND spaceId=?;`,
					utils.BinId(worldId), utils.BinId(userId), utils.BinId(spaceId)); err != nil {
					return errors.WithMessage(err, "failed to set world id")
				}
			}

			return nil
		}(); err != nil {
			return err
		}
	}

	if _, err := DB.Exec(`ALTER TABLE user_lkp DROP PRIMARY KEY;`); err != nil {
		return errors.WithMessage(err, "failed to remove key from user lkp")
	}
	if _, err := DB.Exec(`ALTER TABLE user_lkp ADD PRIMARY KEY (userId,worldId);`); err != nil {
		return errors.WithMessage(err, "failed to add key to user lkp")
	}

	log.Info("Migrated!")
	return nil
}
