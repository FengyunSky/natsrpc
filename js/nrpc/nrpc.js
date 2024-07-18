/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc主要调用基础API
 */

const Assert = require('assert');
const String = require('string');
const Is = require('is');
const Util = require('util');
const Nats = require('nats');
const NatsClient = require('../nats/natsclient/nats_client');
const Proto = require('./nrpc.pb');
const Response = Proto.nrpc.Response;
const Request = Proto.nrpc.Request;
const ResponseResult = Proto.nrpc.ResponseResult;

const EncodeType = {
    String: 'string',
    Json: 'json',
    Protobuf: 'protobuf'
}

const EncodeType1 = {
    Request: 0, //请求
    Response: 1,//响应
}

const ExpectedReplySujectHdr = "Nats-Expented-Reply-Subject";
const ExpectedStreamHdr      = "Nats-Expected-Stream";
const MsgIdHdr               = "Nats-Msg-Id";
const ExpectedLastSeqHdr     = "Nats-Expected-Last-Sequence";
const ExpectedLastSubjSeqHdr = "Nats-Expected-Last-Subject-Sequence";
const ExpectedLastMsgIdHdr   = "Nats-Expected-Last-Msg-Id";
const MsgRollup              = "Nats-Rollup";

class NRPC {
    static StringCode = Nats.StringCodec();
    static JsonCode = Nats.JSONCodec();

    constructor(name, subject, client, encodeType = EncodeType.Json) {
        Assert(Is.string(name) && name != '', 'nrpc constructor failed, invalid name');
        Assert(Is.string(subject) && subject != '', 'nrpc constructor failed, invalid subject');
        Assert(client instanceof NatsClient.NatsClient, 'nrpc constructor failed, invalid client');
        Assert(encodeType === EncodeType.String || encodeType === EncodeType.Json || 
                encodeType === EncodeType.Protobuf, 'nrpc constructor failed, invalid encoding');

        this.name = name;
        this.subject = subject;
        this.client = client;
        this.encodeType = encodeType;
    }

    /**
     * @name: bindStream
     * @msg: 绑定stream
     * @param {StreamConfig} streamCfg
     * @return {Promise(StreamInfo)}
     * @abstract 添加失败可能是已经添加忽略错误
     */    
    async bindStream(streamCfg) {
        Assert(Is.object(streamCfg), 'bindStream failed, invalid streamCfg, not object');
        Assert(Is.array(streamCfg.subjects), 'bindStream failed, invalid streamCfg for subjects, not array');

        let isvalid = false;
        streamCfg.subjects.forEach(subject => {
            if (String(subject).startsWith(this.subject)) {
                isvalid = true;
                return;
            }
        });

        if (!isvalid) {
            Assert(false, Util.format('bindStream failed, invalid streamCfg for subjects, not start with %s', this.subject));
        }

        try {
            const streamInfo = await this.client.addStream(streamCfg);
            return Promise.resolve(streamInfo);
        } catch (err) {
            console.log(Util.format('bindStream failed, addStream err:%s', err.message));
            return Promise.resolve();
        }
    }

    /**
     * @name: bindConsumer
     * @msg: 绑定消费者
     * @param {string} stream
     * @param {ConsumerConfig} consumerCfg
     * @return {Promise(ConsumerInfo)}
     */    
    async bindConsumer(stream, consumerCfg) {
        Assert(Is.string(stream) && stream != '', 'bindConsumer failed, invalid stream');
        return this.client.addConsumer(stream, consumerCfg);
    }

    /**
     * @name: checkStreamWithSubject
     * @msg: 检查是否主题匹配指定的stream
     * @param {string} stream
     * @param {string} subject
     * @return {Promise}
     */    
    async checkStreamWithSubject(stream, subject) {
        Assert(Is.string(stream), 'checkStreamWithSubject failed, invalid stream');
        Assert(Is.string(subject) && subject != '', 'checkStreamWithSubject failed, invalid subject');

        try {
            const name = await this.client.streamNameBySubject(subject);
            if (name !== stream) {
                return Promise.reject(new Error(Util.format('checkStreamWithSubject failed, not found stream:%s for subject:%s',
                                    stream, subject)));
            }
        } catch (err) {
            return Promise.reject(new Error(Util.format('checkStreamWithSubject failed, streamNameBySubject failed, err:%s',
                                    err.message)));
        }
        
        return Promise.resolve();
    }

    /**
     * @name: checkConsumerAndStreamWithSubject
     * @msg: 检查stream与消费者及主题是否匹配
     * @param {string} stream
     * @param {string} consumer
     * @param {string} subject
     * @return {Promise}
     */    
    async checkConsumerAndStreamWithSubject(stream, consumer, subject) {
        Assert(Is.string(stream), 'checkConsumerAndStreamWithSubject failed, invalid stream');
        Assert(Is.string(consumer), 'checkConsumerAndStreamWithSubject failed, invalid consumer');
        Assert(Is.string(subject) && subject != '', 'checkConsumerAndStreamWithSubject failed, invalid subject');

        Assert(stream == '' && consumer != '', 'checkConsumerAndStreamWithSubject failed, invalid empty stream for consumer');

        try {
            const name = await this.client.streamNameBySubject(subject);
            if (name != stream) {
                return Promise.reject(new Error(Util.format('checkConsumerAndStreamWithSubject failed, the stream(%s) for subject(%s) not match the input stream(%s)',
                        stream, subject, name)));
            }
        } catch (err) {
            if (stream != '') {
                return Promise.reject(new Error(Util.format('checkConsumerAndStreamWithSubject failed, not found the stream(%s) for subject(%s)',
                        stream, subject)));
            }
            
            return Promise.resolve();
        }

        try {
            const consumerInfo = await this.client.consumerInfoByStream(stream, consumer);
            if (!String(consumerInfo.config.filter_subject).startsWith(this.subject)) {
                return Promise.reject(
                    new Error(Util.format('checkConsumerAndStreamWithSubject failed, the filter subjectes(%s) of consumer(%s), not match the subject:%s"',
                                consumerInfo.config.filter_subject, consumer, subject)));
            }
        } catch (err) {
            return Promise.reject( 
                new Error(Util.format('checkConsumerAndStreamWithSubject failed, the consumer(%s) not found for stream(%s), ConsumerInfoByStream err:%s',
                consumer, stream, err.message)));
        }
    }

