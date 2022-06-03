package universe

import (
	"context"
	"net/url"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/internal/mqtt"
	"github.com/momentum-xyz/controller/internal/net"
	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/internal/space"
	"github.com/momentum-xyz/controller/internal/storage"
	"github.com/momentum-xyz/controller/internal/user"
	"github.com/momentum-xyz/controller/internal/world"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"

	// External
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	influx_db2 "github.com/influxdata/influxdb-client-go/v2"
	influx_api "github.com/influxdata/influxdb-client-go/v2/api"
	influx_write "github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/pkg/errors"
	"github.com/sasha-s/go-deadlock"
)

const (
	defaultDelayForUsersForRemove = 5 * time.Minute
	defaultInfluxDBTimeout        = 2 * time.Second
)

const (
	selectNodeSettingsIdAndName = `SELECT ns1.value AS id, ns2.value AS name FROM node_settings ns1
    									JOIN node_settings ns2	WHERE ns1.name = 'id' AND ns2.name = 'name';`
)

var log = logger.L()

type node struct {
	id   uuid.UUID
	name string
}

type ControllerHub struct {
	cfg                     *config.Config
	Worlds                  *utils.SyncMap[uuid.UUID, *WorldController]
	node                    node
	mu                      deadlock.RWMutex
	DB                      *storage.Database
	SpaceStorage            space.Storage
	WorldStorage            world.Storage
	UserStorage             user.Storage
	net                     *net.Networking
	influx                  influx_api.WriteAPIBlocking
	mqtt                    safemqtt.Client
	msgBuilder              *message.Builder
	guestUserTypeId         uuid.UUID
	usersForRemoveWithDelay *utils.SyncMap[uuid.UUID, utils.Unique[context.CancelFunc]]
}

func NewControllerHub(cfg *config.Config, networking *net.Networking, msgBuilder *message.Builder) (*ControllerHub, error) {
	db, err := storage.OpenDB(&cfg.MySQL)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to init storage")
	}
	if err := storage.MigrateDb(db, &cfg.MySQL); err != nil {
		return nil, errors.WithMessage(err, "failed to migrate db")
	}

	mqttClient, err := safemqtt.InitMQTTClient(&cfg.MQTT, "worlds_controller-"+uuid.NewString())
	if err != nil {
		return nil, errors.WithMessage(err, "failed to init mqtt client")
	}

	influxClient := influx_db2.NewClient(cfg.Influx.URL, cfg.Influx.TOKEN)

	hub := &ControllerHub{
		cfg:                     cfg,
		Worlds:                  utils.NewSyncMap[uuid.UUID, *WorldController](),
		DB:                      db,
		SpaceStorage:            space.NewStorage(*db.DB),
		WorldStorage:            world.NewStorage(*db.DB),
		UserStorage:             user.NewStorage(*db.DB),
		net:                     networking,
		influx:                  influxClient.WriteAPIBlocking(cfg.Influx.ORG, cfg.Influx.BUCKET),
		mqtt:                    mqttClient,
		msgBuilder:              msgBuilder,
		usersForRemoveWithDelay: utils.NewSyncMap[uuid.UUID, utils.Unique[context.CancelFunc]](),
	}

	deadlock.Opts.DeadlockTimeout = time.Second * 60

	go TimeInformer(hub.mqtt)

	if err := hub.GetNodeSettings(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to get node settings"))
	}
	if err := hub.RemoveGuestsWithDelay(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to remove guests with delay"))
	}

	worlds, err := hub.WorldStorage.GetWorlds()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get worlds")
	}
	for _, worldID := range worlds {
		if _, err := hub.AddWorldController(worldID); err != nil {
			return nil, errors.WithMessagef(err, "failed to add world controller: %s", worldID)
		}
	}
	hub.mqtt.SafeSubscribe("updates/spaces/changed", 1, safemqtt.LogMQTTMessageHandler("spaces changed", hub.ChangeHandler))

	return hub, nil
}

func (ch *ControllerHub) RemoveGuestsWithDelay() error {
	guests, err := ch.DB.GetUsersIDsByType(ch.guestUserTypeId)
	if err != nil {
		return errors.WithMessage(err, "failed to get guests")
	}
	for i := range guests {
		ch.RemoveUserWithDelay(guests[i], defaultDelayForUsersForRemove)
	}
	return nil
}

