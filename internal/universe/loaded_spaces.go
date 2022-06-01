package universe

import (
	"time"

	"github.com/momentum-xyz/controller/internal/cmath"
	"github.com/momentum-xyz/controller/internal/space"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type LoadedSpaces struct {
	spaces       *utils.SyncMap[uuid.UUID, *Space]
	spaceStorage space.Storage
	msgBuilder   *message.Builder
	world        *WorldController
	initialized  bool
	Time1        time.Duration
	Time2        time.Duration
	Time3        time.Duration
	Time4        time.Duration
	Time5        time.Duration
	Time6        time.Duration
	MetaTime     time.Duration
}

func newSpaces(wc *WorldController, spaceStorage space.Storage, msgBuilder *message.Builder) *LoadedSpaces {
	obj := new(LoadedSpaces)
	obj.spaces = utils.NewSyncMap[uuid.UUID, *Space]()
	obj.spaceStorage = spaceStorage
	obj.msgBuilder = msgBuilder
	obj.world = wc
	obj.initialized = false
	return obj
}

func (ls *LoadedSpaces) Num() int {
	ls.spaces.Mu.Lock()
	defer ls.spaces.Mu.Unlock()

	return len(ls.spaces.Data)
}

func (ls *LoadedSpaces) Add(obj *Space) {
	ls.spaces.Store(obj.id, obj)
}

func (ls *LoadedSpaces) Get(spaceId uuid.UUID) (*Space, bool) {
	return ls.spaces.Load(spaceId)
}

func (ls *LoadedSpaces) GetPresent(spaceId uuid.UUID) (*Space, bool) {
	return ls.spaces.Load(spaceId)
}

func (ls *LoadedSpaces) Delete(spaceId uuid.UUID) {
	ls.spaces.Remove(spaceId)
}

func (ls *LoadedSpaces) FindClosest(pos *cmath.Vec3) (uuid.UUID, cmath.Vec3) {
	Rmin := 1.0e10
	var (
		id uuid.UUID
		v  cmath.Vec3
	)

	ls.spaces.Mu.Lock()
	defer ls.spaces.Mu.Unlock()

	for id0, space := range ls.spaces.Data {
		if space.isDynamic {
			continue
		}
		r := cmath.Distance(&space.position, pos)
		if r < Rmin {
			Rmin = r
			id = id0
			v.X = pos.X - space.position.X
			v.Y = pos.Y - space.position.Y
			v.Z = pos.Z - space.position.Z
		}
	}
	return id, v
}

func (ls *LoadedSpaces) Unload(spaceId uuid.UUID) error {
	s, ok := ls.GetPresent(spaceId)
	if !ok {
		return nil
	}

	log.Info("unload request: %s\n", s.id)
	for k := range s.children {
		if err := ls.Unload(k); err != nil {
			// TODO: maybe we need to return error here?
			log.Error(errors.WithMessagef(err, "LoadedSpaces: Unload: failed to unload child: %s", k))
		}
	}
	s.DeInit()
	ls.Delete(s.id)

	msg := posbus.NewRemoveStaticObjectsMsg(1)
	msg.SetObject(0, s.id)
	s.world.Broadcast(msg.WebsocketMessage())
	log.Info("unloaded: ", s.id, ls.Num())

	return nil
}

func (ls *LoadedSpaces) LoadFromEntry(req *RegRequest, entry map[string]interface{}) *Space {
	if req.id == ls.world.ID {
		log.Infof("Starting load world for the world %s", req.id.String())
	}

	log.Infof("load request: %s", req.id)

	tm := time.Now()
	obj := newSpace(ls.spaceStorage, ls.msgBuilder)
	obj.theta = req.theta
	obj.id = req.id
	obj.world = ls.world
	ls.Time1 += time.Since(tm)
	// flag := true

	tm = time.Now()
	obj.SetPosition(req.pos)
	ls.Time2 += time.Since(tm)

	tm = time.Now()
	if err := obj.UpdateMetaFromMap(entry); err != nil {
		return nil
	}
	obj.initialized = true
	ls.Time3 += time.Since(tm)

	tm = time.Now()
	ls.Add(obj)
	ls.Time4 += time.Since(tm)

	if err := obj.Init(); err != nil {
		log.Error(errors.WithMessage(err, "LoadedSpaces: LoadFromEntry: failed to init object"))
	}
	if obj.id == ls.world.ID {
		log.Info("Structure for the world "+obj.id.String()+" is loaded", ls.MetaTime, ls.Time1, ls.Time2, ls.Time3)
	}

	return obj
}

func (ls *LoadedSpaces) Load(req *RegRequest) *Space {
	if req.id == ls.world.ID {
		log.Infof("Starting load world for the world %s", req.id.String())
	}

	log.Infof("load request: %s", req.id)

	tm1 := time.Now()
	tm := time.Now()
	obj := newSpace(ls.spaceStorage, ls.msgBuilder)
	obj.theta = req.theta
	obj.id = req.id
	obj.world = ls.world
	ls.Time1 += time.Since(tm)
	// flag := true

	tm = time.Now()
	obj.SetPosition(req.pos)
	ls.Time2 += time.Since(tm)

	tm = time.Now()
	if err := obj.UpdateMeta(); err != nil {
		return nil
	}
	obj.initialized = true
	ls.Time3 += time.Since(tm)

	tm = time.Now()
	ls.Add(obj)
	ls.Time4 += time.Since(tm)

	if err := obj.Init(); err != nil {
		log.Error(errors.WithMessage(err, "LoadedSpaces: Load: failed to init object"))
	}
	if obj.id == ls.world.ID {
		log.Infof(
			"Structure for the world %s is loaded (%d): %s, %s, %s, %s, %s\n", obj.id.String(), obj.world.spaces.Num(),
			time.Since(tm1),
			ls.MetaTime, ls.Time1,
			ls.Time2, ls.Time3,
		)
	}

	return obj
}

func (ls *LoadedSpaces) GetPos(id uuid.UUID) (cmath.Vec3, error) {
	space, ok := ls.GetPresent(id)
	if !ok {
		log.Warnf("LoadedSpaces: GetPos: space not present: %s", id)
		return cmath.DefaultPosition(), nil
	}
	return space.position, nil
}

func (ls *LoadedSpaces) Init() {
	log.Infof("Initializing world structure for: %s", ls.world.ID)
	req := &RegRequest{
		id:    ls.world.ID,
		pos:   cmath.Vec3{},
		theta: 0,
	}
	ls.Load(req)
	ls.initialized = true
}
