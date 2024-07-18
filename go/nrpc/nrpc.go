/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc主要调用基础API
 */

package nrpc

import (
	"errors"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	jsonpb "google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	LogTag = "nrpc"
)

const (
	EncodeJson     = "json"
	EncodeProtobuf = "protobuf"
)

const (
	ExpectedReplySujectHdr = "Nats-Expented-Reply-Subject"
)

type Logger interface {
	Verbosef(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Panicf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

type NatsClient interface {
	AddStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error)
	StreamNameBySubject(string, ...nats.JSOpt) (string, error)
	Publish(subj string, data []byte, opts ...nats.PubOpt) error
	PublishMsg(m *nats.Msg, opts ...nats.PubOpt) error
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	PublishRequest(subj, reply string, data []byte) error

	AddConsumer(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error)
	ConsumerInfoByStream(stream, consumer string) (*nats.ConsumerInfo, error)
	ChanSubscribe(subj string, ch chan *nats.Msg, opts ...nats.SubOpt) (*nats.Subscription, error)
	QueueSubscribe(subj, queue string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error)
	Subscribe(subj string, handler nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error)
	SubscribeSync(subj string, opts ...nats.SubOpt) (*nats.Subscription, error)
	Unsubscribe(*nats.Subscription) error
}

type NRPC struct {
	name     string
	subject  string
	encoding string
	nc       NatsClient
	logger   Logger
}

func NewNPRC(name, subject, encoding string, nc NatsClient, logger Logger) *NRPC {
	return &NRPC{
		name:     name,
		subject:  subject,
		encoding: encoding,
		nc:       nc,
		logger:   logger,
	}
}

/**
 * @name: BindStream
 * @msg: 绑定流
 * @param {*nats.StreamConfig} cfg
 * @return {*}
 */
func (nrpc *NRPC) BindStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error) {
	//绑定主题必须包含服务绑定的主题
	validSub := false
	for _, sub := range cfg.Subjects {
		if strings.HasPrefix(sub, nrpc.subject) {
			validSub = true
			break
		}
	}
	if !validSub {
		nrpc.logger.Panicf(LogTag, "BindStream failed, invalid subject")
	}
	return nrpc.nc.AddStream(cfg)
}

/**
 * @name: BindConsumer
 * @msg: 绑定消费者
 * @param {string} stream
 * @param {*nats.ConsumerConfig} cfg
 * @return {*}
 */
func (nrpc *NRPC) BindConsumer(stream string, cfg *nats.ConsumerConfig) (*nats.ConsumerInfo, error) {
	//消费者过滤的主题必须包含服务主题
	if !strings.HasPrefix(cfg.FilterSubject, nrpc.subject) {
		nrpc.logger.Panicf(LogTag, "BindConsumer failed, invalid subject:%s for filetersubject;%s", nrpc.subject, cfg.FilterSubject)
	}

	consumer, err := nrpc.nc.AddConsumer(stream, cfg)
	if err != nil {
		nrpc.logger.Warnf(LogTag, "BindConsumer failed, AddConsumer err:%v", err)
	}

	return consumer, err
}

/**
 * @name: Unmarshal
 * @msg: 解码
 * @param {string} encoding
 * @param {[]byte} data
 * @param {proto.Message} msg
 * @return {*}
 */
func Unmarshal(encoding string, data []byte, msg proto.Message) error {
	switch encoding {
	case EncodeProtobuf:
		return proto.Unmarshal(data, msg)
	case EncodeJson:
		return jsonpb.Unmarshal(data, msg)
	default:
		return errors.New("Invalid encoding: " + encoding)
	}
}

/**
 * @name: UnmarshalResponse
 * @msg: 解码响应
 * @param {string} encoding
 * @param {[]byte} data
 * @param {proto.Message} msg
 * @return {*}
 */
func UnmarshalResponse(encoding string, data []byte, msg proto.Message) error {
	switch encoding {
	case EncodeProtobuf:
		return proto.Unmarshal(data, msg)
	case EncodeJson:
		return jsonpb.Unmarshal(data, msg)
	default:
		return errors.New("Invalid encoding: " + encoding)
	}
}

/**
 * @name: Marshal
 * @msg: 编码
 * @param {string} encoding
 * @param {proto.Message} msg
 * @return {*}
 */
func Marshal(encoding string, msg proto.Message) ([]byte, error) {
	switch encoding {
	case EncodeProtobuf:
		return proto.Marshal(msg)
	case EncodeJson:
		return jsonpb.Marshal(msg)
	default:
		return nil, errors.New("Invalid encoding: " + encoding)
	}
}

/**
 * @name: ParseRequest
 * @msg: 解析请求对象
 * @param {string} encoding 编码方式
 * @param {[]byte} data 解析数据
 * @param {proto.Message} req
 * @return {*}
 */
