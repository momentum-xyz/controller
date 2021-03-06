package position

import (
	"math"

	cm "github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/utils"
)

const (
	circularAngleDefaultValue  = 0.0
	circularRadiusDefaultValue = 100.0
	circularVShiftDefaultValue = 10.0
)

type circular struct {
	Angle  float64 `json:"angle"`
	R      float64 `json:"R"`
	VShift float64 `json:"Vshift"`
}

func NewCircular(parameterMap map[string]interface{}) Algo {
	return &circular{
		Angle:  utils.GetFromAnyMap(parameterMap, "angle", circularAngleDefaultValue),
		R:      utils.GetFromAnyMap(parameterMap, "R", circularRadiusDefaultValue),
		VShift: utils.GetFromAnyMap(parameterMap, "Vshift", circularVShiftDefaultValue),
	}
}

func (cir *circular) CalcPos(parentTheta float64, parentVector cm.Vec3, i, n int) (cm.Vec3, float64) {
	parent := parentVector.ToVec3f64()
	phi := -0.5*math.Pi + cir.Angle/180.0*math.Pi + parentTheta
	scl := 2.0 * math.Pi / float64(n)

	angle := phi + float64(i)*scl
	p := cm.Vec3f64{
		X: math.Round((parent.X+cir.R*math.Cos(angle))*10.0) / 10.0,
		Y: parent.Y + cir.VShift,
		Z: math.Round((parent.Z+cir.R*math.Sin(angle))*10.0) / 10.0,
	}

	return p.ToVec3(), math.Atan2(p.Z-parent.Z, p.X-parent.X) /* theta */
}

func (*circular) Name() string {
	return "circular"
}
