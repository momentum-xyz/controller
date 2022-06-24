// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package universe

import (
	"encoding/json"
	_ "net/http/pprof"
	"strconv"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/extensions"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/extension"
	"github.com/momentum-xyz/controller/internal/spacetype"
	"github.com/momentum-xyz/controller/internal/storage"
	"github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/pkg/errors"
)

type WorldMeta struct {
	Name             string
	LODs             []uint32
	Decorations      []message.DecorationMetadata
	AvatarController uuid.UUID
	SkyboxController uuid.UUID
}

type RegRequest struct {
	id    uuid.UUID
	pos   cmath.Vec3
	theta float64
	// rotation math.Vec3
}

// Make sure that WorldController is extension.WorldController - DO NOT REMOVE
var _ extension.WorldController = (*WorldController)(nil)

// WorldController ...
type WorldController struct {
	hub       *ControllerHub
	ID        uuid.UUID
	meta      WorldMeta
	metaMsg   *websocket.PreparedMessage
	influxtag string
	// Register users.
	users *ConnectedUsers

	// spaces
	spaces *LoadedSpaces

	registerSpace   chan *RegRequest
	unregisterSpace chan uuid.UUID
	updateSpace     chan uuid.UUID

	// spaceTypes
	spaceTypes *spacetype.TSpaceTypes

	// registerUser requests from the users.
	registerUser chan *User

	// Unregister requests from users.
	unregisterUser chan *User

	AdditionalExtensions map[string]extension.Extension
	MainExtension        extension.Extension
	EffectsEmitter       uuid.UUID
	Config               config.World
	msgBuilder           *message.Builder
}

func newWorldController(worldID uuid.UUID, hub *ControllerHub, msgBuilder *message.Builder) (*WorldController, error) {
	wName, err := hub.WorldStorage.GetName(worldID)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get world name")
	}

	controller := WorldController{
		ID:              worldID,
		registerUser:    make(chan *User, 96),
		unregisterUser:  make(chan *User, 96),
		influxtag:       hub.node.name + ":" + wName + " // " + hub.node.id.String() + ":" + worldID.String(),
		hub:             hub,
		msgBuilder:      msgBuilder,
		spaceTypes:      spacetype.NewSpaceTypes(hub.SpaceStorage),
		registerSpace:   make(chan *RegRequest, 1024),
		unregisterSpace: make(chan uuid.UUID, 1024),
		updateSpace:     make(chan uuid.UUID, 1024),
	}
	controller.users = NewUsers(&controller)
	controller.spaces = newSpaces(&controller, hub.SpaceStorage, msgBuilder)

	go utils.ChanMonitor("world.registerUser", controller.registerUser, 3*time.Second)
	go utils.ChanMonitor("world.unregisterUser", controller.unregisterUser, 3*time.Second)
	go utils.ChanMonitor("world.registerSpace", controller.registerSpace, 3*time.Second)
	go utils.ChanMonitor("world.unregisterSpace", controller.unregisterSpace, 3*time.Second)
	go utils.ChanMonitor("world.updateSpace", controller.updateSpace, 3*time.Second)

	if err := controller.UpdateMeta(); err != nil {
		return nil, errors.WithMessage(err, "failed to update meta")
	}

	if err := controller.LoadExtensions(); err != nil {
		log.Error(errors.WithMessage(err, "newWorldController: failed to load extension"))
	}

	controller.spaces.Init()

	return &controller, nil
}

type WowMetadata struct {
	UserId  []string `json:"UserId"`
	WowType string   `json:"type"` // +1 -1
	Count   int      `json:"count"`
}

func (wc *WorldController) LoadExtensions() error {
	log.Info("loading WC Extension Kind: ", wc.Config.Kind)
	if wc.Config.Kind == "Kusama" {
		wc.MainExtension = extensions.NewKusama(wc)
	} else {
		wc.MainExtension = extensions.Default()
	}
	if wc.MainExtension == nil {
		// Exit ?
		log.Fatal("Can not load extension", wc.Config.Kind)
	}
	return wc.MainExtension.Init()
}

