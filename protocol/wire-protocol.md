# Laboratory 2 Wire Protocol

This document is the complete interoperability contract between the Rust sender and the persistent Go receiver. The protocol is intentionally small: TCP/IPv4 carries one JSON request and one JSON response per LF-delimited line; only `frame_bits` models the unreliable channel.

## 1. Connection and NDJSON framing

- The receiver listens on a configurable TCP/IPv4 address and port and keeps each accepted connection open for multiple requests.
- The sender encodes JSON as UTF-8 and terminates every request with one LF byte (`0x0A`, `\n`). The receiver terminates every response the same way.
- Bytes before an LF contain exactly one JSON object. Empty lines, JSON arrays, scalar JSON values, and multiple JSON values on one line are invalid.
- A line is at most 1,048,576 bytes, excluding LF. On a longer line, the receiver returns `line_too_long` and closes that connection.
- Requests may be pipelined. The receiver returns exactly one response per complete request line, in request order.
- EOF after a partial line discards that line without a response. A normal EOF after an LF closes only that client connection; the receiver continues listening.
- JSON object keys are case-sensitive. Duplicate or unknown keys are invalid. All integer fields are JSON integers, not strings or floating-point values.

## 2. Request

Every request has exactly these fields:

| Field | Type | Required value |
|---|---|---|
| `protocol_version` | integer | `1` |
| `request_id` | string | 1-64 characters matching `[A-Za-z0-9._-]+` |
| `algorithm` | string | `crc32-iso-hdlc` or `hamming-secded-13-8` |
| `source_octets` | integer | Number of original ASCII octets; 1 or greater |
| `frame_bits` | string | The already noise-affected frame; only `0` and `1`, with no whitespace |
| `noise` | object | Exact metadata defined below |

`noise` has exactly these fields:

| Field | Type | Validation |
|---|---|---|
| `probability_numerator` | integer | `0 <= value <= probability_denominator` |
| `probability_denominator` | integer | `1 <= value <= 1,000,000,000` |
| `seed` | integer | `0 <= value <= 18,446,744,073,709,551,615` |
| `flipped_bits` | integer | Actual sender flip count; `0 <= value <= len(frame_bits)` |

The rational pair represents the requested independent bit-flip probability. It is metadata on the wire: the receiver MUST NOT apply noise again and MUST NOT reject a request merely because the reported seed would produce different flips. The sender is authoritative for `flipped_bits` because the original frame is not transported.

### Request example

```json
{"protocol_version":1,"request_id":"demo-1","algorithm":"hamming-secded-13-8","source_octets":1,"frame_bits":"1000100100010","noise":{"probability_numerator":0,"probability_denominator":1,"seed":42,"flipped_bits":0}}
```

## 3. Response

Every response has exactly these fields:

| Field | Type | Meaning |
|---|---|---|
| `protocol_version` | integer | Always `1` |
| `request_id` | string or `null` | Echoed when a valid ID can be recovered; otherwise `null` |
| `status` | string | One value from the status table |
| `message` | string or `null` | Decoded printable ASCII only for `ok` or `corrected`; otherwise `null` |
| `error` | object or `null` | Error details for non-success statuses; otherwise `null` |
| `metrics` | object or `null` | Metrics for a structurally valid frame; otherwise `null` |

Statuses:

| Status | Meaning |
|---|---|
| `ok` | Integrity passed and no correction was required |
| `corrected` | SECDED corrected at least one single-bit error and no uncorrectable block exists |
| `detected_uncorrectable` | CRC failed, SECDED found an uncorrectable block, or the recovered payload is not printable ASCII |
| `invalid_request` | Framing, JSON, schema, range, length, or version validation failed |

When non-null, `error` has exactly `code` and `detail`, both strings. `code` is one of `invalid_json`, `line_too_long`, `unsupported_version`, `invalid_schema`, `invalid_frame_length`, `integrity_check_failed`, `uncorrectable_error`, or `invalid_ascii_payload`. `detail` is a short human-readable explanation and is not intended for program logic.

When non-null, `metrics` has exactly these non-negative integer fields:

| Field | Meaning |
|---|---|
| `received_bits` | `len(frame_bits)` |
| `source_bits` | `source_octets * 8` |
| `redundancy_bits` | CRC: `32`; SECDED: `source_octets * 5` |
| `reported_flipped_bits` | Request `noise.flipped_bits` |
| `detected_units` | CRC frames or SECDED codewords with a detected integrity event |
| `corrected_bits` | SECDED bits corrected, including corrections to overall parity bits; CRC always `0` |
| `uncorrectable_units` | CRC frames or SECDED codewords that cannot be corrected |

Consumers calculate overhead as `redundancy_bits / source_bits`; no floating-point ratio is sent. If any SECDED codeword is uncorrectable, the receiver still examines every codeword for metrics but returns no partial message.

### Successful response example

