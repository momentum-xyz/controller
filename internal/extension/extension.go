package extension

import (
	// Momentum
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/storage"
	"github.com/momentum-xyz/controller/pkg/cmath"
	"github.com/momentum-xyz/controller/pkg/message"

	// Third-party
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type WorldController interface {
	GetConfig() *config.World
	GetBuilder() *message.Builder
	GetStorage() *storage.Database
	GetId() uuid.UUID
	GetExtensionStorage() string
	GetSpacePosition(id uuid.UUID) (cmath.Vec3, error)
	GetSpacePresent(id uuid.UUID) bool
	BroadcastObjects(array []message.ObjectDefinition)
	Broadcast(websocketMessage *websocket.PreparedMessage)
	SafeSubscribe(topic string, qos byte, callback mqtt.MessageHandler)
	SetSpaceTitle(clock uuid.UUID, title string) error
}

type Space interface {
	// TODO
}

type User interface {
	// TODO
	Send(m *websocket.PreparedMessage)
}

type Extension interface {
	Init() error

	InitSpace(s Space)
	DeinitSpace(s Space)

	InitUser(u User)
	DeinitUser(u User)
	RunUser(u User)

	RunSpace(s Space)
	Run() error

	SortSpaces(s []uuid.UUID, t uuid.UUID)
}
