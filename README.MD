# gossip (protocol)

This is a test project for the library hashicorp/memberlist which incorporates the gossip protocol

## Building the project

```
go build main.go logger.go
```

## Running the project

```
./main -name <node-name> -port <port> -join <address:port>
```

- name: the name of the node
- port: the port to open
- join: (Optional) the address and port of the node to use to join the network