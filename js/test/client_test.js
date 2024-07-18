const {HelloWorld, HelloResponse} = require('../helloworld/helloworld');
const {NatsClient, NatsClientConfig} = require('../nats/natsclient/nats_client');
const {NRPCClient, EncodeType, Response} = require('../nrpc/nrpc_client');

async function main() {
    let clientCfg = new NatsClientConfig({});
    let client = new NatsClient(clientCfg);
    try {
        await client.connect();
    } catch (err) {
        console.log('[main]connect failed, err:', err.message);
    }

    let stream = 'baisahan';
    let subject = 'service';

    let rpcclient = new NRPCClient('testclient', subject, client, EncodeType.Protobuf);
    await rpcclient.bindStream(NatsClient.streamDefaultConfig(stream, subject));
    let helloworld = new HelloWorld(EncodeType.String, rpcclient);

    // try {
    //     let response = await helloworld.sayHello('hello world');
    //     console.log('sayhello success, response', response);
    // } catch (err) {
    //     console.log('sayHello call failed, err:', err.message);
    // }

    helloworld.sayHello('hello world').then((response)=>{
        console.log('sayhello success, response', response);
    }).catch((err)=>{
        console.log('sayhello failed, err:', err.message);
    })

    // await helloworld.broadcast('hello world');
    
}

main();
