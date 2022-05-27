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

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	influx_db2 "github.com/influxdata/influxdb-client-go/v2"
	// External
	"github.com/pkg/errors"
	"github.com/sasha-s/go-deadlock"
	// influx_api "github.com/influxdata/influxdb-client-go/v2/api"
	influx_write "github.com/influxdata/influxdb-client-go/v2/api/write"
)

const (
	defaultDelayForUsersForRemove = 5 * time.Minute
	selectNodeSettingsIdAndName   = `select ns1.value as id, ns2.value as name from node_settings ns1
											 join node_settings ns2	where ns1.name = 'id' and ns2.name = 'name';`
)

var log = logger.L()

type node struct {
	id   uuid.UUID
	name string
}

type ControllerHub struct {
	cfg          *config.Config
	Worlds       map[uuid.UUID]*WorldController
	node         node
	mu           deadlock.RWMutex
	DB           *storage.Database
	SpaceStorage space.Storage
	WorldStorage world.Storage
	UserStorage  user.Storage
	net          *net.Networking
	// influx       influx_api.WriteAPIBlocking
	mqtt                    safemqtt.Client
	msgBuilder              *message.Builder
	guestUserTypeId         uuid.UUID
	usersForRemoveWithDelay *utils.SyncMap[uuid.UUID, utils.Unique[context.CancelFunc]]
}

func NewControllerHub(cfg *config.Config, networking *net.Networking, msgBuilder *message.Builder) (*ControllerHub, error) {
	db := storage.OpenDB(&cfg.MySQL)

	mqttClient, err := safemqtt.InitMQTTClient(&cfg.MQTT, "worlds_controller-"+uuid.NewString())
	if err != nil {
		return nil, errors.WithMessage(err, "failed to init mqtt client")
	}

	hub := &ControllerHub{
		cfg:                     cfg,
		Worlds:                  make(map[uuid.UUID]*WorldController),
		DB:                      db,
		SpaceStorage:            space.NewStorage(*db.DB),
		WorldStorage:            world.NewStorage(*db.DB),
		UserStorage:             user.NewStorage(*db.DB),
		net:                     networking,
		mqtt:                    mqttClient,
		msgBuilder:              msgBuilder,
		usersForRemoveWithDelay: utils.NewSyncMap[uuid.UUID, utils.Unique[context.CancelFunc]](),
	}

	deadlock.Opts.DeadlockTimeout = time.Second * 60
	storage.MigrateDb(hub.DB, &cfg.MySQL)
	go TimeInformer(hub.mqtt)

	// client := influxdb2.NewClient(hub.cfg.Influx.URL, hub.cfg.Influx.TOKEN)
	// hub.influx = client.WriteAPIBlocking(hub.cfg.Influx.ORG, hub.cfg.Influx.BUCKET)

	hub.GetNodeSettings()
	hub.RemoveGuestsWithDelay()

	for _, WorldID := range hub.WorldStorage.GetWorlds() {
		hub.AddWorldController(WorldID)
	}
	hub.mqtt.SafeSubscribe("updates/spaces/changed", 1, hub.ChangeHandler)

	return hub, nil
}

func (ch *ControllerHub) RemoveGuestsWithDelay() {
	guests, err := ch.DB.GetUsersIDsByType(ch.guestUserTypeId)
	if err != nil {
		log.Error(errors.WithMessage(errors.WithMessage(err, "failed to get guests"), "failed to remove guests"))
		return
	}
	for i := range guests {
		ch.RemoveUserWithDelay(guests[i], defaultDelayForUsersForRemove)
	}
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
				log.Warn(errors.WithMessage(err, "RemoveUserWithDelay: failed to remove from db"))
			}
			log.Debug("RemoveUserWithDelay: user removed: %s", id.String())
		case <-ctx.Done():
			dt.Stop()
		}
	}()
}

func (ch *ControllerHub) ChangeHandler(client mqtt.Client, message mqtt.Message) {
	id, _ := uuid.ParseBytes(message.Payload())
	log.Debug("Update for:", id)
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	for u, controller := range ch.Worlds {
		log.Debug("Check update in world:", u)
		_, ok := controller.spaces.GetPresent(id)
		if ok {
			log.Debug("Check update found!")
			controller.updateSpace <- id
		}
	}
}

