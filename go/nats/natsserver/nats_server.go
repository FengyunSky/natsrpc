/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: nats服务端broker并处理客户端的连接/断开事件分发
 */
package natsserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"nrpctest/nats/natsclient"
	"nrpctest/nrpc"
	"strings"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

const (
	natsServerReadyTimeout = 10 * time.Second
	logTag                 = "eventbus"
	UserName               = "test"
	PassWord               = "test123"
)

const (
	AccountUser         = "test"
	AccountUserUserName = "test"
	AccountUserPassword = "test123"
	AccountSys          = "TEST"
	AccountSysUserName  = "test"
	AccountSysPassword  = "test123"
)

const (
	SysAccountEventsSubject    = "$SYS.ACCOUNT." + AccountUser + ".*" //只监听特定账号的事件
	SysAccountEventsConnect    = "CONNECT"
	SysAccountEventsDisconenct = "DISCONNECT"
)

const (
	ClientPrefixName         = "test-"
	ClientSuffixConnectEvent = "-connected"
	ClientSuffixDeadEvent    = "-dead"
)

type ClientInfo struct {
	name  string
	event string
	info  []byte
}

func NewClientInfo(name, event string, info []byte) *ClientInfo {
	return &ClientInfo{
		name:  name,
		event: event,
		info:  info,
	}
}

type NatsMonitorClient struct {
	testclient *natsclient.NatsClient
	sysclient  *natsclient.NatsClient
	sub        *nats.Subscription
}

type NatsServer struct {
	username string
	password string
	logger   nrpc.Logger
	server   *server.Server
	client   *NatsMonitorClient
}

func NewNatsServer(username, password string, logger nrpc.Logger) *NatsServer {
	return &NatsServer{
		username: username,
		password: password,
		logger:   logger,
	}
}

func (natsserver *NatsServer) Start() error {
	natsserver.logger.Infof("natsserver start...")
	defer natsserver.logger.Infof("natsserver start done")
	err := natsserver.StartServer()
	if err != nil {
		return err
	}
	err = natsserver.StartMonitorClient()
	if err != nil {
		return err
	}

	return nil
}

func (natsserver *NatsServer) Stop() error {
	err := natsserver.StopServer()
	if err != nil {
		return err
	}
	err = natsserver.StopMonitorClient()
	if err != nil {
		return err
	}

	return nil
}

// 启动账号jetstream
func (natsserver *NatsServer) settupAccoutJetstream() error {
	accUser, err := natsserver.server.LookupAccount(AccountUser)
	if err != nil {
		return err
	}
	_, err = natsserver.server.LookupAccount(AccountSys)
	if err != nil {
		return err
	}
	limits := map[string]server.JetStreamAccountLimits{
		"": {
			MaxConsumers: -1,
			MaxStreams:   -1,
			MaxMemory:    1024 * 1024,
			MaxStore:     10 * 1024 * 1024,
		},
	}
	err = accUser.EnableJetStream(limits)
	if err != nil {
		return err
	}

	return nil
}

func (natsserver *NatsServer) StartServer() error {
	if natsserver.server != nil && natsserver.server.Running() {
		natsserver.logger.Warnf("[%s]the server is running", logTag)
		return nil
	}

	opts := &server.Options{
		Host:               "127.0.0.1",
		Port:               server.DEFAULT_PORT,
		Username:           natsserver.username,
		Password:           natsserver.password,
		ServerName:         "eventbus",
		Debug:              true,
		Trace:              true,
		TraceVerbose:       true,
		SystemAccount:      AccountSys,
		NoLog:              false,
		Logtime:            true,
		LogFile:            "/tmp/test/eventbus.log",
		Syslog:             true,
		LogSizeLimit:       10 * 1024 * 1024,              //10MB
		ProfPort:           -1,                            //随机端口
		WriteDeadline:      server.DEFAULT_FLUSH_DEADLINE, //flush截止时间
		HTTPPort:           -1,                            //随机端口
		MaxConn:            10,                            //最大允许连接数，限制没必要过多的连接
		JetStream:          true,
		JetStreamMaxMemory: 10 * 1024 * 1024,  //10MB
		JetStreamMaxStore:  100 * 1024 * 1024, //100MB
		JetStreamCipher:    server.AES,
		JetStreamKey:       "n6OD3+Disw2NL9pAVSzZGZfI7kHw/H7U", //随机生成密钥
		StoreDir:           "/tmp/test",                        //TODO
	}
	accountUser := server.NewAccount(AccountUser)
	accountSys := server.NewAccount(AccountSys)
	opts.Accounts = append(opts.Accounts, accountUser, accountSys)
	opts.Users = append(opts.Users,
		&server.User{Username: AccountUserUserName, Password: AccountUserPassword, Account: accountUser},
		&server.User{Username: AccountSysUserName, Password: AccountSysPassword, Account: accountSys})

	svr, err := server.NewServer(opts)
	if err != nil {
		return err
	}

	// Configure the logger based on the flags
	svr.ConfigureLogger()

	natsserver.server = svr
	svr.Start()

	if !svr.ReadyForConnections(natsServerReadyTimeout) {
		return errors.New("nats server is not ready")
	}

	err = natsserver.settupAccoutJetstream()
	if err != nil {
		return fmt.Errorf("nats server account enable jetsteam failed, err:%v", err)
	}

	return nil
}

