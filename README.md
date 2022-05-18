# controller

## Description (TODO)
Responsibility and purpose of service.

## Setup

`config.go` is responsible for handling configs. 

It looks for `config.yaml` file in project directory.
If there is one it reads config from there.
After that it reads env variables and overwrites any variables from local config.
Config will be printed in console on start.

A sample config file is left in repository to be filled.

## Running locally

Open `go.mod` file and check which version of Go is used.
It should have line similar to this one: `go 1.16`. This means you need to install 1.16 version of Go.

After successfull install you can build the service with:
```bash 
    go build
``` 
This will create an executable `posbus` that can be ran.

If you run the service locally Unity client won't be able to connect to it securely. Use `ws:localhost:port/posbus` as URL in `NetworkConfiguration`.