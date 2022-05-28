package extensions

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
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
	"github.com/momentum-xyz/controller/internal/mqtt"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	// Third-Party
	"github.com/pkg/errors"
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

const (
	writeBestBlockQuery               = `INSERT INTO stats(worldId, name, columnId, value) VALUES(?,'BEST BLOCK',1,?) ON DUPLICATE KEY UPDATE columnId=1,value=?;`
	writeFinalizedBlockQuery          = `INSERT INTO stats(worldId, name, columnId, value) VALUES(?,'FINALIZED BLOCK',1,?) ON DUPLICATE KEY UPDATE columnId=1,value=?;`
	getValidatorSpaceIDByAddressQuery = `SELECT spaceId FROM space_attributes WHERE attributeId=? AND value =?;`
	getSpaceNameByIDQuery             = `SELECT name FROM spaces WHERE id=?;`
	evClockQuery                      = `SELECT sie.title, sie.start, sie.image_hash FROM space_integration_events sie
											WHERE sie.start >= NOW()
											ORDER BY sie.start
											LIMIT ?`  // TODO: remove limit
)

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

func (ksm *Kusama) Init() error {
	// When Init is called, world.structure is not available yet!
	// anything which relies on presence of that - should be done in Run()
	var ok bool
	cfg := ksm.world.GetConfig()
	// os.Exit(2)
	if ksm.TransactionCore, ok = cfg.Spaces["transaction_core"]; !ok {
		return errors.New("No transaction_core in Kusama config")
	}

	if ksm.TransactionCore, ok = cfg.Spaces["transaction_core"]; !ok {
		return errors.New("No transaction_core in Kusama config")
	}

	if ksm.EraClock, ok = cfg.Spaces["era_clock"]; !ok {
		return errors.New("No era_clock in Kusama config")
	}

	if ksm.RelayChain, ok = cfg.Spaces["blockchain"]; !ok {
		return errors.New("No blockchain in Kusama config")
	}

	if ksm.RewardsAccumulator, ok = cfg.Spaces["rewards_accumulator"]; !ok {
		return errors.New("No rewards_accumulator in Kusama config")
	}

	if ksm.TransactionBlockSpaceType, ok = cfg.SpaceTypes["transaction_block"]; !ok {
		return errors.New("No transaction_block in Kusama config")
	}

	if ksm.ValidatorSpaceType, ok = cfg.SpaceTypes["validator_node"]; !ok {
		return errors.New("No validator_node in Kusama config")
	}

	if ksm.WorldLobby, ok = cfg.Spaces["world_lobby"]; !ok {
		return errors.New("No world_lobby in Kusama config")
	}

	if ksm.EventsClock, ok = cfg.Spaces["events_clock"]; !ok {
		return errors.New("No events_clock in Kusama config")
	}

	storage := ksm.world.GetStorage()
	r, err := storage.QuerySingleByUUID("space_types", ksm.TransactionBlockSpaceType)
	if err != nil {
		return errors.WithMessage(err, "failed to get transaction block space type")
	}
	var s interface{}
	if s, ok = r["asset"]; !ok {
		return errors.New("failed to get asset")
	}
	ksm.TransactionBlockAsset, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		return errors.WithMessage(err, "failed to get transaction block asset")
	}

	if s, ok = r["infoui_id"]; !ok {
		return errors.New("failed to get info ui id")
	}
	ksm.BlockInfoUI, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		return errors.WithMessage(err, "failed to get block info ui")
	}

	r, err = storage.QuerySingleByField("attributes", "name", "kusama_validator_id")
	if err != nil {
		return errors.WithMessage(err, "failed to get kusama validator id")
	}
	if s, ok = r["id"]; !ok {
		return errors.New("failed to get kusama validator id attribute")
	}
	ksm.ValidatorAddressIdAttribute, err = uuid.FromBytes([]byte(s.(string)))
	if err != nil {
		return errors.WithMessage(err, "failed to get validator address id attribute")
	}

	extStorage := ksm.world.GetExtensionStorage()
	file := filepath.Join(extStorage, fmt.Sprintf("kusama_%s.db", ksm.world.GetId().String()))
	ksm.bDB, err = bbolt.Open(file, 0666, nil)
	if err != nil {
		return errors.WithMessage(err, "failed to open bbolt storage file")
	}

	ksm.blockBucket = []byte("Blocks")
	ksm.RelayChainPos = cmath.Vec3{0, 0, 0}
	if err := ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists(ksm.blockBucket); err != nil {
				return errors.Errorf("failed to create bucket: %s", err)
			}
			return nil
		},
	); err != nil {
		return errors.WithMessage(err, "failed to update bbolt")
	}
	ksm.blocks = make(map[TBlockID]KusamaBlock)
	ksm.bestBlock = 0
	ksm.finalizedBlock = 0
	ksm.Initialized = true

	return nil
}

