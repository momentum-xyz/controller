package universe

import (
	"github.com/pkg/errors"
	"time"

	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

type ConnectedUsers struct {
	users     *utils.SyncMap[uuid.UUID, *User]
	goneUsers *utils.SyncMap[uuid.UUID, struct{}]
	world     *WorldController
	// Inbound messages from the users.
	broadcast chan *websocket.PreparedMessage

	// position lock is going to be used in kind of inverse way
	// Rlock() is used for writes of individual positions
	// Lock() is used for reading of those when assembling position update message
	positionLock deadlock.RWMutex
}

func NewUsers(world *WorldController) *ConnectedUsers {
	obj := new(ConnectedUsers)
	obj.users = utils.NewSyncMap[uuid.UUID, *User]()
	obj.goneUsers = utils.NewSyncMap[uuid.UUID, struct{}]()
	obj.world = world
	obj.broadcast = make(chan *websocket.PreparedMessage, 1000)
	go utils.ChanMonitor("users.bcast", obj.broadcast, 3*time.Second)
	go obj.Run()
	return obj
}

// TODO: this method never ends
func (cu *ConnectedUsers) Run() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case message := <-cu.broadcast:
			// v := reflect.ValueOf(cu.broadcast)
			// fmt.Println(color.Red, "Bcast", wc.users.Num(), v.Len(), color.Reset)
			go cu.PerformBroadcast(message)
			// logger.Logln(4, "BcastE")
		case <-ticker.C:
			// fmt.Println(color.Red, "Ticker", color.Reset)
			go cu.BroadcastUsersUpdate()
		}
	}
}

func (cu *ConnectedUsers) Broadcast(message *websocket.PreparedMessage) {
	cu.broadcast <- message
}

func (cu *ConnectedUsers) Get(id uuid.UUID) (*User, bool) {
	return cu.users.Load(id)
}

func (cu *ConnectedUsers) Add(u *User) {
	cu.users.Store(u.ID, u)
}

func (cu *ConnectedUsers) UserLeft(userId uuid.UUID) {
	cu.goneUsers.Store(userId, struct{}{})
	go func() {
		if err := cu.world.UpdateOnline(); err != nil {
			log.Error(errors.WithMessage(err, "ConnectedUsers: UserLeft: failed to update online"))
		}
	}()
}

func (cu *ConnectedUsers) PerformBroadcast(message *websocket.PreparedMessage) {
	cu.users.Mu.RLock()
	defer cu.users.Mu.RUnlock()

	for _, clnt := range cu.users.Data {
		clnt.connection.Send(message)
	}
}

func (cu *ConnectedUsers) BroadcastDisconnectedUsers() {
	cu.goneUsers.Mu.RLock()
	goneUsersIds := make([]uuid.UUID, 0, len(cu.goneUsers.Data))
	for id := range cu.goneUsers.Data {
		goneUsersIds = append(goneUsersIds, id)
	}
	cu.goneUsers.Mu.RUnlock()

	ng := len(goneUsersIds)
	if ng > 0 {
		cu.positionLock.Lock()
		msg := posbus.NewGoneUsersMsg(ng)
		for i, id := range goneUsersIds {
			start := posbus.MsgArrTypeSize + i*posbus.MsgUUIDTypeSize
			copy(msg.Msg()[start:start+posbus.MsgUUIDTypeSize], utils.BinId(id))
		}
		cu.positionLock.Unlock()
		cu.Broadcast(msg.WebsocketMessage())
		cu.goneUsers.Purge()
	}
}

func (cu *ConnectedUsers) BroadcastPositions() {
	flag := false
	cu.users.Mu.RLock()
	numClients := len(cu.users.Data)
	msg := posbus.NewUserPositionsMsg(numClients)
	if numClients > 0 {
		flag = true
		i := 0
		cu.positionLock.Lock()
		for _, clnt := range cu.users.Data {
			msg.SetPosition(i, clnt.posbuf)
			i++
		}
		cu.positionLock.Unlock()
	}
	cu.users.Mu.RUnlock()
	if flag {
		cu.Broadcast(msg.WebsocketMessage())
	}
}

func (cu *ConnectedUsers) BroadcastUsersUpdate() {
	cu.BroadcastDisconnectedUsers()
	cu.BroadcastPositions()
}

func (cu *ConnectedUsers) Num() int {
	cu.users.Mu.RLock()
	defer cu.users.Mu.RUnlock()

	return len(cu.users.Data)
}

func (cu *ConnectedUsers) Delete(id uuid.UUID) {
	cu.users.Remove(id)
}

func (cu *ConnectedUsers) GetOnSpace(spaceId uuid.UUID) []*User {
	cu.users.Mu.RLock()
	defer cu.users.Mu.RUnlock()

	ul := make([]*User, 0)
	for _, u := range cu.users.Data {
		if utils.GetFromAny(u.currentSpace.Load(), uuid.Nil) == spaceId {
			ul = append(ul, u)
		}
	}
	return ul
}