func (ch *ControllerHub) NetworkRunner() {
	log.Info("Started NetworkRuner")
	for {
		log.Info("ControllerHub::NetworkRunner waiting for successful handshake")
		handshake := <-ch.net.HandshakeChan
		log.Info("ControllerHub::NetworkRunner received successful handshake")
		go ch.spawnUser(handshake)
	}
}

func (ch *ControllerHub) WriteInfluxPoint(point *influx_write.Point) error {
	return nil
	// err := ch.influx.WritePoint(context.Background(), point)
	// err != nil
	// return err
}

func (ch *ControllerHub) UpdateTotalUsers() {
	tag := ch.node.name + " // " + ch.node.id.String()
	numUsers := 0
	for range time.Tick(time.Minute) {
		numUsers = ch.UserStorage.SelectUserCount()
		p := influx_db2.NewPoint(
			"ConnectedUsers", map[string]string{"node": tag}, map[string]interface{}{"count": numUsers}, time.Now(),
		)
		ch.WriteInfluxPoint(p)
	}
}

func (ch *ControllerHub) GetNodeSettings() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := ch.DB.QueryContext(ctx, selectNodeSettingsIdAndName)
	if err != nil {
		log.Info("could not get node settings", err)
		return
	}
	defer rows.Close()
	var n node
	if rows.Next() {
		err = rows.Scan(&n.id, &n.name)
		if err != nil {
			log.Info("could not parse node settings", err)
			return
		}
		ch.node = n
	}
	ch.guestUserTypeId = ch.DB.GetGuestUserTypeId("Temporary User")
}

func (ch *ControllerHub) GetUserSpawnPoint(uid uuid.UUID, URL *url.URL) (uuid.UUID, cmath.Vec3, error) {
	worldID, spaceId, v, err := ch.DB.GetUserSpawnPosition(uid, URL)
	log.Info("Spawn flow: Got SpawnPos", uid)

	if err != nil {
		log.Info("Spawn flow: Pos was wrong", uid)
		return uuid.Nil, cmath.DefaultPosition(), errors.New("no place to spawn")
	}

	// logger.Logln(1, spaceId, worldId)
	log.Info("Spawn flow: Into the calcs", uid)
	v2 := ch.GetWorldController(worldID).PositionRelativeToAbsolute(spaceId, v)
	log.Info("Spawn flow: Got absolute", uid)
	return worldID, v2, nil
}

func (ch *ControllerHub) GetWorldController(worldID uuid.UUID) *WorldController {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if wc, ok := ch.Worlds[worldID]; ok {
		return wc
	}

	return ch.AddWorldController(worldID)
}

func (ch *ControllerHub) AddWorldController(worldID uuid.UUID) *WorldController {
	wc, err := newWorldController(worldID, ch, ch.msgBuilder)
	if err != nil {
		log.Error("error when creating new world world: %v", err)
		return nil
	}
	ch.Worlds[worldID] = wc
	go wc.run()

	return wc
}

func (ch *ControllerHub) spawnUserAt(
	connection *socket.Connection, userID, sessionID, worldID uuid.UUID, pos cmath.Vec3,
) {
	wc := ch.GetWorldController(worldID)
	if wc == nil {
		log.Error("Spawn flow: error WC", userID)
		return
	}
	log.Info("Spawn flow: got WC", userID)
	log.Info("Spawning user %s on %s:[%f,%f,%f]", userID, wc.ID, pos.X, pos.Y, pos.Z)
	name, typeId := ch.DB.GetUserInfo(userID)

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
}

func (ch *ControllerHub) spawnUser(data *net.SuccessfulHandshakeData) {
	log.Info("Spawn flow: request", data.UserID)
	worldID, pos, err := ch.GetUserSpawnPoint(data.UserID, data.URL)
	if err != nil {
		log.Error("Spawn flow: error 1", data.UserID)
		return
	}
	log.Info("Spawn flow: got point", data.UserID)

	ch.spawnUserAt(data.Connection, data.UserID, data.SessionID, worldID, pos)
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
