const {NRPCClient, Request, Response, RequestOpt, NRPC, Qos} = require('../nrpc/nrpc_client');

class HelloWorld {
    constructor(encoding, client, timeout=10) {
        this.encoding = encoding;
        this.client = client;
        this.timeout = timeout*1000;
    }

    // async sayHello(msg) {
    //     let request = NRPC.request(NRPC.getFunArgsName, msg);
    //     try {
    //         const response = await this.client.callSync(request, new RequestOpt(Qos.Qos1));
    //         return Promise.resolve(response);
    //     } catch (err) {
    //         return Promise.reject(err);
    //     }
    // }

    async broadcast(msg) {
        let request = NRPC.request(NRPC.getFunArgsName, msg);
        try {
            const response = await this.client.notify(request);
            return Promise.resolve(response);
        } catch (err) {
            return Promise.reject(err);
        }
    }

    sayHello(msg) {
        let request = NRPC.request(NRPC.getFunArgsName(this.sayHello), msg);
        return new Promise((resolve, reject)=>{
            this.client.call(request, new RequestOpt(Qos.Qos2, 'testsayHello'), (err, response)=>{
                if (err) {
                    reject(err);
                    return;
                }
    
                resolve(response);
            }).catch((err)=>{
                reject(err);
            })
        })
    }
}

module.exports = {
    HelloWorld
}