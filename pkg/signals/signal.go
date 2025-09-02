package signals

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

var shutdownSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT}

var onlyOneSignalHandler = make(chan struct{})

func SetupSignalHandler() context.Context {
	close(onlyOneSignalHandler)

	c := make(chan os.Signal, 2)
	ctx, cancel := context.WithCancel(context.Background())
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		cancel()
		<-c
		os.Exit(1)
	}()

	return ctx
}
