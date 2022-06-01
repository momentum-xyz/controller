package spacetype

import (
	"encoding/json"
	"github.com/momentum-xyz/controller/internal/position"
	"github.com/momentum-xyz/controller/internal/space"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type PositionalParameters struct {
	Algo          string  `json:"algo"`
	Angle         float64 `json:"angle"`
	SpiralScale   float64 `json:"spiralScale"`
	EllipseFactor float64 `json:"ellipseFactor"`
	EllipseAngle  float64 `json:"ellipseAngle"`
	R             float64 `json:"R"`
	Vshift        float64 `json:"Vshift"`
	HelixVshift   float64 `json:"helixVshift"`
}

/*func (par *PositionalParameters) FillDefaultPlacement() {
	par.Algo = "circular"
	par.Angle = 0
	par.SpiralScale = 0
	par.EllipseFactor = 0
	par.EllipseAngle = 0
	par.Vshift = 10
}*/

type T3DPlacements []interface{}

type TSpaceType struct {
	Id         uuid.UUID
	SqlData    map[string]interface{}
	Dhash      [16]byte
	AuxTables  []string
	Placements map[uuid.UUID]position.Algo
	Minimap    uint8
	Visible    int8
	InfoUIId   uuid.UUID
	AssetId    uuid.UUID
}

type TSpaceTypes struct {
	spaceTypes   *utils.SyncMap[uuid.UUID, *TSpaceType]
	spaceStorage space.Storage
}

func NewSpaceTypes(spaceStorage space.Storage) *TSpaceTypes {
	obj := new(TSpaceTypes)
	obj.spaceTypes = utils.NewSyncMap[uuid.UUID, *TSpaceType]()
	obj.spaceStorage = spaceStorage
	return obj
}

func (t *TSpaceTypes) Set(id uuid.UUID, st *TSpaceType) {
	t.spaceTypes.Store(id, st)
}

func (t *TSpaceTypes) Get(id uuid.UUID) (*TSpaceType, error) {
	t.spaceTypes.Mu.Lock()
	defer t.spaceTypes.Mu.Unlock()

	if spaceType, ok := t.spaceTypes.Data[id]; ok {
		return spaceType, nil
	}

	spaceType := &TSpaceType{Id: id}
	if err := t.UpdateMetaSpaceType(spaceType); err != nil {
		return nil, errors.WithMessagef(err, "failed to update meta space type: %s", spaceType.Id)
	}

	t.spaceTypes.Data[id] = spaceType
	return spaceType, nil
}

// UpdateMetaSpaceType TODO: doc and use structs instead of map[string]interface{}
func (t *TSpaceTypes) UpdateMetaSpaceType(x *TSpaceType) error {
	log.Info("Getting SpaceType by Id: ", x.Id)
	entry, err := t.spaceStorage.QuerySingleSpaceTypeByUUID(x.Id)
	if err != nil {
		return errors.WithMessage(err, "failed to get entry")
	}

	if aux, ok := entry[AuxiliaryTables]; ok && aux != nil {
		if err := json.Unmarshal([]byte(utils.FromAny(aux, "")), &x.AuxTables); err != nil {
			return errors.WithMessage(err, "failed to unmarshal aux tables")
		}
		log.Info(x.AuxTables)
	}
	delete(entry, AuxiliaryTables)

	x.Placements = make(map[uuid.UUID]position.Algo)

	x.Minimap = 1
	if minimapEntry, ok := entry["minimap"]; ok && minimapEntry != nil {
		x.Minimap = uint8(utils.FromAny[int64](minimapEntry, -1))
	}
	x.Visible = 1
	if visibleEntry, ok := entry["visible"]; ok && visibleEntry != nil {
		x.Visible = int8(utils.FromAny[int64](visibleEntry, -1))
	}

	x.AssetId = uuid.Nil
	if assetId, ok := entry["asset"]; ok && assetId != nil {
		uid, err := utils.DbToUuid(assetId)
		if err != nil {
			return errors.WithMessage(err, "failed to parse asset id")
		}
		x.AssetId = uid
	}

	x.InfoUIId = uuid.Nil
	if infoUIId, ok := entry["infoui_id"]; ok && infoUIId != nil {
		uid, err := utils.DbToUuid(infoUIId)
		if err != nil {
			return errors.WithMessage(err, "failed to parse info ui id")
		}
		x.InfoUIId = uid
	}

	if childPlace, ok := entry[ChildPlacement]; ok {
		// log.Println("childPlace ", x.Id, ": ", childPlace)
		var t3DPlacements T3DPlacements
		jsonData := []byte(utils.FromAny(childPlace, ""))
		if err := json.Unmarshal(jsonData, &t3DPlacements); err != nil {
			return errors.WithMessage(err, "failed to unmarshal 3d placements")
		}
		for _, placement := range t3DPlacements {
			if err := FillPlacement(utils.FromAny(placement, map[string]interface{}{}), x.Placements); err != nil {
				log.Warn(errors.WithMessage(err, "TSpaceTypes: UpdateMetaSpaceType: failed to fill placement"))
			}
		}
	}

	return nil
}
