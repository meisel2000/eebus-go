package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	"github.com/enbility/eebus-go/usecases/eg/lpc"
	"github.com/enbility/eebus-go/usecases/eg/lpp"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/ship-go/cert"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
	"github.com/gorilla/websocket"
)

var remoteSki string

type WebsocketClient struct {
	websocket *websocket.Conn
	mutex     sync.Mutex
}

func (websocketClient *WebsocketClient) sendMessage(msg interface{}) error {
	if websocketClient.websocket == nil {
		return errors.New("no frontend connected")
	}

	websocketClient.mutex.Lock()
	defer websocketClient.mutex.Unlock()

	err := websocketClient.websocket.WriteJSON(msg)
	if err != nil {
		log.Println(err)
	}
	return err
}

func (websocketClient *WebsocketClient) sendNotification(messageType int) error {
	answer := Message{
		Type: messageType}

	return websocketClient.sendMessage(answer)
}

func (websocketClient *WebsocketClient) sendText(messageType int, text string) error {
	answer := Message{
		Type: messageType,
		Text: text}

	return websocketClient.sendMessage(answer)
}

func (websocketClient *WebsocketClient) sendValue(messageType int, useCase string, value float64) error {
	answer := Message{
		Type:    messageType,
		Value:   value,
		UseCase: useCase}

	return websocketClient.sendMessage(answer)
}

func (websocketClient *WebsocketClient) sendLimit(messageType int, useCase string, limit ucapi.LoadLimit) error {
	answer := Message{
		Type:    messageType,
		Limit:   limit,
		UseCase: useCase}

	return websocketClient.sendMessage(answer)
}

func (websocketClient *WebsocketClient) sendEntityList(messageType int, entities map[spineapi.EntityRemoteInterface][]string) error {
	list := []EntityDescription{}

	for ed, ucs := range entities {
		list = append(list, EntityDescription{
			Name:     ed.Address().String(),
			SKI:      ed.Device().Ski(),
			UseCases: ucs})
	}

	answer := Message{
		Type:       messageType,
		EntityList: list}

	return websocketClient.sendMessage(answer)
}

var frontend WebsocketClient

type failsafeLimits struct {
	Value    float64
	Duration time.Duration
}

type controlbox struct {
	myService *service.Service

	uclpc ucapi.EgLPCInterface
	uclpp ucapi.EgLPPInterface

	isConnected bool

	remoteEntities map[spineapi.EntityRemoteInterface][]string

	consumptionLimits         ucapi.LoadLimit
	productionLimits          ucapi.LoadLimit
	consumptionFailsafeLimits failsafeLimits
	productionFailsafeLimits  failsafeLimits
}

func (h *controlbox) run() {
	var err error
	var certificate tls.Certificate

	if len(os.Args) == 5 {
		remoteSki = os.Args[2]

		certificate, err = tls.LoadX509KeyPair(os.Args[3], os.Args[4])
		if err != nil {
			usage()
			log.Fatal(err)
		}
	} else {
		certificate, err = cert.CreateCertificate("Demo", "Demo", "DE", "Demo-Unit-01")
		if err != nil {
			log.Fatal(err)
		}

		pemdata := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certificate.Certificate[0],
		})
		fmt.Println(string(pemdata))

		b, err := x509.MarshalECPrivateKey(certificate.PrivateKey.(*ecdsa.PrivateKey))
		if err != nil {
			log.Fatal(err)
		}
		pemdata = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
		fmt.Println(string(pemdata))
	}

	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		usage()
		log.Fatal(err)
	}

	configuration, err := api.NewConfiguration(
		"Demo", "Demo", "ControlBox", "123456789",
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeGridConnectionHub},
		model.DeviceTypeTypeElectricitySupplySystem,
		[]model.EntityTypeType{model.EntityTypeTypeGridGuard},
		port, certificate, time.Second*60)
	if err != nil {
		log.Fatal(err)
	}
	configuration.SetAlternateIdentifier("Demo-ControlBox-123456789")

	h.myService = service.NewService(configuration, h)
	h.myService.SetLogging(h)

	if err = h.myService.Setup(); err != nil {
		fmt.Println(err)
		return
	}

	h.consumptionLimits = ucapi.LoadLimit{
		IsActive: false,
		Value:    4200,
		Duration: 2 * time.Hour}

	h.productionLimits = ucapi.LoadLimit{
		IsActive: false,
		Value:    5000,
		Duration: 1 * time.Hour}

	h.consumptionFailsafeLimits = failsafeLimits{
		Value:    4200,
		Duration: 2 * time.Hour}

	h.productionFailsafeLimits = failsafeLimits{
		Value:    5000,
		Duration: 1 * time.Hour}

	localEntity := h.myService.LocalDevice().EntityForType(model.EntityTypeTypeGridGuard)
	h.uclpc = lpc.NewLPC(localEntity, h.OnLPCEvent)
	h.myService.AddUseCase(h.uclpc)

	//h.uclpp = lpp.NewLPP(localEntity, h.OnLPPEvent)
	//h.myService.AddUseCase(h.uclpp)

	h.remoteEntities = map[spineapi.EntityRemoteInterface][]string{}

	if len(remoteSki) == 0 {
		os.Exit(0)
	}

	h.myService.RegisterRemoteSKI(remoteSki)

	h.myService.Start()
	// defer h.myService.Shutdown()
}

