package extensions

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/extension"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	// Third-Party
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"go.etcd.io/bbolt"
)

type TBlockID = uint32

type KusamaBlock struct {
	id            uuid.UUID
	pos           cmath.Vec3
	numExtrinsics int
	name          string
	author        string
}

type EventStruct struct {
	title      string
	start      time.Time
	image_hash string
}

var log = logger.L()

func NewKusama(controller extension.WorldController) extension.Extension {
	return &Kusama{
		blocks:                    nil,
		world:                     controller,
		TransactionCore:           uuid.UUID{},
		EraClock:                  uuid.UUID{},
		RewardsAccumulator:        uuid.UUID{},
		RelayChain:                uuid.UUID{},
		TransactionBlockSpaceType: uuid.UUID{},
		TransactionBlockAsset:     uuid.UUID{},
		Initialized:               false,
		blockMutex:                sync.Mutex{},
		bDB:                       nil,
		blockBucket:               nil,
		RelayChainPos:             cmath.Vec3{},
		ValidatorSpaceType:        uuid.UUID{},
		WorldLobby:                uuid.UUID{},
		EventsClock:               uuid.Nil,
	}
}

type Kusama struct {
	blocks                      map[TBlockID]KusamaBlock
	world                       extension.WorldController
	TransactionCore             uuid.UUID
	EraClock                    uuid.UUID
	RewardsAccumulator          uuid.UUID
	RelayChain                  uuid.UUID
	TransactionBlockSpaceType   uuid.UUID
	TransactionBlockAsset       uuid.UUID
	Initialized                 bool
	blockMutex                  sync.Mutex
	bDB                         *bbolt.DB
	blockBucket                 []byte
	RelayChainPos               cmath.Vec3
	ValidatorSpaceType          uuid.UUID
	BlockInfoUI                 uuid.UUID
	ValidatorAddressIdAttribute uuid.UUID
	bestBlock                   uint32
	finalizedBlock              uint32
	//
	WorldLobby  uuid.UUID
	EventsClock uuid.UUID
	hasTimeFix  uint32
	EraDuration time.Duration
	EraStart    time.Time
	//
	NextEvent EventStruct
}

type kusamaBlockCreatedEvent struct {
	Number    TBlockID `json:"number"`
	AuthorId  string   `json:"authorId"`
	Finalized bool     `json:"finalized"`
	// Extrinsics      []interface{} `json:"extrinsics"`
	ExtrinsicsCount int `json:"extrinsicsCount"`
}
type kusamaBlockFinalizedEvent struct {
	Number    TBlockID `json:"number"`
	AuthorId  string   `json:"authorId"`
	Finalized bool     `json:"finalized"`
}

type eraEvent struct {
	Name                string `json:"name"`
	ActiveEra           int    `json:"activeEra"`
	ActiveValidators    int    `json:"activeValidators"`
	CandidateValidators int    `json:"candidateValidators"`
	TotalStakeInEra     string `json:"totalStakeInEra"`
	LastEraReward       string `json:"lastEraReward"`
}

type sessionEvent struct {
	CurrentSessionIndex    int    `json:"currentSessionIndex"`
	CurrentEra             int    `json:"currentEra"`
	TotalRewardPointsInEra string `json:"totalRewardPointsInEra"`
	CurrentSlotInSession   int    `json:"currentSlotInSession"`
	SlotsPerSession        int    `json:"slotsPerSession"`
	CurrentSlotInEra       int    `json:"currentSlotInEra"`
	SlotsPerEra            int    `json:"slotsPerEra"`
	SessionsPerEra         int    `json:"sessionsPerEra"`
	ActiveEraStart         int64  `json:"activeEraStart"`
	SlotDuration           int    `json:"slotDuration"`
}

type rewardEvent struct {
	Era         uint32            `json:"era"`
	TotalPoints uint32            `json:"totalPoints"`
	Rewards     map[string]uint32 `json:"rewards"`
}

