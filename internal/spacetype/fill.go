package spacetype

import (
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/internal/position"
	"github.com/momentum-xyz/controller/utils"
	"github.com/pkg/errors"

	"github.com/google/uuid"
)

const (
	AuxiliaryTables = "auxiliary_tables"
	ChildPlacement  = "child_placement"
)

var log = logger.L()

func FillPlacement(placementMap map[string]interface{}, placements map[uuid.UUID]position.Algo) error {
	log.Debug("PLSMAP", placementMap)

	kind, err := utils.SpaceTypeFromMap(placementMap)
	if err != nil {
		return errors.WithMessage(err, "failed to get space type for map")
	}

	var par position.Algo
	algo := "circular"
	if v, ok := placementMap["algo"]; ok {
		algo = v.(string)
	}

	switch algo {
	case "circular":
		par = position.NewCircular(placementMap)
	case "helix":
		par = position.NewHelix(placementMap)
	case "sector":
		par = position.NewSector(placementMap)
	case "spiral":
		par = position.NewSpiral(placementMap)
	case "hexaspiral":
		par = position.NewHexaSpiral(placementMap)
	}

	placements[kind] = par
	return nil
}
