/*
 * @Author: FengyunSky lifengsky123@gmail.com
 * @Description: 主要对接nats层，实现核心的nats 连接+订阅+发布等API。后续对接kv存储API
 */

const Assert = require('assert');
const String = require('string');
const Is = require('is');
const Util = require('util');
const Nats = require("nats");

const DefaultServerAddr = ['nats://127.0.0.1:4222', 'nats://127.0.0.1:4223', 'nats://127.0.0.1:4224'];

class NatsClientConfig {
    constructor(options) {
        this.name = options.name || 'test';
        this.server = options.server || DefaultServerAddr;
        this.username = options.username || 'test';
        this.password = options.password || 'test123';
        this.reconnWait = options.reconnWait || 3;//重连的时间间隔(单位：秒)
        this.reconnMax = options.reconnMax || 10;//最大的重连次数
    }

    getUsername() {
        return this.username;
    }

    getPassword() {
        return this.password;
    }
}

class NatsClient {
    constructor(config) {
        Assert(config instanceof NatsClientConfig, 'NatsClient constructor failed, invalid config');
        this.config = config;
        this.connected = false;//是否已连接
        this.reconnected = false;//是否重连
        this.client = null;//nats client对象
        this.jstream = null;
        this.jsm = null;
        this.reconnInterval = null;
    }

    async connect() {
        try {
            this.client = await Nats.connect({
                servers: this.config.server,
                user: this.config.username,
                pass: this.config.password,
                name: this.config.name,
                reconnect: true,
                pingInterval: 5 * 60 * 1000,
                maxPingOut: 3,
                noRandomize: true,
                reconnectJitter: 1 * 1000,
                reconnectTimeWait: this.config.reconnWait * 1000,
                maxReconnectAttempts: this.config.reconnMax,
                waitOnFirstConnect: true,
                // verbose: true,
                // debug: true,
            });
        } catch (err) {
            return Promise.reject(new Error(Util.format('connect failed, err:%s', err.message)));
        }
        
        let that = this;
        //连接断开后重连成功后会导致绑定之前的连接client订阅消息丢失，从而无法处理消息！！！！这里不如直接让进程退出重新连接订阅
        this.client.closed().then(async (err)=>{
            console.log('nats closed, err:', err ? err.message : 'noterror');
            that.connected = false;
            /*
            if (that.reconnected) {
                //连接失败则保持重连而不是退出
                that.reconnInterval = setInterval(()=>{
                    that.connect().then(()=>{
                        console.log(Util.format('reconnect success!!'));
                        clearInterval(that.reconnInterval);
                    })
                    .catch((err)=>{
                        console.log(Util.format('reconned failed, for closed, err:%s', err.message));
                    });
                }, that.config.reconnWait * 1000);
            }*/
        });
        (async () => {
            for await (const s of this.client.status()) {
              switch (s.type) {
                case Nats.Events.Disconnect:
                    console.log(`client disconnected - ${s.data}`);
                    that.connected = false;
                    break;
                case Nats.Events.LDM:
                    console.log("client has been requested to reconnect");
                    break;
                case Nats.Events.Reconnect:
                    console.log(`client reconnected - ${s.data}`);
                    that.connected = true;
                    break;
                case Nats.Events.Error:
                    console.log("client got an async error from the server");
                     break;
                case Nats.DebugEvents.Reconnecting:
                    console.log("client is attempting to reconnect");
                    break;
                case Nats.DebugEvents.StaleConnection:
                    console.log("client has a stale connection");
                    break;
                default:
                    console.log(`got an unknown status ${s.type}`);
              }
            }
        })().then();
        
        //jestream
        try {
            this.jsm = await this.client.jetstreamManager();
        } catch (err) {
            if (this.client.isClosed()) {
                this.client.close();
            }
            return Promise.reject(new Error(Util.format('connect failed, jetstreamManager failed, err:%s', err.message)));
        }
        
        this.jstream = this.client.jetstream();//TODO 选项
        this.reconnected = true;
        return Promise.resolve(this.client);
    }

    disconnect() {
        return this.client.drain();
    }

    isConnected() {
        return this.connected;
    }