type slashEvent struct {
	AccountId string `json:"accountId"`
	Amount    uint32 `json:"amount"`
}

func (ksm *Kusama) Init() bool {
	// When Init is called, world.structure is not available yet!
	// anything which relies on presence of that - should be done in Run()
	var ok bool
	cfg := ksm.world.GetConfig()
	// os.Exit(2)
	if ksm.TransactionCore, ok = cfg.Spaces["transaction_core"]; !ok {
		log.Warn("No transaction_core in Kusama config")
		return false
	}

	if ksm.TransactionCore, ok = cfg.Spaces["transaction_core"]; !ok {
		log.Warn("No transaction_core in Kusama config")
		return false
	}
	if ksm.EraClock, ok = cfg.Spaces["era_clock"]; !ok {
		log.Info("No era_clock in Kusama config")
		return false
	}

	if ksm.RelayChain, ok = cfg.Spaces["blockchain"]; !ok {
		log.Warn("No blockchain in Kusama config")
		return false
	}
	if ksm.RewardsAccumulator, ok = cfg.Spaces["rewards_accumulator"]; !ok {
		log.Warn("No rewards_accumulator in Kusama config")
		return false
	}
	if ksm.TransactionBlockSpaceType, ok = cfg.SpaceTypes["transaction_block"]; !ok {
		log.Warn("No transaction_block in Kusama config")
		return false
	}

	if ksm.ValidatorSpaceType, ok = cfg.SpaceTypes["validator_node"]; !ok {
		log.Warn("No validator_node in Kusama config")
		return false
	}

	if ksm.WorldLobby, ok = cfg.Spaces["world_lobby"]; !ok {
		log.Warn("No world_lobby in Kusama config")
		return false
	}
	if ksm.EventsClock, ok = cfg.Spaces["events_clock"]; !ok {
		log.Warn("No events_clock in Kusama config")
		return false
	}

	storage := ksm.world.GetStorage()
	r, err := storage.QuerySingleByUUID("space_types", ksm.TransactionBlockSpaceType)
	if err != nil {
		log.Warnf("check error: %+v", err)
		return false
	}
	var s interface{}
	if s, ok = r["asset"]; !ok {
		return false
	}
	ksm.TransactionBlockAsset, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		log.Warnf("check error: %+v", err)
		return false
	}

	if s, ok = r["infoui_id"]; !ok {
		return false
	}
	ksm.BlockInfoUI, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		log.Warnf("check error: %+v", err)
		return false
	}

	r, err = storage.QuerySingleByField("attributes", "name", "kusama_validator_id")
	if err != nil {
		log.Warn("error: ", err)
		return false
	}
	if s, ok = r["id"]; !ok {
		return false
	}
	ksm.ValidatorAddressIdAttribute, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		log.Warnf("check error: %+v", err)
		return false
	}

	extStorage := ksm.world.GetExtensionStorage()
	file := extStorage + "/kusama_" + ksm.world.GetId().String() + ".db"
	ksm.bDB, err = bbolt.Open(file, 0666, nil)
	if err != nil {
		log.Error("Can not open bbolt storage file", file)
		return false
	}

	ksm.blockBucket = []byte("Blocks")
	ksm.RelayChainPos = cmath.Vec3{0, 0, 0}
	_ = ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists(ksm.blockBucket); err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			return nil
		},
	)
	ksm.blocks = make(map[TBlockID]KusamaBlock)
	ksm.bestBlock = 0
	ksm.finalizedBlock = 0
	ksm.Initialized = true

	return true
}

func (ksm *Kusama) StoreBlockInfo(id TBlockID, b *KusamaBlock) {
	bid := make([]byte, 4)
	binary.LittleEndian.PutUint32(bid, id)
	_ = ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.Put(bid, utils.BinId(b.id))
		},
	)
}

func (ksm *Kusama) DeleteBlockInfo(id TBlockID) {
	bid := make([]byte, 4)
	binary.LittleEndian.PutUint32(bid, id)
	_ = ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.Delete(bid)
		},
	)
}

