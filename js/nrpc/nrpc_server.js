/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 对接核心 rpc 层实现远程过程调用，实现核心的rpc注册及服务分发处理
 */

const Assert = require('assert');
const Is = require('is');
const Util = require('util');
const Nats = require('nats');
const {EventEmitter} = require('events'); 
const {NRPC, Response, EncodeType, EncodeType1} = require('./nrpc');
const {ResponseResult, ExpectedStreamHdr, ExpectedReplySujectHdr} = require('./nrpc');

class Service {
    constructor(name, stream='', consumer='', queue='', work='') {
        Assert(Is.string(name) && name != '', 'Serive constructor failed, invalid name');
        this.name = name;           //服务名称
        this.consumer = consumer;   //消费者名称(若指定消费者名称则需要绑定专属的消费者，否则会自动创建绑定至stream)
        this.stream = stream;       //stream 流的名称
        this.queue = queue;         //订阅处理的队列名称(用于队列组来实现负载均衡)
        this.work = work;           //订阅处理的工作队列名称(用于多处理事件串行处理)
    }
}

class WorkMsg {
    constructor(msg, stream, encodeType, client, handler) {
        this.msg = msg;//nats.Msg
        this.stream = stream;
        this.encodeType = encodeType;
        this.client = client;
        this.handler = handler;
    }

    process() {
        console.log(Util.format('[workmsg]process msg, for stream:%s and subject:%s', 
                        this.stream, this.msg.subject));
        let msg = this.msg;
        if (this.stream != '') {
            msg.working();
        }

        let request = NRPC.decode(this.encodeType, msg.data, EncodeType1.Request);
        let result = this.handler(request);
        Assert(result instanceof Response, '[workmsg]process msg failed, invalid response');
        let data = NRPC.encode(this.encodeType, Response.create(result ? result : {}));
        if (this.stream == '') {
            msg.respond(data);
            return;
        }

        let stream = msg.headers.get(ExpectedStreamHdr);
        if (this.stream != stream) {
            console.log(Util.format('[workmsg]process, mismatch stream(%s) of the response msg for the stream(%s) of workmsg, to terminate',
                        stream, this.stream));
            msg.term();
            return;
        }

        msg.ack();
        let replySubj = msg.headers.get(ExpectedReplySujectHdr);
        this.client.publish(replySubj, data);
        console.log(Util.format('[workmsg]process, response for subject:%s and reply:%s', 
                            msg.subject, replySubj));
    } 
}


class WorkQueueStopOpt {
    constructor(safe, timeout) {
        this.safe = safe;//是否安全停止
        this.timeout = timeout;//处理超时时间(单位：秒)
    }
}

const WorkQueueDispathEvent = 'work-queue-dispacth-event';
class WorkQueue {
    constructor(name) {
        this.name = name;
        this.queue = new Array();
        this.interval = null;
        this.event = new EventEmitter();

        this.init();
    }

    init() {
        let that = this;
        this.event.on(WorkQueueDispathEvent, function() { 
            console.log(Util.format('[workqueue][%s]handle the workmsg, to dispatch', that.name));
            that.dispatch();
        }); 
    }

    /**
     * @name: destroy
     * @msg: 主动销毁
     * @return {void}
     */    
    destroy() {
        this.event.removeAllListeners();
        this.stop({safe:false});
    }

    /**
     * @name: addMsg
     * @msg: 向工作队列添加消息
     * @param {WorkMsg} msg
     * @return {void}
     */    
    addMsg(msg) {
        this.queue.push(msg);
        this.event.emit(WorkQueueDispathEvent);//发送消息
    }

    /**
     * @name: clearMsg
     * @msg: 清空队列
     * @return {void}
     */    
    clearMsg() {
        this.queue = [];
    }

    /**
     * @name: dispatch
     * @msg: 事件分发消息处理
     * @return {void}
     */    
    dispatch() {
        //接收消息处理
        if (this.interval) {
            return;
        }

        let that = this;
        this.interval = setInterval(function(){
            if (!that.queue.length) {
                clearInterval(this.interval);//队列为空则停止轮询处理，等待下一次触发
                that.interval = null;
                return;
            }

            const msg = that.queue.shift();
            msg.process();
        }, 100);//间隔时间久一些避免高负载
    }

    /**
     * @name: stop
     * @msg: 停止消息处理
     * @param {WorkQueueStopOpt} opts
     * @return {void}
     */    
    stop(opts) {
        Assert(opts instanceof WorkQueueStopOpt, 'stop failed, invalid opts');
        if (this.interval) {
            clearInterval(this.interval);
            this.interval = null;
        }

        if (!opts.safe) {
            return;
        }

        let finished = false;
        setTimeout(() => {
            finished = true;
        }, opts.timeout*1000);

        this.queue.forEach(msg => {
            if (finished) {
                return;
            }
            msg.process()
        })
    }

}

