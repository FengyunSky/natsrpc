/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 主要对接nats层，实现核心的nats 连接+订阅+发布等API。后续对接kv存储API
 */
package natsclient

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	LogTag = "natsclient"
)

const (
	DefaultUserName   = "test"
	DefaultPassword   = "test123"
	DefaultReconnWait = 3
	DefaultMaxReconn  = 10
)

// 默认服务器集群地址
var DefaultServerAddr = []string{"nats://127.0.0.1:4222", "nats://127.0.0.1:4223", "nats://127.0.0.1:4224"}

type Logger interface {
	Verbosef(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Panicf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

type NatsClient struct {
	config       *NatsConfig
	conn         *nats.Conn
	closehandler nats.ConnHandler //连接关闭的回调
	js           nats.JetStreamContext
	mux          sync.Mutex
	logger       Logger
}

// Config 配置
type NatsConfig struct {
	Name          string
	Server        []string // nats://127.0.0.1:4222,nats://127.0.0.1:4223
	User          string   // 用户名
	Pwd           string   //密码
	ReconnectWait int16    // 重连间隔
	MaxReconnects int16    // 重连次数
}

func NewNatsConfig(name string, server []string, user string, pwd string, reconnWait int16, maxReconn int16) *NatsConfig {
	if len(server) == 0 {
		server = DefaultServerAddr
	}
	if user == "" {
		user = DefaultUserName
	}
	if pwd == "" {
		pwd = DefaultPassword
	}
	if reconnWait == 0 {
		reconnWait = DefaultReconnWait
	}
	if maxReconn == 0 {
		maxReconn = DefaultMaxReconn
	}

	return &NatsConfig{
		Name:          name,
		Server:        server,
		User:          user,
		Pwd:           pwd,
		ReconnectWait: reconnWait,
		MaxReconnects: maxReconn,
	}
}

/**
 * @name: setupConnOptions
 * @msg: 设置连接选项
 * @param {*NatsConfig} cfg
 * @return {*}
 */
func (client *NatsClient) setupConnOptions(cfg *NatsConfig) []nats.Option {
	opts := make([]nats.Option, 0)
	opts = append(opts, nats.Name(cfg.Name))
	if len(cfg.User) > 0 {
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Pwd))
	}
	opts = append(opts, nats.ReconnectWait(time.Second*time.Duration(cfg.ReconnectWait)))
	opts = append(opts, nats.MaxReconnects(int(cfg.MaxReconnects)))
	opts = append(opts, nats.ReconnectHandler(func(nc *nats.Conn) {
		client.logger.Warnf("reconnect success, for url:%s", nc.ConnectedUrl())
	}))
	opts = append(opts, nats.DiscoveredServersHandler(func(nc *nats.Conn) {
		client.logger.Warnf("discover:%s", nc.DiscoveredServers()[0])
	}))
	opts = append(opts, nats.DisconnectHandler(func(nc *nats.Conn) {
		client.logger.Warnf("%s", "disconnect...")
	}))
	return opts
}

func NewNatsClient(cfg *NatsConfig, logger Logger) (*NatsClient, error) {
	if logger == nil {
		log.Panicf("NewNatsClient failed, invalid logger")
	}

	client := &NatsClient{
		config: cfg,
		logger: logger,
	}

	//连接关闭自动重连
	client.closehandler = func(c *nats.Conn) {
		logger.Errorf("nats client close!!! Reconnect")
		nc, err := client.Connect()
		if err != nil {
			client.logger.Errorf("NewNatsClient failed, connect err:%v", LogTag, err)
			return
		}

		client.conn = nc
		//TODO 指定jetstream选项
		js, err := nc.JetStream(nats.PublishAsyncMaxPending(100))
		if err != nil {
			client.logger.Panicf(LogTag, "NewNatsClient failed, JetStream err:%v", err)
		}
		client.js = js
	}
	nc, err := client.Connect()
	if err != nil {
		client.logger.Errorf("NewNatsClient failed, connect err:%v", LogTag, err)
		return nil, err
	}

	client.conn = nc
	//TODO 指定jetstream选项
	js, err := nc.JetStream(nats.PublishAsyncMaxPending(100))
	if err != nil {
		client.logger.Panicf(LogTag, "NewNatsClient failed, JetStream err:%v", err)
	}
	client.js = js
	return client, nil
}