func ParseRequest(encoding string, data []byte, req proto.Message) {
	err := Unmarshal(encoding, data, req)
	if err != nil {
		log.Panicf(" parse request failed, err:%v \n", err)
	}
}

/**
 * @name: NewResponse
 * @msg: 创建响应对象
 * @param {ResponseResult} result
 * @param {int32} count
 * @param {[]byte} data
 * @return {*}
 */
func NewResponse(result ResponseResult, message string, data []byte) *Response {
	return &Response{
		Result:  result,
		Message: message,
		Data:    data,
	}
}

/**
 * @name: CheckSubject
 * @msg: 校验主题
 * @param {string} subject
 * @return {*}
 */
func CheckSubject(subject string) {
	if subject == "" &&
		(!strings.Contains(subject, ".*") || !strings.Contains(subject, ".>")) {
		log.Panicf(LogTag, "CheckSubject failed, invalid subject, must not empty or .* or .>")
	}
}

/**
 * @name: CheckEncode
 * @msg: 校验编码
 * @param {string} encoding
 * @return {*}
 */
func CheckEncode(encoding string) {
	if encoding != EncodeProtobuf &&
		encoding != EncodeJson {
		log.Panicf(LogTag, "CheckEncode failed, invalid encoding, must be protobuf or json or string")
	}
}

/**
 * @name: CheckStreamWithSubject
 * @msg: 检查是否主题匹配指定的stream
 * @param {*} streamName
 * @param {string} subject
 * @return {*}
 */
func (nrpc *NRPC) CheckStreamWithSubject(streamName, subject string) {
	if subject == "" && streamName == "" {
		nrpc.logger.Panicf(LogTagServer, "CheckStreamWithSubject failed, invalid subject or stream, is empty")
	}

	stream, err := nrpc.nc.StreamNameBySubject(subject)
	if err != nil {
		if streamName != "" {
			nrpc.logger.Errorf(LogTagServer, "CheckStreamWithSubject failed, not found the stream(%s) for subject(%s)",
				streamName, subject)
		}

		return
	}
	if stream != streamName {
		nrpc.logger.Errorf(LogTagServer, "CheckStreamWithSubject failed, the stream(%s) for subject(%s) not match the input stream(%s)",
			stream, subject, streamName)
	}
}

/**
 * @name: CheckConsumerAndStreamWithSubject
 * @msg: 检查stream与消费者及主题是否匹配
 * @param {*} streamName
 * @param {*} consumerName
 * @param {string} subject
 * @return {*}
 */
func (nrpc *NRPC) CheckConsumerAndStreamWithSubject(streamName, consumerName, subject string) {
	if subject == "" {
		nrpc.logger.Panicf(LogTagServer, "CheckConsumerAndStreamWithSubject failed, invalid subject, is empty")
	}

	if streamName == "" && consumerName != "" {
		nrpc.logger.Panicf(LogTagServer, "CheckConsumerAndStreamWithSubject failed, invalid empty stream for consumer")
	}

	stream, err := nrpc.nc.StreamNameBySubject(subject)
	if err != nil {
		if streamName != "" {
			nrpc.logger.Panicf(LogTagServer, "CheckConsumerAndStreamWithSubject failed, not found the stream(%s) for subject(%s)",
				streamName, subject)
		}

		return
	}
	if stream != streamName {
		nrpc.logger.Panicf(LogTagServer, "CheckConsumerAndStreamWithSubject failed, the stream(%s) for subject(%s) not match the input stream(%s)",
			stream, subject, streamName)
	}

	consumer, err := nrpc.nc.ConsumerInfoByStream(streamName, consumerName)
	if err != nil {
		nrpc.logger.Panicf(LogTagServer,
			"CheckConsumerAndStreamWithSubject failed, the consumer(%s) not found for stream(%s), ConsumerInfoByStream err:%v",
			consumerName, streamName, err)
	}

	if !strings.HasPrefix(consumer.Config.FilterSubject, nrpc.subject) {
		nrpc.logger.Panicf(LogTagServer,
			"CheckConsumerAndStreamWithSubject failed, the filter subjectes(%s) of consumer(%s), not match the subject:%s",
			consumer.Config.FilterSubject, consumerName, subject)
	}
}

/**
 * @name: GetCurrentFuncName
 * @msg: 获取正在运行的函数名
 * @return {*}
 */
func GetCurrentFuncName() string {
	pc, _, _, _ := runtime.Caller(1)
	name := runtime.FuncForPC(pc).Name()
	names := strings.Split(name, ".")
	return names[len(names)-1]
}

/**
 * @name: GetCallerFuncName
 * @msg: 获取调用者函数名称
 * @return {*}
 */
func GetCallerFuncName() string {
	// 获取调用者函数的名称
	pc, _, _, _ := runtime.Caller(2)
	name := runtime.FuncForPC(pc).Name()
	names := strings.Split(name, ".")
	return names[len(names)-1]
}
