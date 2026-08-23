# Error Detection and Correction Laboratory

This project is an executable two-language TCP experiment for comparing CRC-32 error detection with Hamming SECDED error correction over a simulated unreliable channel. The Rust sender, Go receiver, shared protocol, deterministic vectors, end-to-end integration, and joint launcher are complete. The academic report and experiment analysis remain intentionally out of scope for this implementation handoff.

## Quick start

Go 1.26 (as declared by `receiver-go/go.mod`), Rust 1.85 or newer, Cargo, Bash, and GNU `timeout` are required.

Run from any working directory; the script resolves all project paths from its own location:

```bash
./scripts/run-lab.sh
```

With no arguments, the Rust sender remains interactive. Accept the default receiver address `127.0.0.1:9000`.

Reproduce the normative Hamming noise case:

```bash
./scripts/run-lab.sh \
  --message A \
  --algorithm hamming-secded-13-8 \
  --numerator 1 \
  --denominator 10 \
  --seed 3 \
  --request-id hamming-demo
```

Run a clean CRC request:

```bash
./scripts/run-lab.sh \
  --message "123456789" \
  --algorithm crc32-iso-hdlc \
  --numerator 0 \
  --denominator 1 \
  --seed 42 \
  --request-id crc-demo
```

## Architecture

The implementation maps directly to the laboratory responsibilities and avoids unused abstractions:

| Layer | Rust sender | Go receiver |
|---|---|---|
| Application | Interactive or reproducible CLI | Persistent listener and structured response |
| Presentation | Validate printable ASCII; encode octets MSB-first | Reconstruct and validate printable ASCII |
| Link | Append CRC-32 or encode SECDED codewords | Verify CRC or analyze/correct every SECDED codeword |
| Noise | Apply deterministic SplitMix64 once to the complete frame | Trust the received `frame_bits`; never reapply noise |
| Transmission | Send one LF-terminated JSON request and read one response | Process multiple ordered NDJSON lines per TCP connection |

TCP/IPv4 carries only the envelope. [`protocol/wire-protocol.md`](protocol/wire-protocol.md) and [`protocol/test-vectors.json`](protocol/test-vectors.json) are normative; endpoint code must not redefine the contract.

## Repository map

```text
.
├── protocol/                 # Normative wire contract and deterministic vectors
├── receiver-go/              # Persistent Go server, algorithms, tests, and guide
├── scripts/run-lab.sh        # Joint lifecycle launcher
├── sender-rust/              # Rust CLI sender and tests
└── docs/                     # Assignment source material
```

## Joint launcher

`scripts/run-lab.sh`:

1. verifies that `go` and `cargo` exist;
2. builds the receiver into a temporary directory;
3. starts only that receiver in the background;
4. actively waits for its TCP port with a one-second timeout per probe and a bounded total startup timeout;
5. runs the Rust sender in the foreground with the supplied arguments; and
6. stops only the receiver PID it created on success, failure, or signal.

Configuration:

| Environment variable | Default | Meaning |
|---|---:|---|
| `HOST` | `127.0.0.1` | Receiver listen host and non-interactive sender target |
| `PORT` | `9000` | Receiver port and non-interactive sender target |
| `STARTUP_TIMEOUT_SECONDS` | `10` | Maximum readiness wait |

Example with another port:

```bash
PORT=9100 ./scripts/run-lab.sh --message A --algorithm hamming --request-id port-demo
```

For non-interactive use, the script appends `--host` and `--port` after the supplied arguments so both processes always use `HOST` and `PORT`. In interactive mode, enter the same address when using non-default environment values.

## Manual commands

Terminal 1:

```bash
cd receiver-go
go run . -host 127.0.0.1 -port 9000
```

Terminal 2:

```bash
cd sender-rust
cargo run -- \
  --host 127.0.0.1 \
  --port 9000 \
  --message A \
  --algorithm hamming-secded-13-8 \
  --numerator 1 \
  --denominator 10 \
  --seed 3 \
  --request-id manual-demo
```

See [`receiver-go/README.md`](receiver-go/README.md) and [`sender-rust/README.md`](sender-rust/README.md) for endpoint details.

## Verification

Go receiver, including the real Rust-Go integration:

```bash
cd receiver-go
gofmt -w .
go test ./...
go vet ./...
```

Rust sender, without changing source files:

```bash
cd sender-rust
cargo fmt --check
cargo clippy --all-targets --all-features -- -D warnings
cargo test
```

Use `go test -short ./...` only when intentionally skipping the external Cargo process.

## Implementation status

| Work unit | Status |
|---|---|
| Normative NDJSON/TCP protocol and vectors | Complete |
| Rust sender, encoding, deterministic noise, and response validation | Complete |
| Go persistent receiver, strict validation, CRC, SECDED, and metrics | Complete |
| Hostile-input, normative-vector, persistent-connection, and listener tests | Complete |
| Deterministic real Rust-Go integration test | Complete |
| Safe joint launcher | Complete |
| Experiment CSV, graphs, discussion, conclusions, and academic report | Teammate handoff |

## Team handoff

### Implemented here

- The protocol contract and deterministic vectors are frozen.
- The Rust sender creates already-noisy `frame_bits`; the Go receiver never simulates noise.
- The receiver rejects duplicate and unknown JSON keys, processes multiple lines per connection, closes only an overlong client connection, and remains alive after malformed input.
- Both algorithms return the exact protocol statuses, errors, and metrics.
- The integration test executes the actual Rust binary against an in-process Go TCP server.

### Remaining teammate checklist

- [ ] Define the experiment matrix: messages, both algorithms, probabilities, seeds, and repetitions.
- [ ] Run reproducible experiments and capture raw results as CSV.
- [ ] Preserve raw CSV separately from any cleaned or aggregated dataset.
- [ ] Generate labeled graphs comparing detection, correction, overhead, and failure behavior.
- [ ] Explain the experimental method and connect observations to CRC and SECDED theory.
- [ ] Discuss limitations, including independent bit flips, printable-ASCII scope, and TCP not being the simulated unreliable channel.
- [ ] Write conclusions supported by the collected data.
- [ ] Assemble the academic report using the course rubric and required format.
- [ ] Include exact commands, parameter values, and seeds so results are reproducible.
- [ ] Re-run both language suites and at least one joint launcher example before submission.

Do not copy implementation documentation verbatim as analysis. The report must interpret measured evidence.

## Operational boundaries

- The server has no authentication or TLS; default loopback binding is intentional.
- The receiver handles at most 64 clients concurrently, closes excess connections, and applies 10-second read and write deadlines per operation. The listener remains available after limits or timeouts are reached.
- The sender limits connection, request writing, and response reading to 10 seconds each. Address resolution still depends on the operating-system resolver.
- Launcher readiness probes are individually limited to one second, pass host and port as positional shell arguments, and cleanup targets only the receiver PID created by the launcher.
- A SECDED codeword can reliably correct one bit and detect two; patterns with more errors are outside that guarantee.
- CRC detects corruption but cannot correct it.
