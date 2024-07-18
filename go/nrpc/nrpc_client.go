/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc主要调用API
 */

package nrpc

import (
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

const (
	LogTagClient = "nrpclient"
)

type QosType int8

// 定义消息的qos
const (
	Qos0 QosType = 0 //不保证消息不丢失
	Qos1 QosType = 1 //保证消息不丢失，但不保证消息唯一
	Qos2 QosType = 2 //保证消息不丢失，且保证消息唯一
)

type RequestOpt struct {
	Qos     QosType       //消息的qos
	MsgId   string        //消息唯一ID(只有qos2需要指定，其他情况丢弃)
	Timeout time.Duration //消息的过期时间
}

func RequestDefaultOpt() *RequestOpt {
	return &RequestOpt{
		Qos:     Qos0,
		MsgId:   "",
		Timeout: 10 * time.Second,
	}
}

type NRPCClient struct {
	NRpc *NRPC
}

func NewNRPCClient(name, subject, encoding string, logger Logger, nc NatsClient) *NRPCClient {
	CheckEncode(encoding)
	CheckSubject(subject)
	return &NRPCClient{
		NRpc: NewNPRC(name, subject, encoding, nc, logger),
	}
}

/**
 * @name: CallSync
 * @msg: 同步调用
 * @param {proto.Message} req
 * @param {proto.Message} rsp
 * @param {*RequestOpt} opt
 * @return {error}
 */
func (client *NRPCClient) CallSync(req proto.Message, rsp proto.Message, opt *RequestOpt) error {
	// encode request
	rawRequest, err := Marshal(client.NRpc.encoding, req)
	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "Marshal err: %v", err)
		return fmt.Errorf("CallSync failed, Marshal err: %v", err)
	}

	subject := client.NRpc.subject + "." + strings.ToLower(GetCallerFuncName())
	stream, _ := client.checkRequestOpt(opt, subject)
	client.NRpc.logger.Infof(LogTagClient, "handler the subject:%s qos:%d timeout:%d msgid:%s",
		subject, opt.Qos, opt.Timeout/time.Millisecond, opt.MsgId)

	if opt.Qos == Qos0 {
		reply, err := client.NRpc.nc.Request(subject, rawRequest, opt.Timeout)
		if err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "Request err: %v", err)
			return fmt.Errorf("Request err: %v", err)
		}

		if err := UnmarshalResponse(client.NRpc.encoding, reply.Data, rsp); err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "response unmarshal err:%v", err)
			return fmt.Errorf("CallSync failed, UnmarshalResponse err: %v", err)
		}
	} else {
		replyChan, err := client.request(subject, stream, rawRequest, opt)
		if err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "request err:%v", err)
			return fmt.Errorf("Call failed, request err: %v", err)
		}

		select {
		case msg := <-replyChan:
			client.NRpc.logger.Infof(LogTagClient, "response for subject:%s len:%d", msg.Subject, len(msg.Data))
			if err := UnmarshalResponse(client.NRpc.encoding, msg.Data, rsp); err != nil {
				client.NRpc.logger.Errorf(LogTagClient, "response for subject:%s unmarshal err:%v", msg.Subject, err)
				return fmt.Errorf("CallSync failed, response for subject:%s unmarshal err:%v", msg.Subject, err)
			}
		case <-time.After(opt.Timeout):
			client.NRpc.logger.Errorf(LogTagClient, "reply timeout")
			return fmt.Errorf("CallSync failed, err: timeout")
		}
	}

	return nil
}

type ResponseCallback func(*Response, error)

/**
 * @name: Call
 * @msg: 异步调用
 * @param {proto.Message} req
 * @param {ResponseCallback} cb
 * @param {*RequestOpt} opt
 * @return {error}
 */
