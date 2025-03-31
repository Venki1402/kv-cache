# Sharded In-Memory Cache

A high-performance, sharded in-memory cache with LRU eviction and memory monitoring, implemented in Go.

## Features

- **Sharded Design**: Uses multiple shards to improve concurrency.
- **LRU Eviction**: Removes least recently used entries when memory is full.
- **Memory Monitoring**: Automatically evicts items when memory usage exceeds 70%.
- **REST API**: Supports `PUT` and `GET` requests for cache operations.

## API Endpoints

### Store a key-value pair

```
PUT /put
Content-Type: application/json
{
  "key": "example",
  "value": "data"
}
```

### Retrieve a value by key

```
GET /get?key=example
```

## Running with Docker

### Build the Docker image

```
docker build -t sharded-cache .
```

### Run the container

```
docker run -p 7171:7171 sharded-cache
```

## Running Locally

```
go run main.go
```

## Requirements

- Go 1.18+
- Docker (optional)
