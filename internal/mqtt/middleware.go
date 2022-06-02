package safemqtt

import (
	"github.com/pkg/errors"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type ErrMQTTMessageHandler func(client mqtt.Client, msg mqtt.Message) error

func LogMQTTMessageHandler(name string, handler ErrMQTTMessageHandler) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		if err := handler(client, msg); err != nil {
			log.Error(errors.WithMessagef(err, "MQTT: failed to handle: %s", name))
		}
	}
}
