/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc主要调用API
 */

const Assert = require('assert');
const Is = require('is');
const Util = require('util');
const Nats = require('nats');
const {NRPC, Request, Response} = require('./nrpc');
const {EncodeType, EncodeType1, ExpectedReplySujectHdr} = require('./nrpc');

const Qos = {
    Qos0: 0,//不保证消息不丢失
    Qos1: 1,//保证消息不丢失，但不保证消息唯一
    Qos2: 2,//保证消息不丢失，且保证消息唯一
}
class RequestOpt {
    constructor(qos = Qos.Qos0, msgID = '', timeout = 10*1000) {
        Assert(qos >= Qos.Qos0 && qos <= Qos.Qos2, 'RequestOpt failed, invalid qos');
        Assert(Is.string(msgID), 'RequestOpt failed, invalid msgid');
        Assert(Is.number(timeout), 'RequestOpt failed, invalid timeout');
        this.qos = qos; //消息的qos
        this.msgID = msgID;//消息唯一ID(只有qos2需要指定，其他情况丢弃)
        this.timeout = timeout;//消息的过期时间
    }

    getQos() {
        return this.qos;
    }

    setQos(qos) {
        Assert(qos >= Qos.Qos0 && qos <= Qos.Qos2, 'RequestOpt failed, invalid qos');
        this.qos = qos;
    }

    getMsgID() {
        return this.msgID;
    }

    getTimeout() {
        return this.timeout;
    }
}

class NRPCClient extends NRPC {
    constructor(name, subject, client, encodeType) {
        super(name, subject, client, encodeType);
    }

    /**
     * @name: callSync
     * @msg: 同步调用
     * @param {Request} request
     * @param {RequestOpt} opts
     * @return {Promise(Response)}
     */    
    async callSync(request, opts) {
        Assert(request, 'callSync failed, invalid request');
        Assert(opts instanceof RequestOpt, 'callSync failed, invalid opts');
        let req = NRPC.encode(this.encodeType, request, EncodeType1.Request);
        let subject = this.subject + '.' + NRPC.getCallerFuncName().toLowerCase();
        console.log(Util.format('callSync, handler the subject:%s, qos:%d timeout:%d msgid:%s', 
                        subject, opts.getQos(), opts.getTimeout()/1000, opts.getMsgID()));

        if (opts.getQos() === Qos.Qos0) {
            try {
                let msg = await this.client.request(subject, req, opts.getTimeout());
                let err = Response.verify(msg.data);
                if (err) {
                    return Promise.reject(new Error(Util.format('callSync failed, response verify err:%s', err)));
                }
                let reponse = NRPC.decode(this.encodeType, msg.data);
                return Promise.resolve(reponse);
            } catch (err) {
                return Promise.reject(new Error(Util.format('callSync failed, request err:%s', err.message)));
            }
        } else {
            let time = setTimeout(function(){
                return Promise.reject(new Error('callSync failed, err: timeout'));
            }, opts.getTimeout());

            try {
                let stream = await this._checkRequestOpt(opts, subject);
                let msg = await this._request(subject, stream, req, opts);

                clearTimeout(time);
                let err = Response.verify(msg.data);
                if (err) {
                    return Promise.reject(new Error(Util.format('callSync failed, response verify err:%s', err)));
                }
                let reponse = NRPC.decode(this.encodeType, msg.data);
                return Promise.resolve(reponse);
            } catch (err) {
                clearTimeout(time);
                return Promise.reject(new Error(Util.format('callSync failed, request err:%s', err.message)));
            }
            
        }
    }

