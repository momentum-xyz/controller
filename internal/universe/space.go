package universe

import (
	// STD
	"encoding/json"
	"strconv"
	"strings"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/message"
	"github.com/momentum-xyz/controller/internal/posbus"
	"github.com/momentum-xyz/controller/internal/position"
	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/internal/space"
	"github.com/momentum-xyz/controller/internal/spacetype"
	"github.com/momentum-xyz/controller/utils"

	// Third-Party
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	peopleOnlineStringKey = "people_online"
	vibesStringKey        = "vibes"
)

type Space struct {
	id       uuid.UUID
	parentId uuid.UUID
	Name     string
	// flatbufObjDef ObjectDefinition
	world    *WorldController
	position cmath.Vec3
	rotation cmath.Vec3
	tether   bool
	minimap  uint8
	// SqlData  map[string]interface{}
	children   map[uuid.UUID]bool
	theta      float64
	stype      *spacetype.TSpaceType
	pls        map[uuid.UUID]position.Algo
	textures   map[string]string
	attributes map[string]int32
	assetId    uuid.UUID
	InfoUI     uuid.UUID
	// addMsg   *websocket.PreparedMessage
	// uiTypeId uuid.UUID
	// userMappedAttributes map[string]int32
	visible int8
	// isDynamic indicates that this space has no DB entry, so it is managed local by world service,
	// and should not be removed if there is no such space in DB, only explicitly by world
	isDynamic        bool
	storage          space.Storage
	initialized      bool
	msgBuilder       *message.Builder
	stringAttributes map[string]string
}

func newSpace(spaceStorage space.Storage, msgBuilder *message.Builder) *Space {
	return &Space{
		position:         cmath.MNan32Vec3(),
		children:         make(map[uuid.UUID]bool),
		textures:         make(map[string]string),
		attributes:       make(map[string]int32),
		isDynamic:        false,
		storage:          spaceStorage,
		msgBuilder:       msgBuilder,
		stringAttributes: map[string]string{},
	}
}

func (s *Space) MQTTMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	const topicOffset = 51
	log.Debug("space mqqt topic:", msg.Topic())
	log.Debug("space mqqt offset topic:", msg.Topic()[topicOffset:])
	subtopics := strings.Split(msg.Topic()[topicOffset:], "/")

	switch subtopics[0] {
	case "text":
		s.SendToUsersOnSpace(
			posbus.NewSimpleNotificationMsg(
				posbus.DestinationReact, posbus.NotificationTextMessage, 0, string(msg.Payload()),
			).WebsocketMessage(),
		)
	case "relay":
		var module string
		if len(subtopics) > 1 {
			module = subtopics[1]
		}
		s.SendToUsersOnSpace(
			posbus.NewRelayToReactMsg(
				module, msg.Payload(),
			).WebsocketMessage(),
		)
	case "trigger-effect":
		s.MQTTEffectsHandler(msg.Payload())
	}
}

func (s *Space) MQTTEffectsHandler(msg []byte) {
	switch string(msg) {
	case "vibe":
		effect := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
		effect.SetEffect(0, s.world.EffectsEmitter, s.id, 1002)
		s.world.UpdateVibesBySpaceId(s.id)
		s.world.Broadcast(effect.WebsocketMessage())
	}
}

//func (s *Space) SendToListOfUsersOnSpace(msg *websocket.PreparedMessage, users []uuid.UUID) {
//	s.world.userMutex.RLock()
//	defer s.world.userMutex.RUnlock()
//	isWorld := s.id == s.world.ID
//	for _, ui := range users {
//		u, ok := s.world.users[ui]
//		if ok && (isWorld || u.currentSpace == s.id) {
//			u.connection.send <- msg
//		}
//	}
//}

func (s *Space) SendToUsersOnSpace(msg *websocket.PreparedMessage) {
	if s.id == s.world.ID {
		s.world.Broadcast(msg)
	} else {
		for _, u := range s.world.users.GetOnSpace(s.id) {
			u.connection.Send(msg)
		}
	}
}

