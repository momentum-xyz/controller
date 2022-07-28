package universe

import (
	"context"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"net/url"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/internal/mqtt"
	"github.com/momentum-xyz/controller/internal/net"
	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/internal/space"
	"github.com/momentum-xyz/controller/internal/storage"
	"github.com/momentum-xyz/controller/internal/user"
	"github.com/momentum-xyz/controller/internal/world"
	"github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	influx_db2 "github.com/influxdata/influxdb-client-go/v2"
	influx_api "github.com/influxdata/influxdb-client-go/v2/api"
	influx_write "github.com/influxdata/influxdb-client-go/v2/api/write"
	// External
	"github.com/pkg/errors"
	"github.com/sasha-s/go-deadlock"
)

const (
	usersCleanupDelay  = 3 * time.Minute
	spacesCleanupDelay = 4 * time.Minute
	influxDBTimeout    = 2 * time.Second
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
	cfg              *config.Config
	Worlds           *utils.SyncMap[uuid.UUID, *WorldController]
	node             node
	mu               deadlock.RWMutex
	DB               *storage.Database
	SpaceStorage     space.Storage
	WorldStorage     world.Storage
	UserStorage      user.Storage
	net              *net.Networking
	influx           influx_api.WriteAPIBlocking
	mqtt             safemqtt.Client
	msgBuilder       *message.Builder
	guestUserTypeId  uuid.UUID
	usersForCleanup  *utils.TimerSet[uuid.UUID]
	spacesForCleanup *utils.TimerSet[uuid.UUID]
}

func NewControllerHub(cfg *config.Config, networking *net.Networking, msgBuilder *message.Builder, db *storage.Database, mqttClient safemqtt.Client) (*ControllerHub, error) {
	influxClient := influx_db2.NewClient(cfg.Influx.URL, cfg.Influx.TOKEN)

	hub := &ControllerHub{
		cfg:              cfg,
		Worlds:           utils.NewSyncMap[uuid.UUID, *WorldController](),
		DB:               db,
		SpaceStorage:     space.NewStorage(*db.DB),
		WorldStorage:     world.NewStorage(*db.DB),
		UserStorage:      user.NewStorage(*db.DB),
		net:              networking,
		influx:           influxClient.WriteAPIBlocking(cfg.Influx.ORG, cfg.Influx.BUCKET),
		mqtt:             mqttClient,
		msgBuilder:       msgBuilder,
		usersForCleanup:  utils.NewTimerSet[uuid.UUID](),
		spacesForCleanup: utils.NewTimerSet[uuid.UUID](),
	}

	deadlock.Opts.DeadlockTimeout = time.Second * 60

	go TimeInformer(hub.mqtt)

	if err := hub.GetNodeSettings(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to get node settings"))
	}
	if err := hub.CleanupUsersWithDelay(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to cleanup users with delay"))
	}
	if err := hub.CleanupSpacesWithDelay(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to cleanup spaces with delay"))
	}
	if err := hub.CleanupOnlineUsers(); err != nil {
		log.Warn(errors.WithMessage(err, "NewControllerHub: failed to cleanup online users"))
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

func (ch *ControllerHub) CleanupOnlineUsers() error {
	log.Info("Cleanup online users")
	if err := ch.DB.RemoveAllOnlineUsers(); err != nil {
		return errors.WithMessage(err, "failed to remove all online users")
	}
	return ch.DB.RemoveAllDynamicMembership()

	// NOTE: for future
	// rows, err := wc.hub.DB.Query(`call GetSpaceDescendantsIDs(?,100);`, utils.BinId(wc.ID))
	// if err != nil {
	//	return err
	// }
	// defer rows.Close()
	//
	// var ids [][]byte
	// for rows.Next() {
	//	var spaceId []byte
	//	var parentId []byte
	//	var level uint32
	//	err := rows.Scan(&spaceId, &parentId, &level)
	//	if err != nil {
	//		return err
	//	}
	//	ids = append(ids, spaceId)
	// }
	//
	// _, err = wc.hub.DB.Exec(`DELETE FROM online_users WHERE spaceId IN(?)`, bytes.Join(ids, []byte(",")))
	// err != nil
	// return err
}

func (ch *ControllerHub) CleanupSpacesWithDelay() error {
	spaces, err := ch.SpaceStorage.GetOnlineSpaceIDs()
	if err != nil {
		return errors.WithMessage(err, "failed to get online space ids")
	}
	for i := range spaces {
		ch.CleanupSpaceWithDelay(spaces[i], ch.CleanupSpace)
	}
	return nil
}

func (ch *ControllerHub) CleanupSpaceWithDelay(id uuid.UUID, fn utils.TimerFunc[uuid.UUID]) {
	ch.spacesForCleanup.Set(id, spacesCleanupDelay, fn)
}

func (ch *ControllerHub) CleanupSpace(id uuid.UUID) error {
	log.Infof("ControllerHub: CleanupSpace: %s", id)
	ch.mqtt.SafePublish("clean_up/space", 0, false, utils.BinId(id))
	return nil
}

func (ch *ControllerHub) CancelCleanupSpace(id uuid.UUID) {
	ch.spacesForCleanup.Stop(id)
}

func (ch *ControllerHub) CleanupUsersWithDelay() error {
	users, err := ch.UserStorage.GetOnlineUserIDs()
	if err != nil {
		return errors.WithMessage(err, "failed to get online user ids")
	}
	for i := range users {
		ch.CleanupUserWithDelay(users[i], ch.CleanupUser)
	}
	return nil
}

func (ch *ControllerHub) CleanupUserWithDelay(id uuid.UUID, fn utils.TimerFunc[uuid.UUID]) {
	ch.usersForCleanup.Set(id, usersCleanupDelay, fn)
}

func (ch *ControllerHub) CleanupUser(id uuid.UUID) error {
	log.Infof("ControllerHub: CleanupUser: %s", id)
	ch.mqtt.SafePublish("clean_up/user", 0, false, utils.BinId(id))
	return nil
}

func (ch *ControllerHub) CancelCleanupUser(id uuid.UUID) {
	ch.usersForCleanup.Stop(id)
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
	ctx, cancel := context.WithTimeout(context.Background(), influxDBTimeout)
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
		numUsers, err := ch.UserStorage.GetUserCount()
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
		log.Warn(errors.WithMessagef(err, "ControllerHub: spawnUserAt: failed to get user info: %s", userID))
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
