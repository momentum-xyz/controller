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
	logRuntimeStatInterval    = 10 * time.Second
)

// ExtensionLoader is global variable that holds available world extensions
var ExtensionLoader extension.Loader

var log = logger.L()

func main() {
	if err := run(); err != nil {
		log.Fatal(errors.WithMessage(err, "failed to run service"))
	}
}

func run() error {
	cfg := config.GetConfig()
	logger.SetLevel(zapcore.Level(cfg.Settings.LogLevel))
	defer logger.Close()

	networking := net.NewNetworking(cfg)
	msgBuilder := message.InitBuilder(20, 1024*32)
	hub, err := universe.NewControllerHub(cfg, networking, msgBuilder)
	if err != nil {
		return errors.WithMessage(err, "failed to create controller hub")
	}

	ExtensionLoader = extension.NewLoader()
	// ExtensionLoader.Set("kusama", extensions.NewKusama)
	// extLoader := ExtensionLoader.Get("kusama")
	// kusamaExt := extLoader()

	go hub.UpdateTotalUsers()
	go hub.NetworkRunner()

	go runGarbageCollection(garbageCollectionInterval)
	go logRuntimeStat(logRuntimeStatInterval)
	address, port := cfg.Settings.Address, strconv.FormatUint(uint64(cfg.Settings.Port), 10)

	return networking.ListenAndServe(address, port)
}

func logRuntimeStat(interval time.Duration) {
	for {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		log.Warnf("Runtime Stat:\n\tAlloc: %dMiB\n\tSys: %dMiB\n\tMallocs: %d\n\tFreese: %d\n\tMallDiff: %d\n\tGoroutines: %d",
			bToMb(m.Alloc), bToMb(m.Sys), m.Mallocs, m.Frees, m.Mallocs-m.Frees, runtime.NumGoroutine())
		time.Sleep(interval)
	}
}

func runGarbageCollection(gcInterval time.Duration) {
	for {
		time.Sleep(gcInterval)
		runtime.GC()
	}
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
