package universe

import (
	"time"

	"github.com/momentum-xyz/controller/internal/posbus"
	"github.com/momentum-xyz/controller/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

type ConnectedUsers struct {
	users     map[uuid.UUID]*User
	goneUsers map[uuid.UUID]bool
	lock      deadlock.RWMutex
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
	obj.users = make(map[uuid.UUID]*User)
	obj.goneUsers = make(map[uuid.UUID]bool)
	obj.world = world
	obj.broadcast = make(chan *websocket.PreparedMessage, 1000)
	go utils.ChanMonitor("users.bcast", obj.broadcast, 3*time.Second)
	go obj.Run()
	return obj
}

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
	cu.lock.RLock()
	up, ok := cu.users[id]
	cu.lock.RUnlock()
	return up, ok
}

func (cu *ConnectedUsers) Add(u *User) {
	cu.lock.Lock()
	cu.users[u.ID] = u
	cu.lock.Unlock()
}

func (cu *ConnectedUsers) UserLeft(userId uuid.UUID) {
	cu.lock.Lock()
	cu.goneUsers[userId] = true
	cu.lock.Unlock()
	go cu.world.UpdateOnline()
}

func (cu *ConnectedUsers) PerformBroadcast(message *websocket.PreparedMessage) {
	cu.lock.RLock()
	for _, clnt := range cu.users {
		clnt.connection.Send(message)
	}
	cu.lock.RUnlock()
}
func (cu *ConnectedUsers) BroadcastDisconnectedUsers() {
	var goneUsersIds []uuid.UUID
	cu.lock.RLock()
	for id, isGone := range cu.goneUsers {
		if isGone {
			goneUsersIds = append(goneUsersIds, id)
		}
	}
	cu.lock.RUnlock()

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
		cu.goneUsers = make(map[uuid.UUID]bool)
	}
}

func (cu *ConnectedUsers) BroadcastPositions() {
	flag := false
	cu.lock.RLock()
	numClients := len(cu.users)
	msg := posbus.NewUserPositionsMsg(numClients)
	if numClients > 0 {
		flag = true
		i := 0
		cu.positionLock.Lock()
		for _, clnt := range cu.users {
			msg.SetPosition(i, clnt.posbuf)
			i++
		}
		cu.positionLock.Unlock()
	}
	cu.lock.RUnlock()
	if flag {
		cu.Broadcast(msg.WebsocketMessage())
	}
}

func (cu *ConnectedUsers) BroadcastUsersUpdate() {
	cu.BroadcastDisconnectedUsers()
	cu.BroadcastPositions()

}

func (cu *ConnectedUsers) Num() int {
	cu.lock.RLock()
	n := len(cu.users)
	cu.lock.RUnlock()
	return n
}

func (cu *ConnectedUsers) Delete(id uuid.UUID) {
	cu.lock.Lock()
	delete(cu.users, id)
	cu.lock.Unlock()
}

func (cu *ConnectedUsers) GetOnSpace(spaceId uuid.UUID) []*User {
	cu.lock.RLock()
	ul := make([]*User, 0)
	for _, u := range cu.users {
		if u.currentSpace.Load().(uuid.UUID) == spaceId {
			ul = append(ul, u)
		}
	}
	cu.lock.RUnlock()
	return ul
}
