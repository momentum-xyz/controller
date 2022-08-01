package safemqtt

import (
	"github.com/pkg/errors"
	// Std
	"strconv"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/logger"

	// Third-Party
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/sasha-s/go-deadlock"
)

// Client is bridge between our app and MQTT
type Client interface {
	SafePublish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token
	SafeSubscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token
	SafeUnsubscribe(topics ...string) mqtt.Token
	IsConnected() bool
}

type mqttClient struct {
	mutex deadlock.Mutex
	mqtt  mqtt.Client
}

var (
	log                                  = logger.L().With("package", "mqtt")
	connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
		// TODO: use proper logger
		log.Info("Connected to MQTT broker")
	}
	connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		// TODO: use proper logger
		log.Infof("Connection to MQTT broker lost: %v\n", err)
		client.Connect()
	}
)

func InitMQTTClient(cfg *config.MQTT, id string) (Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + cfg.HOST + ":" + strconv.FormatUint(uint64(cfg.PORT), 10))
	opts.SetClientID("controller" + id)
	opts.SetUsername(cfg.USER)
	opts.SetPassword(cfg.PASSWORD)
	// opts.SetDefaultPublishHandler(messagePubHandler)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(2 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, errors.WithMessage(token.Error(), "failed to connect")
	}

	return &mqttClient{
		mutex: deadlock.Mutex{},
		mqtt:  client,
	}, nil
}

func (m *mqttClient) SafePublish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.mqtt.Publish(topic, qos, retained, payload)
}

func (m *mqttClient) SafeSubscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.mqtt.Subscribe(topic, qos, callback)
}

func (m *mqttClient) SafeUnsubscribe(topics ...string) mqtt.Token {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.mqtt.Unsubscribe(topics...)
}

func (m *mqttClient) IsConnected() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.mqtt.IsConnected()
}
