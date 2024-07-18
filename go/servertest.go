package main

import (
	"fmt"
	"log"
	"nrpctest/logger"
	natsclient "nrpctest/nats/natsclient"
	"nrpctest/nrpc"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	LogTag = "[server]"
)

const (
	UserName = "test"
	UserPwd  = "test123"
)

func main() {
	log.Println("[main]Hello, World!")
	logger1 := logger.NewLogger("natsclient")
	cfg := natsclient.NewNatsConfig("service", []string{nats.DefaultURL}, UserName, UserPwd, 3, 10)
	client1, err := natsclient.NewNatsClient(cfg, logger1)
	if err != nil {
		log.Panicf("%s natsclient connect failed, err:%v\n", LogTag, err)
	}

	stream := "test"
	// consumer := "testServiceSayHello"
	subject := "service"
	logger2 := logger.NewLogger("nrpcserver")
	rpcserver := nrpc.NewNRPCServer("testservice", subject, nrpc.EncodeProtobuf, logger2, client1)
	rpcserver.NRpc.BindStream(natsclient.StreamDefaultConfig(stream, subject))
	// rpcserver.NRpc.BindConsumer(stream, natsclient.ConsumerDefaultConfig(consumer, subject+".SayHello"))

	service := nrpc.NewSerivce("SayHello", stream, "", "", "")
	rpcserver.Register(service, func(request *nrpc.Request) *nrpc.Response {
		log.Printf("[server]handler:%s ...\n", request.Name)
		// time.Sleep(10 * time.Second)

		return nrpc.NewResponse(nrpc.ResponseResult_Success, "",
			[]byte(fmt.Sprintf("SayHello, hello world %v", time.Now())))
	})

	// consumer = "ServiceSayHello1"
	// rpcserver.NRpc.BindConsumer(stream, natsclient.ConsumerDefaultConfig(consumer, subject+".SayHello1"))
	// service1 := nrpc.NewSerivce("SayHello1", stream, consumer, "", "test")
	// service1 := nrpc.NewSerivce("SayHello1", stream, "", "", "test")
	// rpcserver.Register(service1, func(data []byte) *nrpc.Response {
	// 	//解码获取request请求
	// 	var req helloworld.HelloRequest
	// 	nrpc.ParseRequest(rpcserver.GetEncode(), data, &req)

	// 	log.Printf("[server]handler:%s ...\n", req.Request.FuncName)
	// 	// time.Sleep(3 * time.Second)

	// 	return nrpc.NewResponse(nrpc.ResponseResult_Success, "hello1",
	// 		[]byte(fmt.Sprintf("SayHello, hello world1 %v", time.Now())))
	// })
	fmt.Println("wait...")

	select {}
}
