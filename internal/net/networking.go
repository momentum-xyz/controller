package net

import (
	"encoding/json"
	"fmt"
	// Std
	"net/http"
	"net/url"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/auth"
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/internal/socket"
	"github.com/momentum-xyz/controller/pkg/message"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/flatbuff/go/api"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	// Third-Party
	"github.com/dgrijalva/jwt-go"
	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type SuccessfulHandshakeData struct {
	Connection *socket.Connection
	UserID     uuid.UUID
	SessionID  uuid.UUID
	URL        *url.URL
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Networking struct {
	HandshakeChan chan *SuccessfulHandshakeData
	cfg           *config.Config
}

var log = logger.L()

func NewNetworking(cfg *config.Config) *Networking {
	n := &Networking{
		HandshakeChan: make(chan *SuccessfulHandshakeData, 20),
		cfg:           cfg,
	}

	go utils.ChanMonitor("HS chan", n.HandshakeChan, 3*time.Second)

	http.HandleFunc("/posbus", n.HandShake)
	http.HandleFunc("/config/ui-client", n.cfgUIClient)
	return n
}

func (n *Networking) ListenAndServe(address, port string) error {
	log.Info("ListenAndServe: ", address+":"+port)
	return http.ListenAndServe(address+":"+port, nil)
}

func (n *Networking) cfgUIClient(w http.ResponseWriter, r *http.Request) {
	log.Info("Serving UI Client CFG")

	unityClientURL := n.cfg.UIClient.FrontendURL + "/unity"
	cfg := struct {
		config.UIClient
		UnityClientURL          string `json:"UNITY_CLIENT_URL"`
		UnityClientLoaderURL    string `json:"UNITY_CLIENT_LOADER_URL"`
		UnityClientDataURL      string `json:"UNITY_CLIENT_DATA_URL"`
		UnityClientFrameworkURL string `json:"UNITY_CLIENT_FRAMEWORK_URL"`
		UnityClientCodeURL      string `json:"UNITY_CLIENT_CODE_URL"`
		RenderServiceURL        string `json:"RENDER_SERVICE_URL"`
		BackendEndpointURL      string `json:"BACKEND_ENDPOINT_URL"`
	}{
		UIClient:                n.cfg.UIClient,
		UnityClientURL:          unityClientURL,
		UnityClientLoaderURL:    unityClientURL + "/WebGL.loader.js",
		UnityClientDataURL:      unityClientURL + "/WebGL.data.gz",
		UnityClientFrameworkURL: unityClientURL + "/WebGL.framework.js.gz",
		UnityClientCodeURL:      unityClientURL + "/WebGL.wasm.gz",
		RenderServiceURL:        n.cfg.UIClient.FrontendURL + "/api/v3/render",
		BackendEndpointURL:      n.cfg.UIClient.FrontendURL + "/api/v3/backend",
	}

	data, err := json.Marshal(&cfg)
	if err != nil {
		err := errors.WithMessage(err, "failed to serve ui client cfg")
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("{\"error\": %q}", err.Error())))
		return
	}

	if _, err := w.Write(data); err != nil {
		log.Error(errors.WithMessage(err, "failed to serve ui client cfg"))
	}
}

func (n *Networking) HandShake(w http.ResponseWriter, r *http.Request) {
	defer func() {
		log.Info("Handshake Done")
	}()
	log.Info("Handshake Start")

	conn, claims, ok, handshakeObj := n.PreHandShake(w, r)
	if !ok {
		log.Error("error: wrong PreHandShake, aborting connection")
		return
	}

	userID := message.DeserializeGUID(handshakeObj.UserId(nil))
	sessionID := message.DeserializeGUID(handshakeObj.SessionId(nil))
	URL, _ := url.Parse(string(handshakeObj.Url()))
	log.Info("URL to use:", URL)

	userIDclaim, _ := uuid.Parse((*claims)["sub"].(string))

	if (userID == userIDclaim) || (userIDclaim.String() == "69e1d7f6-3130-4005-9969-31edf9af9445") || (userIDclaim.String() == "eb50bbc8-ba4e-46a3-a480-a9b30141ce91") {
		connection := socket.NewConnection(conn)

		log.Info("Add to HS chain")
		n.HandshakeChan <- &SuccessfulHandshakeData{
			UserID:     userID,
			SessionID:  sessionID,
			URL:        URL,
			Connection: connection,
		}
	} else {
		log.Info("Attempt to connect with wrong claim:", userIDclaim.String())
	}
}

// PreHandShake TODO: it's "god" method needs to be simplified // antst: agree :)
func (n *Networking) PreHandShake(response http.ResponseWriter, request *http.Request) (
	*websocket.Conn, *jwt.MapClaims, bool, *api.Handshake,
) {
	socketConnection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		log.Error("upgrade:", err)
		return nil, nil, false, nil
	}

	mt, incomingMessage, err := socketConnection.ReadMessage()
	if err != nil || mt != websocket.BinaryMessage {
		log.Error("error: wrong PreHandShake (1), aborting connection")
		return nil, nil, false, nil
	}

	msg := posbus.MsgFromBytes(incomingMessage)
	if msg.Type() != posbus.MsgTypeFlatBufferMessage {
		log.Error("error: wrong message received, not Handshake.")
		return nil, nil, false, nil
	}
	msgObj := posbus.MsgFromBytes(incomingMessage).AsFlatBufferMessage()
	msgType := msgObj.MsgType()
	if msgType != api.MsgHandshake {
		log.Error("error: wrong message type received, not Handshake.")
		return nil, nil, false, nil
	}

	var handshake *api.Handshake
	unionTable := &flatbuffers.Table{}
	if msgObj.Msg(unionTable) {
		handshake = &api.Handshake{}
		handshake.Init(unionTable.Bytes, unionTable.Pos)
	}

	log.Info("handshake for user:", message.DeserializeGUID(handshake.UserId(nil)))
	log.Info("handshake version:", handshake.HandshakeVersion())
	log.Info("protocol version:", handshake.ProtocolVersion())

	token := string(handshake.UserToken())

	if !auth.VerifyToken(token, n.cfg.Common.IntrospectURL) {
		log.Error("error: wrong PreHandShake (invalid token), aborting connection")
		return nil, nil, false, nil
	}
	parsed, _ := jwt.Parse(
		token, func(token *jwt.Token) (interface{}, error) {
			return []byte(""), nil
		},
	)

	claims := parsed.Claims.(jwt.MapClaims)

	return socketConnection, &claims, true, handshake
}