    /**
     * @name: streamDefaultConfig
     * @msg: 默认stream配置
     * @param {string} stream
     * @param {string} subject
     * @return {object}
     */    
    static streamDefaultConfig(stream, subject) {
        Assert(Is.string(stream) && stream != '', 'streamDefaultConfig failed, invalid stream');
        Assert(Is.string(subject) && subject != '', 'streamDefaultConfig failed, invalid subject');
        return {
            name: stream,
            subjects: [subject+'.>'],
            retention: Nats.RetentionPolicy.Interest,
            max_age: 10 * 1000 * 1000 * 1000,//TODO 确认单位纳秒
            discard: Nats.DiscardPolicy.Old,
            storage: Nats.StorageType.File,
            duplicate_window: 10 * 1000 * 1000 * 1000,
            max_bytes: 10 * 1024 * 1024,
            max_msg_size: 1024 * 1024,
            max_msgs: 100,
        };
    }

    /**
     * @name: consumerDefaultConfig
     * @msg: 默认消费者配置
     * @param {string} consumer
     * @param {string} subject
     * @return {object}
     */    
    static consumerDefaultConfig(consumer, subject) {
        Assert(Is.string(consumer) && consumer != '', 'consumerDefaultConfig failed, invalid consumer');
        Assert(Is.string(subject) && subject != '', 'consumerDefaultConfig failed, invalid subject');
        return {
            name: consumer,         //消费者名称
            durable_name: consumer,//持久化消费者名称
            filter_subjects: [subject],//过滤主题
            deliver_subject: Nats.createInbox(),
            deliver_policy: Nats.DeliverPolicy.Last,
            ack_policy: Nats.AckPolicy.Explicit,
            max_deliver: 10,//最大传送数目
            max_ack_pending: 10,//最大等待消息数目
        }
    }

    /**
     * @name: addStream
     * @msg: 添加stream
     * @param {StreamConfig} streamCfg
     * @return {Promise(StreamInfo)}
     * @abstract 若jestream中存在此stream则更新，否则直接添加
     */    
    async addStream(streamCfg) {
        Assert(this.client, 'addStream failed, must nats connect!');
        //添加stream的主题必须包含.>通配符模式
        let isvalid = false;
        streamCfg.subjects.forEach(subject => {
            if (String(subject).contains('.>')) {
                isvalid = true;
                return;
            }
        });
        if (!isvalid) {
            return Promise.reject(new Error('addStream failed, invalid config for subject, not contains .> wildcard'));
        }

        try {
            await this.jsm.streams.info(streamCfg.name);
        } catch(err) {
            console.log(Util.format('addStream failed, not found the stream:%s, to add', streamCfg.name));
            try {
                const info = await this.jsm.streams.add(streamCfg);
                return Promise.resolve(info);
            } catch(err) {
                return Promise.reject(new Error(Util.format('addStream failed, add failed, err:%s', err.message)));
            }
        }

        try {
            const info = await this.jsm.streams.update(streamCfg.name, streamCfg);
            return Promise.resolve(info);
        } catch (err) {
            return Promise.reject(new Error(Util.format('addStream failed, update failed, err:%s', err.message)));
        }
        
    }

    /**
     * @name: addConsumer
     * @msg: 添加消费者
     * @param {string} stream
     * @param {ConsumerConfig} consumerCfg
     * @return {Promise(ConsumerInfo)}
     * @abstract 若stream包含了改消费者则更新消费者配置，否则直接添加
     */    
    async addConsumer(stream, consumerCfg) {
        Assert(this.client, 'addConsumer failed, must nats connect!');
        Assert(stream != '', 'addConsumer failed, invalid stream, for empty!');

        //消费者名称须与持久化名称相同
        if (consumerCfg.name != '' && consumerCfg.name != consumerCfg.durable_name) {
            return Promise.reject(new Error('addConsumer failed, the consumer config for Name and Duralbe not equal'));
        }

        try {
            const info = await this.jsm.consumers.info(stream, consumerCfg.durable_name);
            this.jsm.consumers.update(stream, consumerCfg.durable_name, consumerCfg).then((info)=>{
                return Promise.resolve(info);
            })
            .catch((err)=>{
                return Promise.reject(new Error(Util.format('addConsumer failed, update consumer err:%s', err.message)));
            })
        } catch(err) {
            console.log('addConsumer failed, not found consumer:', consumerCfg.name, 'for the stream:', stream, 'to add');
        }

        try {
            const info = await this.jsm.consumers.add(stream,consumerCfg);
            return Promise.resolve(info);
        } catch (err) {
            return Promise.reject(new Error(Util.format('addConsumer failed, add err:%s', err.message)));
        }
    }