func (wc *WorldController) UpdateMeta() error {
	query := `SELECT name FROM spaces WHERE id = ?;`
	res, err := wc.hub.DB.Query(query, utils.BinId(wc.ID))
	if err != nil {
		return errors.WithMessagef(err, "failed to get space name: %s", wc.ID)
	}
	defer res.Close()

	if res.Next() {
		var name string
		if err = res.Scan(&name); err != nil {
			return errors.WithMessage(err, "failed to scan space name")
		}
		log.Debug("W Meta:", name)
		wc.meta.Name = name
	}

	query = `SELECT AVAController,SkyboxController,Decorations,LODs,config FROM world_definition WHERE id = ?;`
	res, err = wc.hub.DB.Query(query, utils.BinId(wc.ID))
	if err != nil {
		return errors.WithMessage(err, "failed to load world definition")
	}
	defer res.Close()

	if res.Next() {
		var AVAData, SkyboxData, DecorationsData, LODsData, ConfigData []byte
		// fmt.Println(res)
		if err := res.Scan(&AVAData, &SkyboxData, &DecorationsData, &LODsData, &ConfigData); err != nil {
			return errors.WithMessage(err, "failed to scan world definition")
		}
		wc.meta.AvatarController, err = uuid.FromBytes(AVAData)
		if err != nil {
			wc.meta.AvatarController = uuid.Nil
		}

		wc.meta.SkyboxController, err = uuid.FromBytes(SkyboxData)
		if err != nil {
			wc.meta.SkyboxController = uuid.Nil
		}

		if err := json.Unmarshal(LODsData, &wc.meta.LODs); err != nil {
			wc.meta.LODs = []uint32{1000, 4000, 6400}
		}

		if err := json.Unmarshal(DecorationsData, &wc.meta.Decorations); err != nil {
			wc.meta.Decorations = make([]message.DecorationMetadata, 0)
		}
		// fmt.Println(string(ConfigData))
		if err := json.Unmarshal(ConfigData, &wc.Config); err != nil {
			return errors.WithMessage(err, "failed to unmarshal config")
		}
		if r, ok := wc.Config.Spaces["effects_emitter"]; ok {
			wc.EffectsEmitter = r
		} else {
			log.Error("WorldController: UpdateMeta: effects emitter is not defined for world", wc.ID)
		}
		// if err != nil {
		//
		// }
		log.Debug("W Meta5:", wc.meta)
		wc.metaMsg = wc.msgBuilder.MsgSetWorld(
			wc.ID, wc.meta.Name, wc.meta.AvatarController, wc.meta.SkyboxController, wc.meta.LODs, wc.meta.Decorations,
		)
	}

	return nil
}

func (wc *WorldController) UpdateOnline() error {
	p := influxdb2.NewPoint(
		"online_users",
		map[string]string{"world": wc.influxtag},
		map[string]interface{}{"count": wc.users.Num()},
		time.Now(),
	)
	return wc.hub.WriteInfluxPoint(p)
}

func (wc *WorldController) UpdateOnlineBySpaceId(spaceId uuid.UUID) (int64, error) {
	s, ok := wc.spaces.GetPresent(spaceId)
	if !ok {
		// not necessary an error
		log.Info("Trying to update online user on non-present space ", spaceId.String())
		return 0, nil
	}

	onlineUsers, err := s.GetOnlineUsers()
	if err != nil {
		return 0, errors.WithMessage(err, "failed to get online users")
	}

	wc.Broadcast(
		wc.msgBuilder.SetObjectStrings(
			spaceId, map[string]string{
				peopleOnlineStringKey: strconv.Itoa(int(onlineUsers)),
			},
		),
	)

	return onlineUsers, nil
}

func (wc *WorldController) UpdateVibesBySpaceId(spaceId uuid.UUID) (int64, error) {
	space, ok := wc.spaces.Get(spaceId)
	if !ok {
		return 0, errors.Errorf("failed to get space")
	}

	vibesCount, err := space.CalculateVibes()
	if err != nil {
		return 0, errors.WithMessage(err, "failed to calculate vibes")
	}

	wc.Broadcast(
		wc.msgBuilder.SetObjectStrings(
			spaceId, map[string]string{
				vibesStringKey: strconv.Itoa(int(vibesCount)),
			},
		),
	)

	return vibesCount, nil
}

func (wc *WorldController) InsertOnline(id uuid.UUID) error {
	if err := wc.hub.DB.InsertOnline(id, wc.ID); err != nil {
		return errors.WithMessagef(err, "failed to insert online to db: %s", id)
	}
	go func() {
		if err := wc.UpdateOnline(); err != nil {
			log.Warn(errors.WithMessage(err, "WorldController: InsertOnline: failed update online"))
		}
	}()
	return nil
}

