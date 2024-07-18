#### 特点

nrpc基于高性能分布式nats框架，实现了RPC远程调用功能，基于eventbus事件分发机制，具备高性能、消息不丢失、消息重传、跨平台等特点。

#### 功能

nrpc主要是对接nats消息队列来实现RPC远程过程调用功能，封装了基础了nats核心功能以及jetstream流存储的能力，并基于此实现了不同的qos。具体包括：

* qos0，不保证消息不丢失
* qos1，保证消息不丢失，但不保证消息唯一
* qos2，保证消息不丢失，且保证消息唯一

#### 代码结构

主要包括了go及js的封装代码，具体
``` cobol
├── Readme.md
├── go
│   ├── clientdeadtest.go 客户端断开连接监控测试程序
│   ├── clienttest.go 客户端远程调用测试程序
│   ├── eventbustest.go eventbus broker测试程序
│   ├── helloworld 上层封装的helloworld示例代码
│   │   └── helloworld.go
│   ├── logger
│   │   └── logger.go
│   ├── nats 核心的nats客户端/服务端调用API
│   │   ├── natsclient
│   │   └── natsserver
│   ├── nrpc nrpc远程过程调用核心代码
│   │   ├── nrpc.go nrpc核心基础API类
│   │   ├── nrpc.pb.go nrpc协议代码(为protoc工具自动生成，具体命令为`protoc -I ../ -I ./ --go_out=./ *.proto`)
│   │   ├── nrpc.proto nrpc协议定义，保持与js相同!
│   │   ├── nrpc_client.go nrpc客户端核心API类
│   │   └── nrpc_server.go nrpc服务端核心API类
│   └── servertest.go 服务端测试程序
└── js
    ├── helloworld
    │   └── helloworld.js
    ├── nats
    │   ├── natsclient nats客户端核心API
    │   └── natsserver nats服务端核心API，暂未实现!
    ├── nrpc
    │   ├── nrpc.js nrpc核心基础API
    │   ├── nrpc.pb.js nrpc协议代码(为pbjs自动生成，具体命令为`pbjs -t static-module -w commonjs -o nrpc.pb.js nrpc.proto`)
    │   ├── nrpc.proto nrpc协议定义，保持与go一致！
    │   ├── nrpc_client.js nrpc客户端核心API
    │   └── nrpc_server.js nrpc服务端核心API
    ├── package.json
    └── test
        ├── client_test.js
        └── server_test.js
```

#### 使用说明
> 具体见示例代码