func StreamDefaultConfig(stream, subject string) *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:       stream,
		Subjects:   []string{subject + ".>"},
		Retention:  nats.InterestPolicy,
		MaxAge:     10 * time.Second,
		Discard:    nats.DiscardOld,
		Storage:    nats.FileStorage,
		Duplicates: 10 * time.Second,
		MaxBytes:   10 * 1024 * 1024,
		MaxMsgSize: 1024 * 1024,
		MaxMsgs:    100,
	}
}

func ConsumerDefaultConfig(consumer, subject string) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Name:           consumer,
		Durable:        consumer,
		FilterSubject:  subject,
		DeliverSubject: nats.NewInbox(),
		DeliverPolicy:  nats.DeliverLastPolicy,
		AckPolicy:      nats.AckExplicitPolicy,
		MaxDeliver:     10,
		MaxAckPending:  10,
	}
}

/**
 * @name: AddStream
 * @msg: 添加流
 * @param {*nats.StreamConfig} cfg
 * @return {*}
 * @abstract 若jestream中存在此stream则更新，否则直接添加
 */
func (client *NatsClient) AddStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error) {
	if cfg == nil {
		client.logger.Panicf(LogTag, "AddStream failed, invalid config, is nil")
	}
	// 添加stream的主题必须包含.>通配符模式
	isvalid := false
	for _, sub := range cfg.Subjects {
		if strings.Contains(sub, ".>") {
			isvalid = true
			break
		}
	}
	if !isvalid {
		client.logger.Panicf(LogTag, "AddStream failed, invalid config for subject, not contain .> wildcard")
	}

	info, err := client.js.StreamInfo(cfg.Name)
	if err != nil && err == nats.ErrStreamNotFound {
		client.logger.Warnf(LogTag, "AddStream failed, not found the stream(%s), to add", cfg.Name)
		stream, err := client.js.AddStream(cfg)
		if err != nil {
			client.logger.Errorf(LogTag, "AddStream failed, err:%v", err)
		}
		return stream, err
	} else {
		info, err = client.js.UpdateStream(cfg)
		if err != nil {
			client.logger.Panicf(LogTag, "AddStream failed, UpdateStream err:%v", err)
		}
	}

	return info, nil
}

/**
 * @name: StreamNameBySubject
 * @msg: 查找匹配主题的流
 * @param {string} subject
 * @param {...nats.JSOpt} opts
 * @return {*}
 */
func (client *NatsClient) StreamNameBySubject(subject string, opts ...nats.JSOpt) (string, error) {
	return client.js.StreamNameBySubject(subject, opts...)
}

/**
 * @name: AddConsumer
 * @msg: 添加消费者
 * @param {string} stream
 * @param {*nats.ConsumerConfig} cfg
 * @param {...nats.JSOpt} opts
 * @return {*}
 * @abstract 若stream包含了改消费者则更新消费者配置，否则直接添加
 */
func (client *NatsClient) AddConsumer(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	if cfg == nil {
		client.logger.Panicf(LogTag, "AddConsumer failed, invliad cfg")
	}
	//消费者名称须与持久化名称相同
	if cfg.Name != "" && cfg.Name != cfg.Durable {
		client.logger.Panicf(LogTag, "AddConsumer failed, the consumer config for Name and Duralbe not equal")
	}

	client.logger.Infof(LogTag, "AddConsumer, for stream:%s and comsumer:%s", stream, cfg.Durable)

	info, err := client.js.ConsumerInfo(stream, cfg.Durable)
	if err != nil && err == nats.ErrConsumerNotFound {
		client.logger.Warnf(LogTag, "AddConsumer failed, not found consumer(%s) for the stream(%s), to add",
			cfg.Name, stream)
		consumer, err := client.js.AddConsumer(stream, cfg, opts...)
		if err != nil {
			client.logger.Panicf(LogTag, "AddConsumer failed, err:%v", err)
		}

		return consumer, err
	} else {
		info, err = client.js.UpdateConsumer(stream, cfg, opts...)
		if err != nil {
			client.logger.Panicf(LogTag, "AddConsumer failed, UpdateConsumer err:%v", err)
		}
	}

	return info, nil
}

/**
 * @name: ConsumerInfoByStream
 * @msg: 查找stream相关的消费者信息
 * @param {*} stream
 * @param {string} consumer
 * @return {*}
 */
func (client *NatsClient) ConsumerInfoByStream(stream, consumer string) (*nats.ConsumerInfo, error) {
	return client.js.ConsumerInfo(stream, consumer)
}