// EEBUSServiceHandler

func (h *controlbox) RemoteSKIConnected(service api.ServiceInterface, ski string) {
	h.isConnected = true
}

func (h *controlbox) RemoteSKIDisconnected(service api.ServiceInterface, ski string) {
	h.isConnected = false
}

func (h *controlbox) VisibleRemoteServicesUpdated(service api.ServiceInterface, entries []shipapi.RemoteService) {
}

func (h *controlbox) ServiceShipIDUpdate(ski string, shipdID string) {}

func (h *controlbox) ServicePairingDetailUpdate(ski string, detail *shipapi.ConnectionStateDetail) {
	if ski == remoteSki && detail.State() == shipapi.ConnectionStateRemoteDeniedTrust {
		fmt.Println("The remote service denied trust. Exiting.")
		h.myService.CancelPairingWithSKI(ski)
		h.myService.UnregisterRemoteSKI(ski)
		h.myService.Shutdown()
		os.Exit(0)
	}
}

func (h *controlbox) AllowWaitingForTrust(ski string) bool {
	return ski == remoteSki
}

// LPC Event Handler

func (h *controlbox) sendConsumptionLimit(entity spineapi.EntityRemoteInterface) {
	resultCB := func(msg model.ResultDataType) {
		if *msg.ErrorNumber == model.ErrorNumberTypeNoError {
			fmt.Println("Consumption limit accepted.")
		} else {
			fmt.Println("Consumption limit rejected. Code", *msg.ErrorNumber, "Description", *msg.Description)
		}
	}
	msgCounter, err := h.uclpc.WriteConsumptionLimit(entity, h.consumptionLimits, resultCB)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent consumption limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendConsumptionFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpc.WriteFailsafeConsumptionActivePowerLimit(entity, h.consumptionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send consumption failsafe limit", err)
		return
	}
	fmt.Println("Sent consumption failsafe limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendConsumptionFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpc.WriteFailsafeDurationMinimum(entity, h.consumptionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send consumption failsafe duration", err)
		return
	}
	fmt.Println("Sent consumption failsafe duration to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductionFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeProductionActivePowerLimit(entity, h.productionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send production failsafe limit", err)
		return
	}
	fmt.Println("Sent production failsafe limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductionFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeDurationMinimum(entity, h.productionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send production failsafe duration", err)
		return
	}
	fmt.Println("Sent production failsafe duration to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) readConsumptionNominalMax(entity spineapi.EntityRemoteInterface) {
	nominal, err := h.uclpc.ConsumptionNominalMax(entity)

	if err != nil {
		fmt.Println("Failed to get consumption nominal max", err)
		return
	}

	frontend.sendValue(GetConsumptionNominalMax, "LPC", nominal)
}