func (s *Space) UpdateSpace() error {
	log.Debugf("update request: %s", s.id)
	err := s.UpdateMeta()
	if err != nil {
		return err
	}
	_ = s.UpdateChildren()

	return nil
}

func checkStringForChange(newval *string, current *string, flag *bool) {
	if *newval != *current {
		*current = *newval
		*flag = true
	}
}

func checkBoolForChange(newval *bool, current *bool, flag *bool) {
	if *newval != *current {
		*current = *newval
		*flag = true
	}
}

func checkUUIDForChange(newval *uuid.UUID, current *uuid.UUID, flag *bool) {
	if *newval != *current {
		*current = *newval
		*flag = true
	}
}

func checkUint8ForChange(newval *uint8, current *uint8, flag *bool) {
	if *newval != *current {
		*current = *newval
		*flag = true
	}
}

func (s *Space) UpdateMetaFromMap(entry map[string]interface{}) error {
	// Update space type
	var err error
	if entry["spaceTypeId"] == nil {
		log.Debug("E", entry)
	}
	s.stype, err = s.world.spaceTypes.Get(utils.DbToUuid(entry["spaceTypeId"]))
	if err != nil {
		log.Error(err)
		return err
	}
	s.pls = s.stype.Placements
	isdefchanged := false
	name := entry["name"].(string)
	checkStringForChange(&name, &s.Name, &isdefchanged)
	// fmt.Println("PR:", entry["parentId"])
	parentId, _ := uuid.FromBytes([]byte((entry["parentId"]).(string)))
	checkUUIDForChange(&parentId, &s.parentId, &isdefchanged)

	// INFO_UI_ID
	if s.stype.InfoUIId != uuid.Nil {
		s.InfoUI = s.stype.InfoUIId
		log.Debug("Info Id: %v", s.InfoUI)
	}

	if minimapEntry, ok := entry["minimap"]; ok && minimapEntry != nil {
		minimap := uint8(minimapEntry.(int64))
		checkUint8ForChange(&minimap, &s.minimap, &isdefchanged)
	} else {
		checkUint8ForChange(&s.stype.Minimap, &s.minimap, &isdefchanged)
	}

	assetId := uuid.Nil
	if entry["asset"] != nil {
		assetId, _ = uuid.FromBytes([]byte((entry["asset"]).(string)))
	} else if s.stype.AssetId != uuid.Nil {
		assetId = s.stype.AssetId
	}
	// logger.Logln(1, "Asset:", s.id, assetId)
	// fmt.Println("AT:", s.id, s.assetId)
	checkUUIDForChange(&assetId, &s.assetId, &isdefchanged)

	tether := true
	checkBoolForChange(&tether, &s.tether, &isdefchanged)

	textures := s.storage.LoadSpaceTileTextures(s.id)

	if namehash, ok := entry["name_hash"]; ok && namehash != nil {
		textures["name"] = namehash.(string)
	}

	updatedTextures := make(map[string]string)
	for k, v := range textures {
		if v1, ok := s.textures[k]; !ok || (v1 != v) {
			log.Debugf("changed: %s %s", k, v)
			s.textures[k] = v
			updatedTextures[k] = v
		}
	}

	if val, ok := entry["visible"]; ok && val != nil {
		s.visible = int8(val.(int64))
	} else {
		s.visible = s.stype.Visible
	}

	// ATTRIBUTES
	spaceAttributes, err := s.storage.QuerySpaceAttributesById(s.id)

	intAttributes := make(map[string]int32)
	intAttributes["private"] = int32(entry["secret"].(int64))

	stringAttributes := make(map[string]string)

	for i := range spaceAttributes {
		if !spaceAttributes[i].Value.Valid {
			intAttributes[spaceAttributes[i].Name] = int32(spaceAttributes[i].Flag)
		} else {
			stringAttributes[spaceAttributes[i].Name] = spaceAttributes[i].Value.String
		}
	}

	// Int Attributes
	updatedAttributes := make(map[string]int32)
	for k, v := range intAttributes {
		if v1, ok := s.attributes[k]; !ok || (v1 != v) {
			log.Debugf("changed: %s %d", k, v)
			s.attributes[k] = v
			updatedAttributes[k] = v
		}
	}

	// Set online users
	onlineUsers := s.GetOnlineUsers()
	stringAttributes[peopleOnlineStringKey] = strconv.Itoa(int(onlineUsers))

	// Set vibes
	vibes := s.CalculateVibes()
	stringAttributes[vibesStringKey] = strconv.Itoa(int(vibes))

	updatedStringAttributes := make(map[string]string)
	// String attributes
	for k, v := range stringAttributes {
		if v1, ok := s.stringAttributes[k]; !ok || (v1 != v) {
			log.Debugf("changed: %s %s", k, v)
			s.stringAttributes[k] = v
			updatedStringAttributes[k] = v
		}
	}

	if childPlace, ok := entry[spacetype.ChildPlacement]; ok && childPlace != nil {
		// log.Println("childPlace ", s.id, ": ", childPlace)
		var t3DPlacements spacetype.T3DPlacements
		jsonData := []byte(childPlace.(string))
		_ = json.Unmarshal(jsonData, &t3DPlacements)
		s.pls = make(map[uuid.UUID]position.Algo)
		for _, placement := range t3DPlacements {
			spacetype.FillPlacement(placement.(map[string]interface{}), &s.pls)
		}
	}
	// if s.pls != nil {
	//	for u, algo := range s.pls {
	//		if algo.Name() != "" {
	//			fmt.Println("ALGO:", s.id, algo.Name(), u, algo)
	//		}
	//
	//	}
	// }

	//if s.initialized {
	if isdefchanged {
		s.world.spawnNeedUpdate = true
		log.Debug("send addStaticOBject")
		defArray := make([]message.ObjectDefinition, 1)
		s.filObjDef(&defArray[0])
		s.world.Broadcast(s.msgBuilder.MsgAddStaticObjects(defArray))
	}

	if len(updatedTextures) > 0 {
		log.Debug("send setTexture")
		s.world.Broadcast(s.msgBuilder.SetObjectTextures(s.id, updatedTextures))
	}

	if len(updatedAttributes) > 0 {
		log.Debug("send setObjectAttributes")
		s.world.Broadcast(s.msgBuilder.SetObjectAttributes(s.id, updatedAttributes))
	}
	if len(updatedStringAttributes) > 0 {
		log.Debug("send setObjectStrings")
		s.world.Broadcast(s.msgBuilder.SetObjectStrings(s.id, updatedStringAttributes))
	}
	//}
	// hash := md5.Sum(sentry)
	// logger.Logln(1, "meta:", s.id, time.Since(tm))
	return nil
}

