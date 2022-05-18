package posbus

//
// import (
//	"encoding/binary"
//
//	"github.com/momentum-xyz/controller/utils"
//	"github.com/google/uuid"
// )
//
// type NotificationDualID struct {
//	*Message
// }
//
// func NewNotificationDualIDMsg(dest Destination, kind Notification, id1, id2 uuid.UUID, flag int32, s string) *NotificationDualID {
//	obj := NewMessage(MsgTypeNotificationDualID, 1+MsgTypeSize+2*MsgUUIDTypeSize+MsgTypeSize+MsgArrTypeSize+len(s))
//	obj.Msg()[0] = byte(dest)
//	binary.LittleEndian.PutUint32(obj.Msg()[1:], uint32(kind))
//	copy(obj.Msg()[1+MsgTypeSize:], utils.BinId(id1))
//	copy(obj.Msg()[1+MsgTypeSize+MsgUUIDTypeSize:], utils.BinId(id2))
//
//	binary.LittleEndian.PutUint32(obj.Msg()[1+MsgTypeSize+2*MsgUUIDTypeSize:], uint32(flag))
//	binary.LittleEndian.PutUint32(obj.Msg()[1+MsgTypeSize+2*MsgUUIDTypeSize+MsgTypeSize:], uint32(len(s)))
//	copy(obj.Msg()[1+MsgTypeSize+2*MsgUUIDTypeSize+MsgTypeSize+MsgArrTypeSize:], s)
//	return &NotificationDualID{
//		Message: obj,
//	}
// }