func (h *controlbox) OnLPCEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	if !h.isConnected {
		return
	}

	switch event {
	case lpc.UseCaseSupportUpdate:
		var listUCs = h.remoteEntities[entity]
		if listUCs == nil {
			listUCs = []string{}
		}
		h.remoteEntities[entity] = append(listUCs, "LPC")

		fmt.Println("Sending consumption limit in 5s...")

		time.AfterFunc(5*time.Second, func() {
			frontend.sendNotification(EntityListChanged)

			//h.readConsumptionNominalMax(entity)
			h.sendConsumptionLimit(entity)
			h.sendConsumptionFailsafeLimit(entity)
			h.sendConsumptionFailsafeDuration(entity)
		})
	case lpc.DataUpdateLimit:
		if currentLimit, err := h.uclpc.ConsumptionLimit(entity); err == nil {
			h.consumptionLimits = currentLimit

			if currentLimit.IsActive {
				fmt.Println("New consumption limit received: active,", currentLimit.Value, "W,", currentLimit.Duration)
			} else {
				fmt.Println("New consumption limit received: inactive,", currentLimit.Value, "W,", currentLimit.Duration)
			}
			frontend.sendLimit(GetConsumptionLimit, "LPC", ucapi.LoadLimit{
				IsActive: currentLimit.IsActive,
				Duration: currentLimit.Duration / time.Second,
				Value:    currentLimit.Value})
		}
	case lpc.DataUpdateFailsafeConsumptionActivePowerLimit:
		if limit, err := h.uclpc.FailsafeConsumptionActivePowerLimit(entity); err == nil {
			h.consumptionFailsafeLimits.Value = limit

			frontend.sendValue(GetConsumptionFailsafeValue, "LPC", limit)
		}
	case lpc.DataUpdateFailsafeDurationMinimum:
		if duration, err := h.uclpc.FailsafeDurationMinimum(entity); err == nil {
			h.consumptionFailsafeLimits.Duration = duration

			frontend.sendValue(GetConsumptionFailsafeDuration, "LPC", float64(duration/time.Second))
		}
	case lpc.DataUpdateHeartbeat:
		frontend.sendNotification(GetConsumptionHeartbeat)
	default:
		return
	}
}

// LPP Event Handler

