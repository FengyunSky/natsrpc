/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc注册及服务分发处理
 */

package nrpc

import (
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	LogTagServer = "nrpcserver"
)

type ServiceHandler func(*Request) *Response
type ResponseHandler func([]byte) error
type NatsSub *nats.Subscription

// msg 队列处理消息
type WorkMsg struct {
	msg      *nats.Msg //消息
	stream   string
	encoding string
	nc       NatsClient
	logger   Logger
	handler  ServiceHandler // 事件处理
}

func NewWorkMsg(msg *nats.Msg, stream, encoding string, nc NatsClient, logger Logger, handler ServiceHandler) *WorkMsg {
	return &WorkMsg{
		msg:      msg,
		stream:   stream,
		encoding: encoding,
		nc:       nc,
		logger:   logger,
		handler:  handler,
	}
}

/**
 * @name: Process
 * @msg: 消息处理器
 * @return {void}
 */
func (workMsg *WorkMsg) Process() {
	msg := workMsg.msg
	workMsg.logger.Infof(LogTagServer, "[workmsg] Process msg, for stream:%s subject:%s",
		workMsg.stream, msg.Subject)
	if workMsg.stream != "" {
		msg.InProgress()
	}
	var req Request
	ParseRequest(workMsg.encoding, msg.Data, &req)
	rep := workMsg.handler(&req)
	data, err := Marshal(workMsg.encoding, rep)
	if err != nil {
		workMsg.logger.Errorf(LogTagServer, "[workmsg] Process failed, Marshal failed, err:%v", err)
		msg.Term()
		return
	}

	if workMsg.stream != "" {
		//判定是否期望的主题与响应消息的主题匹配，不匹配则丢弃
		stream := msg.Header.Get(nats.ExpectedStreamHdr)
		if workMsg.stream != stream {
			workMsg.logger.Errorf(LogTagServer,
				"[workmsg] Process, mismatch stream(%s) of the response msg for the stream(%s) of workmsg, to terminate",
				stream, workMsg.stream)
			msg.Term()
			return
		}
		err := msg.Ack()
		if err != nil {
			workMsg.logger.Warnf(LogTagServer, "[workmsg] Process, response for subject:%s, ACK err:%v", msg.Subject, err)
		}
		replySub := msg.Header.Get(ExpectedReplySujectHdr)
		err = workMsg.nc.Publish(replySub, data)
		if err != nil {
			workMsg.logger.Warnf(LogTagServer, "[workmsg] Process, response for subject:%s, err:%v", replySub, err)
		}
	} else {
		msg.Respond(data)
	}

	workMsg.logger.Infof(LogTagServer, "[workmsg] Process, response for subject:%s header:%v len:%d",
		msg.Subject, msg.Header, len(data))

}

// Dispatcher NATS消息分发器
type WorkQueue struct {
	name        string        //队列名称
	workMsgChan chan *WorkMsg // 消息通道
	closeChan   chan struct{} //关闭通道
}

func NewWorkQueue(name string, maxMsg int16) *WorkQueue {
	return &WorkQueue{
		name:        name,
		workMsgChan: make(chan *WorkMsg, maxMsg),
		closeChan:   make(chan struct{}),
	}
}

/**
 * @name: AddMsg
 * @msg: 向工作队列中添加消息
 * @param {*WorkMsg} msg
 * @return {*}
 */
func (work *WorkQueue) AddMsg(msg *WorkMsg) {
	work.workMsgChan <- msg
}

/**
 * @name: Dispatch
 * @msg: 工作队列事件分发处理
 * @return {void}
 */
func (work *WorkQueue) Dispatch() {
	go func() {
		for {
			select {
			case workMsg := <-work.workMsgChan:
				workMsg.Process()
			case <-work.closeChan:
				return
			}
		}
	}()
}

// 可以由用户自定义的参数集合
type WorkQueueStopOption struct {
	safe bool
	time time.Duration
}

// 定义修改默认参数的钩子函数
type WorkQueueStopOptionFunc func(opt *WorkQueueStopOption)