func (ksm *Kusama) WriteBestBlock(id uint32) {
	if id > ksm.bestBlock {
		ksm.bestBlock = id
		_, _ = ksm.world.GetStorage().DB.Exec(
			`insert into stats(worldId, name, columnId, value) values(?,'BEST BLOCK',1,?) on duplicate key update  columnId=1,value=?;`,
			utils.BinId(ksm.world.GetId()), strconv.Itoa(int(id)), strconv.Itoa(int(id)),
		)
	}
}

func (ksm *Kusama) WriteFinalizedBlock(id uint32) {
	if id > ksm.finalizedBlock {
		ksm.finalizedBlock = id
		_, _ = ksm.world.GetStorage().DB.Exec(
			`insert into stats(worldId, name, columnId, value) values(?,'FINALIZED BLOCK',1,?) on duplicate key update  columnId=1,value=?;`,
			utils.BinId(ksm.world.GetId()), strconv.Itoa(int(id)), strconv.Itoa(int(id)),
		)
	}
}

func (ksm *Kusama) LoadBlocks() {
	ksm.blockMutex.Lock()
	defer ksm.blockMutex.Unlock()
	_ = ksm.bDB.View(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.ForEach(
				func(k, v []byte) error {
					id := binary.LittleEndian.Uint32(k)
					blockSpaceId, _ := uuid.FromBytes(v)
					block := KusamaBlock{id: blockSpaceId, name: "#:" + strconv.Itoa(int(id))}
					log.Debug("block loaded:", id, block)
					ksm.blocks[id] = block
					return nil
				},
			)
		},
	)
}

func (ksm *Kusama) GetValidatorSpaceIdByAddress(address string) (uuid.UUID, error) {
	rows, err := ksm.world.GetStorage().Query(
		`select spaceId from space_attributes where attributeId=? and value =?;`,
		utils.BinId(ksm.ValidatorAddressIdAttribute),
		address,
	)
	if err != nil {
		log.Warnf("check error: %+v", err)
		return uuid.Nil, err
	}
	defer rows.Close()
	if rows.Next() {
		bid := make([]byte, 16)
		_ = rows.Scan(&bid)
		id, err := uuid.FromBytes(bid)
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil

	}
	return uuid.Nil, err
}

func (ksm *Kusama) GetSpaceNameByID(id uuid.UUID) (string, error) {
	rows, err := ksm.world.GetStorage().Query(
		`select name from spaces where id=? ;`,
		utils.BinId(id),
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Error(err)
		}
		return name, nil

	}
	return "", err
}

func (ksm *Kusama) SpawnBlock(b *KusamaBlock) {
	log.Info("Spawn Block")
	defArray := make([]message.ObjectDefinition, 1)
	defArray[0] = message.ObjectDefinition{
		ObjectID:         b.id,
		ParentID:         ksm.RelayChain,
		AssetType:        ksm.TransactionBlockAsset,
		Name:             b.name,
		Position:         b.pos,
		TetheredToParent: false,
		Minimap:          0,
		InfoUI:           ksm.BlockInfoUI,
	}
	ksm.world.BroadcastObjects(defArray)

	time.Sleep(time.Millisecond * 300)
	// fmt.Println("author:", b.author)
	msg := posbus.NewTriggerTransitionalBridgingEffectsOnObjectMsg(1)
	// FIXME: real validator nodes
	// source := uuid.MustParse("593dfed9-7627-4ad7-bbf1-808585ab68e7")
	StringAttributes := make(map[string]string)
	StringAttributes["kusama_block_num_transactions"] = "TRANSACTIONS " + strconv.Itoa(b.numExtrinsics)

	source, err := ksm.GetValidatorSpaceIdByAddress(b.author)
	if err == nil {
		if !ksm.world.GetSpacePresent(source) {
			log.Errorf("space is missing: %s %s", source.String(), b.author)
		}

		msg.SetEffect(0, b.id, b.id, source, 10)
		ksm.world.Broadcast(msg.WebsocketMessage())

		validatorName, err1 := ksm.GetSpaceNameByID(source)
		if err1 == nil {
			StringAttributes["kusama_block_validator"] = validatorName
		}

	} else {
		log.Error("had problem with block!", b)
	}
	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectStrings(b.id, StringAttributes))

}

