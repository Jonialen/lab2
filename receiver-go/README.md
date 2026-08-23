# Go Receiver

The receiver is a persistent concurrent TCP/IPv4 NDJSON server. It strictly validates the shared wire contract, verifies CRC-32/ISO-HDLC frames, corrects Hamming SECDED (13,8) single-bit errors, and returns protocol-defined metrics without applying noise again.

Go 1.26 is required, matching `go.mod`.

## Quick path

```bash
cd receiver-go
go run . -host 127.0.0.1 -port 9000
```

The defaults are `127.0.0.1:9000`. Flags override the `HOST` and `PORT` environment variables:

```bash
HOST=0.0.0.0 PORT=9100 go run .
```

Use `0.0.0.0` only when other machines must connect and the network is trusted. The protocol has no authentication or encryption.

## Design

| File | Responsibility |
|---|---|
| `main.go` | Parse listen configuration and create the TCP/IPv4 listener. |
| `server.go` | Accept clients, enforce the NDJSON line limit, and keep valid connections open. |
| `protocol.go` | Reject duplicate/unknown keys, validate the exact schema and ranges, and build responses. |
| `crc.go` | Reconstruct bytes and verify CRC-32/ISO-HDLC. |
| `hamming.go` | Analyze every SECDED codeword, correct single-bit errors, and aggregate metrics. |

The split follows runtime responsibilities without interfaces, dependency injection, or framework layers. Up to 64 clients are handled concurrently; excess connections are closed without stopping the listener. Each accepted client has a 10-second deadline for every read and write operation, so an idle or blocked client cannot retain a handler indefinitely. Recoverable request errors produce one response and processing continues at the next LF. A line over 1,048,576 bytes receives `line_too_long` and closes only that connection.

## Response states

| Status | Receiver behavior |
|---|---|
| `ok` | Integrity passed and the recovered payload is printable ASCII. |
| `corrected` | At least one SECDED bit was corrected and no codeword was uncorrectable. |
| `detected_uncorrectable` | CRC failed, SECDED found an uncorrectable codeword, or recovered ASCII was invalid. |
| `invalid_request` | Framing, JSON, exact schema, version, range, or frame length validation failed. |

The stable error codes and exact metrics are defined in [`../protocol/wire-protocol.md`](../protocol/wire-protocol.md).

## Tests

```bash
cd receiver-go
gofmt -w .
go test ./...
go vet ./...
go test -short ./...  # skips the external Cargo integration test
```

The suite loads every normative vector from `../protocol/test-vectors.json`. It also covers hostile JSON and schemas, CRC and SECDED failures, invalid ASCII, multiple requests on one connection, listener survival, connection limits and deadlines, the line limit, invalid environment ports, and real deterministic Rust-to-Go execution with a bounded Cargo subprocess.

## Manual protocol probe

With the receiver running, a clean Hamming request can be sent using any NDJSON-capable TCP client:

```json
{"protocol_version":1,"request_id":"demo-1","algorithm":"hamming-secded-13-8","source_octets":1,"frame_bits":"1000100100010","noise":{"probability_numerator":0,"probability_denominator":1,"seed":42,"flipped_bits":0}}
```

Expected status: `ok`, with message `A`.

## Troubleshooting

| Symptom | Action |
|---|---|
| `bind: address already in use` | Stop the process using the port or select another `PORT`. |
| Sender reports `could not connect` | Confirm the receiver host, port, and firewall settings. |
| `invalid_schema` | Compare every field and type with the normative contract; duplicate and unknown keys are rejected. |
| `invalid_frame_length` | CRC requires `source_octets * 8 + 32` bits; Hamming requires `source_octets * 13`. |
| Integration test skips | Run without `-short` and ensure `cargo` is available. |