func WorkQueueStopDefaultOption() WorkQueueStopOption {
	return WorkQueueStopOption{
		safe: true,
		time: 3 * time.Second,
	}
}

// 实际修改默认参数的函数
func WorkQueueStopSafeOption(safe bool) WorkQueueStopOptionFunc {
	return func(opt *WorkQueueStopOption) {
		opt.safe = safe
	}
}

func WorkQueueStopTimeOption(time time.Duration) WorkQueueStopOptionFunc {
	return func(opt *WorkQueueStopOption) {
		opt.time = time
	}
}

/**
 * @name: Stop
 * @msg: 停止工作队列处理
 * @param {...WorkQueueStopOptionFunc} options
 * @return {void}
 */
func (work *WorkQueue) Stop(options ...WorkQueueStopOptionFunc) {
	opts := WorkQueueStopDefaultOption()
	// 调用钩子函数，并对默认值进行修改
	for _, fun := range options {
		fun(&opts)
	}

	if opts.safe {
		for {
			select {
			case workMsg := <-work.workMsgChan:
				workMsg.Process()
			case <-time.After(opts.time * time.Second):
				return
			}
		}
	} else {
		close(work.closeChan)
	}
}

type NRPCSever struct {
	NRpc        *NRPC
	serviceSubs map[string]NatsSub    //事件订阅组
	mapLock     *(sync.Mutex)         //锁
	wokers      map[string]*WorkQueue //工作队列
	workLock    *(sync.Mutex)
}

type Service struct {
	serviceName  string //服务名称
	consumerName string //消费者名称(若指定消费者名称则需要绑定专属的消费者，否则会自动创建绑定至stream)
	streamName   string //stream 流的名称
	queueName    string //订阅处理的队列名称(用于队列组来实现负载均衡)
	workName     string //订阅处理的工作队列名称(用于多处理事件串行处理)
}

/**
 * @name: NewSerivce
 * @msg: 创建服务
 * @param {string} service 服务名称
 * @param {string} stream 服务流名称
 * @param {string} consumer 服务消费者名称
 * @param {string} queue 服务队列
 * @param {string} work 服务工作队列名称
 * @return {*Service}
 */
func NewSerivce(service, stream, consumer, queue, work string) *Service {
	return &Service{
		serviceName:  service,
		consumerName: consumer,
		streamName:   stream,
		queueName:    queue,
		workName:     work,
	}
}

/**
 * @name: NewNRPCServer
 * @msg: 创建rpc服务
 * @param {string} name rpc服务名称
 * @param {string} subject 服务主题
 * @param {string} encoding 编码方式
 * @param {Logger} logger 日志接口
 * @param {NatsClient} nc nats客户端
 * @return {*NRPCSever}
 */
func NewNRPCServer(name string, subject string, encoding string, logger Logger, nc NatsClient) *NRPCSever {
	CheckEncode(encoding)
	CheckSubject(subject)
	return &NRPCSever{
		NRpc:        NewNPRC(name, subject, encoding, nc, logger),
		serviceSubs: make(map[string]NatsSub, 10),
		wokers:      make(map[string]*WorkQueue, 10),
		mapLock:     &sync.Mutex{},
		workLock:    &sync.Mutex{},
	}
}

/**
 * @name: GetEncode
 * @msg: 获取编码方式
 * @return {string}
 */
func (server *NRPCSever) GetEncode() string {
	return server.NRpc.encoding
}

/**
 * @name: workQueue
 * @msg: 创建服务队列
 * @param {string} name
 * @return {*WorkQueue}
 */
func (server *NRPCSever) workQueue(name string) *WorkQueue {
	(*server.workLock).Lock()
	defer (*server.workLock).Unlock()
	if server.wokers[name] != nil {
		return server.wokers[name]
	}
	WorkQueue := NewWorkQueue(name, 10)
	WorkQueue.Dispatch() //创建则启动消息分发
	server.wokers[name] = WorkQueue
	return WorkQueue
}