func (ksm *Kusama) UnSpawnBlock(id uuid.UUID) {
	log.Info("UnSpawn Block")
	effectMsg := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
	effectMsg.SetEffect(0, id, id, 11)
	ksm.world.Broadcast(effectMsg.WebsocketMessage())
	time.Sleep(time.Second * 3)
	msg := posbus.NewRemoveStaticObjectsMsg(1)
	msg.SetObject(0, id)
	ksm.world.Broadcast(msg.WebsocketMessage())
}

func (ksm *Kusama) BlockCreationCallback(client mqtt.Client, msg mqtt.Message) {
	var blockEvent kusamaBlockCreatedEvent
	err := json.Unmarshal(msg.Payload(), &blockEvent)
	if err != nil {
		log.Error("wrong block message")
		return
	}
	id := blockEvent.Number
	ksm.blockMutex.Lock()
	defer ksm.blockMutex.Unlock()
	NumTransactions := 0
	if bl, ok := ksm.blocks[id]; ok {
		// we have a block
		if bl.numExtrinsics < blockEvent.ExtrinsicsCount {
			NumTransactions = blockEvent.ExtrinsicsCount - bl.numExtrinsics
			bl.numExtrinsics = blockEvent.ExtrinsicsCount
			ksm.blocks[id] = bl
		}
	} else {
		// we don't have a block yet
		block := KusamaBlock{id: uuid.New(), pos: ksm.RelayChainPos, name: "#" + strconv.Itoa(int(id)), numExtrinsics: blockEvent.ExtrinsicsCount, author: blockEvent.AuthorId}

		ksm.StoreBlockInfo(id, &block)
		ksm.blocks[id] = block
		NumTransactions = block.numExtrinsics
		// spawn the block here
		ksm.WriteBestBlock(id)
		ksm.SpawnBlock(&block)
	}
	for i := 0; i < NumTransactions; i++ {
		time.Sleep(time.Millisecond * 100)
		effect := uint32(i%4 + 1)
		m := posbus.NewTriggerTransitionalBridgingEffectsOnObjectMsg(1)
		m.SetEffect(0, ksm.TransactionCore, ksm.TransactionCore, ksm.blocks[id].id, effect)
		ksm.world.Broadcast(m.WebsocketMessage())
	}
}

func (ksm *Kusama) BlockFinalizationCallback(client mqtt.Client, message mqtt.Message) {
	var blockEvent kusamaBlockFinalizedEvent
	err := json.Unmarshal(message.Payload(), &blockEvent)
	if err != nil {
		log.Errorf("wrong block message")
		return
	}
	id := blockEvent.Number
	ksm.WriteFinalizedBlock(id)
	ksm.blockMutex.Lock()
	defer ksm.blockMutex.Unlock()
	for blockID := range ksm.blocks {
		if (blockID) <= id {
			bid := ksm.blocks[blockID].id
			go ksm.UnSpawnBlock(bid)
			time.Sleep(time.Millisecond * 30)
			delete(ksm.blocks, blockID)
			ksm.DeleteBlockInfo(blockID)
		}
	}
}

