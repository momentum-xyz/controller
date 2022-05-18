package position

import (
	"math"
	"math/rand"

	cm "github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/utils"
)

const (
	hexaSpiralAngleDefaultValue            = 0.0
	hexaSpiralRSpaceDefaultValue           = 50.0
	hexaSpiralRandDisplacementDefaultValue = 0.0
	hexaSpiralVShiftDefaultValue           = 10.0
	hexaSpiralDrawCenterDefaultValue       = true
	hexaSpiralScatterDefaultValue          = true
)

var hexaSpiralSideChoice = []int{0, 3, 5, 2, 4, 1}

type hexaSpiral struct {
	Angle            float64 `json:"angle"`
	Rspace           float64 `json:"Rspace"`
	Vshift           float64 `json:"Vshift"`
	DrawCenter       bool    `json:"DrawCenter"`
	RandDisplacement float64 `json:"RandDisplacement"`
	Scatter          bool    `json:"Scatter"`
}

func NewHexaSpiral(parameterMap map[string]interface{}) Algo {
	return &hexaSpiral{
		Angle:            utils.F64FromMap(parameterMap, "angle", hexaSpiralAngleDefaultValue),
		Vshift:           utils.F64FromMap(parameterMap, "Vshift", hexaSpiralVShiftDefaultValue),
		Rspace:           utils.F64FromMap(parameterMap, "Rspace", hexaSpiralRSpaceDefaultValue),
		RandDisplacement: utils.F64FromMap(parameterMap, "RandDisplacement", hexaSpiralRandDisplacementDefaultValue),
		DrawCenter:       utils.BoolFromMap(parameterMap, "DrawCenter", hexaSpiralDrawCenterDefaultValue),
		Scatter:          utils.BoolFromMap(parameterMap, "Scatter", hexaSpiralDrawCenterDefaultValue),
	}
}

func (h *hexaSpiral) CalcPos(parentTheta float64, parentVector cm.Vec3, i, n int) (cm.Vec3, float64) {
	parent := parentVector.ToVec3f64()

	x, y := getHexPosition(i, h.DrawCenter, h.Scatter)

	if h.RandDisplacement > 0.0 {
		rand.Seed(int64(i + i*n + n*n*int(h.Rspace*100000) + int(h.Vshift*100)))
		randomAngle := rand.Float64() * 2 * math.Pi
		randomDisplacement := math.Sqrt(rand.Float64()) * h.RandDisplacement / h.Rspace
		xShift := randomDisplacement * math.Cos(randomAngle)
		yShift := randomDisplacement * math.Sin(randomAngle)
		x += xShift
		y += yShift
	}
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

func getHexPosition(i int, dc bool, scatter bool) (float64, float64) {

	j := i
	if !dc {
		j++
	}

	if j == 0 {
		return 0, 0
	}

	layer := int(math.Round(math.Sqrt(float64(j) / 3.0)))

	firstIdxInLayer := 3*layer*(layer-1) + 1
	if scatter {
		lastIdxInLayer := 3*layer*(layer+1) + 1
		numInLayer := j - firstIdxInLayer
		totalInSide := (lastIdxInLayer - firstIdxInLayer) / 6
		side0 := numInLayer % 6
		side1 := hexaSpiralSideChoice[side0]
		pos := numInLayer / 6
		j = side1*totalInSide + pos + firstIdxInLayer
	}

	side := float64((j - firstIdxInLayer) / layer)
	idx := float64((j - firstIdxInLayer) % layer)

	x := float64(layer)*math.Cos((side-1.0)*math.Pi/3.0) + (idx+1.0)*math.Cos((side+1.0)*math.Pi/3.0)
	y := -float64(layer)*math.Sin((side-1)*math.Pi/3) - (idx+1)*math.Sin((side+1)*math.Pi/3)

	return x, y
}