func (natsserver *NatsServer) StopServer() error {
	if natsserver.server != nil && natsserver.server.Running() {
		natsserver.server.Shutdown()
		natsserver.server = nil
	} else {
		return errors.New("nats server is not running")
	}

	return nil
}

/**
 * @name: StartMonitorClient
 * @msg: 启动客户端连接/断开事件监听并广播
 * @return {*}
 */
func (natsserver *NatsServer) StartMonitorClient() error {
	natsserver.logger.Infof("natsserver start to monitor the clientes...")
	defer natsserver.logger.Infof("natsserver start to monitor the clientes done")
	syscfg := natsclient.NewNatsConfig("test-client-monitor",
		natsclient.DefaultServerAddr, AccountSysUserName, AccountSysPassword, 3, 10)
	sysclient, err := natsclient.NewNatsClient(syscfg, natsserver.logger)
	if err != nil {
		natsserver.logger.Warnf("natsclient connect failed, err:%v\n", err)
		return err
	}

	natsserver.logger.Infof("create test-client-event")
	usercfg := natsclient.NewNatsConfig("test-client-event",
		natsclient.DefaultServerAddr, AccountUserUserName, AccountUserPassword, 3, 10)
	natsserver.logger.Infof("create test-client-event, config:%v", usercfg)
	testclient, err := natsclient.NewNatsClient(usercfg, natsserver.logger)
	if err != nil {
		natsserver.logger.Warnf("natsclient connect failed, err:%v\n", err)
		return err
	}

	natsserver.client = &NatsMonitorClient{
		testclient: testclient,
		sysclient:  sysclient,
	}

	sub, err := sysclient.Subscribe(SysAccountEventsSubject, func(msg *nats.Msg) {
		natsserver.logger.Infof("recv client event, for subject:%s", msg.Subject)
		if strings.HasSuffix(msg.Subject, SysAccountEventsDisconenct) {
			natsserver.logger.Infof("handler the client disconnect event...")
			defer natsserver.logger.Infof("handler the client disconnect event done")
			eventMsg := server.DisconnectEventMsg{}
			if err := json.Unmarshal(msg.Data, &eventMsg); err != nil {
				natsserver.logger.Errorf("Error unmarshalling disconnect event message: %v", err)
				return
			}
			if eventMsg.Type != server.DisconnectEventMsgType {
				natsserver.logger.Warnf("Incorrect schema in disconnect event: %s", eventMsg.Type)
				return
			}

			natsserver.logger.Infof("handler the client(%s) disconnect event", eventMsg.Client.Name)
			clientInfo, err := json.Marshal(eventMsg.Client)
			if err != nil {
				natsserver.logger.Warnf("marshall client info failed, err: %v", err)
				return
			}

			client := NewClientInfo(eventMsg.Client.Name, SysAccountEventsDisconenct, clientInfo)
			natsserver.handlerClientEvent(client)

		} else if strings.HasSuffix(msg.Subject, SysAccountEventsConnect) {
			natsserver.logger.Infof("handler the client connect event...")
			defer natsserver.logger.Infof("handler the client connect event done")
			eventMsg := server.ConnectEventMsg{}
			if err := json.Unmarshal(msg.Data, &eventMsg); err != nil {
				natsserver.logger.Errorf("Error unmarshalling connect event message: %v", err)
				return
			}
			if eventMsg.Type != server.ConnectEventMsgType {
				natsserver.logger.Warnf("Incorrect schema in connect event: %s", eventMsg.Type)
				return
			}

			natsserver.logger.Infof("handler the client(%s) connect event", eventMsg.Client.Name)
			clientInfo, err := json.Marshal(eventMsg.Client)
			if err != nil {
				natsserver.logger.Warnf("marshall client info failed, err: %v", err)
				return
			}

			client := NewClientInfo(eventMsg.Client.Name, SysAccountEventsConnect, clientInfo)
			natsserver.handlerClientEvent(client)
		}
	})
	if err != nil {
		natsserver.logger.Errorf("natsclient subscribe failed, err:%v", err)
		return err
	}

	natsserver.client.sub = sub
	return nil
}

func (natsserver *NatsServer) StopMonitorClient() error {
	if natsserver.client != nil {
		err := natsserver.client.sysclient.Unsubscribe(natsserver.client.sub)
		if err != nil {
			natsserver.logger.Errorf("natsclient unsubscribe failed, err:%v", err)
			return err
		}
	}

	return nil
}

func (natsserver *NatsServer) handlerClientEvent(client *ClientInfo) {
	//客户端名称必须以test-前缀
	if !strings.HasPrefix(client.name, ClientPrefixName) {
		natsserver.logger.Warnf("the client(%s) not prefix with %s, not to handler", client.name, ClientPrefixName)
		return
	}

	sub := client.name
	if client.event == SysAccountEventsConnect {
		sub = sub + ClientSuffixConnectEvent
	} else if client.event == SysAccountEventsDisconenct {
		sub = sub + ClientSuffixDeadEvent
	}

	err := natsserver.client.testclient.Publish(sub, client.info)
	if err != nil {
		natsserver.logger.Warnf("publish the client(%s) event(%s) failed, err:%v", client.event, err)
	}
}