func (s *Space) UpdateMeta() error {
	log.Debugf("updateMeta request: %s", s.id)
	entry, err := s.storage.QuerySingleSpaceById(s.id)
	if err != nil {
		log.Error(err)
		return err
	}
	return s.UpdateMetaFromMap(entry)
}

func (s *Space) SetPosition(pos cmath.Vec3) {
	log.Debugf("setpos request: %s %v", s.id, pos)
	if s.position != pos {
		log.Debugf("setpos need update from %v", s.position)
		s.position = pos
		// s.world.broadcast
		// s, _ := json.Marshal(x.position)
		// logger.Logln(4, string(s))
		// token :=
		// fmt.Println("Z1", x.topic)
		// s.world.hub.mqtt.SafePublish(x.topic, 1, true, s)
		// token.Wait()
	}
}

func (s *Space) Init() {
	s.world.hub.mqtt.SafeSubscribe("space_control/"+s.id.String()+"/#", 1, s.MQTTMessageHandler)
	// log.Println(0, "*************************!!!")
	_ = s.UpdateChildren()

}

func (s *Space) DeInit() {
	s.world.hub.mqtt.SafeUnsubscribe("space_control/" + s.id.String())
}

func (s *Space) UpdatePosition(pos cmath.Vec3, theta float64, force bool) {
	// logger.Logln(0, "www")
	// logger.Logf(0, "request: %s\n", x.id)
	log.Debugf("udatepos request: %s %v\n", s.id, pos)
	if s.position != pos || force {
		log.Debugf("udatepos need update from: %v", s.position)
		s.position = pos
		s.theta = theta

		s.world.spawnNeedUpdate = true

		msg := posbus.NewSetStaticObjectPositionMsg()
		msg.SetPosition(s.id, pos)
		if s.initialized {
			s.world.Broadcast(msg.WebsocketMessage())
		}

		// Update children positions

		ChildMap := make(map[uuid.UUID][]uuid.UUID)

		for u := range s.pls {
			ChildMap[u] = make([]uuid.UUID, 0)
		}

		for k := range s.children {
			st := s.world.spaces.Get(k).stype.Id
			if _, ok := s.pls[st]; !ok {
				st = uuid.Nil
			}
			ChildMap[st] = append(ChildMap[st], k)
		}

		for u := range s.pls {
			lpm := ChildMap[u]
			s.world.MainExtension.SortSpaces(lpm, u)

			for i, k := range lpm {
				pos, theta := s.pls[u].CalcPos(s.theta, s.position, i, len(lpm))
				s.world.spaces.Get(k).UpdatePosition(pos, theta, force)
			}
		}
	}
}

