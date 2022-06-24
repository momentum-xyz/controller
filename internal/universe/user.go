package universe

import (
	"github.com/momentum-xyz/controller/utils"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
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
		log.Infof("Spawned %s on %s", u.ID, u.world.ID)
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
	if err := u.connection.SendDirectly(posbus.NewSendPositionMsg(ipos).WebsocketMessage()); err != nil {
		return errors.WithMessage(err, "failed to send position")
	}
	log.Info("send initial world")
	// AddToWorld is happening there
	if err := u.world.AddUserToWorld(u); err != nil {
		return errors.WithMessage(err, "failed to add user to world")
	}
	wc.hub.mqtt.SafeSubscribe("user_control/"+wc.ID.String()+"/"+u.ID.String()+"/#", 1, u.MQTTMessageHandler)
	if err := u.OnlineAction(); err != nil {
		log.Warn(errors.WithMessagef(err, "User: Register: failed to handle online action: %s", u.ID))
	}
	log.Warnf("Registration done for %s(%s) : guest=%+v ", u.name, u.ID.String(), u.isGuest)
	go func() {
		time.Sleep(30 * time.Second)
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
				log.Warn(errors.WithMessage(err, "User: Unregister: failed to write last known position"))
			}
		}
		// remove from online_users in DB
		if err := u.OfflineAction(); err != nil {
			log.Warn(errors.WithMessage(err, "User: Unregister: failed to handle offline action"))
		}
		// close connection
		u.connection.Close()
		h.users.UserLeft(u.ID)
	}
	return nil
}

func (u *User) OnlineAction() error {
	u.world.hub.CancelCleanupUser(u.ID)
	if err := u.world.InsertOnline(u.ID); err != nil {
		return errors.WithMessage(err, "failed to insert online")
	}
	return u.world.InsertWorldDynamicMembership(u.ID)
}

func (u *User) OfflineAction() error {
	hub := u.world.hub
	hub.CleanupUserWithDelay(u.ID, func(id uuid.UUID) error {
		if err := hub.WorldStorage.RemoveWorldOnlineUser(id, u.world.GetID()); err != nil {
			return errors.WithMessagef(err, "failed to remove world online user: %s, %s", id, u.world.GetID())
		}
		if err := hub.WorldStorage.RemoveUserDynamicMembership(id); err != nil {
			return errors.WithMessagef(err, "failed to remove user dynamic membership: %s, %s", id, u.world.GetID())
		}
		return nil
	})

	spaceID := utils.GetFromAny(u.currentSpace.Load(), uuid.Nil)
	if spaceID == uuid.Nil {
		return nil
	}
	if ok, err := hub.SpaceStorage.CheckOnlineSpaceByID(spaceID); err != nil {
		return errors.WithMessagef(err, "failed to check online space by id: %s, %s", u.ID, spaceID)
	} else if !ok {
		hub.CleanupSpaceWithDelay(spaceID, utils.EmptyTimerFunc[uuid.UUID])
	}

	return nil
}

func (u *User) MQTTMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	// client.IsConnected()
	const topicOffset = 87 // nice
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
			log.Error(errors.WithMessage(err, "User: OnMessage: failed to switch world"))
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
		if err := u.world.SendWorldData(u); err != nil {
			log.Error(errors.WithMessagef(err, "User: HandleSignals: SignalReady: failed to send world data: %s", u.ID))
			u.world.unregisterUser <- u
			return
		}
		u.connection.EnableWriting()
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
		log.Warn(errors.WithMessage(err, "SwitchWorld: SwitchWorld: failed to write last known position"))
	}

	spaceId, v := u.world.hub.DB.GetUserSpawnPositionInWorld(u.ID, newWorldId)
	u.writeLkp = false
	if err := u.world.hub.DB.WriteLastKnownPosition(u.ID, newWorldId, spaceId, &v, 1); err != nil {
		log.Warn(errors.WithMessage(err, "SwitchWorld: SwitchWorld: failed to write last known position"))
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

func (u *User) UpdateUsersOnSpace(spaceID uuid.UUID) error {
	space, ok := u.world.spaces.Get(spaceID)
	if !ok {
		return nil
	}
	p := influxdb2.NewPoint(
		"space_users",
		map[string]string{"world": u.world.influxtag, "space": space.Name},
		map[string]interface{}{"count": len(u.world.users.GetOnSpace(spaceID))},
		time.Now(),
	)
	return u.world.hub.WriteInfluxPoint(p)
}
