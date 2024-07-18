package main

import (
	"log"
	"nrpctest/logger"
	natsclient "nrpctest/nats/natsclient"
)

const (
	LogTag = "[client]"
)

const (
	UserName = "test"
	UserPwd  = "test123"
)

func main() {
	cfg := natsclient.NewNatsConfig("test-client-listenr", natsclient.DefaultServerAddr, UserName, UserPwd, 3, 10)
	logger1 := logger.NewLogger("natsclient")
	client, err := natsclient.NewNatsClient(cfg, logger1)
	if err != nil {
		log.Panicf("%s natsclient connect failed, err:%v\n", LogTag, err)
	}

	// client.Subscribe("test-dead", func(msg *nats.Msg) {
	// 	log.Printf("recv msg, sub:%s", msg.Subject)

	// })
	client.Publish("test-dead", []byte{})

	select {}
}