func (h *controlbox) sendProductionLimit(entity spineapi.EntityRemoteInterface) {
	resultCB := func(msg model.ResultDataType) {
		if *msg.ErrorNumber == model.ErrorNumberTypeNoError {
			fmt.Println("Production limit accepted.")
		} else {
			fmt.Println("Production limit rejected. Code", *msg.ErrorNumber, "Description", *msg.Description)
		}
	}
	msgCounter, err := h.uclpp.WriteProductionLimit(entity, h.productionLimits, resultCB)
	if err != nil {
		fmt.Println("Failed to send production limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductiomFailsafeLimit(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeProductionActivePowerLimit(entity, h.productionFailsafeLimits.Value)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) sendProductiomFailsafeDuration(entity spineapi.EntityRemoteInterface) {
	msgCounter, err := h.uclpp.WriteFailsafeDurationMinimum(entity, h.productionFailsafeLimits.Duration)
	if err != nil {
		fmt.Println("Failed to send consumption limit", err)
		return
	}
	fmt.Println("Sent production limit to", entity.Device().Ski(), "with msgCounter", msgCounter)
}

func (h *controlbox) readProductionNominalMax(entity spineapi.EntityRemoteInterface) {
	nominal, err := h.uclpp.ProductionNominalMax(entity)

	if err != nil {
		fmt.Println("Failed to get production nominal max", err)
		return
	}

	frontend.sendValue(GetProductionNominalMax, "LPP", nominal)
}

func (h *controlbox) OnLPPEvent(ski string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event api.EventType) {
	if !h.isConnected {
		return
	}

	switch event {
	case lpp.UseCaseSupportUpdate:
		var listUCs = h.remoteEntities[entity]
		if listUCs == nil {
			listUCs = []string{}
		}
		h.remoteEntities[entity] = append(listUCs, "LPP")

		fmt.Println("Sending production limit in 5s...")

		time.AfterFunc(5*time.Second, func() {
			frontend.sendNotification(EntityListChanged)

			//h.readProductionNominalMax(entity)
			h.sendProductionLimit(entity)
			h.sendProductiomFailsafeLimit(entity)
			h.sendProductiomFailsafeDuration(entity)
		})
	case lpp.DataUpdateLimit:
		if currentLimit, err := h.uclpp.ProductionLimit(entity); err == nil {
			h.productionLimits = currentLimit

			if currentLimit.IsActive {
				fmt.Println("New production limit received: active,", currentLimit.Value, "W,", currentLimit.Duration)
			} else {
				fmt.Println("New production limit received: inactive,", currentLimit.Value, "W,", currentLimit.Duration)
			}

			frontend.sendLimit(GetProductionLimit, "LPP", ucapi.LoadLimit{
				IsActive: currentLimit.IsActive,
				Duration: currentLimit.Duration / time.Second,
				Value:    currentLimit.Value})
		}
	case lpp.DataUpdateFailsafeProductionActivePowerLimit:
		if limit, err := h.uclpp.FailsafeProductionActivePowerLimit(entity); err == nil {
			h.productionFailsafeLimits.Value = limit

			frontend.sendValue(GetProductionFailsafeValue, "LPP", limit)
		}
	case lpp.DataUpdateFailsafeDurationMinimum:
		if duration, err := h.uclpp.FailsafeDurationMinimum(entity); err == nil {
			h.productionFailsafeLimits.Duration = duration

			frontend.sendValue(GetProductionFailsafeDuration, "LPP", float64(duration/time.Second))
		}
	case lpp.DataUpdateHeartbeat:
		frontend.sendNotification(GetProductionHeartbeat)
	default:
		return
	}
}

// main app
func usage() {
	fmt.Println("First Run:")
	fmt.Println("  go run /examples/controlbox/main.go <serverport>")
	fmt.Println()
	fmt.Println("General Usage:")
	fmt.Println("  go run /examples/controlbox/main.go <serverport> <remoteski> <crtfile> <keyfile>")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	h := controlbox{}
	h.run()

	setupRoutes(&h)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(httpdPort), nil))

	// Clean exit to make sure mdns shutdown is invoked
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	// User exit
}

// Logging interface

func (h *controlbox) Trace(args ...interface{}) {
	// h.print("TRACE", args...)
}

func (h *controlbox) Tracef(format string, args ...interface{}) {
	// h.printFormat("TRACE", format, args...)
}

func (h *controlbox) Debug(args ...interface{}) {
	// h.print("DEBUG", args...)
}

func (h *controlbox) Debugf(format string, args ...interface{}) {
	// h.printFormat("DEBUG", format, args...)
}

func (h *controlbox) Info(args ...interface{}) {
	h.print("INFO ", args...)
}

func (h *controlbox) Infof(format string, args ...interface{}) {
	h.printFormat("INFO ", format, args...)
}

func (h *controlbox) Error(args ...interface{}) {
	h.print("ERROR", args...)
}

func (h *controlbox) Errorf(format string, args ...interface{}) {
	h.printFormat("ERROR", format, args...)
}

func (h *controlbox) currentTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (h *controlbox) print(msgType string, args ...interface{}) {
	value := fmt.Sprintln(args...)
	fmt.Printf("%s %s %s", h.currentTimestamp(), msgType, value)
}

func (h *controlbox) printFormat(msgType, format string, args ...interface{}) {
	value := fmt.Sprintf(format, args...)
	fmt.Println(h.currentTimestamp(), msgType, value)
}

// web frontend

const (
	httpdPort int = 7070
)

const (
	Text                           = 0
	QRCode                         = 1
	Acknowledge                    = 2
	EntityListChanged              = 3
	GetEntityList                  = 4
	GetAllData                     = 5
	SetConsumptionLimit            = 6
	GetConsumptionLimit            = 7
	SetProductionLimit             = 8
	GetProductionLimit             = 9
	SetConsumptionFailsafeValue    = 10
	GetConsumptionFailsafeValue    = 11
	SetConsumptionFailsafeDuration = 12
	GetConsumptionFailsafeDuration = 13
	SetProductionFailsafeValue     = 14
	GetProductionFailsafeValue     = 15
	SetProductionFailsafeDuration  = 16
	GetProductionFailsafeDuration  = 17
	GetConsumptionNominalMax       = 18
	GetProductionNominalMax        = 19
	GetConsumptionHeartbeat        = 20
	StopConsumptionHeartbeat       = 21
	StartConsumptionHeartbeat      = 22
	GetProductionHeartbeat         = 23
	StopProductionHeartbeat        = 24
	StartProductionHeartbeat       = 25
)

