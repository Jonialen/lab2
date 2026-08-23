# Rust Sender

The sender converts printable ASCII into a CRC-32/ISO-HDLC or Hamming SECDED (13,8) frame, applies deterministic SplitMix64 noise, sends one NDJSON request over TCP, and explains the receiver's response.

## Install and verify

Rust 1.85 or newer is required because this crate uses the Rust 2024 edition.

```bash
cd sender-rust
cargo build
cargo fmt --check
cargo clippy --all-targets --all-features -- -D warnings
cargo test
```

No external CRC, random-number, networking, or CLI framework is used. Runtime dependencies are limited to `serde` and `serde_json`.

## Interactive use

Start the receiver first, then run without arguments:

```bash
cargo run
```

The sender prompts for the message, algorithm, receiver address, rational noise probability, seed, and request ID. Press Enter to accept displayed defaults.

## Reproducible non-interactive use

```bash
cargo run -- \
  --host 127.0.0.1 \
  --port 9000 \
  --message "A" \
  --algorithm hamming-secded-13-8 \
  --numerator 1 \
  --denominator 10 \
  --seed 3 \
  --request-id experiment-001
```

Use the same message, algorithm, probability pair, and seed to reproduce exactly the same `frame_bits` and flip count.

### Parameters

| Parameter | Default | Rule |
|---|---:|---|
| `--message` | Required with any CLI arguments | Non-empty printable ASCII (`0x20..0x7E`) |
| `--algorithm` | `crc` | `crc`, `crc32-iso-hdlc`, `hamming`, or `hamming-secded-13-8` |
| `--host` | `127.0.0.1` | Receiver hostname or IPv4 address |
| `--port` | `9000` | TCP port in `1..65535` |
| `--numerator` | `0` | At most the denominator |
| `--denominator` | `1` | `1..1,000,000,000` |
| `--seed` | `0` | Unsigned 64-bit integer |
| `--request-id` | `request-1` | 1-64 letters, digits, dots, underscores, or hyphens |

## Reading results

The first output line is the protocol status:

- `ok`: integrity passed and no correction was needed.
- `corrected`: Hamming corrected at least one bit and recovered the complete message.
- `detected_uncorrectable`: integrity failed, a SECDED block was uncorrectable, or the payload was not printable ASCII.
- `invalid_request`: the receiver rejected framing, schema, ranges, version, or frame length.

Before printing a response, the sender checks its protocol version and request ID, field and status/error combinations, request-derived bit metrics, and algorithm-specific integrity counters. When the request reports zero flipped bits, the sender only accepts an `ok` response with no integrity events and the exact original message. With one or more reported flips, a successful response must contain a printable message of the expected byte length, but the sender cannot prove that noise left its contents unchanged. Error responses print the stable error code and human-readable detail. Structurally valid frames also print bit counts and integrity metrics.

## Tests

`tests/normative_vectors.rs` loads `../protocol/test-vectors.json` directly and verifies:

- printable ASCII representation and MSB-first bit order;
- the standard CRC check value and complete CRC frame;
- clean, single-error, and double-error Hamming codewords;
- deterministic SplitMix64 flip indexes and output;
- input validation and exact request serialization.

Unit tests additionally cover CLI parsing and boundary validation.

## Common errors

| Error | Cause and action |
|---|---|
| `could not connect` | Start the receiver or correct `--host`/`--port`. |
| `message byte ... expected printable ASCII` | Remove newlines, tabs, accents, emoji, or other bytes outside `0x20..0x7E`. |
| `probability numerator must not exceed...` | Use a rational probability where `0 <= numerator <= denominator`. |
| `receiver returned invalid JSON/schema` | The peer does not implement the exact response contract. Compare it with `protocol/wire-protocol.md`. |
| `response ... is not LF-terminated` | The peer did not send one complete NDJSON line or exceeded the line limit. |

The sender applies 10-second timeouts to connection attempts, request writes, and response reads. Operating-system hostname resolution occurs before a socket connection can be attempted. It sends one request per process invocation; the receiver supports multiple requests on a connection for other protocol clients.