func (ch *ControllerHub) RemoveUserWithDelay(id uuid.UUID, delay time.Duration) {
	ch.usersForRemoveWithDelay.Mu.Lock()
	val, ok := ch.usersForRemoveWithDelay.Data[id]
	if ok {
		val.Value()()
	}

	ctx, cancel := context.WithCancel(context.Background())
	val = utils.NewUnique(cancel)
	ch.usersForRemoveWithDelay.Data[id] = val
	ch.usersForRemoveWithDelay.Mu.Unlock()

	go func() {
		defer func() {
			ch.usersForRemoveWithDelay.Mu.Lock()
			defer ch.usersForRemoveWithDelay.Mu.Unlock()

			if val1, ok := ch.usersForRemoveWithDelay.Data[id]; ok && val1.Equals(val) {
				delete(ch.usersForRemoveWithDelay.Data, id)
			}
		}()

		dt := time.NewTimer(delay)
		select {
		case <-dt.C:
			if err := ch.DB.RemoveFromUsers(id); err != nil {
				log.Warn(errors.WithMessage(err, "ControllerHub: RemoveUserWithDelay: failed to remove from db"))
			}
			log.Debug("ControllerHub: RemoveUserWithDelay: user removed: %s", id.String())
		case <-ctx.Done():
			dt.Stop()
		}
	}()
}

func (ch *ControllerHub) ChangeHandler(client mqtt.Client, message mqtt.Message) error {
	id, err := uuid.ParseBytes(message.Payload())
	if err != nil {
		return errors.WithMessage(err, "failed to parse id")
	}
	log.Debug("Update for:", id)

	ch.Worlds.Mu.RLock()
	defer ch.Worlds.Mu.RUnlock()

	for u, controller := range ch.Worlds.Data {
		log.Debug("Check update in world:", u)
		if _, ok := controller.spaces.GetPresent(id); ok {
			log.Debug("Check update found!")
			controller.updateSpace <- id
		}
	}

	return nil
}

// TODO: this method never ends
func (ch *ControllerHub) NetworkRunner() {
	log.Info("Started NetworkRuner")
	for {
		log.Info("ControllerHub::NetworkRunner waiting for successful handshake")
		handshake := <-ch.net.HandshakeChan
		log.Info("ControllerHub::NetworkRunner received successful handshake")
		go func() {
			if err := ch.spawnUser(handshake); err != nil {
				log.Error(errors.WithMessage(err, "ControllerHub: NetworkRunner: failed to spawn user"))
			}
		}()
	}
}

func (ch *ControllerHub) WriteInfluxPoint(point *influx_write.Point) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultInfluxDBTimeout)
	defer func() {
		cancel()
		log.Warn("ControllerHub: WriteInfluxPoint: stat sent")
	}()

	return ch.influx.WritePoint(ctx, point)
}

// TODO: this method never ends
func (ch *ControllerHub) UpdateTotalUsers() {
	tag := ch.node.name + " // " + ch.node.id.String()
	for range time.Tick(time.Minute) {
		numUsers, err := ch.UserStorage.SelectUserCount()
		if err != nil {
			log.Warn(errors.WithMessage(err, "ControllerHub: UpdateTotalUsers: failed to get users count"))
			continue
		}

		p := influx_db2.NewPoint(
			"ConnectedUsers",
			map[string]string{"node": tag},
			map[string]interface{}{"count": numUsers},
			time.Now(),
		)
		if err := ch.WriteInfluxPoint(p); err != nil {
			log.Warn(errors.WithMessage(err, "ControllerHub: UpdateTotalUsers: failed to write influx point"))
		}
	}
}