func (s *Space) UpdateChildren() error {
	tm := time.Now()
	log.Debugf("updatechildren request: %s", s.id)
	// logger.Logln(1, "q1")
	// query := `SELECT id FROM spaces WHERE parentId = ?;`
	children, err := s.storage.SelectChildrenEntriesByParentId(utils.BinId(s.id))
	if err != nil {
		log.Warnf("error: %+v", err)
	}
	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("childrenQ:", s.id, time.Since(tm))
	}
	// fmt.Println(children)
	// os.Exit(2)
	// logger.Logln(1, "q2")
	cids := make(map[uuid.UUID]bool)

	fixedChildren := make(map[uuid.UUID]bool)
	fixedChildrenPos := make(map[uuid.UUID]cmath.Vec3)
	spaceTypes := make(map[uuid.UUID]uuid.UUID)
	for cid, m := range children {
		spaceTypes[cid] = utils.DbToUuid(m["spaceTypeId"])
		spaceVisible := m["visible"]
		stVisible := m["st_visible"]
		spacePosition := m["position"]
		if (spaceVisible != nil && spaceVisible.(int64) != 0) || (spaceVisible == nil && stVisible.(int64) != 0) {
			if spacePosition == nil {
				// logger.Logln(1, cid)
				cids[cid] = true
			} else {
				fixedChildren[cid] = true
				if !s.children[cid] {
					var vpos cmath.Vec3
					_ = json.Unmarshal([]byte(spacePosition.(string)), &vpos)
					fixedChildrenPos[cid] = vpos
				}
			}
		}
	}

	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("children1:", s.id, time.Since(tm))
	}

	for _, st := range spaceTypes {
		_, _ = s.world.spaceTypes.Get(st)
	}

	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("children1a:", s.id, time.Since(tm))
	}

	// logger.Logln(4, "F1:", cids)
	childIDs := make([]uuid.UUID, len(cids))
	i := 0
	for k := range cids {
		childIDs[i] = k
		i++
	}

	// logger.Logln(4, "F2:", childIDs)
	delCh := make(map[uuid.UUID]bool)
	for k := range s.children {
		if !cids[k] && !fixedChildren[k] {
			delCh[k] = true
		}
	}
	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("children3:", s.id, time.Since(tm))
	}

	// logger.Logln(1, "aa:", s.pls)

	ChildMap := make(map[uuid.UUID][]uuid.UUID)
	for u := range s.pls {
		ChildMap[u] = make([]uuid.UUID, 0)
	}
	// fmt.Println("q1")
	// TODO: to refactor with use of single query above, which also will include spaceTypeId
	for _, k := range childIDs {
		st := spaceTypes[k]
		// fmt.Println("F10:", st)
		if _, ok := s.pls[st]; !ok {
			st = uuid.Nil
		}
		ChildMap[st] = append(ChildMap[st], k)
	}
	// fmt.Println("F10:", ChildMap)
	changed := false

	// logger.Logln(5, delCh)
	// logger.Logln(5, s.pls)
	if len(delCh) != 0 {
		changed = true
		log.Debugf("children changed, del: %d", len(delCh))

		for k := range delCh {
			s.world.spaces.Unload(k)
		}
	}

	// logger.Logln(1, "ee:", s.pls)
	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("children5:", s.id, time.Since(tm))
	}

	for u := range s.pls {
		// logger.Logln(1, "ee2")
		lpm := ChildMap[u]
		addCh := make(map[uuid.UUID]int)
		movCh := make(map[uuid.UUID]int)

		s.world.MainExtension.SortSpaces(lpm, u)

		for i, k := range lpm {
			// logger.Logln(1, k)
			if !s.children[k] {
				// logger.Logln(1, "qw2")
				addCh[k] = i
				changed = true
			} else {
				// logger.Logln(1, "qw3")
				movCh[k] = i
			}
		}
		if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
			log.Debug("children6:", s.id, time.Since(tm))
		}

		// logger.Logln(1, len(movCh), len(addCh))
		if len(addCh) == 0 && len(movCh) == 0 {
			log.Debug("children unchanged for child type:", u)
		} else {
			log.Debugf(
				"children changed for child type:%s, add:%d, mv:%d, tot:%d\n", u, len(addCh), len(movCh), len(lpm),
			)

			N := len(lpm)

			for k, v := range movCh {
				log.Debugf("Moving %s", k)
				pos, theta := s.pls[u].CalcPos(s.theta, s.position, v, N)
				s.world.spaces.Get(k).UpdatePosition(pos, theta, false)
			}

			if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
				log.Debug("children7:", s.id, time.Since(tm))
			}

			for k, v := range addCh {
				pos, theta := s.pls[u].CalcPos(s.theta, s.position, v, N)
				// fmt.Println("Dyn:", k, pos)
				s.world.spaces.LoadFromEntry(
					&RegRequest{
						id:    k,
						pos:   pos,
						theta: theta,
					}, children[k],
				)
				// s.world.registerUser <- RegRequest{id: k, pos: pos, theta: theta}
			}
		}
	}

	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("children8:", s.id, time.Since(tm))
	}

	if len(fixedChildrenPos) > 0 {
		changed = true
		for k, v := range fixedChildrenPos {
			// fmt.Println("Fixed:", k, v)
			s.world.spaces.LoadFromEntry(
				&RegRequest{
					id:    k,
					pos:   v,
					theta: 0,
				}, children[k],
			)
			// s.world.registerUser <- RegRequest{id: k, pos: pos, theta: theta}
		}

	}
	// if s.id.String() == "d83670c7-a120-47a4-892d-f9ec75604f74" {
	// 	os.Exit(0)
	// }
	if changed {
		s.world.spawnNeedUpdate = true
		// logger.Logln(4, "New children:", cids)
		s.children = cids
		for u := range fixedChildren {
			s.children[u] = true
		}
	}
	if s.id.String() == "0317d64a-1317-409b-b0b6-9f1621b9e01e" {
		log.Debug("childrenE:", s.id, time.Since(tm))
	}

	return nil
}

