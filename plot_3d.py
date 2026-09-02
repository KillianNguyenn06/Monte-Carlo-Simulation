import argparse
from pathlib import Path
from typing import Union

import pandas as pd
import plotly.graph_objects as go


def load_surface_data(csv_file: Union[str, Path]):
    frame = pd.read_csv(csv_file)
    required = {"Strike_X", "Expiration_Y", "OptionPrice_Z"}
    missing = required.difference(frame.columns)
    if missing:
        raise ValueError(f"Missing required columns: {', '.join(sorted(missing))}")
    if frame.empty:
        raise ValueError("Option-price CSV is empty")

    numeric = frame[["Strike_X", "Expiration_Y", "OptionPrice_Z"]].apply(
        pd.to_numeric, errors="raise"
    )
    if numeric.isna().any().any():
        raise ValueError("Option-price CSV contains missing numeric values")
    if numeric.duplicated(subset=["Strike_X", "Expiration_Y"]).any():
        raise ValueError("Option-price CSV contains duplicate strike/expiration pairs")

    surface = (
        numeric.pivot(
            index="Expiration_Y", columns="Strike_X", values="OptionPrice_Z"
        )
        .sort_index()
        .sort_index(axis=1)
    )
    if surface.isna().any().any():
        raise ValueError("Option-price CSV does not contain a complete rectangular grid")
    metadata = {}
    for column in ("ContractType", "ExerciseStyle"):
        if column in frame.columns:
            values = frame[column].dropna().astype(str).unique()
            if len(values) == 1:
                metadata[column] = values[0]
    return (
        surface.columns.to_numpy(),
        surface.index.to_numpy(),
        surface.to_numpy(),
        metadata,
    )


def create_surface_figure(x_values, y_values, z_values, metadata=None):
    metadata = metadata or {}
    style = metadata.get("ExerciseStyle", "").title()
    contract = metadata.get("ContractType", "").title()
    model_label = " ".join(value for value in (style, contract) if value)
    title = "Interactive Monte Carlo Option-Price Surface"
    if model_label:
        title = f"{title} — {model_label}"
    figure = go.Figure(
        data=[
            go.Surface(
                x=x_values,
                y=y_values,
                z=z_values,
                colorscale="Inferno",
                colorbar=dict(title="Option Price ($)"),
                hovertemplate=(
                    "Strike: $%{x:.2f}<br>"
                    "Expiration: %{y} days<br>"
                    "Option Price: $%{z:.4f}<extra></extra>"
                ),
            )
        ]
    )
    figure.update_layout(
        title=title,
        scene=dict(
            xaxis_title="Strike Price ($)",
            yaxis_title="Expiration (Days)",
            zaxis_title="Option Price ($)",
            aspectmode="manual",
            aspectratio=dict(x=1, y=1, z=0.8),
            camera=dict(eye=dict(x=1.6, y=1.6, z=1.2)),
        ),
        autosize=True,
        margin=dict(l=65, r=50, b=65, t=90),
    )
    return figure


def parse_args():
    parser = argparse.ArgumentParser(description="Plot the option-price CSV surface")
    parser.add_argument("--input", default="MonteCarloSim.csv", help="Input CSV path")
    parser.add_argument(
        "--output", default="option_surface_interactive.html", help="Output HTML path"
    )
    parser.add_argument(
        "--no-show", action="store_true", help="Create HTML without opening a browser"
    )
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        x_values, y_values, z_values, metadata = load_surface_data(args.input)
    except (FileNotFoundError, pd.errors.EmptyDataError, pd.errors.ParserError, ValueError) as error:
        raise SystemExit(f"Error: {error}") from error

    figure = create_surface_figure(x_values, y_values, z_values, metadata)
    figure.write_html(args.output)
    print(f"Saved interactive option-price surface to '{args.output}'")
    if not args.no_show:
        figure.show()


if __name__ == "__main__":
    main()