func (client *NatsClient) ReConnect() error {
	nc, err := client.Connect()
	if err != nil {
		client.logger.Errorf("ReConnect failed, connect err:%v", LogTag, err)
		return err
	}

	client.SetConnection(nc)
	//TODO 指定jetstream选项
	js, err := nc.JetStream(nats.PublishAsyncMaxPending(100))
	if err != nil {
		client.logger.Panicf(LogTag, "ReConnect failed, JetStream err:%v", err)
	}
	client.SetJetStream(js)
	return nil
}

func (client *NatsClient) GetConnection() *nats.Conn {
	client.mux.Lock()
	defer client.mux.Unlock()
	return client.conn
}

func (client *NatsClient) SetConnection(conn *nats.Conn) error {
	if conn == nil {
		return fmt.Errorf("SetConnection failed, invalid conn")
	}
	client.mux.Lock()
	defer client.mux.Unlock()
	client.conn = conn

	return nil
}

func (client *NatsClient) GetJetStream() nats.JetStreamContext {
	client.mux.Lock()
	defer client.mux.Unlock()
	return client.js
}

func (client *NatsClient) SetJetStream(js nats.JetStreamContext) {
	client.mux.Lock()
	defer client.mux.Unlock()
	client.js = js
}

/**
 * @name:Connect
 * @msg: 建立连接
 * @return {*}
 */
func (client *NatsClient) Connect() (*nats.Conn, error) {
	if client.conn != nil {
		client.logger.Warnf("%s Connect, has already connected, not to handler", LogTag)
		return client.conn, nil
	}

	// Connect Options.
	opts := client.setupConnOptions(client.config)
	if client.closehandler != nil {
		opts = append(opts, nats.ClosedHandler(client.closehandler))
	}
	nc, err := nats.Connect(strings.Join(client.config.Server, ","), opts...)
	if err != nil {
		return nil, fmt.Errorf("%s Connect failed, error:%v", LogTag, err)
	}

	return nc, nil
}

/**
 * @name: Publish
 * @msg: 推送消息
 * @param {string} subj
 * @param {[]byte} data
 * @param {...nats.PubOpt} opts
 * @return {*}
 */
func (client *NatsClient) Publish(subj string, data []byte, opts ...nats.PubOpt) error {
	client.logger.Infof(LogTag, "Publish, for sub:%s", subj)
	var err error
	if opts != nil {
		_, err = client.GetJetStream().Publish(subj, data, opts...)
	} else {
		err = client.GetConnection().Publish(subj, data)
	}

	if err != nil {
		client.logger.Errorf(LogTag, "Publish failed, err:%v", err)
		return fmt.Errorf("%s Publish failed, err:%v", LogTag, err)
	}

	return nil
}

/**
 * @name: PublishMsg
 * @msg: 推送消息
 * @param {*nats.Msg} m
 * @param {...nats.PubOpt} opts
 * @return {*}
 */
func (client *NatsClient) PublishMsg(m *nats.Msg, opts ...nats.PubOpt) error {
	client.logger.Infof(LogTag, "PublishMsg, for sub:%s", m.Subject)

	var err error
	if opts != nil {
		_, err = client.GetJetStream().PublishMsg(m, opts...)
	} else {
		err = client.GetConnection().PublishMsg(m)
	}

	if err != nil {
		client.logger.Errorf(LogTag, "PublishMsg failed, err:%v", err)
		return fmt.Errorf("%s PublishMsg failed, err:%v", LogTag, err)
	}

	return nil

}

/**
 * @name: PublishRequest
 * @msg: 推送请求
 * @param {*} subj
 * @param {string} reply
 * @param {[]byte} data
 * @return {*}
 */
func (client *NatsClient) PublishRequest(subj, reply string, data []byte) error {
	client.logger.Infof(LogTag, "PublishRequest, for sub:%s and reply:%s", subj, reply)

	err := client.GetConnection().PublishRequest(subj, reply, data)
	if err != nil {
		client.logger.Errorf(LogTag, "PublishRequest failed, err:%v", err)
		return fmt.Errorf("%s PublishRequest failed, err:%v", LogTag, err)
	}

	return nil
}

/**
 * @name: Request
 * @msg: 请求响应结果
 * @param {string} subj
 * @param {[]byte} data
 * @param {time.Duration} timeout
 * @return {*}
 */