```json
{"protocol_version":1,"request_id":"demo-1","status":"ok","message":"A","error":null,"metrics":{"received_bits":13,"source_bits":8,"redundancy_bits":5,"reported_flipped_bits":0,"detected_units":0,"corrected_bits":0,"uncorrectable_units":0}}
```

## 4. Presentation and bit order

- Application input is non-empty printable 7-bit ASCII: octets `0x20` through `0x7E`, inclusive.
- Each octet becomes eight bits, most-significant bit first. `A` (`0x41`) is `01000001`.
- Bit strings are read left to right. Indexes in metrics and noise processing are zero-based; Hamming positions are explicitly one-based.
- `frame_bits` is at most 1,000,000 bits.
- CRC frame length MUST equal `source_octets * 8 + 32`.
- SECDED frame length MUST equal `source_octets * 13`.
- The transport envelope is never encoded by CRC or Hamming and never receives simulated noise. The sender first constructs the complete `frame_bits`, applies noise to every bit including redundancy, and only then serializes the request JSON.

## 5. CRC-32/ISO-HDLC

Use the standard CRC-32/ISO-HDLC parameters exactly:

| Parameter | Value |
|---|---|
| Width | 32 |
| Polynomial | `0x04C11DB7` |
| Initial register | `0xFFFFFFFF` |
| Reflect input | `true` |
| Reflect output | `true` |
| Final XOR | `0xFFFFFFFF` |
| Check value for ASCII `123456789` | `0xCBF43926` |

Encoding computes the CRC over the original ASCII octets. The frame is `payload_bits || crc_bits`, where `payload_bits` contains each octet MSB-first and `crc_bits` is the 32-bit numeric CRC value MSB-first. No padding is added.

Decoding splits the last 32 bits, reconstructs payload octets, computes CRC-32/ISO-HDLC over those octets, and compares numeric values. Equality yields `ok` if the payload remains printable ASCII. Inequality yields `detected_uncorrectable`, `integrity_check_failed`, `detected_units = 1`, and `uncorrectable_units = 1`. CRC never corrects bits.

## 6. Hamming SECDED (13,8)

Each ASCII octet is independently encoded into one 13-bit codeword. Codeword positions 1 through 13 are transmitted left to right.

| Positions | Purpose |
|---|---|
| `1, 2, 4, 8` | Hamming even-parity bits |
| `3, 5, 6, 7, 9, 10, 11, 12` | Data bits `d7` through `d0`, MSB-first |
| `13` | Overall even-parity bit |

For each parity position `p` in `1, 2, 4, 8`, choose its bit so the XOR of positions `1..12` whose one-based position has bit `p` set is zero, including position `p`. Choose position 13 so the XOR of all positions `1..13` is zero.

At decode time, recompute the four Hamming checks over positions `1..12`. Their failed-check weights form `syndrome` in `0..15`. Let `overall_mismatch` be the XOR of all 13 received bits.

| Syndrome | Overall mismatch | Result |
|---|---:|---|
| `0` | `0` | No error |
| `0` | `1` | Correct position 13 |
| `1..12` | `1` | Correct the position named by the syndrome |
| Nonzero | `0` | Detect an uncorrectable double-bit error |
| `13..15` | `1` | Treat as uncorrectable rather than indexing outside the codeword |

Any corrected bit increments `corrected_bits`. Any nonzero syndrome or overall mismatch increments `detected_units` for that codeword. A corrected request returns `corrected`; any uncorrectable codeword returns `detected_uncorrectable` with `uncorrectable_error`.

## 7. Deterministic noise

The sender applies noise once to the complete clean `frame_bits`, from left to right. This deterministic generator requires no external random-number dependency and makes experiments reproducible.

1. Set unsigned 64-bit `state = seed`.
2. For each bit, set `state = state + 0x9E3779B97F4A7C15`, wrapping modulo `2^64`.
3. Set `z = state`; then, with 64-bit wrapping multiplication:
   - `z = (z XOR (z >> 30)) * 0xBF58476D1CE4E5B9`
   - `z = (z XOR (z >> 27)) * 0x94D049BB133111EB`
   - `draw = z XOR (z >> 31)`
4. If numerator equals denominator, flip the bit. Otherwise compute `threshold = floor(numerator * 2^64 / denominator)` using wider integer arithmetic and flip exactly when `draw < threshold`.
5. Record the total flips in `noise.flipped_bits`.

For zero probability the threshold is zero and no bit flips. Noise changes only the bit character (`0` to `1` or `1` to `0`); it never changes length or any JSON field.

## 8. Validation and error handling

Validation order is framing/JSON, exact schema, version and ranges, algorithm-specific frame length, integrity algorithm, then printable ASCII. The receiver returns `invalid_request` without metrics before a structurally valid frame exists. It returns `detected_uncorrectable` with metrics after integrity processing begins.

Malformed requests do not terminate a valid-length connection; the receiver sends an error response and continues at the next LF. Internal I/O failures may close only the affected connection. Implementations MUST NOT panic or terminate the listening process because of client input.

Shared deterministic examples are normative and live in [`test-vectors.json`](test-vectors.json).
