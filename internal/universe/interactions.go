package universe

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/momentum-xyz/posbus-protocol/posbus"
	pputils "github.com/momentum-xyz/posbus-protocol/utils"
)

func (u *User) InteractionHandler(m *posbus.TriggerInteraction) {
	kind := m.Kind()
	targetUUID := m.Target()
	flag := m.Flag()
	label := m.Label()
	log.Info(
		"Incoming interaction for user", u.ID, "kind:", kind, "target:", targetUUID, "flag:", flag, "label:", label,
	)
	switch kind {
	case posbus.TriggerEnteredSpace:
		targetUUID := m.Target()
		u.currentSpace.Store(targetUUID)
		u.world.hub.DB.InsertOnline(u.ID, targetUUID)
		u.world.UpdateOnlineBySpaceId(targetUUID)
	case posbus.TriggerLeftSpace:
		targetUUID := m.Target()
		u.currentSpace.Store(uuid.Nil)
		u.world.hub.DB.RemoveOnline(u.ID, targetUUID)
		u.world.UpdateOnlineBySpaceId(targetUUID)
	case posbus.TriggerHighFive:
		u.HandleHighFive(m)
	case posbus.TriggerStake:
		u.HandleStake(m)
	default:
		log.Warn("Got unknown interaction for user:", u.ID, "kind:", kind)
	}
}

func (u *User) HandleStake(m *posbus.TriggerInteraction) {
	objectUUID := m.Target()
	log.Info("Got stake from user:", u.ID, "to object:", objectUUID)

	_, found := u.world.spaces.GetPresent(objectUUID)
	if !found {
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, "Staking space doesn't exist",
			).WebsocketMessage(),
		)
		return
	}

	effect := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
	effect.SetEffect(0, objectUUID, objectUUID, 401)
	u.world.Broadcast(effect.WebsocketMessage())
}

func (u *User) HandleHighFive(m *posbus.TriggerInteraction) {
	targetUUID := m.Target()
	log.Info("Got H5 from user:", u.ID, "to user:", targetUUID)

	if targetUUID == u.ID {
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, "You can't high-five yourself",
			).WebsocketMessage(),
		)
		return
	}

	target, found := u.world.users.Get(targetUUID)
	if !found {
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, "Receiving user not found",
			).WebsocketMessage(),
		)
		return
	}

	u.world.hub.DB.UpdateHighFives(u.ID, targetUUID)
	uname := u.world.hub.DB.GetUserName(u.ID)

	msg := make(map[string]interface{})
	msg["senderId"] = u.ID.String()
	msg["receiverId"] = targetUUID.String()
	msg["message"] = uname + " has high-fived you!"
	data, err := json.Marshal(&msg)
	if err != nil {
		return
	}

	target.connection.Send(
		posbus.NewRelayToReactMsg("high5", data).
			WebsocketMessage(),
	)

	u.connection.Send(
		posbus.NewSimpleNotificationMsg(
			posbus.DestinationReact, posbus.NotificationTextMessage, 0, "High five sent!",
		).WebsocketMessage(),
	)

	effect := posbus.NewTriggerTransitionalBridgingEffectsOnPositionMsg(1)
	effect.SetEffect(0, u.world.EffectsEmitter, pputils.Vec3(*u.pos), pputils.Vec3(*target.pos), 1001)
	u.world.Broadcast(effect.WebsocketMessage())
}