func (client *NatsClient) Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error) {
	client.logger.Infof(LogTag, "Request, for sub:%s", subj)

	msg, err := client.GetConnection().Request(subj, data, timeout)
	if err != nil {
		client.logger.Errorf(LogTag, "Request failed, err:%v", err)
		return nil, fmt.Errorf("%s Request failed, err:%v", LogTag, err)
	}

	return msg, nil
}

/**
 * @name: ChanSubscribe
 * @msg: 管道订阅消息
 * @param {string} subj
 * @param {chan*nats.Msg} ch
 * @param {...nats.SubOpt} opts
 * @return {*}
 */
func (client *NatsClient) ChanSubscribe(subj string, ch chan *nats.Msg, opts ...nats.SubOpt) (sub *nats.Subscription, err error) {
	client.logger.Infof(LogTag, "ChanSubscribe, for sub:%s", subj)

	if opts != nil {
		sub, err = client.GetJetStream().ChanSubscribe(subj, ch, opts...)
	} else {
		sub, err = client.GetConnection().ChanSubscribe(subj, ch)
	}

	if err != nil {
		client.logger.Errorf(LogTag, "ChanSubscribe failed, err:%v", err)
		return nil, fmt.Errorf("%s ChanSubscribe failed, err:%v", LogTag, err)
	}

	return sub, nil
}

/**
 * @name: QueueSubscribe
 * @msg: 队列订阅消息
 * @param {*NatsClient} client
 * @return {*}
 */
func (client *NatsClient) QueueSubscribe(subj, queue string,
	cb nats.MsgHandler, opts ...nats.SubOpt) (sub *nats.Subscription, err error) {
	client.logger.Infof(LogTag, "QueueSubscribe, for sub:%s and queue:%s", subj, queue)

	if opts != nil {
		sub, err = client.GetJetStream().QueueSubscribe(subj, queue, cb, opts...)
	} else {
		sub, err = client.GetConnection().QueueSubscribe(subj, queue, cb)
	}

	if err != nil {
		client.logger.Errorf(LogTag, "QueueSubscribe failed, err:%v", err)
		return nil, fmt.Errorf("%s QueueSubscribe failed, err:%v", LogTag, err)
	}

	return sub, nil
}

/**
 * @name: Subscribe
 * @msg: 订阅消息
 * @param {*NatsClient} client
 * @return {*}
 */
func (client *NatsClient) Subscribe(
	subj string, handler nats.MsgHandler, opts ...nats.SubOpt) (sub *nats.Subscription, err error) {
	client.logger.Infof(LogTag, "Subscribe, for sub:%s", subj)

	if opts != nil {
		sub, err = client.GetJetStream().Subscribe(subj, handler, opts...)
	} else {
		sub, err = client.GetConnection().Subscribe(subj, handler)
	}
	if err != nil {
		client.logger.Errorf(LogTag, "Subscribe failed, err:%v", err)
		return nil, fmt.Errorf("%s Subscribe failed, err:%v", LogTag, err)
	}

	return sub, nil
}

/**
 * @name: SubscribeSync
 * @msg: 同步订阅消息
 * @param {string} subj
 * @param {...nats.SubOpt} opts
 * @return {*}
 */
func (client *NatsClient) SubscribeSync(subj string, opts ...nats.SubOpt) (sub *nats.Subscription, err error) {
	client.logger.Infof(LogTag, "SubscribeSync, for sub:%s", subj)

	if opts != nil {
		sub, err = client.GetJetStream().SubscribeSync(subj, opts...)
	} else {
		sub, err = client.GetConnection().SubscribeSync(subj)
	}

	if err != nil {
		client.logger.Errorf(LogTag, "SubscribeSync failed, err:%v", err)
		return nil, fmt.Errorf("%s SubscribeSync failed, err:%v", LogTag, err)
	}

	return sub, nil
}

/**
 * @name: Unsubscribe
 * @msg: 取消订阅
 * @param {*nats.Subscription} sub
 * @return {*}
 */
func (client *NatsClient) Unsubscribe(sub *nats.Subscription) error {
	client.logger.Infof(LogTag, "Unsubscribe, for sub:%s", sub.Subject)

	err := sub.Unsubscribe()
	if err != nil {
		client.logger.Errorf(LogTag, "Unsubscribe err:%v", err)
		return fmt.Errorf("%s Unsubscribe failed, err:%v", LogTag, err)
	}

	return nil
}