/*func AsSha256(o interface{}) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%v", o)))

	return fmt.Sprintf("%x", h.Sum(nil))
}*/

/*func AsMD5(o interface{}) [16]byte {
	return md5.Sum([]byte(fmt.Sprintf("%v", o)))
}*/

// type ObjectMetadata struct {
// 	objectID, parentID, assetId, assetSubtype uuid.UUID
// 	name                                        string
// 	position                                    math.Vec3
// 	textures                                    []TextureMetadata
// 	attributes                                  []AttributeMetadata
// 	tetheredToParent                            bool
// }

// s.flatbufObjDef.textures = make([]TextureMetadata, len(s.meta.textures))
// i := 0
// for k, v := range s.meta.textures {
// 	s.flatbufObjDef.textures[i].label = k
// 	s.flatbufObjDef.textures[i].data = v
// }

// s.flatbufObjDef.attributes = make([]AttributeMetadata, len(s.meta.attributes))
// i = 0
// for k, v := range s.meta.attributes {
// 	s.flatbufObjDef.attributes[i].label = k
// 	s.flatbufObjDef.attributes[i].attribute = v
// }

func (s *Space) PushObjDef(metaArray []message.ObjectDefinition, i *int) {
	s.filObjDef(&(metaArray[*i]))
	*i++

	for id := range s.children {
		s.world.spaces.Get(id).PushObjDef(metaArray, i)
	}
}

