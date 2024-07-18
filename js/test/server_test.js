const {NatsClient, NatsClientConfig} = require('../nats/natsclient/nats_client');
const {NRPC, NRPCServer, Service, EncodeType, ResponseResult} = require('../nrpc/nrpc_server');


async function main() {
    let clientCfg = new NatsClientConfig({});
    let client = new NatsClient(clientCfg);
    try {
        await client.connect();
    } catch (err) {
        console.log('[main]connect failed, err:', err.message);
    }

    let stream = 'test';
    let subject = 'service';
    let service = new Service('sayhello', stream, '', 'test', 'proxy');
    let rpcserver = new NRPCServer('testserver', subject, client, EncodeType.Protobuf);
    rpcserver.bindStream(NatsClient.streamDefaultConfig(stream, subject));

    rpcserver.register(service, function(request){
        console.log('handlr service:', service.name, request);

        return NRPC.response(ResponseResult.Success, '', 'hello world');
    });

}

main();