func (ksm *Kusama) StoreBlockInfo(id TBlockID, b *KusamaBlock) error {
	bid := make([]byte, 4)
	binary.LittleEndian.PutUint32(bid, id)
	return ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.Put(bid, utils.BinId(b.id))
		},
	)
}

func (ksm *Kusama) DeleteBlockInfo(id TBlockID) error {
	bid := make([]byte, 4)
	binary.LittleEndian.PutUint32(bid, id)
	return ksm.bDB.Update(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.Delete(bid)
		},
	)
}

func (ksm *Kusama) WriteBestBlock(id uint32) error {
	if id > ksm.bestBlock {
		ksm.bestBlock = id
		_, err := ksm.world.GetStorage().DB.Exec(writeBestBlockQuery,
			utils.BinId(ksm.world.GetId()), strconv.Itoa(int(id)), strconv.Itoa(int(id)),
		)
		return err
	}
	return nil
}

func (ksm *Kusama) WriteFinalizedBlock(id uint32) error {
	if id > ksm.finalizedBlock {
		ksm.finalizedBlock = id
		_, err := ksm.world.GetStorage().DB.Exec(
			writeFinalizedBlockQuery,
			utils.BinId(ksm.world.GetId()), strconv.Itoa(int(id)), strconv.Itoa(int(id)),
		)
		return err
	}
	return nil
}

func (ksm *Kusama) LoadBlocks() error {
	ksm.blockMutex.Lock()
	defer ksm.blockMutex.Unlock()

	return ksm.bDB.View(
		func(tx *bbolt.Tx) error {
			bb := tx.Bucket(ksm.blockBucket)
			return bb.ForEach(
				func(k, v []byte) error {
					id := binary.LittleEndian.Uint32(k)
					blockSpaceId, err := uuid.FromBytes(v)
					if err != nil {
						return errors.WithMessage(err, "failed to parse block space id")
					}
					block := KusamaBlock{id: blockSpaceId, name: "#:" + strconv.Itoa(int(id))}
					log.Debug("block loaded:", id, block)
					ksm.blocks[id] = block
					return nil
				},
			)
		},
	)
}

func (ksm *Kusama) GetValidatorSpaceIDByAddress(address string) (uuid.UUID, error) {
	rows, err := ksm.world.GetStorage().Query(
		getValidatorSpaceIDByAddressQuery,
		utils.BinId(ksm.ValidatorAddressIdAttribute),
		address,
	)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to get from db")
	}
	defer rows.Close()

	if rows.Next() {
		bid := make([]byte, 16)
		if err := rows.Scan(&bid); err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to scan rows")
		}
		id, err := uuid.FromBytes(bid)
		if err != nil {
			return uuid.Nil, errors.WithMessage(err, "failed to parse id")
		}
		return id, nil

	}
	return uuid.Nil, nil
}

func (ksm *Kusama) GetSpaceNameByID(id uuid.UUID) (string, error) {
	rows, err := ksm.world.GetStorage().Query(getSpaceNameByIDQuery, utils.BinId(id))
	if err != nil {
		return "", errors.WithMessage(err, "failed to get from db")
	}
	defer rows.Close()

	if rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", errors.WithMessage(err, "failed to scan rows")
		}
		return name, nil
	}
	return "", nil
}

