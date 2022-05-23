package main

import (
	// Std
	"runtime"
	"strconv"
	"time"

	// Momentum
	"github.com/momentum-xyz/controller/internal/config"
	"github.com/momentum-xyz/controller/internal/extension"
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/internal/net"
	"github.com/momentum-xyz/controller/internal/universe"
	"github.com/momentum-xyz/controller/pkg/message"

	// Third-Party
	"github.com/pkg/errors"
	"go.uber.org/zap/zapcore"
)

const (
	garbageCollectionInterval = 10 * time.Second
	logGoRoutinesInterval     = 2 * time.Minute
)

// ExtensionLoader is global variable that holds available world extensions
var ExtensionLoader extension.Loader

var log = logger.L()

func main() {
	if err := run(); err != nil {
		log.Fatal(errors.WithMessage(err, "error running"))
	}
}

func run() error {
	cfg := config.GetConfig()
	logger.SetLevel(zapcore.Level(cfg.Settings.LogLevel))
	defer logger.Close()

	networking := net.NewNetworking(cfg)
	msgBuilder := message.InitBuilder(20, 1024*32)
	hub := universe.NewControllerHub(cfg, networking, msgBuilder)

	ExtensionLoader = extension.NewLoader()
	// ExtensionLoader.Set("kusama", extensions.NewKusama)
	// extLoader := ExtensionLoader.Get("kusama")
	// kusamaExt := extLoader()

	go hub.UpdateTotalUsers()
	go hub.NetworkRunner()

	go runGarbageCollection(garbageCollectionInterval)
	go logNumberOfGoroutines(logGoRoutinesInterval)
	address, port := cfg.Settings.Address, strconv.FormatUint(uint64(cfg.Settings.Port), 10)

	return networking.ListenAndServe(address, port)
}

func logNumberOfGoroutines(interval time.Duration) {
	for {
		log.Infof("Num Goroutines: %d", runtime.NumGoroutine())
		time.Sleep(interval)
	}
}

func runGarbageCollection(gcInterval time.Duration) {
	for {
		time.Sleep(gcInterval)
		runtime.GC()
	}
}