func (client *NRPCClient) Call(req proto.Message, cb ResponseCallback, opt *RequestOpt) error {
	// encode request
	rawRequest, err := Marshal(client.NRpc.encoding, req)
	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "Marshal err: %v", err)
		return fmt.Errorf("Call failed, Marshal err: %v", err)
	}

	reply := nats.NewInbox()
	replyChan := make(chan *nats.Msg)
	//服务名称转化为小写，避免不同语言的命名差异
	subject := client.NRpc.subject + "." + strings.ToLower(GetCallerFuncName())

	client.NRpc.logger.Infof(LogTagClient, "handler the subject:%s qos:%d timeout:%d msgid:%s",
		subject, opt.Qos, opt.Timeout/time.Millisecond, opt.MsgId)

	//检查opt是否合法并修改为合法的opt
	stream, _ := client.checkRequestOpt(opt, subject)
	if opt.Qos == Qos0 {
		_, err := client.NRpc.nc.ChanSubscribe(reply, replyChan)
		if err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "ChanSubscribe err:%v", err)
			return fmt.Errorf("Call failed, ChanSubscribe err: %v", err)
		}
		if err := client.NRpc.nc.PublishRequest(subject, reply, rawRequest); err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "PublishRequest err: %v", err)
			return fmt.Errorf("Call failed, PublishRequest err: %v", err)
		}
	} else {
		replyChan, err = client.request(subject, stream, rawRequest, opt)
		if err != nil {
			client.NRpc.logger.Errorf(LogTagClient, "request err:%v", err)
			return fmt.Errorf("Call failed, request err: %v", err)
		}
	}

	go func() {
		for {
			select {
			case msg := <-replyChan:
				client.NRpc.logger.Errorf(LogTagClient, "reply msg for subject:%s reply:%s len:%d",
					msg.Subject, msg.Reply, len(msg.Data))
				//若推送消息为stream会存在PubAck，这里需要去除
				data := msg.Data
				reply := string(data)
				if strings.Contains(reply, "\"stream\"") || strings.Contains(reply, "+ACK") ||
					strings.Contains(reply, "+WPI") {
					client.NRpc.logger.Errorf(LogTagClient,
						"reply for subject %s is the PubAck message or +ACK, maybe the stream message, to drop",
						msg.Subject)
					continue
				}

				var rep Response
				if err := UnmarshalResponse(client.NRpc.encoding, data, &rep); err != nil {
					client.NRpc.logger.Errorf(LogTagClient, "response unmarshal err:%v", err)
					cb(nil, err)
					return
				}
				cb(&rep, nil)
				return
			case <-time.After(opt.Timeout):
				cb(nil, nats.ErrTimeout)
			}
		}
	}()
	return nil
}

/**
 * @name: Notify
 * @msg: 广播消息不接收处理响应
 * @param {proto.Message} req
 * @param {string} subject
 * @return {error}
 */
func (client *NRPCClient) Notify(req proto.Message, subject string) error {
	// encode request
	rawRequest, err := Marshal(client.NRpc.encoding, req)
	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "Marshal err: %v", err)
		return fmt.Errorf("Notify failed, Marshal err: %v", err)
	}

	subject = subject + "." + GetCallerFuncName()
	err = client.NRpc.nc.Publish(subject, rawRequest)
	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "Notify failed, Publish err: %v", err)
		return fmt.Errorf("Notify failed, Publish err: %v", err)
	}

	return nil
}

func (client *NRPCClient) request(subject, stream string, data []byte, opt *RequestOpt) (chan *nats.Msg, error) {
	// 定义响应回复的主题为添加后缀.reply 来处理
	replyChan := make(chan *nats.Msg)
	reply := nats.NewInbox() + "." + subject
	client.NRpc.logger.Infof(LogTagServer, "request for subject %s by %s", subject, reply)
	_, err := client.NRpc.nc.ChanSubscribe(reply, replyChan)
	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "ChanSubscribe err:%v", err)
		return replyChan, fmt.Errorf("Request failed, ChanSubscribe err: %v", err)
	}

	client.NRpc.logger.Infof(LogTagServer, "publish the msg for the subject:%s", subject)
	ackWait := opt.Timeout / 2
	opts := []nats.PubOpt{
		nats.ExpectStream(stream),
		nats.AckWait(ackWait),
		nats.RetryWait(ackWait / 3),
		nats.RetryAttempts(3),
	}
	if opt.Qos == Qos2 && opt.MsgId != "" {
		opts = append(opts, nats.MsgId(opt.MsgId))
	}

	err = client.NRpc.nc.PublishMsg(&nats.Msg{
		Subject: subject,
		Header: nats.Header{
			ExpectedReplySujectHdr: []string{reply}, //通过header通过响应主题
		},
		Data: data},
		opts...)

	if err != nil {
		client.NRpc.logger.Errorf(LogTagClient, "pushlish err: %v", err)
		return replyChan, fmt.Errorf("Request failed, pushlish err: %v", err)
	}

	return replyChan, nil
}

/**
 * @name: checkRequestOpt
 * @msg: 校验请求选项并修正qos
 * @param {*RequestOpt} opt
 * @param {string} subject
 * @return {string, error}
 */
func (client *NRPCClient) checkRequestOpt(opt *RequestOpt, subject string) (string, error) {
	if opt == nil {
		opt = RequestDefaultOpt()
	}

	if opt.Timeout == 0 {
		client.NRpc.logger.Panicf(LogTagClient, "invalid timeou(0)")
	}

	stream, err := client.NRpc.nc.StreamNameBySubject(subject)
	//判定订阅主题是否存在stream，若订阅主题包含了stream则需要使用Qos1/2
	if opt.Qos == Qos0 {
		if err != nil {
			client.NRpc.logger.Warnf(LogTagClient,
				"lookup the stream for subject %s, by StreamNameBySubject err:%v",
				subject, err)
			return "", err
		} else {
			if opt.MsgId != "" {
				opt.Qos = Qos2
			} else {
				opt.Qos = Qos1
			}
		}
	}

	return stream, nil
}
