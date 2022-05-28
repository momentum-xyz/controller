package universe

import (
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/posbus-protocol/posbus"
	pputils "github.com/momentum-xyz/posbus-protocol/utils"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type User struct {
	world        *WorldController
	ID           uuid.UUID
	SessionID    uuid.UUID
	connection   *socket.Connection
	posbuf       []byte
	pos          *cmath.Vec3
	queueID      uuid.UUID
	lastUpdate   int64
	writeLkp     bool
	currentSpace atomic.Value
	name         string
	isGuest      bool
}

func (u *User) Register(wc *WorldController) error {
	log.Info("reg user: ", wc.ID)

	if exclient, ok := wc.users.Get(u.ID); ok && exclient.queueID != u.queueID {
		log.Info("Spawn flow: user is already registered", u.ID)
		if exclient.SessionID == u.SessionID {
			log.Info("Same session, must be teleport")
			wc.unregisterUser <- exclient
		} else {
			log.Info("Double-login detected for", u.ID)

			u.connection.Send(posbus.NewSignalMsg(posbus.SignalDualConnection).WebsocketMessage())

			time.Sleep(time.Millisecond * 300)
			wc.unregisterUser <- exclient
		}
		log.Info("Spawn flow: did action on unreg", u.ID)
		go func() {
			time.Sleep(time.Millisecond * 100)
			wc.registerUser <- u
		}()
		return nil
	}

	defer func() {
		log.Info("Spawned %s on %s", u.ID, u.world.ID)
	}()

	// go utils.ChanMonitor("user:"+u.ID.String(), u.connection.send, 3*time.Second)
	log.Info("Registering user: " + u.ID.String())
	u.writeLkp = true
	// allocate and pre-fill with ID buffer for positions
	u.posbuf = message.NewSendPosBuffer(u.ID)

	// make u.pos to be pointer to relevant part of posbuf
	ipos := *u.pos
	u.pos = (*cmath.Vec3)(unsafe.Add(unsafe.Pointer(&u.posbuf[0]), 16))
	*u.pos = ipos
	u.world = wc
	u.world.UserOnlineAction(u.ID)
	u.lastUpdate = int64(0)
	u.currentSpace.Store(uuid.Nil)

	u.connection.SetReceiveCallback(u.OnMessage)
	u.connection.SetPumpEndCallback(func() { u.world.unregisterUser <- u })
	go u.connection.StartReadPump()
	go u.connection.StartWritePump()

	log.Info("send world meta")
	// TODO: Update MetaMSg
	if err := u.connection.SendDirectly(u.world.metaMsg); err != nil {
		return errors.WithMessage(err, "failed to send meta msg")
	}
	log.Info("send own position")
	if err := u.connection.SendDirectly(posbus.NewSendPositionMsg(pputils.Vec3(ipos)).WebsocketMessage()); err != nil {
		return errors.WithMessage(err, "failed to send position")
	}
	log.Info("send initial world")
	u.world.AddUserToWorld(u) // AddToWorld is happening there
	wc.hub.mqtt.SafeSubscribe("user_control/"+wc.ID.String()+"/"+u.ID.String()+"/#", 1, u.MQTTMessageHandler)
	// remove user from delay remove list
	if val, ok := u.world.hub.usersForRemoveWithDelay.Load(u.ID); ok {
		val.Value()()
	}
	log.Warnf("Registration done for %s(%s) : guest=%+v ", u.name, u.ID.String(), u.isGuest)
	go func() {
		time.Sleep(15 * time.Second)
		u.connection.EnableWriting()
	}()

	return nil
}

func (u *User) Unregister(h *WorldController) error {
	if x, ok := h.users.Get(u.ID); ok && x.queueID == u.queueID {
		defer func() {
			log.Warnf("User disconnected %s(%s)", x.name, x.ID.String())
		}()

		log.Info("Unregistering user")
		u.world.hub.mqtt.SafeUnsubscribe("user_control/" + u.ID.String() + "/#")
		h.users.Delete(x.ID)
		// write last known position
		if u.writeLkp {
			anchorId, vector := u.world.spaces.FindClosest(u.pos)
			if err := u.world.hub.DB.WriteLastKnownPosition(u.ID, u.world.ID, anchorId, &vector, 0); err != nil {
				log.Warn(errors.WithMessage(err, "Unregister: failed to write last known position")
			}
		}
		// remove from online_users in DB
		if err := u.UserOfflineAction(); err != nil {
			log.Warn(errors.WithMessage(err, "Unregister: failed to handle offline action")
		}
		// close connection
		u.connection.Close()
		h.users.UserLeft(u.ID)
	}
	return nil
}

func (u *User) MQTTMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	// client.IsConnected()
	const topicOffset = 87
	log.Debug("user mqtt topic:", msg.Topic())
	log.Debug("user mqtt offset topic:", msg.Topic()[topicOffset:])
	subtopics := strings.Split(msg.Topic()[topicOffset:], "/")

	switch subtopics[0] {
	case "text":
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, string(msg.Payload()),
			).WebsocketMessage(),
		)
	case "relay":
		var module string
		if len(subtopics) > 1 {
			module = subtopics[1]
		}
		u.connection.Send(
			posbus.NewRelayToReactMsg(
				module, msg.Payload(),
			).WebsocketMessage(),
		)
	}
}