    /**
     * @name: call
     * @msg: 异步调用
     * @param {string|Uint8Array} request
     * @param {RequestOpt} opts
     * @param {function(Error, Response)} callback
     * @return {Promise}
     */    
    async call(request, opts, callback) {
        Assert(request, 'call failed, invalid request');
        Assert(Is.function(callback), 'call failed, invalid callback');
        Assert(opts instanceof RequestOpt, 'call failed, invalid opts');

        let req = NRPC.encode(this.encodeType, request, EncodeType1.Request);
        let subject = this.subject + '.' + request.name.toLowerCase();
        console.log(Util.format('call, handler the subject:%s, qos:%d timeout:%d msgid:%s', 
                        subject, opts.getQos(), opts.getTimeout()/1000, opts.getMsgID()));

        let time = setTimeout(function(){
            callback(new Error('call failed, err: timeout'), null);
        }, opts.getTimeout());

        let that = this;
        let handler = function(err, msg, callback) {
            if (err) {
                console.log(Util.format('[call]recv subject:%s failed, err:%s', reply, err.message));
                return;
            }
            let errMsg = Response.verify(msg.data);
            if (errMsg) {
                callback(new Error(Util.format('call failed, response verify err:%s', errMsg)), null);
                return;
            }
            let reponse = NRPC.decode(that.encodeType, msg.data);
            callback(null, reponse);
        };
        
        if (opts.getQos() === Qos.Qos0) {
            try {
                let reply = Nats.createInbox() + '.' + subject;
                await this.client.subscribe(reply, function(err, msg){
                    clearTimeout(time);
                    handler(err, msg, callback);
                })
                this.client.publishRequest(subject, reply, req);
            } catch (err) {
                return Promise.reject(new Error(Util.format('call failed, subscribe err:%s', err.message)));
            }
        } else {
            let stream = await this._checkRequestOpt(opts, subject);
            this._request(subject, stream, req, opts).then((msg)=>{
                clearTimeout(time);
                handler(null, msg, callback);
            }).catch((err)=>{
                clearTimeout(time);
                return Promise.reject(new Error(Util.format('callSync failed, request err:%s', err.message)));
            });
        }
    }

    /**
     * @name: notify
     * @msg: 广播消息不接收处理响应
     * @param {Request} request
     * @return {*}
     */    
    async notify(request) {
        Assert(request, 'call failed, invalid request');
        let req = NRPC.encode(this.encodeType, request, EncodeType1.Request);
        let subj = this.subject + '.' + NRPC.getCallerFuncName();

        try {
            await this.client.publish(subj, req);
            return Promise.resolve();
        } catch (err) {
            return Promise.reject(new Error(Util.format('notify failed, err:%s', err.message)));
        }
    }

    /**
     * @name: _checkRequestOpt
     * @msg: 检查请求配置并修正qos
     * @param {RequestOpt} opt
     * @param {string} subject
     * @return {Promise}
     */    
    async _checkRequestOpt(opt, subject) {
        if (!opt) {
            opt = new RequestOpt();
        }

        Assert(opt.timeout !== 0, '_checkRequestOpt failed, invalid timeout');

        try {
            let stream = await this.client.streamNameBySubject(subject);
            if (opt.getQos() == Qos.Qos0 && opt.getMsgID() != '') {
                opt.setQos(Qos.Qos2);
            } else {
                opt.setQos(Qos.Qos1);
            }
            return Promise.resolve(stream);
        } catch (err) {
            if (opt.getQos() !== Qos.Qos0 ) {
                opt.setQos(Qos.Qos0);
            }
        }

        return Promise.resolve('');
    }

    /**
     * @name: _request
     * @msg: 请求响应消息
     * @param {string} subject
     * @param {string} stream
     * @param {Uint8Array} data
     * @param {RequestOpt} opts
     * @return {Promise}
     */    
    async _request(subject, stream, data, opts) {
        let reply = Nats.createInbox() + '.' + subject;
        console.log(Util.format('request for subject:%s by %s', subject, reply));
        try {
            //订阅接收的响应消息主题
            let sub = await this.client.subscribe(reply, null, null, opts.timeout);
            //发送主题消息
            //TODO JetStreamPublishOptions不存在ackwait选项
            let pubOpts = {
                expect: {
                    streamName: stream,
                }
            };
            if (opts.getQos() === Qos.Qos2 && opts.getMsgID() != '') {
                pubOpts.msgID = opts.getMsgID();
            }

            let msg = {
                subject: subject,
                data: data,
                header: Nats.headers(),
            };
            msg.header.set(ExpectedReplySujectHdr, reply);
            await this.client.publishMsg(msg, pubOpts);
            
            //接收响应
            for await (const m of sub) {
                console.log(Util.format('_request, recv subject:%s, msg:%s', reply, m.subject));
                if (!m.data) {
                    console.log(Util.format('_request, subscribe msg data null, to continue'));
                    continue;
                }
                return Promise.resolve(m);
            }
        } catch (err) {
            return Promise.reject(new Error(Util.format('_request failed, subscribe err:%s', err.message)));
        }
    }
}

module.exports = {
    NRPC,
    Request,
    Response,
    EncodeType,
    NRPCClient,
    RequestOpt,
    Qos,
}