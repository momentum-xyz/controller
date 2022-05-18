package posbus

import (
	"encoding/binary"

	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
)

const TriggerTransitionalBridgingEffectsOnObjectElementSize = 3*MsgUUIDTypeSize + MsgTypeSize

type TriggerTransitionalBridgingEffectsOnObject struct {
	*Message
}

func NewTriggerTransitionalBridgingEffectsOnObjectMsg(numEffects int) *TriggerTransitionalBridgingEffectsOnObject {
	obj := NewMessage(MsgTypeTriggerTransitionalBridgingEffectsOnObject, MsgArrTypeSize+numEffects*TriggerTransitionalBridgingEffectsOnObjectElementSize)
	binary.LittleEndian.PutUint32(obj.Msg(), uint32(numEffects))
	return &TriggerTransitionalBridgingEffectsOnObject{
		Message: obj,
	}
}

func (m *TriggerTransitionalBridgingEffectsOnObject) SetEffect(i int, emitter, from, to uuid.UUID, effect uint32) {
	start := MsgArrTypeSize + i*TriggerTransitionalBridgingEffectsOnObjectElementSize
	copy(m.Msg()[start:], utils.BinId(emitter))
	copy(m.Msg()[start+MsgUUIDTypeSize:], utils.BinId(from))
	copy(m.Msg()[start+2*MsgUUIDTypeSize:], utils.BinId(to))
	binary.LittleEndian.PutUint32(m.Msg()[start+3*MsgUUIDTypeSize:], effect)
}
