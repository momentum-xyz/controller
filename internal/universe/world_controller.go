// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package universe

import (
	// STD
	"encoding/json"
	_ "net/http/pprof"
	"strconv"
	"sync/atomic"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/extensions"
	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/extension"
	"github.com/momentum-xyz/controller/internal/spacetype"
	"github.com/momentum-xyz/controller/internal/storage"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"

	// Third-party
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
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

// Hub maintains the set of active users and broadcasts messages to the users

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

	// activity chan string

	// spawn stuff
	spawnMsg        atomic.Value
	spawnNeedUpdate utils.TAtomBool

	AdditionalExtensions map[string]extension.Extension
	MainExtension        extension.Extension
	EffectsEmitter       uuid.UUID
	Config               config.World
	msgBuilder           *message.Builder
}

func newWorldController(worldID uuid.UUID, hub *ControllerHub, msgBuilder *message.Builder) (*WorldController, error) {
	wName, err := hub.WorldStorage.GetName(worldID)
	if err != nil {
		return nil, err
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
	controller.spawnNeedUpdate.Set(true)
	controller.users = NewUsers(&controller)
	controller.spaces = newSpaces(&controller, hub.SpaceStorage, msgBuilder)

	go utils.ChanMonitor("world.registerUser", controller.registerUser, 3*time.Second)
	go utils.ChanMonitor("world.unregisterUser", controller.unregisterUser, 3*time.Second)
	go utils.ChanMonitor("world.registerSpace", controller.registerSpace, 3*time.Second)
	go utils.ChanMonitor("world.unregisterSpace", controller.unregisterSpace, 3*time.Second)
	go utils.ChanMonitor("world.updateSpace", controller.updateSpace, 3*time.Second)

	if err := controller.SanitizeOnlineUsers(); err != nil {
		return nil, err
	}

	if err := controller.UpdateMeta(); err != nil {
		return nil, err
	}

	controller.LoadExtensions()

	controller.spaces.Init()

	return &controller, nil
}

type WowMetadata struct {
	UserId  []string `json:"UserId"`
	WowType string   `json:"type"` // +1 -1
	Count   int      `json:"count"`
}

type StageModeSetMetadata struct {
	StageModeStatus string   `json:"stageModeStatus"`
	Users           []string `json:"ConnectedUsers"`
}

func (wc *WorldController) SanitizeOnlineUsers() error {
	log.Info("Sanitizing online users:", wc.ID)
	if err := wc.hub.WorldStorage.CleanOnlineUsers(wc.ID); err != nil {
		return err
	}
	return wc.hub.WorldStorage.CleanDynamicMembership(wc.ID)

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

func (wc *WorldController) LoadExtensions() {
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
	wc.MainExtension.Init()
}

func (wc *WorldController) UpdateMeta() error {
	err := func() error {
		query := `SELECT name FROM spaces WHERE id = ?;`
		res, err := wc.hub.DB.Query(query, utils.BinId(wc.ID))
		if err != nil {
			log.Error(err)
			return err
		}
		defer res.Close()
		if res.Next() {
			var name string
			err = res.Scan(&name)
			if err != nil {
				log.Error(err)
				return err
			}
			log.Debug("W Meta:", name)
			wc.meta.Name = name
		}
		return nil
	}()

	if err != nil {
		log.Error(err)
		return err
	}

	query := `SELECT AVAController,SkyboxController,Decorations,LODs,config FROM world_definition WHERE id = ?;`
	res, err := wc.hub.DB.Query(query, utils.BinId(wc.ID))

	if err != nil {
		log.Error(err)
		return err
	}
	defer res.Close()
	if res.Next() {
		var AVAData, SkyboxData, DecorationsData, LODsData, ConfigData []byte
		// fmt.Println(res)
		err = res.Scan(&AVAData, &SkyboxData, &DecorationsData, &LODsData, &ConfigData)
		if err != nil {
			log.Error(err)
			return err
		}

		wc.meta.AvatarController, err = uuid.FromBytes(AVAData)
		if err != nil {
			wc.meta.AvatarController = uuid.Nil
		}

		wc.meta.SkyboxController, err = uuid.FromBytes(SkyboxData)
		if err != nil {
			wc.meta.SkyboxController = uuid.Nil
		}

		err = json.Unmarshal(LODsData, &wc.meta.LODs)
		if err != nil {
			wc.meta.LODs = []uint32{1000, 4000, 6400}
		}

		err = json.Unmarshal(DecorationsData, &wc.meta.Decorations)
		if err != nil {
			wc.meta.Decorations = make([]message.DecorationMetadata, 0)
		}
		// fmt.Println(string(ConfigData))
		err = json.Unmarshal(ConfigData, &wc.Config)
		if r, ok := wc.Config.Spaces["effects_emitter"]; ok {
			wc.EffectsEmitter = r
		} else {
			log.Error("Effects emitter is not defined for world", wc.ID)
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

func (wc *WorldController) UpdateOnline() {
	p := influxdb2.NewPoint(
		"online_users",
		map[string]string{"world": wc.influxtag},
		map[string]interface{}{"count": wc.users.Num()},
		time.Now(),
	)
	_ = wc.hub.WriteInfluxPoint(p)
}

func (wc *WorldController) UpdateOnlineBySpaceId(spaceId uuid.UUID) int64 {
	if s, ok := wc.spaces.GetPresent(spaceId); ok {
		onlineUsers := s.GetOnlineUsers()
		wc.Broadcast(
			wc.msgBuilder.SetObjectStrings(
				spaceId, map[string]string{
					peopleOnlineStringKey: strconv.Itoa(int(onlineUsers)),
				},
			),
		)

		return onlineUsers
	}
	// not necessary an error
	log.Info("Trying to update online user on non-present space ", spaceId.String())
	return 0
}

func (wc *WorldController) UpdateVibesBySpaceId(spaceId uuid.UUID) int64 {
	vibesCount := wc.spaces.Get(spaceId).CalculateVibes()
	wc.Broadcast(
		wc.msgBuilder.SetObjectStrings(
			spaceId, map[string]string{
				vibesStringKey: strconv.Itoa(int(vibesCount)),
			},
		),
	)

	return vibesCount
}

func (wc *WorldController) UserOnlineAction(id uuid.UUID) {
	wc.InsertOnline(id)
	wc.InsertWorldDynamicMembership(id)
}

func (wc *WorldController) InsertOnline(id uuid.UUID) {
	wc.hub.DB.InsertOnline(id, wc.ID)
	go wc.UpdateOnline()
}

func (wc *WorldController) InsertWorldDynamicMembership(id uuid.UUID) {
	querybase := `INSERT INTO user_spaces_dynamic (spaceId,UserId) VALUES (?,?) ON DUPLICATE KEY UPDATE spaceId=?;`

	_, err := wc.hub.DB.Exec(querybase, utils.BinId(wc.ID), utils.BinId(id), utils.BinId(wc.ID))

	if err != nil {
		log.Warnf("error: %+v", err)
	}
	go wc.UpdateOnline()
}

func (wc *WorldController) PositionRelativeToAbsolute(spaceID uuid.UUID, v cmath.Vec3) cmath.Vec3 {
	v2, err := wc.spaces.GetPos(spaceID)
	if err != nil {
		log.Warnf("error: %+v", err)
		return cmath.Vec3{}
	}

	v.Plus(v2)
	return v
}

func (wc *WorldController) AddUserToWorld(u *User) {
	log.Info("(HHHHH)")
	wc.users.Add(u)
	log.Info("User added to the world")

	if wc.spawnNeedUpdate.Get() {
		if wc.spaces.Num() > 0 {
			log.Info("***********Update")
			defArray := make([]message.ObjectDefinition, wc.spaces.Num())
			i := 0
			wc.spaces.Get(wc.ID).PushObjDef(defArray, &i)
			wc.spawnMsg.Store(wc.msgBuilder.MsgAddStaticObjects(defArray))
			wc.spawnNeedUpdate.Set(false)
		}
	}
	u.connection.SendDirectly(wc.spawnMsg.Load().(*websocket.PreparedMessage))
	// fmt.Println(len(wc.spaces))
	wc.spaces.Get(wc.ID).RecursiveSendAllAttributes(u.connection)
	wc.spaces.Get(wc.ID).RecursiveSendAllStrings(u.connection)
	wc.spaces.Get(wc.ID).RecursiveSendAllTextures(u.connection)
	wc.MainExtension.InitUser(u)
}

func (wc *WorldController) run() {
	log.Info("Starting to serve world: ", wc.ID)
	if wc.MainExtension != nil {
		go wc.MainExtension.Run()
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
				err := o.UpdateSpace()
				if err != nil {
					log.Errorf("%s update error: %+v", o.id.String(), err)
				}
			}
		case id := <-wc.unregisterSpace:
			// fmt.Println(color.Red, "RegSpace", color.Reset)
			// log.Infof("unreg request: %s", id)
			wc.spaces.Unload(id)

		case client := <-wc.registerUser:
			// log.Info("Reg User0")

			log.Info("Spawn flow: got reg request for", client.ID)
			go client.Register(wc)
			log.Info("Spawn flow: reg done for", client.ID)
			n := len(wc.registerUser)
			for i := 0; i < n; i++ {
				client := <-wc.registerUser
				go client.Register(wc)
			}

			log.Info("Reg UserDone0")
		case client := <-wc.unregisterUser:
			client.Unregister(wc)
			n := len(wc.unregisterUser)
			for i := 0; i < n; i++ {
				client := <-wc.unregisterUser
				client.Unregister(wc)
			}
		}
		// logger.Logln(4, "Pass loop")
	}
}

// signal about removed ConnectedUsers

func (wc *WorldController) SafeSubscribe(topic string, qos byte, callback func(client mqtt.Client, msg mqtt.Message)) {
	wc.hub.mqtt.SafeSubscribe(topic, qos, callback)
}

func (wc *WorldController) Broadcast(websocketMessage *websocket.PreparedMessage) {
	wc.users.Broadcast(websocketMessage)
}

func (wc *WorldController) BroadcastObjects(array []message.ObjectDefinition) {
	wc.users.Broadcast(wc.msgBuilder.MsgAddStaticObjects(array))
}

func (wc *WorldController) GetSpacePosition(id uuid.UUID) cmath.Vec3 {
	return wc.spaces.Get(id).position
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

func (wc *WorldController) GetId() uuid.UUID {
	return wc.ID
}

func (wc *WorldController) SetSpaceTitle(spaceId uuid.UUID, title string) {
	space := wc.spaces.Get(spaceId)
	space.Name = title
	defArray := make([]message.ObjectDefinition, 1)
	space.filObjDef(&defArray[0])
	wc.BroadcastObjects(defArray)
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

// Make sure that WorldController is extension.WorldController - DO NOT REMOVE
var _ extension.WorldController = (*WorldController)(nil)