    /**
     * @name: publish
     * @msg: 推送消息
     * @param {string} subject
     * @param {Uint8Array|string} data
     * @param {PublishOptions|JetStreamPublishOptions} opts
     * @return {Promise}
     */    
    async publish(subject, data, opts) {
        Assert(this.client, 'publish failed, must nats connect!');
        Assert(Is.string(subject) && subject != '', 'pulbish failed, invalid subject');
        Assert(Buffer.isBuffer(data) || Is.string(data) || data instanceof Uint8Array, 
                    'publish failed, invalid data');
        if (!opts) {
            this.client.publish(subject, data);
            return Promise.resolve();
        }

        try {
            await this.jstream.publish(subject, data, opts);
            return Promise.resolve();
        } catch(err) {
            return Promise.reject(new Error(Util.format('publish failed, err:%s', err.message)));
        }
    }

    /**
     * @name: publishMsg
     * @msg: 推送消息
     * @param {object{subject, data, header}} msg
     * @param {PublishOptions|JetStreamPublishOptions} opts
     * @return {*}
     */    
    async publishMsg(msg, opts) {
        Assert(this.client, 'publishMsg failed, must nats connect!');
        Assert(Is.object(msg), 'publishMsg failed, invalid msg');
        Assert(Is.string(msg.subject), 'publishMsg failed, invalid msg.subject');
        Assert(msg.header instanceof Nats.MsgHdrsImpl, 'publishMsg failed, invalid msg.header');
        Assert(Buffer.isBuffer(msg.data) || Is.string(msg.data) || msg.data instanceof Uint8Array, 
                    'publishMsg failed, invalid msg.data');

        if (!opts) {
            this.client.publish(msg.subject. msg.data, {headers:msg.header});
            return Promise.resolve();
        }

        try {
            opts.headers = msg.header;
            await this.jstream.publish(msg.subject, msg.data, opts);
        } catch(err) {
            return Promise.reject(new Error(Util.format('publishMsg failed, publish err:%s', err.message)));
        }
    }

    /**
     * @name: publishRequest
     * @msg: 推送请求
     * @param {string} subject 主题
     * @param {string} reply 响应主题
     * @param {Uint8Array|string} data
     * @return {void}
     */    
    publishRequest(subject, reply, data) {
        Assert(this.client, 'publishRequest failed, must nats connect!');
        Assert(Is.string(subject) && subject != '', 'publishRequest failed, invalid subject');
        Assert(Is.string(reply) && reply != '', 'publishRequest failed, invalid reply');
        Assert(Buffer.isBuffer(data) || Is.string(data), 'publishRequest failed, invalid data');

        this.client.publish(subject, data, {
            reply: reply
        });
    }

    /**
     * @name: request
     * @msg: 请求响应结果
     * @param {string} subject
     * @param {Uint8Array|string} data
     * @param {number} timeout 超时时间
     * @return {Nats.Msg} msg响应消息
     */    
    async request(subject, data='', timeout=10) {
        Assert(this.client, 'request failed, must nats connect!');
        Assert(Buffer.isBuffer(data) || Is.string(data), 'request failed, invalid data');
        Assert(Is.string(subject) && subject != '', 'request failed, invalid subject');

        try {
            const msg = await this.client.request(subject, data, {
                timeout: timeout
            });
            return Promise.resolve(msg);
        } catch(err) {
            return Promise.reject(new Error(Util.format('request failed, err:%s', err.message)));
        }
    }

