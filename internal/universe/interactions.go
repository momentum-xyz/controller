package universe

import (
	"encoding/json"
	"github.com/pkg/errors"

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
		if err := u.world.hub.DB.InsertOnline(u.ID, targetUUID); err != nil {
			log.Warn(errors.WithMessage(err, "InteractionHandler: trigger entered space: failed to insert one"))
		}
		if _, err := u.world.UpdateOnlineBySpaceId(targetUUID); err != nil {
			log.Warn(errors.WithMessage(err, "InteractionHandler: trigger entered space: failed to update online by space id"))
		}
	case posbus.TriggerLeftSpace:
		targetUUID := m.Target()
		u.currentSpace.Store(uuid.Nil)
		if err := u.world.hub.DB.RemoveOnline(u.ID, targetUUID); err != nil {
			log.Warn(errors.WithMessage(err, "InteractionHandler: trigger left space: failed to remove online from db"))
		}
		if _, err := u.world.UpdateOnlineBySpaceId(targetUUID); err != nil {
			log.Warn(errors.WithMessagef(err, "InteractionHandler: trigger left space: failed to update online by space id"))
		}
	case posbus.TriggerHighFive:
		if err := u.HandleHighFive(m); err != nil {
			log.Warn(errors.WithMessage(err, "InteractionHandler: trigger high fives: failed to handle high five"))
		}
	case posbus.TriggerStake:
		u.HandleStake(m)
	default:
		log.Warn("InteractionHandler: got unknown interaction for user:", u.ID, "kind:", kind)
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

func (u *User) HandleHighFive(m *posbus.TriggerInteraction) error {
	targetUUID := m.Target()
	log.Info("Got H5 from user:", u.ID, "to user:", targetUUID)

	if targetUUID == u.ID {
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, "You can't high-five yourself",
			).WebsocketMessage(),
		)
		return nil
	}

	target, found := u.world.users.Get(targetUUID)
	if !found {
		u.connection.Send(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, "Receiving user not found",
			).WebsocketMessage(),
		)
		return nil
	}

	if err := u.world.hub.DB.UpdateHighFives(u.ID, targetUUID); err != nil {
		log.Warn(errors.WithMessage(err, "User: HandleHighFive: failed to update high fives"))
	}
	uname, err := u.world.hub.UserStorage.GetUserName(u.ID)
	if err != nil {
		log.Warn(errors.WithMessage(err, "User: HandleHighFive: failed to get user name"))
	}

	msg := map[string]interface{}{
		"senderId":   u.ID.String(),
		"receiverId": targetUUID.String(),
		"message":    uname + " has high-fived you!",
	}
	data, err := json.Marshal(&msg)
	if err != nil {
		return errors.WithMessage(err, "failed to marshal data")
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

	return nil
}
