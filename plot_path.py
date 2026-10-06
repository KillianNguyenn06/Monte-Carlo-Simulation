import argparse
import math
from pathlib import Path
from typing import Union

import pandas as pd
import plotly.graph_objects as go


def load_path_data(csv_file: Union[str, Path]):
    frame = pd.read_csv(csv_file)
    if frame.empty or len(frame.columns) == 0:
        raise ValueError("Asset-path CSV is empty")
    prefix = "Day_" if str(frame.columns[0]).startswith("Day_") else "Step_"
    if not all(str(column).startswith(prefix) for column in frame.columns):
        raise ValueError("Asset-path CSV headers must consistently use Day_ or Step_ values")
    try:
        coordinates = [float(str(column)[len(prefix):]) for column in frame.columns]
    except ValueError as error:
        raise ValueError("Invalid path time coordinates") from error
    if (not all(math.isfinite(value) for value in coordinates)
            or coordinates[0] != 0
            or any(b <= a for a, b in zip(coordinates, coordinates[1:]))):
        raise ValueError("Path times must start at zero and increase")
    if prefix == "Step_" and coordinates != list(range(len(coordinates))):
        raise ValueError("Legacy step headers must be consecutive from Step_0")
    numeric = frame.apply(pd.to_numeric, errors="raise")
    if numeric.isna().any().any():
        raise ValueError("Asset-path CSV contains missing numeric values")
    return numeric


def create_path_figure(frame: pd.DataFrame, horizon_days=None):
    figure = go.Figure()
    calendar_axis = str(frame.columns[0]).startswith("Day_")
    if horizon_days is not None:
        if not math.isfinite(horizon_days) or horizon_days <= 0 or frame.shape[1] < 2:
            raise ValueError("Horizon must be positive with at least two path points")
        if calendar_axis:
            raise ValueError("Day_ CSV already specifies its horizon; omit --horizon-days")
        time_steps = [step * horizon_days / (frame.shape[1] - 1) for step in range(frame.shape[1])]
        calendar_axis = True
    else:
        time_steps = [float(str(column).split("_", 1)[1]) for column in frame.columns]
    axis_label = "Calendar days from valuation" if calendar_axis else "Simulation steps"
    hover_label = "Day" if calendar_axis else "Step"
    for path_index, row in frame.iterrows():
        figure.add_trace(
            go.Scatter(
                x=time_steps,
                y=row.to_numpy(),
                mode="lines",
                name=f"Path {path_index + 1}",
                hovertemplate=(
                    f"<b>Path {path_index + 1}</b><br>"
                    f"{hover_label}: %{{x:.2f}}<br>Price: $%{{y:.2f}}<extra></extra>"
                ),
                line=dict(width=1.2),
                opacity=0.7,
            )
        )
    figure.update_layout(
        title=dict(
            text="<b>Monte Carlo Asset-Price Paths</b>",
            y=0.95,
            x=0.5,
            xanchor="center",
            yanchor="top",
            font=dict(size=20),
        ),
        xaxis_title=axis_label,
        xaxis=dict(range=[0, time_steps[-1]]),
        yaxis_title="Asset Price ($)",
        template="plotly_dark",
        showlegend=False,
        hovermode="closest",
    )
    return figure


def parse_args():
    parser = argparse.ArgumentParser(description="Plot simulated asset-price paths")
    parser.add_argument("--input", default="AssetPrice.csv", help="Input CSV path")
    parser.add_argument(
        "--output", default="asset_paths_interactive.html", help="Output HTML path"
    )
    parser.add_argument(
        "--no-show", action="store_true", help="Create HTML without opening a browser"
    )
    parser.add_argument("--horizon-days", type=float, help="Actual calendar horizon for legacy Step_ CSVs; relabels existing data only")
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        frame = load_path_data(args.input)
        figure = create_path_figure(frame, args.horizon_days)
    except (FileNotFoundError, pd.errors.EmptyDataError, pd.errors.ParserError, ValueError) as error:
        raise SystemExit(f"Error: {error}") from error

    figure.write_html(args.output)
    print(f"Saved interactive asset paths to '{args.output}'")
    if not args.no_show:
        figure.show()


if __name__ == "__main__":
    main()
