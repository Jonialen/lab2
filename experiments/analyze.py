#!/usr/bin/env python3
"""Aggregate raw_results.csv and render the report figures.

Reads the untouched raw CSV produced by run_experiments.py, writes a
separate aggregated summary CSV, and saves PNG figures used directly by the
report.
"""

from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import pandas as pd

ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = ROOT / "experiments" / "data"
FIG_DIR = ROOT / "experiments" / "figures"
RAW_CSV = DATA_DIR / "raw_results.csv"
SUMMARY_CSV = DATA_DIR / "summary_by_probability.csv"
OVERHEAD_CSV = DATA_DIR / "summary_overhead.csv"

ALGO_LABELS = {
    "crc32-iso-hdlc": "CRC-32/ISO-HDLC",
    "hamming-secded-13-8": "Hamming SECDED(13,8)",
}
ALGO_COLORS = {
    "crc32-iso-hdlc": "#2E5EAA",
    "hamming-secded-13-8": "#C1502E",
}


def load_raw():
    df = pd.read_csv(RAW_CSV)
    df["message_correct"] = df["message_correct"].map(
        {"True": True, "False": False, True: True, False: False}
    )
    return df


def build_summary(main_df: pd.DataFrame) -> pd.DataFrame:
    grouped = main_df.groupby(["size", "algorithm", "probability_numerator",
                                "probability_denominator", "probability"])
    summary = grouped.agg(
        n=("status", "size"),
        ok_rate=("status", lambda s: (s == "ok").mean()),
        corrected_rate=("status", lambda s: (s == "corrected").mean()),
        uncorrectable_rate=("status", lambda s: (s == "detected_uncorrectable").mean()),
        mean_detected_units=("detected_units", "mean"),
        mean_corrected_bits=("corrected_bits", "mean"),
        mean_uncorrectable_units=("uncorrectable_units", "mean"),
        mean_overhead_ratio=("overhead_ratio", "mean"),
        silent_corruption=(
            "message_correct",
            lambda s: (~s.dropna().astype(bool)).sum() if s.notna().any() else 0,
        ),
    ).reset_index()
    summary["success_rate"] = summary["ok_rate"] + summary["corrected_rate"]
    return summary.sort_values(["size", "algorithm", "probability"])


def build_overhead_summary(overhead_df: pd.DataFrame) -> pd.DataFrame:
    return overhead_df[
        ["size", "algorithm", "source_bits", "redundancy_bits", "overhead_ratio"]
    ].sort_values(["algorithm", "size"])


def plot_success_rate_by_size(summary: pd.DataFrame):
    sizes = sorted(summary["size"].unique())
    fig, axes = plt.subplots(2, 2, figsize=(11, 8), sharey=True)
    for ax, size in zip(axes.flat, sizes):
        subset = summary[summary["size"] == size]
        for algorithm, group in subset.groupby("algorithm"):
            group = group.sort_values("probability")
            xs = group["probability"].replace(0, 1e-6)
            ax.plot(xs, group["success_rate"], marker="o",
                    label=ALGO_LABELS[algorithm], color=ALGO_COLORS[algorithm])
        ax.set_xscale("log")
        ax.set_title(f"Tamaño de mensaje = {size} caracteres")
        ax.set_xlabel("Probabilidad de error por bit (escala log)")
        ax.set_ylabel("Tasa de éxito (ok + corrected)")
        ax.set_ylim(-0.05, 1.05)
        ax.grid(True, which="both", alpha=0.3)
    axes.flat[0].legend(loc="lower left")
    fig.suptitle("Tasa de éxito vs. probabilidad de error, por tamaño de mensaje")
    fig.tight_layout()
    fig.savefig(FIG_DIR / "success_rate_by_size.png", dpi=150, bbox_inches="tight")
    plt.close(fig)


def plot_success_rate_comparison(summary: pd.DataFrame, size: int = 128):
    subset = summary[summary["size"] == size]
    fig, ax = plt.subplots(figsize=(7, 5))
    for algorithm, group in subset.groupby("algorithm"):
        group = group.sort_values("probability")
        xs = group["probability"].replace(0, 1e-6)
        ax.plot(xs, group["success_rate"], marker="o",
                label=ALGO_LABELS[algorithm], color=ALGO_COLORS[algorithm])
    ax.set_xscale("log")
    ax.set_xlabel("Probabilidad de error por bit (escala log)")
    ax.set_ylabel("Tasa de éxito (ok + corrected)")
    ax.set_ylim(-0.05, 1.05)
    ax.set_title(f"CRC-32 vs. Hamming SECDED — tamaño de mensaje = {size} caracteres")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(FIG_DIR / "success_rate_comparison.png", dpi=150, bbox_inches="tight")
    plt.close(fig)