    /**
     * @name: checkSubject
     * @msg: 校验主题
     * @param {string} subject
     * @return {void}
     */    
    checkSubject(subject) {
        Assert(Is.string(subject) && subject !== '' && 
        (String(subject).contains('.*') || String(subject).contains('.>')),
        Util.format('checkSubject failed, invalid subject(%s), must not empty or .* or .>', subject));
    }

    /**
     * @name: encode
     * @msg: 序列化编码
     * @param {EncodeType} encodeType
     * @param {string|Uint8Array} data
     * @param {EncodeType1} type
     * @return {Uint8Array}
     */    
    static encode(encodeType, data, type=EncodeType1.Response) {
        switch(encodeType) {
        case (EncodeType.String):
            return NRPC.StringCode.encode(data);
        case (EncodeType.Json):
            return NRPC.JsonCode.encode(data);
        case (EncodeType.Protobuf):
            if (type == EncodeType1.Request) {
                return Request.encode(data).finish();
            }
            return Response.encode(data).finish();
        default:
            Assert(false, Util.format('encode failed, invalid encode:%s', encodeType));
        }
    }

    /**
     * @name: decode
     * @msg: 序列化解码
     * @param {EncodeType} encodeType
     * @param {Uint8Array} data
     * @param {EncodeType1} type
     * @return {*}
     */    
    static decode(encodeType, data, type=EncodeType1.Response) {
        switch(encodeType) {
        case (EncodeType.String):
            return NRPC.StringCode.decode(data);
        case (EncodeType.Json):
            return NRPC.JsonCode.decode(data);
        case (EncodeType.Protobuf):
            if (type == EncodeType1.Request) {
                return Request.decode(data);
            }
            return Response.decode(data);
        default:
            Assert(false, Util.format('decode failed, invalid encode:%s', encodeType));
        }
    }

    /**
     * @name: getCallerFuncName
     * @msg: 获取调用者函数名称
     * @return {string}
     */    
    static getCallerFuncName() {
        const stackArray = (new Error().stack).split('\n');
        const funMathcer = /([^(]+)@|at ([^(]+) \(/;
        const regexResult = funMathcer.exec(stackArray[3]);
        const callerName = regexResult[1] || regexResult[2];
        return callerName.split('.')[1];
    }

    /**
     * @name: getCurrentFuncName
     * @msg: 获取当前函数的名称
     * @return {string}
     */    
    static getCurrentFuncName() {
        const stackArray = (new Error().stack).split('\n');
        const funMathcer = /([^(]+)@|at ([^(]+) \(/;
        const regexResult = funMathcer.exec(stackArray[2]);
        const callerName = regexResult[1] || regexResult[2];
        return callerName.split('.')[1];
    }

    /**
     * @name: getFunArgsName
     * @msg: 获取函数参数名称数组
     * @param {function} func 函数
     * @return {Array} 函数参数名称数组
     */    
    static getFunArgsName(func) {
        Assert(Is.function(func), 'getFunArgsName failed, invalid func');
        // 先用正则匹配,取得符合参数模式的字符串.
        // 第一个分组是这个:  ([^)]*) 非右括号的任意字符
        var args = func.toString().match(/\(\s*([^)]+?)\s*\)/)[1];
        // 用逗号来分隔参数(arguments string).
        return args.split(",").map(function(arg) {
            // 去除注释(inline comments)以及空格
            return arg.replace(/\/\*.*\*\//, "").trim();
        }).filter(function(arg) {
            // 确保没有 undefined.
            return arg;
        });
      }

    /**
     * @name: response
     * @msg: 创建响应对象
     * @param {ResponseResult} result 响应结果
     * @param {string} message 响应结果描述
     * @param {array} args 响应结果参数列表
     * @return {Response}
     */    
    static response(result, message='', ...args) {
        Assert(Is.string(message), 'response failed, invalid message');
        //组装数据对象
        let data = {};
        for(let index = 0; index < arguments.length-2; index++) {
            data[index] = arguments[index+2];
        }

        let response = Response.create();
        response.result = result;
        response.message = message;
        response.data = Buffer.from(JSON.stringify(data));
        return response;
    }

    /**
     * @name: request
     * @msg: 创建reqeust请求对象
     * @param {Array[string]} names 调用函数的参数名称列表
     * @param {array} args 调用函数的参数列表
     * @return {Request}
     */    
    static request(names, ...args) {
        Assert(Is.array(names), 'request failed, invalid names, not the array');
        //组装数据对象
        let data = {};
        for(let index = 0; index < arguments.length-1; index++) {
            data[names[index]] = arguments[index+1];
        }

        let request = Request.create();
        request.name = NRPC.getCallerFuncName();
        request.data = Buffer.from(JSON.stringify(data));
        return request;
    }
      
}

module.exports = {
    NRPC,
    Request,
    Response,
    ResponseResult,
    EncodeType,
    EncodeType1,
    ExpectedReplySujectHdr,
    ExpectedStreamHdr,
}