type EntityDescription struct {
	Name     string
	SKI      string
	UseCases []string
}

type Message struct {
	Type       int
	Text       string
	Limit      ucapi.LoadLimit
	Value      float64
	EntityList []EntityDescription
	UseCase    string
}

func sendData(h *controlbox) {
	frontend.sendText(QRCode, h.myService.QRCodeText())

	frontend.sendLimit(GetConsumptionLimit, "LPC", ucapi.LoadLimit{
		IsActive: h.consumptionLimits.IsActive,
		Duration: h.consumptionLimits.Duration / time.Second,
		Value:    h.consumptionLimits.Value})

	frontend.sendLimit(GetProductionLimit, "LPP", ucapi.LoadLimit{
		IsActive: h.productionLimits.IsActive,
		Duration: h.productionLimits.Duration / time.Second,
		Value:    h.productionLimits.Value})

	frontend.sendValue(GetConsumptionFailsafeValue, "LPC", h.consumptionFailsafeLimits.Value)

	frontend.sendValue(GetConsumptionFailsafeDuration, "LPC", float64(h.consumptionFailsafeLimits.Duration/time.Second))

	frontend.sendValue(GetProductionFailsafeValue, "LPP", h.productionFailsafeLimits.Value)

	frontend.sendValue(GetProductionFailsafeDuration, "LPP", float64(h.productionFailsafeLimits.Duration/time.Second))
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// allow connection from any host
		return true
	},
}

func setupRoutes(h *controlbox) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(h, w, r)
	})
}

func serveWs(h *controlbox, w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	frontend = WebsocketClient{
		websocket: ws}

	log.Println("Client Connected")

	sendData(h)

	reader(h, ws)
}

func reader(h *controlbox, ws *websocket.Conn) {
	for {
		// read in a message
		_, p, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		// print out that message for clarity
		fmt.Println(string(p))

		data := Message{}
		json.Unmarshal([]byte(p), &data)

		if data.Type == GetEntityList {
			frontend.sendEntityList(GetEntityList, h.remoteEntities)
		} else if data.Type == GetAllData {
			sendData(h)
		} else if data.Type == SetConsumptionLimit {
			var limit = data.Limit

			h.consumptionLimits.IsActive = limit.IsActive
			h.consumptionLimits.Value = limit.Value
			h.consumptionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionLimit {
			var limit = data.Limit

			h.productionLimits.IsActive = limit.IsActive
			h.productionLimits.Value = limit.Value
			h.productionLimits.Duration = limit.Duration * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetConsumptionFailsafeValue {
			var limit = data.Value

			h.consumptionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetConsumptionFailsafeDuration {
			var limit = data.Value

			h.consumptionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpc.RemoteEntitiesScenarios() {
				h.sendConsumptionFailsafeDuration(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionFailsafeValue {
			var limit = data.Value

			h.productionFailsafeLimits.Value = limit

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeLimit(remoteEntityScenario.Entity)
			}
		} else if data.Type == SetProductionFailsafeDuration {
			var limit = data.Value

			h.productionFailsafeLimits.Duration = time.Duration(limit) * time.Second

			for _, remoteEntityScenario := range h.uclpp.RemoteEntitiesScenarios() {
				h.sendProductionFailsafeDuration(remoteEntityScenario.Entity)
			}
		} else if data.Type == StopConsumptionHeartbeat {
			h.uclpc.StopHeartbeat()
		} else if data.Type == StartConsumptionHeartbeat {
			h.uclpc.StartHeartbeat()
		}

		answer := Message{
			Type: Acknowledge}

		bytes, _ := json.Marshal(answer)
		if err := ws.WriteMessage(1, bytes); err != nil {
			log.Println(err)
			return
		}
	}
}
