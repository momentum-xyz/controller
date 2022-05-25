# Momentum controller

Controller is responsible for Worlds logic as for handling of backend and frontend messages.

## Building

```console
make build
```

Executable is placed in `bin/controller`.

Or
```console
make run
```

To run the application.

## Running

It can be configured by a yaml config file and/or environment variables.
For details look in `internal/config` folder.

## Note

If you run the service locally Unity client won't be able to connect to it securely. Use `ws:localhost:port/posbus` as URL in `NetworkConfiguration`.