class NRPCServer extends NRPC {
    constructor(name, subject, client, encodeType) {
        super(name, subject, client, encodeType);
        this.workQueue = null;
        this.serviceSubs = new Map();//事件订阅组
    }

    getEncode() {
        return this.encodeType;
    }

    /**
     * @name: register
     * @msg: 注册服务
     * @param {Service} service
     * @param {function(Request) Response} handler
     * @abstract handler 接收Request请求参数，返回Response响应结果，若为空则默认Response结果
     * @return {void}
     */    
    async register(service, handler) {
        Assert(service instanceof Service, 'register failed, invalid service');
        Assert(Is.function(handler), 'register failed, invalid hanlder');

        let subject = this.subject + '.' + service.name;
        let stream = service.stream;
        let consumer = service.consumer;
        console.log(Util.format('register the subject:%s for the stream:%s and the consumer:%s', 
                        subject, stream, consumer));
        //检查stream匹配消费者及主题并修正
        try {
            if (consumer != '') {
                await this.checkConsumerAndStreamWithSubject(stream, consumer, subject);
            } else {
                await this.checkStreamWithSubject(stream, subject);
            }
        } catch (err) {
            console.log(Util.format('register, err:%s', err.message));
        }
        
        let that = this;
        let workHandler = function(err, msg) {
            if (err) {
                console.log(Util.format('register failed, handler failed, err:%s', err.message));
                return;
            }
            Assert(msg, Util.format('register failed, handler failed, invalid msg for null'));
            if (subject != msg.subject) {
                console.log(Util.format('register failed, handler failed, invalid subject %s', msg.Subject));
                return;
            }

            let workMsg = new WorkMsg(msg, service.stream, that.encodeType, that.client, handler);
            if (service.work != '') {
                if (!that.workQueue) {
                    that.workQueue = new WorkQueue(service.work);
                }
                that.workQueue.addMsg(workMsg);
            } else {
                workMsg.process();
            }
        }

        let sub = null;
        //若不指定stream则使用核心nast模式
        if (service.stream == '') {
            if (service.queue != '') {
                sub = this.client.queueSubscribe(subject, service.queue, workHandler);
            } else {
                sub = this.client.subscribe(subject, workHandler);
            }
        } else {
            //如果指定消费者则使用绑定并订阅主题为消费者指定过滤主题
            let consumerOpts = Nats.consumerOpts();
            if (service.consumer != '') {
                consumerOpts = Nats.consumerOpts()
                                    .manualAck()
                                    .bind(stream, consumer);
                
            } else {
                //若未指定消费者名称则为push模式，自动创建consumer
                consumer = stream + '-' + this.subject + '-' + service.name;
                consumerOpts = Nats.consumerOpts()
                                    .manualAck()
                                    .bindStream(stream)
                                    .durable(consumer)
                                    .deliverTo(Nats.createInbox())
                                    .deliverLast()
                                    .ackExplicit()
                                    .maxDeliver(10)
                                    .maxAckPending(10);
            }

            try {
                if (service.queue != '') {
                    sub = this.client.queueSubscribe(subject, service.queue, workHandler, consumerOpts);
                } else {
                    sub = this.client.subscribe(subject, workHandler, consumerOpts);
                }
            } catch (err) {
                console.log(Util.format('register failed, subscribe err:%s, not to handler', err.message));
                return;
            }
        }

        this.serviceSubs.set(service.name, sub);
    }

    /**
     * @name: unregister
     * @msg: 取消订阅服务处理
     * @param {Service} service
     * @return {void}
     * @abstract 若未指定服务名称则删除所有服务，否则删除指定服务；
     */    
    async unregister(service) {
        Assert(typeof(service) == Service, 'unregister failed, invalid service');
        //若 service 为空则删除所有
        if (service.name == '') {
            this.serviceSubs.forEach((sub, name)=>{
                console.log(Util.format('unregister the service:%s', name));
                if (sub) {
                    this.client.unregister(sub);
                }
                this.serviceSubs.delete(name);
            })
            return;
        } 

        //取消订阅处理
        if (this.serviceSubs.has(service.name)) {
            let sub = this.serviceSubs.get(service.name);
            this.client.unregister(sub);
            this.serviceSubs.delete(service.name);
        }
    }
}

module.exports = {
    NRPC,
    Response,
    EncodeType,
    NRPCServer,
    Service,
    ResponseResult
}