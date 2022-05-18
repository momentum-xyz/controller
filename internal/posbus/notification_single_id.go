package posbus

//
//import (
//	"encoding/binary"
//
//	"github.com/momentum-xyz/controller/utils"
//	"github.com/google/uuid"
//)
//
//type NotificationSingleID struct {
//	*Message
//}
//
//func NewNotificationSingleIDMsg(dest Destination, kind Notification, id uuid.UUID, flag int32, s string) *NotificationSingleID {
//	obj := NewMessage(MsgTypeNotificationSingleID, 1+MsgTypeSize+MsgUUIDTypeSize+MsgTypeSize+MsgArrTypeSize+len(s))
//	obj.Msg()[0] = byte(dest)
//	binary.LittleEndian.PutUint32(obj.Msg()[1:], uint32(kind))
//	copy(obj.Msg()[1+MsgTypeSize:], utils.BinId(id))
//
//	binary.LittleEndian.PutUint32(obj.Msg()[1+MsgTypeSize+MsgUUIDTypeSize:], uint32(flag))
//	binary.LittleEndian.PutUint32(obj.Msg()[1+MsgTypeSize+MsgUUIDTypeSize+MsgTypeSize:], uint32(len(s)))
//	copy(obj.Msg()[1+MsgTypeSize+MsgUUIDTypeSize+MsgTypeSize+MsgArrTypeSize:], s)
//	return &NotificationSingleID{
//		Message: obj,
//	}
//}