func (wc *WorldController) InsertWorldDynamicMembership(id uuid.UUID) error {
	query := `INSERT INTO user_spaces_dynamic (spaceId,UserId) VALUES (?,?) ON DUPLICATE KEY UPDATE spaceId=?;`
	_, err := wc.hub.DB.Exec(query, utils.BinId(wc.ID), utils.BinId(id), utils.BinId(wc.ID))
	if err != nil {
		return errors.WithMessagef(err, "failed to exec db: %s", id)
	}
	go func() {
		if err := wc.UpdateOnline(); err != nil {
			log.Warn(errors.WithMessage(err, "WorldController: InsertWorldDynamicMembership: failed to update online"))
		}
	}()
	return nil
}

func (wc *WorldController) PositionRelativeToAbsolute(spaceID uuid.UUID, v cmath.Vec3) (cmath.Vec3, error) {
	v2, err := wc.spaces.GetPos(spaceID)
	if err != nil {
		return cmath.MNan32Vec3(), errors.WithMessage(err, "failed to get pos")
	}

	v.Plus(v2)
	return v, nil
}

func (wc *WorldController) AddUserToWorld(u *User) error {
	log.Info("(HHHHH)")
	wc.users.Add(u)
	log.Info("User added to the world")

	if err := wc.SendSpaceData(wc.ID, u); err != nil {
		return errors.WithMessage(err, "failed to send space data")
	}

	if err := u.connection.SendDirectly(
		posbus.NewSignalMsg(
			posbus.SignalSpawn,
		).WebsocketMessage(),
	); err != nil {
		return errors.WithMessage(err, "failed to send spawn signal")
	}

	if err := u.connection.SendDirectly(
		posbus.NewRelayToReactMsg(
			"posbus", []byte(`{"status":"connected"}`),
		).WebsocketMessage(),
	); err != nil {
		return errors.WithMessage(err, "failed to send connection notification message")
	}

	return nil
}

func (wc *WorldController) SendSpaceData(spaceID uuid.UUID, u *User) error {
	space, ok := wc.spaces.Get(spaceID)
	if !ok {
		return errors.Errorf("failed to get space: %s", spaceID)
	}
	if err := u.connection.SendDirectly(space.GetObjectDef()); err != nil {
		return errors.WithMessagef(err, "failed to send space object defition: %s", space.id)
	}
	for id := range space.children {
		if err := wc.SendSpaceData(id, u); err != nil {
			log.Error(errors.WithMessagef(err, "failed to send child object definition: %s", id))
		}
	}
	return nil
}

func (wc *WorldController) SendWorldData(u *User) error {
	world, ok := wc.spaces.Get(wc.ID)
	if !ok {
		return errors.Errorf("failed to get world: %s", wc.ID)
	}

	world.RecursiveSendAllAttributes(u.connection)
	world.RecursiveSendAllStrings(u.connection)
	world.RecursiveSendAllTextures(u.connection)
	wc.MainExtension.InitUser(u)

	log.Info("WorldController: SendWorldData: world data sent")
	return nil
}

