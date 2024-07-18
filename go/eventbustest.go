package main

import (
	"log"
	"nrpctest/logger"
	natsserver "nrpctest/nats/natsserver"
)

const (
	LogTag = "[eventbus]"
)

func main() {
	logger1 := logger.NewLogger("eventbus")
	server := natsserver.NewNatsServer(natsserver.UserName, natsserver.PassWord, logger1)
	err := server.Start()
	if err != nil {
		log.Panicf("%s eventbus start failed, err:%v\n", err)
	}
	select {}
}
