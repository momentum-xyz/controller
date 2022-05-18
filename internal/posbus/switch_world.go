package posbus

import (
	"github.com/momentum-xyz/controller/utils"

	// External
	"github.com/google/uuid"
)

type SwitchWorld struct {
	*Message
}

func NewSwitchWorldMsg(id uuid.UUID) *SwitchWorld {
	obj := NewMessage(MsgTypeSwitchWorld, MsgUUIDTypeSize)
	copy(obj.Msg()[:MsgUUIDTypeSize], utils.BinId(id))
	return &SwitchWorld{
		Message: obj,
	}
}

func (m *Message) AsSwitchWorld() *SwitchWorld {
	return &SwitchWorld{
		Message: m,
	}
}

func (m *SwitchWorld) World() uuid.UUID {
	id, _ := uuid.FromBytes(m.Msg()[:MsgUUIDTypeSize])
	return id

}
