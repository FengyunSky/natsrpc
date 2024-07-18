package main

import (
	"log"
	"nrpctest/helloworld"
	"nrpctest/logger"
	natsclient "nrpctest/nats/natsclient"
	"nrpctest/nrpc"

	"github.com/nats-io/nats.go"
)

const (
	LogTag = "[client]"
)

const (
	UserName = "test"
	UserPwd  = "test123"
)

func main() {
	cfg := natsclient.NewNatsConfig("testclient", []string{nats.DefaultURL}, UserName, UserPwd, 3, 10)

	stream := "test"
	subject := "service"

	logger1 := logger.NewLogger("natsclient")
	client2, err := natsclient.NewNatsClient(cfg, logger1)
	if err != nil {
		log.Panicf("%s natsclient connect failed, err:%v\n", LogTag, err)
	}

	logger2 := logger.NewLogger("nrpcclient")
	rpcclient := nrpc.NewNRPCClient("testclient", subject, nrpc.EncodeProtobuf, logger2, client2)
	rpcclient.NRpc.BindStream(natsclient.StreamDefaultConfig(stream, subject))
	cli := helloworld.NewGreeterClient(rpcclient)

	go func() {
		rsp, err := cli.SayHello("I can say hello")
		if err != nil {
			log.Panicf("%s SayHello failed, err:%v\n", LogTag, err)
		}
		if rsp.Result != nrpc.ResponseResult_Success {
			log.Printf("[main]SayHello failed, ret:%d\n", rsp.Result)
		} else {
			log.Printf("[main]SayHello success, msg:%s\n", rsp.Data)
		}
	}()

	// go func() {
	// 	rsp, err := cli.SayHello1("I can say hello1")
	// 	if err != nil {
	// 		log.Panicf("%s SayHello1 failed, err:%v\n", LogTag, err)
	// 	}
	// 	if rsp.Result != nrpc.ResponseResult_Success {
	// 		log.Printf("[main]SayHello1 failed, ret:%d\n", rsp.Result)
	// 	} else {
	// 		log.Printf("[main]SayHello1 success, msg:%s\n", rsp.Data)
	// 	}
	// }()

	// for index := 0; index < 10; index++ {
	// 	go func(index int) {
	// 		msg := fmt.Sprintf("Say Hello %d", index)
	// 		rep, err := cli.SayHello(msg)
	// 		if err != nil {
	// 			log.Panicf("%s SayHello%d failed, err:%v\n", LogTag, index, err)
	// 		}
	// 		if rep.Response.Result == nrpc.ResponseResultType_Success {
	// 			log.Printf("%s SayHello%d response, msg:%s %s\n", LogTag, index, string(rep.Response.Data), rep.Response.Message)
	// 		} else {
	// 			log.Printf("%s SayHello%d failed, result:%d\n", LogTag, index, rep.Response.Result)
	// 		}
	// 	}(index)
	// }

	select {}
}