func (ksm *Kusama) SpawnBlock(b *KusamaBlock) error {
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
	StringAttributes := map[string]string{
		"kusama_block_num_transactions": "TRANSACTIONS " + strconv.Itoa(b.numExtrinsics),
	}

	source, err := ksm.GetValidatorSpaceIDByAddress(b.author)
	if err != nil {
		return errors.WithMessage(err, "failed to get validator space id by address")
	}

	if !ksm.world.GetSpacePresent(source) {
		log.Error("Kusama: SpawnBlock: space is missing: %s %s", source.String(), b.author)
	}

	msg.SetEffect(0, b.id, b.id, source, 10)
	ksm.world.Broadcast(msg.WebsocketMessage())

	var validatorName string
	validatorName, err = ksm.GetSpaceNameByID(source)
	if err == nil {
		StringAttributes["kusama_block_validator"] = validatorName
	}

	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectStrings(b.id, StringAttributes))

	return nil
}

func (ksm *Kusama) UnSpawnBlock(id uuid.UUID) error {
	log.Info("UnSpawn Block")
	effectMsg := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
	effectMsg.SetEffect(0, id, id, 11)
	ksm.world.Broadcast(effectMsg.WebsocketMessage())
	time.Sleep(time.Second * 3)
	msg := posbus.NewRemoveStaticObjectsMsg(1)
	msg.SetObject(0, id)
	ksm.world.Broadcast(msg.WebsocketMessage())
	return nil
}

func (ksm *Kusama) BlockCreationCallback(client mqtt.Client, msg mqtt.Message) error {
	var blockEvent kusamaBlockCreatedEvent
	if err := json.Unmarshal(msg.Payload(), &blockEvent); err != nil {
		return errors.WithMessage(err, "failed to unmarshal kusama block created event")
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
		block := KusamaBlock{
			id:            uuid.New(),
			pos:           ksm.RelayChainPos,
			name:          "#" + strconv.Itoa(int(id)),
			numExtrinsics: blockEvent.ExtrinsicsCount,
			author:        blockEvent.AuthorId,
		}

		if err := ksm.StoreBlockInfo(id, &block); err != nil {
			return errors.WithMessage(err, "failed to store block info")
		}
		ksm.blocks[id] = block
		NumTransactions = block.numExtrinsics
		// spawn the block here
		if err := ksm.WriteBestBlock(id); err != nil {
			return errors.WithMessage(err, "failed to write best block")
		}
		if err := ksm.SpawnBlock(&block); err != nil {
			return errors.WithMessage(err, "failed to spawn block")
		}
	}

	for i := 0; i < NumTransactions; i++ {
		time.Sleep(time.Millisecond * 100)
		effect := uint32(i%4 + 1)
		m := posbus.NewTriggerTransitionalBridgingEffectsOnObjectMsg(1)
		m.SetEffect(0, ksm.TransactionCore, ksm.TransactionCore, ksm.blocks[id].id, effect)
		ksm.world.Broadcast(m.WebsocketMessage())
	}

	return nil
}

func (ksm *Kusama) BlockFinalizationCallback(client mqtt.Client, message mqtt.Message) error {
	var blockEvent kusamaBlockFinalizedEvent
	if err := json.Unmarshal(message.Payload(), &blockEvent); err != nil {
		return errors.WithMessage(err, "failed to unmarshal kusama block finalized event")
	}

	id := blockEvent.Number
	if err := ksm.WriteFinalizedBlock(id); err != nil {
		return errors.WithMessage(err, "failed to write finalized block")
	}

	ksm.blockMutex.Lock()
	defer ksm.blockMutex.Unlock()

	for blockID := range ksm.blocks {
		if blockID <= id {
			bid := ksm.blocks[blockID].id
			go ksm.UnSpawnBlock(bid)
			time.Sleep(time.Millisecond * 30)
			delete(ksm.blocks, blockID)
			if err := ksm.DeleteBlockInfo(blockID); err != nil {
				return errors.WithMessage(err, "failed to delete block info")
			}
		}
	}

	return nil
}