    /**
     * @name: subscribe
     * @msg: 订阅消息并异步接收响应消息
     * @param {string} subject
     * @param {function callback(error, Msg|JsMsg) {
     }} 回调消息或者错误
     * @param {ConsumerOpts} opts
     * @param {number} timeout
     * @return {Subscription}
     */    
    async subscribe(subject, callback, opts, timeout=10*1000) {
        Assert(this.client, 'subscribe failed, must nats connect!');
        Assert(Is.string(subject) && subject != '', 'subscribe failed, invalid subject');
        // Assert(Is.function(callback), 'subscribe failed, invalid callback');

        if (opts) {
            opts.callbackFn = callback;
            try {
                const subscription =  await this.jstream.subscribe(subject, opts);
                return Promise.resolve(subscription);
            } catch (err) {
                return Promise.reject(new Error(Util.format('subscribe failed, err:%s', err.message)));
            }
        }

        let subscription = null;
        if (callback) {
            subscription = this.client.subscribe(subject, {
                                    max: 1,
                                    callback: callback,
                                    timeout: timeout
                                });
        } else {
            subscription = this.client.subscribe(subject, {
                                    max: 1,
                                    timeout: timeout
                                });
        }
        
        return Promise.resolve(subscription);
        
    }

    /**
     * @name: queueSubscribe
     * @msg: 指定队列订阅
     * @param {string} subject 主题
     * @param {string} queue 队列名称
     * @param {function} callback 订阅消息处理回调
     * @param {ConsumerOpts} opts stream订阅选项，若为stream方式必须指定!!!
     * @return {Subscription}订阅成功后的描述符
     */    
    async queueSubscribe(subject, queue, callback, opts) {
        Assert(this.client, 'queueSubscribe failed, must nats connect!');
        Assert(Is.string(subject) && subject != '', 'queueSubscribe failed, invalid subject');
        Assert(Is.string(queue) && queue != '', 'queueSubscribe failed, invalid queue');
        Assert(Is.function(callback), 'queueSubscribe failed, invalid callback');
        if (opts) {
            opts.callbackFn = callback;
            opts.queue = queue;
            try {
                const subscription =  await this.jstream.subscribe(subject, opts);
                return Promise.resolve(subscription);
            } catch (err) {
                return Promise.reject(new Error(Util.format('subscribe failed, err:%s', err.message)));
            }
        }

        const subscription = this.client.subscribe(subject, {
                                queue: queue,
                                callback: callback
                            });
        return Promise.resolve(subscription);
    }

    /**
     * @name: unsubscribe
     * @msg: 取消订阅
     * @param {Subscription} subject
     * @return {*}
     */    
    unsubscribe(subscription) {
        Assert(this.client, 'unsubscribe failed, must nats connect!');
        Assert(Is.object(subscription), 'unsubscribe failed, invalid subscription');
        subscription.unsubscribe();
    }


    /**
     * @name: streamNameBySubject
     * @msg: 找到匹配主题的stream
     * @param {string} subject
     * @return {Promise(string)} stream名称
     */    
    async streamNameBySubject(subject) {
        Assert(this.client, 'streamNameBySubject failed, must nats connect!');
        Assert(Is.string(subject) && subject != '', 'streamNameBySubject failed, invalid subject');
        try {
            const stream = await this.jsm.streams.find(subject);
            return Promise.resolve(stream);
        } catch (err) {
            return Promise.reject(new Error(Util.format('streamNameBySubject failed, find stream by subject:%s, err:%s', 
                                    subject, err.message)));
        }
    }

    /**
     * @name: consumerInfoByStream
     * @msg: 查找stream相关的消费者信息
     * @param {string} stream
     * @param {string} consumer
     * @return {Promise(ConsumerInfo)}
     */    
    async consumerInfoByStream(stream, consumer) {
        Assert(this.client, 'consumerInfoByStream failed, must nats connect!');
        Assert(Is.string(stream) && stream != '', 'consumerInfoByStream failed, invalid stream');
        Assert(Is.string(consumer) && consumer != '', 'consumerInfoByStream failed, invalid consumer');

        try {
            const info = await this.jsm.consumers.info(stream, consumer);
            return Promise.resolve(info);
        } catch (err) {
            return Promise.reject(new Error(Util.format('consumerInfoByStream failed, err:%s', err.message)));
        }
    }

}

module.exports = {
    NatsClient,
    NatsClientConfig,
}