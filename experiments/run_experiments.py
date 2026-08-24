#!/usr/bin/env python3
"""Experiment driver for Laboratory 2.

Starts the real Go receiver, drives the real Rust sender binary through a
matrix of message sizes, algorithms, noise probabilities, and seeds, and
records one raw CSV row per request. No noise or algorithm logic is
reimplemented here: every bit flip, integrity check, and correction is
produced by the actual lab implementation over a real TCP connection.
"""

import csv
import random
import socket
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RECEIVER_BIN = ROOT / "receiver-go" / "receiver-go"
SENDER_BIN = ROOT / "sender-rust" / "target" / "release" / "lab2-sender"
RAW_CSV = ROOT / "experiments" / "data" / "raw_results.csv"

HOST = "127.0.0.1"
PORT = 9321

ALGORITHMS = ["crc32-iso-hdlc", "hamming-secded-13-8"]

# Main sweep: success/overhead behavior across sizes, probabilities, algorithms.
MAIN_SIZES = [8, 32, 128, 512]
PROBABILITIES = [
    (0, 1),
    (1, 100_000),
    (1, 10_000),
    (1, 1_000),
    (1, 200),
    (1, 100),
    (1, 50),
    (1, 20),
    (1, 10),
]
REPETITIONS = 20  # seeds 0..REPETITIONS-1 per (size, algorithm, probability)

# Overhead-focused sweep: fine-grained sizes at zero noise (redundancy ratio
# depends only on source length and algorithm, not on noise).
OVERHEAD_SIZES = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024]


def make_message(length: int, tag: str) -> str:
    rng = random.Random(f"msg-{tag}-{length}")
    return "".join(chr(rng.randint(0x20, 0x7E)) for _ in range(length))


def wait_for_port(host: str, port: int, timeout: float = 10.0) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.5):
                return True
        except OSError:
            time.sleep(0.05)
    return False


def parse_output(stdout: str):
    status = None
    message = None
    metrics = {}
    for line in stdout.splitlines():
        if line.startswith("Status: "):
            status = line[len("Status: ") :].strip()
        elif line.startswith("Message: "):
            message = line[len("Message: ") :]
        elif line.startswith("Metrics: "):
            body = line[len("Metrics: ") :]
            for part in body.split(", "):
                key, _, value = part.partition("=")
                key = key.strip()
                value = value.strip().split(" ")[0]
                metrics[key] = int(value)
    return status, message, metrics


def run_case(message, algorithm, numerator, denominator, seed, request_id):
    try:
        result = subprocess.run(
            [
                str(SENDER_BIN),
                "--message", message,
                "--algorithm", algorithm,
                "--numerator", str(numerator),
                "--denominator", str(denominator),
                "--seed", str(seed),
                "--request-id", request_id,
                "--host", HOST,
                "--port", str(PORT),
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
    except subprocess.TimeoutExpired:
        return "process_timeout", None, {}, ""

    if result.returncode != 0:
        return "sender_error", None, {}, result.stderr.strip()

    status, message_out, metrics = parse_output(result.stdout)
    return status, message_out, metrics, ""


def build_cases():
    cases = []
    request_counter = 0

    for size in MAIN_SIZES:
        message = make_message(size, "main")
        for algorithm in ALGORITHMS:
            for numerator, denominator in PROBABILITIES:
                for rep in range(REPETITIONS):
                    request_counter += 1
                    cases.append(
                        {
                            "sweep": "main",
                            "size": size,
                            "algorithm": algorithm,
                            "message": message,
                            "numerator": numerator,
                            "denominator": denominator,
                            "seed": rep,
                            "request_id": f"main-{request_counter}",
                        }
                    )

    for size in OVERHEAD_SIZES:
        message = make_message(size, "overhead")
        for algorithm in ALGORITHMS:
            request_counter += 1
            cases.append(
                {
                    "sweep": "overhead",
                    "size": size,
                    "algorithm": algorithm,
                    "message": message,
                    "numerator": 0,
                    "denominator": 1,
                    "seed": 0,
                    "request_id": f"overhead-{request_counter}",
                }
            )

    return cases


def main():
    if not SENDER_BIN.exists():
        sys.exit(f"missing sender binary: {SENDER_BIN} (run cargo build --release first)")

    subprocess.run(
        ["go", "build", "-o", str(RECEIVER_BIN), "."],
        cwd=ROOT / "receiver-go",
        check=True,
    )

    receiver = subprocess.Popen(
        [str(RECEIVER_BIN), "-host", HOST, "-port", str(PORT)],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        if not wait_for_port(HOST, PORT):
            sys.exit("receiver did not open its listening port in time")

        cases = build_cases()
        RAW_CSV.parent.mkdir(parents=True, exist_ok=True)
        fieldnames = [
            "sweep", "size", "algorithm", "probability_numerator",
            "probability_denominator", "probability", "seed", "request_id",
            "status", "message_correct", "sender_note",
            "received_bits", "source_bits", "redundancy_bits",
            "reported_flipped_bits", "detected_units", "corrected_bits",
            "uncorrectable_units", "overhead_ratio",
        ]

        with RAW_CSV.open("w", newline="") as handle:
            writer = csv.DictWriter(handle, fieldnames=fieldnames)
            writer.writeheader()

            for index, case in enumerate(cases, start=1):
                status, message_out, metrics, note = run_case(
                    case["message"],
                    case["algorithm"],
                    case["numerator"],
                    case["denominator"],
                    case["seed"],
                    case["request_id"],
                )

                message_correct = ""
                if status in ("ok", "corrected"):
                    message_correct = message_out == case["message"]

                source_bits = metrics.get("source")
                redundancy_bits = metrics.get("redundancy")
                overhead_ratio = (
                    redundancy_bits / source_bits
                    if source_bits and redundancy_bits is not None
                    else ""
                )
                probability = (
                    case["numerator"] / case["denominator"]
                    if case["denominator"]
                    else ""
                )

                writer.writerow(
                    {
                        "sweep": case["sweep"],
                        "size": case["size"],
                        "algorithm": case["algorithm"],
                        "probability_numerator": case["numerator"],
                        "probability_denominator": case["denominator"],
                        "probability": probability,
                        "seed": case["seed"],
                        "request_id": case["request_id"],
                        "status": status,
                        "message_correct": message_correct,
                        "sender_note": note,
                        "received_bits": metrics.get("received", ""),
                        "source_bits": source_bits if source_bits is not None else "",
                        "redundancy_bits": redundancy_bits if redundancy_bits is not None else "",
                        "reported_flipped_bits": metrics.get("reported_flips", ""),
                        "detected_units": metrics.get("detected_units", ""),
                        "corrected_bits": metrics.get("corrected_bits", ""),
                        "uncorrectable_units": metrics.get("uncorrectable_units", ""),
                        "overhead_ratio": overhead_ratio,
                    }
                )

                if index % 100 == 0 or index == len(cases):
                    print(f"[{index}/{len(cases)}] {case['sweep']} size={case['size']} "
                          f"algo={case['algorithm']} p={case['numerator']}/{case['denominator']} "
                          f"seed={case['seed']} -> {status}")
    finally:
        receiver.terminate()
        try:
            receiver.wait(timeout=5)
        except subprocess.TimeoutExpired:
            receiver.kill()

    print(f"\nWrote {RAW_CSV}")


if __name__ == "__main__":
    main()