func (ksm *Kusama) Run() error {
	if !ksm.Initialized {
		return errors.New("not initialized")
	}

	if err := ksm.LoadBlocks(); err != nil {
		return errors.WithMessage(err, "failed to load blocks")
	}

	ksm.world.SafeSubscribe("harvester/kusama/block-creation-event", 2, safemqtt.LogMQTTMessageHandler("block creation", ksm.BlockCreationCallback))
	ksm.world.SafeSubscribe("harvester/kusama/block-finalized-event", 2, safemqtt.LogMQTTMessageHandler("block finalized", ksm.BlockFinalizationCallback))
	ksm.world.SafeSubscribe("harvester/kusama/slash-event", 2, safemqtt.LogMQTTMessageHandler("slash", ksm.SlashCallback))
	ksm.world.SafeSubscribe("harvester/kusama/reward-event", 2, safemqtt.LogMQTTMessageHandler("reward", ksm.RewardCallback))
	ksm.world.SafeSubscribe("harvester/kusama/era", 2, safemqtt.LogMQTTMessageHandler("era", ksm.EraCallback))
	ksm.world.SafeSubscribe("harvester/kusama/session", 2, safemqtt.LogMQTTMessageHandler("session", ksm.SessionCallback))
	ksm.world.SafeSubscribe("updates/events/changed", 2, safemqtt.LogMQTTMessageHandler("events changed", ksm.SpaceChangedCallback))

	time.Sleep(time.Duration(1<<63 - 1))

	// handle clock
	if err := ksm.EvClock(); err != nil {
		return errors.WithMessage(err, "failed to run ev clock")
	}
	go ksm.EraClockTimer()

	return nil
}

func (ksm *Kusama) EraClockTimer() {
	for {
		time.Sleep(time.Second * 30)
		ksm.BroadCastEraTimer()
	}
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

func (ksm *Kusama) RewardCallback(client mqtt.Client, msg mqtt.Message) error {
	var r rewardEvent
	if err := json.Unmarshal(msg.Payload(), &r); err != nil {
		return errors.WithMessage(err, "failed to unmarshal reward event")
	}

	m := posbus.NewTriggerTransitionalEffectsOnObjectMsg(1)
	m.SetEffect(0, ksm.RewardsAccumulator, ksm.RewardsAccumulator, 301)
	ksm.world.Broadcast(m.WebsocketMessage())

	vl := make([]uuid.UUID, 0)
	for s := range r.Rewards {
		dest, err := ksm.GetValidatorSpaceIDByAddress(s)
		if err != nil {
			log.Warnf("Kusama: RewardCallback: failed to get validator space id by address: %s", s)
		} else {
			vl = append(vl, dest)
		}
	}

	numValidators := len(vl)
	m2 := posbus.NewTriggerTransitionalBridgingEffectsOnObjectMsg(numValidators)
	for i := 0; i < numValidators; i++ {
		m2.SetEffect(i, ksm.RewardsAccumulator, ksm.RewardsAccumulator, vl[i], 302)
	}
	ksm.world.Broadcast(m2.WebsocketMessage())

	return nil
}

func (ksm *Kusama) EraCallback(client mqtt.Client, msg mqtt.Message) error {
	var r eraEvent
	if err := json.Unmarshal(msg.Payload(), &r); err != nil {
		return errors.WithMessage(err, "failed to parse era event")
	}
	return nil
}

func (ksm *Kusama) SessionCallback(client mqtt.Client, msg mqtt.Message) error {
	var r sessionEvent
	if err := json.Unmarshal(msg.Payload(), &r); err != nil {
		return errors.WithMessage(err, "failed to unmarshal session event")
	}

	ksm.SetRewardsAccumulatorState(r.CurrentSlotInEra, r.SlotsPerEra)
	ksm.EraDuration = time.Duration(r.SlotDuration*r.SlotsPerEra) * 1000000
	newEraStart := time.UnixMilli(r.ActiveEraStart)
	if newEraStart != ksm.EraStart {
		ksm.EraStart = newEraStart
		atomic.StoreUint32(&ksm.hasTimeFix, 1)
		ksm.BroadCastEraTimer()
	}

	return nil
}
func (ksm *Kusama) BroadCastEraTimer() {
	stringAttributes := map[string]string{
		"kusama_clock_era_time": strconv.FormatInt(int64(ksm.TimeLeftInEra().Milliseconds()), 10),
	}

	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectStrings(ksm.EraClock, stringAttributes))
}

func (ksm *Kusama) SpaceChangedCallback(client mqtt.Client, msg mqtt.Message) error {
	return ksm.EvClock()
}