func (s *Space) filObjDef(metaArray *message.ObjectDefinition) {
	metaArray.ObjectID = s.id
	metaArray.ParentID = s.parentId
	metaArray.AssetType = s.assetId
	metaArray.Name = s.Name
	metaArray.Position = s.position
	metaArray.TetheredToParent = true
	metaArray.Minimap = s.minimap
	metaArray.InfoUI = s.InfoUI
}

func (s *Space) RecursiveSendAllTextures(connection *socket.Connection) {
	connection.Send(s.msgBuilder.SetObjectTextures(s.id, s.textures))
	for id := range s.children {
		s.world.spaces.Get(id).RecursiveSendAllTextures(connection)
	}
}

func (s *Space) RecursiveSendAllAttributes(connection *socket.Connection) {
	connection.Send(s.msgBuilder.SetObjectAttributes(s.id, s.attributes))
	for id := range s.children {
		s.world.spaces.Get(id).RecursiveSendAllAttributes(connection)
	}
}

func (s *Space) RecursiveSendAllStrings(connection *socket.Connection) {
	connection.Send(s.msgBuilder.SetObjectStrings(s.id, s.stringAttributes))
	for id := range s.children {
		s.world.spaces.Get(id).RecursiveSendAllStrings(connection)
	}
}

func (s *Space) CalculateVibes() int64 {
	q := `select count(*) from vibes where spaceId=?`
	rows, err := s.world.GetStorage().Queryx(q, utils.BinId(s.id))
	//noinspection GoUnhandledErrorResult
	defer rows.Close()
	if err != nil {
		log.Errorf("could not get vibes for spaceId: %v", s.id)
	}
	vibesCount := int64(0)
	rows.Next()
	err = rows.Scan(&vibesCount)
	if err != nil {
		log.Error("could not scan number of vibes: ", err)
	}
	log.Debugf("Vibes for spaceId %v: %d", s.id, vibesCount)
	return vibesCount
}

func (s *Space) GetOnlineUsers() int64 {
	q := `SELECT count(*) FROM online_users WHERE spaceId = ?`
	rows, err := s.world.GetStorage().Queryx(q, utils.BinId(s.id))
	//noinspection GoUnhandledErrorResult
	defer rows.Close()
	if err != nil {
		log.Errorf("could not get online users for spaceId: %v", s.id)
	}
	onlineUsers := int64(0)
	rows.Next()
	err = rows.Scan(&onlineUsers)
	if err != nil {
		log.Error("could not scan number of online users: ", err)
	}
	log.Debugf("Online users for spaceId %v: %d", s.id, onlineUsers)
	return onlineUsers
}
