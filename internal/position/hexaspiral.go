package position

import (
	"math"

	cm "github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/utils"
)

const (
	hexaSpiralAngleDefaultValue            = 0.0
	hexaSpiralRSpaceDefaultValue           = 50.0
	hexaSpiralRandDisplacementDefaultValue = 10
	hexaSpiralVShiftDefaultValue           = 10.0
)

type hexaSpiral struct {
	Angle            float64 `json:"angle"`
	Rspace           float64 `json:"Rspace"`
	Vshift           float64 `json:"Vshift"`
	RandDisplacement float64 `json:"RandDisplacement"`
}

func NewHexaSpiral(parameterMap map[string]interface{}) Algo {
	return &hexaSpiral{
		Angle:            utils.F64FromMap(parameterMap, "angle", hexaSpiralAngleDefaultValue),
		Vshift:           utils.F64FromMap(parameterMap, "Vshift", hexaSpiralVShiftDefaultValue),
		Rspace:           utils.F64FromMap(parameterMap, "Rspace", hexaSpiralRSpaceDefaultValue),
		RandDisplacement: utils.F64FromMap(parameterMap, "RandDisplacement", hexaSpiralRandDisplacementDefaultValue),
	}
}

func (h *hexaSpiral) CalcPos(parentTheta float64, parentVector cm.Vec3, i, n int) (cm.Vec3, float64) {
	parent := parentVector.ToVec3f64()

	x, y := getHexPosition(i)

	p := cm.Vec3f64{
		X: math.Round((parent.X+x*h.Rspace)*10.0) / 10.0,
		Y: parent.Y + h.Vshift,
		Z: math.Round((parent.Z+y*h.Rspace)*10.0) / 10.0,
	}

	return p.ToVec3(), math.Atan2(p.Z-parent.Z, p.X-parent.X) /* theta */
}

func (*hexaSpiral) Name() string {
	return "hexaspiral"
}

func getHexPosition(i int) (float64, float64) {

	if i == 0 {
		return 0, 0
	}

	layer := int(math.Round(math.Sqrt(float64(i) / 3.0)))

	firstIdxInLayer := 3*layer*(layer-1) + 1
	side := float64((i - firstIdxInLayer) / layer)
	idx := float64((i - firstIdxInLayer) % layer)

	x := float64(layer)*math.Cos((side-1.0)*math.Pi/3.0) + (idx+1.0)*math.Cos((side+1.0)*math.Pi/3.0)
	y := -float64(layer)*math.Sin((side-1)*math.Pi/3) - (idx+1)*math.Sin((side+1)*math.Pi/3)

	return x, y
}