func (u *User) UserOfflineAction() error {
	log.Info("User: UserOfflineAction:", u.ID.String())
	if err := u.world.hub.DB.RemoveOnline(u.ID, u.world.ID); err != nil {
		log.Warn(errors.WithMessage(err, "UserOfflineAction: failed to remove online from db by world id"))
	}
	cspace, ok := u.currentSpace.Load().(uuid.UUID)
	if !ok {
		return errors.Errorf("invalid current space type: %T", cspace)
	}
	if cspace != uuid.Nil {
		if err := u.world.hub.DB.RemoveOnline(u.ID, cspace); err != nil {
			log.Warn(errors.WithMessage(err, "UserOfflineAction: failed to remove online from db by space"))
		}
		// u.currentSpace. = uuid.Nil
	}
	if err := u.world.hub.DB.RemoveDynamicWorldMembership(u.ID, u.world.ID); err != nil {
		log.Warn(errors.WithMessage(err, "UserOfflineAction: failed to remove membership from db"))
	}
	if u.isGuest {
		u.world.hub.RemoveUserWithDelay(u.ID, defaultDelayForUsersForRemove)
	}
}

func (u *User) OnMessage(msg *posbus.Message) {
	switch msg.Type() {
	case posbus.MsgTypeFlatBufferMessage:
		switch msg.AsFlatBufferMessage().MsgType() {
		default:
			log.Warn("Got unknown Flatbuffer message for user:", u.ID, "msg:", msg.AsFlatBufferMessage().MsgType())
			return
		}
	case posbus.MsgTriggerInteraction:
		u.InteractionHandler(msg.AsTriggerInteraction())
	case posbus.MsgTypeSendPosition:
		u.UpdatePosition(msg.AsSendPos())
	case posbus.MsgTypeSwitchWorld:
		if err := u.SwitchWorld(msg.AsSwitchWorld().World()); err != nil {
			log.Error(errors.WithMessage(err, "OnMessage: failed to switch world"))
		}
	case posbus.MsgTypeSignal:
		u.HandleSignals(msg.AsSignal().Signal())
	default:
		log.Warn("Got unknown message for user:", u.ID, "msg:", msg)
	}
}
func (u *User) HandleSignals(s posbus.Signal) {
	switch s {
	case posbus.SignalReady:
		log.Debugf("Got signalReady from %s", u.ID.String())
		go u.connection.EnableWriting()
	}
}

func (u *User) SwitchWorld(newWorldId uuid.UUID) error {
	if newWorldId == u.world.ID {
		return nil
	}
	log.Info("Request to teleport to", newWorldId)

	_, err := u.world.hub.GetWorldController(newWorldId)
	if err != nil {
		return errors.WithMessage(err, "failed to get world controller")
	}

	u.world.users.Delete(u.ID)

	anchorId, vector := u.world.spaces.FindClosest(u.pos)
	if err := u.world.hub.DB.WriteLastKnownPosition(u.ID, u.world.ID, anchorId, &vector, 0); err != nil {
		log.Warn(errors.WithMessage(err, "SwitchWorld: failed to write last known position"))
	}

	spaceId, v := u.world.hub.DB.GetUserSpawnPositionInWorld(u.ID, newWorldId)
	u.writeLkp = false
	if err := u.world.hub.DB.WriteLastKnownPosition(u.ID, newWorldId, spaceId, &v, 1); err != nil {
		log.Warn(errors.WithMessage(err,"SwitchWorld: failed to write last known position"))
	}

	u.connection.Close()
	return nil
	// oldworld := u.world
	// TODO: u must be changed, as currently it does not write last position in previous world
	// time.Sleep(100 * time.Millisecond)

	// world.registerUser <- u
	// TODO: shouldn't u be run in a separate goroutine not to sleep caller goroutine?

	// world.unregisterUser <- u
}

func (u *User) UpdatePosition(data []byte) {
	u.world.users.positionLock.RLock()
	copy(u.posbuf[16:28], data)
	u.world.users.positionLock.RUnlock()
	// logger.Logln(4, "Updated pos:", u.pos, *data)
	currentTime := time.Now().Unix()
	// if (currentTime - u.lastUpdate) > 5 {
	// u.world.hub.mqtt.SafePublish("activity/user/posbus", 0, false, []byte(u.ID.String()))
	u.lastUpdate = currentTime
	// }
}

func (u *User) Send(m *websocket.PreparedMessage) {
	u.connection.Send(m)
}