/**
 * @name:Register
 * @msg: 注册服务
 * @param {*Service} service
 * @param {ServiceHandler} handler
 * @return {void}
 */
func (server *NRPCSever) Register(service *Service, handler ServiceHandler) error {
	if service == nil {
		server.NRpc.logger.Panicf(LogTagServer, "service invalid, is nil")
	}
	if service.serviceName == "" {
		server.NRpc.logger.Panicf(LogTagServer, "Register failed, service invalid", server.NRpc.name)
	}

	//匹配校验
	subject := server.NRpc.subject + "." + strings.ToLower(service.serviceName)
	server.NRpc.logger.Infof(LogTagServer, "Register the subject:%s for the stream:%s and the consumer:%s",
		subject, service.streamName, service.consumerName)
	stream := service.streamName
	consumer := service.consumerName

	if service.consumerName != "" {
		server.NRpc.CheckConsumerAndStreamWithSubject(stream, consumer, subject)
	} else {
		server.NRpc.CheckStreamWithSubject(stream, subject)
	}

	(*server.mapLock).Lock()
	defer (*server.mapLock).Unlock()

	subHandler := func(msg *nats.Msg) {
		// 解析函数名称是否与处理的一致
		if subject != msg.Subject {
			server.NRpc.logger.Panicf(LogTagServer, "Register failed, handler failed, invalid subject %s", msg.Subject)
		}

		workMsg := NewWorkMsg(msg, service.streamName, server.NRpc.encoding, server.NRpc.nc, server.NRpc.logger, handler)
		if service.workName != "" {
			workQueue := server.workQueue(service.workName)
			workQueue.AddMsg(workMsg)
		} else {
			workMsg.Process()
		}
	}

	var sub NatsSub
	var err error
	//若不指定Steam则使用核心nats模式
	if service.streamName == "" {
		if service.queueName != "" {
			sub, err = server.NRpc.nc.QueueSubscribe(subject, service.queueName, subHandler)
		} else {
			sub, err = server.NRpc.nc.Subscribe(subject, subHandler)
		}
	} else {
		//如果指定消费者则使用绑定并订阅主题为消费者指定过滤主题
		opts := []nats.SubOpt{
			nats.ManualAck(),
		}
		if service.consumerName != "" {
			opts = append(opts,
				nats.Bind(stream, consumer))
		} else {
			//若未指定消费者名称则为push模式，自动创建consumer
			consumer = stream + "-" + server.NRpc.subject + "-" + service.serviceName
			opts = append(opts,
				nats.BindStream(stream),
				nats.Durable(consumer),
				nats.DeliverLast(),
				nats.AckExplicit(),
				nats.MaxDeliver(10),
				nats.MaxAckPending(10))
		}

		if service.queueName != "" {
			sub, err = server.NRpc.nc.QueueSubscribe(subject, service.queueName, subHandler, opts...)
		} else {
			sub, err = server.NRpc.nc.Subscribe(subject, subHandler, opts...)
		}
	}

	if err != nil {
		server.NRpc.logger.Errorf(LogTagServer, "Register failed, Subscribe err:%v", err)
		return err
	}

	server.serviceSubs[service.serviceName] = sub
	return nil
}

/**
 * @name: UnRegister
 * @msg: 取消订阅服务处理
 * @param {*Service} service
 * @return {*}
 */
func (server *NRPCSever) UnRegister(service *Service) {
	(*server.mapLock).Lock()
	defer (*server.mapLock).Unlock()
	//若 service 为空则删除所有
	if service.serviceName == "" {
		for name := range server.serviceSubs {
			server.NRpc.nc.Unsubscribe(server.serviceSubs[name])
			delete(server.serviceSubs, name)
		}
		return
	}

	//取消指定服务的订阅
	if _, ok := server.serviceSubs[service.serviceName]; ok {
		server.NRpc.nc.Unsubscribe(server.serviceSubs[service.serviceName])
		delete(server.serviceSubs, service.serviceName)
	}
}