func (ksm *Kusama) Run() {
	if !ksm.Initialized {
		return
	}
	ksm.LoadBlocks()
	ksm.world.SafeSubscribe("harvester/kusama/block-creation-event", 2, ksm.BlockCreationCallback)
	ksm.world.SafeSubscribe("harvester/kusama/block-finalized-event", 2, ksm.BlockFinalizationCallback)
	ksm.world.SafeSubscribe("harvester/kusama/slash-event", 2, ksm.SlashCallback)
	ksm.world.SafeSubscribe("harvester/kusama/reward-event", 2, ksm.RewardCallback)
	ksm.world.SafeSubscribe("harvester/kusama/era", 2, ksm.EraCallback)
	ksm.world.SafeSubscribe("harvester/kusama/session", 2, ksm.SessionCallback)
	ksm.world.SafeSubscribe("updates/events/changed", 2, ksm.SpaceChangedCallback)

	time.Sleep(time.Duration(1<<63 - 1))

	// handle clock
	ksm.EvClock()
}

func (ksm *Kusama) SortSpaces(s []uuid.UUID, t uuid.UUID) {
	// DONOT remove commented
	// if t == ksm.ValidatorSpaceType {
	//	fmt.Println("HERE!")
	//
	//	spaces := &ksm.world.structure.spaces
	//	sort.Slice(
	//		s, func(i, j int) bool { return (*spaces)[s[i]].attributes["kk"] < (*spaces)[s[j]].attributes["kk"] },
	//	)
	//	fmt.Println("Done!")
	//	os.Exit(1)
	//
	// } else {
	sort.Slice(s, func(i, j int) bool { return s[i].ClockSequence() < s[j].ClockSequence() })
	// }
}

func (ksm *Kusama) RewardCallback(client mqtt.Client, msg mqtt.Message) {
	var r rewardEvent
	err := json.Unmarshal(msg.Payload(), &r)
	if err != nil {
		log.Errorf("Wrong rewards message: %v", err)
		return
	}

	m := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
	m.SetEffect(0, ksm.RewardsAccumulator, ksm.RewardsAccumulator, 301)
	ksm.world.Broadcast(m.WebsocketMessage())

	vl := make([]uuid.UUID, 0)
	for s := range r.Rewards {
		dest, err := ksm.GetValidatorSpaceIdByAddress(s)
		if err == nil {
			vl = append(vl, dest)
		}
	}

	numValidators := len(vl)
	m2 := posbus.NewTriggerTransitionalBridgingEffectsOnObjectMsg(numValidators)
	for i := 0; i < numValidators; i++ {
		m2.SetEffect(i, ksm.RewardsAccumulator, ksm.RewardsAccumulator, vl[i], 302)
	}
	ksm.world.Broadcast(m2.WebsocketMessage())
}

func (ksm *Kusama) EraCallback(client mqtt.Client, msg mqtt.Message) {
	var r eraEvent
	err := json.Unmarshal(msg.Payload(), &r)
	if err != nil {
		log.Errorf("Wrong era message: %v", err)
		return
	}
}

func (ksm *Kusama) SessionCallback(client mqtt.Client, msg mqtt.Message) {
	var r sessionEvent
	err := json.Unmarshal(msg.Payload(), &r)
	if err != nil {
		log.Errorf("Wrong session message: %v", err)
		return
	}
	ksm.SetRewardsAccumulatorState(r.CurrentSlotInEra, r.SlotsPerEra)
	ksm.EraDuration = time.Duration(r.SlotDuration*r.SlotsPerEra) * 1000000
	ksm.EraStart = time.UnixMilli(r.ActiveEraStart)
	atomic.StoreUint32(&ksm.hasTimeFix, 1)
}

func (ksm *Kusama) SpaceChangedCallback(client mqtt.Client, msg mqtt.Message) {
	ksm.EvClock()
}