func (ch *ControllerHub) GetNodeSettings() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := ch.DB.QueryContext(ctx, selectNodeSettingsIdAndName)
	if err != nil {
		return errors.WithMessage(err, "failed to query db")
	}
	defer rows.Close()

	var n node
	if rows.Next() {
		if err := rows.Scan(&n.id, &n.name); err != nil {
			return errors.WithMessage(err, "failed to scan rows")
		}
		ch.node = n
	}

	guestUserType, err := ch.DB.GetGuestUserTypeId("Temporary User")
	if err != nil {
		log.Warn(errors.WithMessage(err, "ControllerHub: GetNodeSettings: failed to get guest user type"))
	}
	ch.guestUserTypeId = guestUserType

	return nil
}

func (ch *ControllerHub) GetUserSpawnPoint(uid uuid.UUID, URL *url.URL) (uuid.UUID, cmath.Vec3, error) {
	worldID, spaceId, v, err := ch.DB.GetUserSpawnPosition(uid, URL)
	log.Info("Spawn flow: Got SpawnPos", uid)

	if err != nil {
		log.Info("Spawn flow: Pos was wrong", uid)
		return uuid.Nil, cmath.DefaultPosition(), errors.WithMessage(err, "no place to spawn")
	}

	// logger.Logln(1, spaceId, worldId)
	log.Info("Spawn flow: Into the calcs", uid)
	wc, err := ch.GetWorldController(worldID)
	if err != nil {
		return uuid.Nil, cmath.DefaultPosition(), errors.WithMessage(err, "failed to get world controller")
	}
	v2, err := wc.PositionRelativeToAbsolute(spaceId, v)
	if err != nil {
		return uuid.Nil, cmath.DefaultPosition(), errors.WithMessage(err, "failed to get relative position")
	}

	log.Info("Spawn flow: Got absolute", uid)
	return worldID, v2, nil
}

func (ch *ControllerHub) GetWorldController(worldID uuid.UUID) (*WorldController, error) {
	if wc, ok := ch.Worlds.Load(worldID); ok {
		return wc, nil
	}
	return ch.AddWorldController(worldID)
}

func (ch *ControllerHub) AddWorldController(worldID uuid.UUID) (*WorldController, error) {
	wc, err := newWorldController(worldID, ch, ch.msgBuilder)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create new world controller")
	}
	ch.Worlds.Store(worldID, wc)
	go func() {
		if err := wc.run(); err != nil {
			log.Error(errors.WithMessage(err, "ControllerHub: AddWorldController: failed to run world controller"))
		}
	}()

	return wc, nil
}

func (ch *ControllerHub) spawnUserAt(
	connection *socket.Connection, userID, sessionID, worldID uuid.UUID, pos cmath.Vec3,
) error {
	wc, err := ch.GetWorldController(worldID)
	if err != nil {
		return errors.WithMessage(err, "failed to get world controller")
	}
	log.Info("Spawn flow: got WC", userID)
	log.Infof("Spawning user %s on %s:[%f,%f,%f]", userID, wc.ID, pos.X, pos.Y, pos.Z)
	name, typeId, err := ch.DB.GetUserInfo(userID)
	if err != nil {
		return errors.WithMessage(err, "failed to get user info")
	}

	wc.registerUser <- &User{
		ID:         userID,
		SessionID:  sessionID,
		connection: connection,
		queueID:    uuid.New(),
		pos:        &pos,
		name:       name,
		isGuest:    typeId == ch.guestUserTypeId,
	}

	log.Info("Sent Reg")
	return nil
}

func (ch *ControllerHub) spawnUser(data *net.SuccessfulHandshakeData) error {
	log.Info("Spawn flow: request", data.UserID)
	worldID, pos, err := ch.GetUserSpawnPoint(data.UserID, data.URL)
	if err != nil {
		return errors.WithMessage(err, "failed to get user spawn position")
	}
	log.Info("Spawn flow: got point", data.UserID)

	return ch.spawnUserAt(data.Connection, data.UserID, data.SessionID, worldID, pos)
	// worldID, err := uuid.Parse("d83670c7-a120-47a4-892d-f9ec75604f74")
	// if err != nil {
	// 	return
	// }
}

func TimeInformer(client safemqtt.Client) {
	for {
		token := client.SafePublish(
			"control/periodic/reftime", 1, false, []byte(time.Now().UTC().Format("2006-01-02T15:04:05.999999Z")),
		)
		token.Wait()
		time.Sleep(10 * time.Second)
	}
}