def plot_hamming_status_breakdown(summary: pd.DataFrame, size: int = 128):
    subset = summary[
        (summary["size"] == size) & (summary["algorithm"] == "hamming-secded-13-8")
    ].sort_values("probability")
    xs = subset["probability"].replace(0, 1e-6)
    fig, ax = plt.subplots(figsize=(7, 5))
    ax.stackplot(
        xs,
        subset["ok_rate"],
        subset["corrected_rate"],
        subset["uncorrectable_rate"],
        labels=["ok (sin errores)", "corrected (corregido)", "detected_uncorrectable"],
        colors=["#3E8E5A", "#C1962E", "#A33A3A"],
        alpha=0.85,
    )
    ax.set_xscale("log")
    ax.set_xlabel("Probabilidad de error por bit (escala log)")
    ax.set_ylabel("Proporción de solicitudes")
    ax.set_title(f"Desglose de resultados — Hamming SECDED(13,8), tamaño = {size}")
    ax.set_ylim(0, 1)
    ax.legend(loc="center left", bbox_to_anchor=(1.0, 0.5))
    fig.tight_layout()
    fig.savefig(FIG_DIR / "hamming_status_breakdown.png", dpi=150, bbox_inches="tight")
    plt.close(fig)


def plot_overhead_vs_size(overhead_summary: pd.DataFrame):
    fig, ax = plt.subplots(figsize=(7, 5))
    for algorithm, group in overhead_summary.groupby("algorithm"):
        group = group.sort_values("size")
        ax.plot(group["size"], group["overhead_ratio"] * 100, marker="o",
                label=ALGO_LABELS[algorithm], color=ALGO_COLORS[algorithm])
    ax.set_xscale("log", base=2)
    ax.set_xlabel("Tamaño del mensaje (caracteres, escala log2)")
    ax.set_ylabel("Overhead (redundancy_bits / source_bits, %)")
    ax.set_title("Overhead de redundancia vs. tamaño del mensaje")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(FIG_DIR / "overhead_vs_size.png", dpi=150, bbox_inches="tight")
    plt.close(fig)


def plot_mean_corrected_bits(summary: pd.DataFrame, size: int = 128):
    subset = summary[
        (summary["size"] == size) & (summary["algorithm"] == "hamming-secded-13-8")
    ].sort_values("probability")
    xs = subset["probability"].replace(0, 1e-6)
    fig, ax = plt.subplots(figsize=(7, 5))
    ax.plot(xs, subset["mean_corrected_bits"], marker="o", color=ALGO_COLORS["hamming-secded-13-8"],
             label="Bits corregidos promedio por solicitud")
    ax.plot(xs, subset["mean_uncorrectable_units"], marker="s", color="#A33A3A",
             linestyle="--", label="Codewords no corregibles promedio")
    ax.set_xscale("log")
    ax.set_xlabel("Probabilidad de error por bit (escala log)")
    ax.set_ylabel("Promedio por solicitud")
    ax.set_title(f"Hamming SECDED(13,8) — esfuerzo de corrección, tamaño = {size}")
    ax.grid(True, which="both", alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(FIG_DIR / "hamming_correction_effort.png", dpi=150, bbox_inches="tight")
    plt.close(fig)


def main():
    FIG_DIR.mkdir(parents=True, exist_ok=True)
    df = load_raw()

    main_df = df[df["sweep"] == "main"].copy()
    overhead_df = df[df["sweep"] == "overhead"].copy()

    summary = build_summary(main_df)
    summary.to_csv(SUMMARY_CSV, index=False)

    overhead_summary = build_overhead_summary(overhead_df)
    overhead_summary.to_csv(OVERHEAD_CSV, index=False)

    plot_success_rate_by_size(summary)
    plot_success_rate_comparison(summary, size=128)
    plot_hamming_status_breakdown(summary, size=128)
    plot_overhead_vs_size(overhead_summary)
    plot_mean_corrected_bits(summary, size=128)

    print(f"Wrote {SUMMARY_CSV}")
    print(f"Wrote {OVERHEAD_CSV}")
    print(f"Wrote figures to {FIG_DIR}")


if __name__ == "__main__":
    main()