func (ksm *Kusama) EvClock() {
	// TODO: to change query events only in the current world and to process only first row
	q := `select sie.title, sie.start, sie.image_hash from space_integration_events sie
				where sie.start >= curdate()
				order by sie.start
				limit ?` // TODO: remove limit
	rows, err := ksm.world.GetStorage().Queryx(q, 3)
	if err != nil {
		log.Error(1, "error getting space integration events: ", err)
		return
	}
	defer rows.Close()
	var events []EventStruct
	for rows.Next() {
		event := EventStruct{}
		err := rows.Scan(&event.title, &event.start, &event.image_hash)
		if err != nil {
			log.Error(1, "could not scan event: ", err)
			continue
		}
		events = append(events, event)
	}

	if !reflect.DeepEqual(ksm.NextEvent, events[0]) {
		ksm.NextEvent = events[0]
		ff := strconv.FormatInt(int64(time.Until(ksm.NextEvent.start).Milliseconds()), 10)
		log.Warnf(
			"Updatimg timer state to %s (now %s) , diff: %s", ksm.NextEvent.start.String(), time.Now().String(), ff,
		)
		log.Warnf(
			"Updatimg timer state (UTC) to %s (now %s) , diff: %s", ksm.NextEvent.start.UTC().String(),
			time.Now().UTC().String(), ff,
		)
		ksm.world.SetSpaceTitle(ksm.EventsClock, ksm.NextEvent.title)
		ksm.SendEventClockUpdate()

	}

	// Change Event Clock name

	// descendantsQuery := `call GetSpaceDescendantsIDs(?,100)`
	// childrenSpaces, err := ksm.world.GetStorage().Query(descendantsQuery, utils.BinId(ksm.EventsClock))
	//
	// childrenSpaces = childrenSpaces // TODO: this ??
}

func (ksm *Kusama) SendEventClockUpdate() {

	textures := make(map[string]string)
	imgHash := ksm.NextEvent.image_hash
	log.Warnf("Clock Texture: %s", imgHash)
	if imgHash == "" {
		imgHash = "some_default_value"
	}
	textures["clock_texture"] = imgHash
	// textures["name_hash"] = "fc28df0fd2627765ce5c171cfdd5561b"
	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectTextures(ksm.EventsClock, textures))

	stringAttributes := make(map[string]string)
	stringAttributes["timer"] = strconv.FormatInt(int64(time.Until(ksm.NextEvent.start).Milliseconds()), 10)
	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectStrings(ksm.EventsClock, stringAttributes))

	log.Warnf("Sending timer state:%s", stringAttributes["timer"])
}

func (ksm *Kusama) TimeLeftInEra() time.Duration {
	for atomic.LoadUint32(&ksm.hasTimeFix) != 1 {
		time.Sleep(time.Second)
	}
	return ksm.EraDuration - time.Since(ksm.EraStart)
}

// func (ksm *Kusama) TimeLeftToEvent() time.Duration {
//	//for atomic.LoadUint32(&ksm.hasTimeFix) != 1 {
//	//	time.Sleep(time.Second)
//	//}
//	return time.Until(ksm.NextEvent)
// }

func (ksm *Kusama) SetRewardsAccumulatorState(i, n int) {
	attr := make(map[string]int32)
	attr["rewardsamount"] = int32(math.Round(float64(i) / float64(n) * 100.0))
	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectAttributes(ksm.RewardsAccumulator, attr))
}

func (ksm *Kusama) SlashCallback(client mqtt.Client, msg mqtt.Message) {
	var r slashEvent
	err := json.Unmarshal(msg.Payload(), &r)
	if err != nil {
		log.Errorf("Wrong Slash message: %v", err)
		return
	}

}
func (ksm *Kusama) InitSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}

func (ksm *Kusama) DeinitSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}

func (ksm *Kusama) InitUser(u extension.User) {
	// TODO implement me
	go func() {
		StringAttributes := make(map[string]string)
		StringAttributes["kusama_clock_era_time"] = strconv.FormatInt(int64(ksm.TimeLeftInEra().Milliseconds()), 10)

		u.Send(ksm.world.GetBuilder().SetObjectStrings(ksm.EraClock, StringAttributes))
	}()

	ksm.SendEventClockUpdate()

}

func (ksm *Kusama) DeinitUser(u extension.User) {
	// TODO implement me
	panic("implement me")
}

func (ksm *Kusama) RunUser(u extension.User) {
	// TODO implement me
	panic("implement me")
}

func (ksm *Kusama) RunSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}