func (wc *WorldController) run() error {
	log.Info("Starting to serve world: ", wc.ID)
	if wc.MainExtension != nil {
		go func() {
			if err := wc.MainExtension.Run(); err != nil {
				log.Error(errors.WithMessage(err, "WorldController: run: failed to run main extension"))
			}
		}()
	}

	// fireworksTopic := fmt.Sprintf("/control/%v/fireworks", wc.ID.String())
	// defer wc.hub.mqtt.SafeUnsubscribe(fireworksTopic)
	// wc.hub.mqtt.SafeSubscribe(fireworksTopic, 1, func(client mqtt.Client, message mqtt.Message) {
	// 	var pos math.Vec3
	// 	if err := json.Unmarshal(message.Payload(), &pos); err == nil {
	// 		for _, c := range wc.users {
	// 			net.SendActionFireworks(c.connectionID, pos)
	// 		}
	// 	}
	// })

	for {
		select {
		case req := <-wc.registerSpace:
			// fmt.Println(color.Red, "RegSpace", color.Reset)
			wc.spaces.Load(req)
			n := len(wc.registerSpace)
			for i := 0; i < n; i++ {
				req = <-wc.registerSpace
				wc.spaces.Load(req)
			}
		case id := <-wc.updateSpace:
			// fmt.Println(color.Red, "UpdateSpace", color.Reset)
			if o, ok := wc.spaces.GetPresent(id); ok {
				log.Debug("Cal space update")
				if err := o.UpdateSpace(); err != nil {
					log.Error(errors.WithMessagef(err, "WorldController: run: failed to update space: %s", id))
				}
			}
		case id := <-wc.unregisterSpace:
			// fmt.Println(color.Red, "RegSpace", color.Reset)
			// log.Infof("unreg request: %s", id)
			if err := wc.spaces.Unload(id); err != nil {
				log.Warn(errors.WithMessagef(err, "WorldController: run: failed to unload space: %s", id))
			}
		case client := <-wc.registerUser:
			// log.Info("Reg User0")
			log.Info("Spawn flow: got reg request for", client.ID)
			go func() {
				if err := client.Register(wc); err != nil {
					log.Warn(errors.WithMessagef(err, "WorldController: run: failed to register client: %s", client.ID))
				}
			}()
			log.Info("Spawn flow: reg done for", client.ID)
			n := len(wc.registerUser)
			for i := 0; i < n; i++ {
				client := <-wc.registerUser
				go func() {
					if err := client.Register(wc); err != nil {
						log.Warn(
							errors.WithMessagef(
								err, "WorldController: run: failed to register client: %s", client.ID,
							),
						)
					}
				}()
			}
			log.Info("Reg UserDone")
		case client := <-wc.unregisterUser:
			go func() {
				if err := client.Unregister(wc); err != nil {
					log.Warn(
						errors.WithMessagef(
							err, "WorldController: run: failed to unregister client: %s", client.ID,
						),
					)
				}
			}()
			n := len(wc.unregisterUser)
			for i := 0; i < n; i++ {
				client := <-wc.unregisterUser
				go func() {
					if err := client.Unregister(wc); err != nil {
						log.Warn(
							errors.WithMessagef(
								err, "WorldController: run: failed to unregister client: %s", client.ID,
							),
						)
					}
				}()
			}
		}
		// logger.Logln(4, "Pass loop")
	}
}

// signal about removed ConnectedUsers

func (wc *WorldController) SafeSubscribe(topic string, qos byte, callback mqtt.MessageHandler) {
	wc.hub.mqtt.SafeSubscribe(topic, qos, callback)
}

func (wc *WorldController) Broadcast(websocketMessage *websocket.PreparedMessage) {
	wc.users.Broadcast(websocketMessage)
}

func (wc *WorldController) GetSpacePosition(id uuid.UUID) (cmath.Vec3, error) {
	if space, ok := wc.spaces.Get(id); ok {
		return space.position, nil
	}
	return cmath.MNan32Vec3(), errors.Errorf("failed to get space: %s", id)
}

func (wc *WorldController) GetSpacePresent(id uuid.UUID) bool {
	_, ok := wc.spaces.GetPresent(id)
	return ok
}

func (wc *WorldController) GetExtensionStorage() string {
	return wc.hub.cfg.Settings.ExtensionStorage
}

func (wc *WorldController) GetConfig() *config.World {
	return &wc.Config
}

func (wc *WorldController) GetStorage() *storage.Database {
	return wc.hub.DB
}

func (wc *WorldController) GetBuilder() *message.Builder {
	return wc.msgBuilder
}

func (wc *WorldController) GetID() uuid.UUID {
	return wc.ID
}

func (wc *WorldController) SetSpaceTitle(spaceId uuid.UUID, title string) error {
	space, ok := wc.spaces.Get(spaceId)
	if !ok {
		return errors.Errorf("failed to get space: %s", spaceId)
	}
	space.Name = title
	space.UpdateObjectDef()
	wc.Broadcast(space.GetObjectDef())

	return nil
}

// TODO: temporary, to move later
// func GetNameHash(name, url string) (string, error) {
//	nameToRender := strings.ReplaceAll(nameTemplate, "%NAME%", name)
//	r := strings.NewReader(nameToRender)
//	resp, err := http.Post(url, "`application/json", r)
//	if CheckError(err) {
//		return "", err
//	}
//	body, err := ioutil.ReadAll(resp.Body)
//	if CheckError(err) {
//		return "", err
//	}
//
//	v := make(map[string]interface{})
//	err = json.Unmarshal(body, &v)
//	if CheckError(err) {
//		return "", err
//	}
//	if hash, ok := v["hash"]; ok {
//		return hash.(string), nil
//	}
//
//	return "", errors.New("wrong")
// }
