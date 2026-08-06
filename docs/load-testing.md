# Load Simulation Guide

This project includes a Docker Compose setup with three backend echo servers and the Go load balancer in front of them. Use the commands below to simulate traffic against the balancer and observe how requests are distributed.

## Start the stack

Run the full setup from the project root:

```bash
docker compose up --build
```

The balancer is published on `http://127.0.0.1:8080/`.

## Send a single request

Use `curl` to confirm the balancer is up and forwarding to a backend:

```bash
curl -v http://127.0.0.1:8080/
```

## Simulate many connections

The simplest way to create many separate TCP connections is to run multiple `curl` requests in parallel.

### Moderate load

```bash
seq 1 50 | xargs -n1 -P10 curl -s http://127.0.0.1:8080/ > /dev/null
```

### Heavier load

```bash
seq 1 500 | xargs -n1 -P50 curl -s http://127.0.0.1:8080/ > /dev/null
```

### Background burst

```bash
for i in $(seq 1 200); do curl -s http://127.0.0.1:8080/ >/dev/null & done
wait
```

## Keep connections open longer

If you want to stress the L4 relay for longer, make the backend responses slower. In `docker-compose.yml`, set `RESPONSE_DELAY` for each backend to something like `2s` or `5s`, then restart Compose.

## What to watch

The balancer logs should show L4 activity such as:

- backend selection
- relay start and finish
- backend dial failures
- connection resets or copy errors

The backend containers will only show request logs if the backend service itself logs them.

## Optional load tool

For repeatable load testing, you can use a dedicated tool such as `hey`:

```bash
hey -n 1000 -c 50 http://127.0.0.1:8080/
```

If `hey` is not installed, the `curl` commands above are enough to generate concurrent connections.