func (ksm *Kusama) EvClock() error {
	// TODO: to change query events only in the current world and to process only first row
	rows, err := ksm.world.GetStorage().Queryx(evClockQuery, 3)
	if err != nil {
		return errors.WithMessage(err, "failed to get space integration events")
	}
	defer rows.Close()

	var events []EventStruct
	for rows.Next() {
		event := EventStruct{}
		if err := rows.Scan(&event.title, &event.start, &event.image_hash); err != nil {
			log.Error(errors.WithMessage(err, "Kusama: EvClock: failed to scan event"))
			continue
		}
		events = append(events, event)
	}

	if !reflect.DeepEqual(ksm.NextEvent, events[0]) {
		ksm.NextEvent = events[0]
		ff := strconv.FormatInt(time.Until(ksm.NextEvent.start).Milliseconds(), 10)
		log.Warnf(
			"EvClock: updatimg timer state to %s (now %s) , diff: %s", ksm.NextEvent.start.String(), time.Now().String(), ff,
		)
		log.Warnf(
			"EvClock: updatimg timer state (UTC) to %s (now %s) , diff: %s", ksm.NextEvent.start.UTC().String(),
			time.Now().UTC().String(), ff,
		)
		ksm.world.SetSpaceTitle(ksm.EventsClock, ksm.NextEvent.title)
		ksm.SendEventClockUpdate()
	}

	return nil

	// Change Event Clock name

	// descendantsQuery := `call GetSpaceDescendantsIDs(?,100)`
	// childrenSpaces, err := ksm.world.GetStorage().Query(descendantsQuery, utils.BinId(ksm.EventsClock))
	//
	// childrenSpaces = childrenSpaces // TODO: this ??
}

func (ksm *Kusama) SendEventClockUpdate() {
	imgHash := ksm.NextEvent.image_hash
	log.Warnf("Kusama: SendEventClockUpdate: clock texture: %s", imgHash)
	if imgHash == "" {
		imgHash = "some_default_value"
	}

	textures := map[string]string{
		"clock_texture": imgHash,
	}
	// textures["name_hash"] = "fc28df0fd2627765ce5c171cfdd5561b"
	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectTextures(ksm.EventsClock, textures))

	stringAttributes := map[string]string{
		"timer": strconv.FormatInt(int64(time.Until(ksm.NextEvent.start).Milliseconds()), 10),
	}

	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectStrings(ksm.EventsClock, stringAttributes))

	log.Warnf("Sending timer state:%s", stringAttributes["timer"])
}

func (ksm *Kusama) TimeLeftInEra() time.Duration {
	for atomic.LoadUint32(&ksm.hasTimeFix) != 1 {
		time.Sleep(time.Second)
	}
	log.Warnf(
		"ERA: %v %v %v %v %v", ksm.EraStart.UTC(), ksm.EraStart.UTC(), time.Since(ksm.EraStart.UTC()).Minutes(),
		time.Since(ksm.EraStart).Minutes(), ksm.EraDuration,
	)
	return ksm.EraDuration - time.Since(ksm.EraStart.UTC()) - 100*time.Second
}

// func (ksm *Kusama) TimeLeftToEvent() time.Duration {
//	//for atomic.LoadUint32(&ksm.hasTimeFix) != 1 {
//	//	time.Sleep(time.Second)
//	//}
//	return time.Until(ksm.NextEvent)
// }

func (ksm *Kusama) SetRewardsAccumulatorState(i, n int) {
	attr := map[string]int32{
		"rewardsamount": int32(math.Round(float64(i) / float64(n) * 100.0)),
	}

	ksm.world.Broadcast(ksm.world.GetBuilder().SetObjectAttributes(ksm.RewardsAccumulator, attr))
}

func (ksm *Kusama) SlashCallback(client mqtt.Client, msg mqtt.Message) error {
	var r slashEvent
	if err := json.Unmarshal(msg.Payload(), &r); err != nil {
		return errors.WithMessage(err, "failed to unmarshal slash event")
	}
	return nil
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
		stringAttributes := map[string]string{
			"kusama_clock_era_time": strconv.FormatInt(int64(ksm.TimeLeftInEra().Milliseconds()), 10),
		}

		u.Send(ksm.world.GetBuilder().SetObjectStrings(ksm.EraClock, stringAttributes))
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
