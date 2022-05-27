package position

import (
	"math"

	cm "github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/utils"
)

const (
	helixAngleDefaultValue       = 0.0
	helixSpiralScaleDefaultValue = 10.0
	helixRadiusDefaultValue      = 100.0
	helixHelixVShiftDefaultValue = 500.0
	helixVShiftDefaultValue      = 10.0
)

type helix struct {
	Angle       float64 `json:"angle"`
	SpiralScale float64 `json:"spiralScale"`
	R           float64 `json:"R"`
	VShift      float64 `json:"Vshift"`
	HelixVShift float64 `json:"helixVshift"`
}

func NewHelix(parameterMap map[string]interface{}) Algo {
	return &helix{
		Angle:       utils.FromAnyMap(parameterMap, "angle", helixAngleDefaultValue),
		VShift:      utils.FromAnyMap(parameterMap, "Vshift", helixVShiftDefaultValue),
		R:           utils.FromAnyMap(parameterMap, "R", helixRadiusDefaultValue),
		SpiralScale: utils.FromAnyMap(parameterMap, "spiralScale", helixSpiralScaleDefaultValue),
		HelixVShift: utils.FromAnyMap(parameterMap, "helixVshift", helixHelixVShiftDefaultValue),
	}
}

func (h *helix) CalcPos(parentTheta float64, parentVector cm.Vec3, i, n int) (cm.Vec3, float64) {
	parent := parentVector.ToVec3f64()
	id := float64(i)

	acf := h.Angle / 360.0 * id
	r := h.R + h.SpiralScale*acf

	phi := 0.5*math.Pi + parentTheta
	angle := phi + id*h.Angle/180.0*math.Pi

	p := cm.Vec3f64{
		X: math.Round((parent.X+r*math.Cos(angle))*10.0) / 10.0,
		Y: parent.Y + h.VShift + h.HelixVShift*acf,
		Z: math.Round((parent.Z+r*math.Sin(angle))*10.0) / 10.0,
	}

	return p.ToVec3(), math.Atan2(p.Z-parent.Z, p.X-parent.X) /* theta */
}

func (*helix) Name() string {
	return "helix"
}
