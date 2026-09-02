import argparse
from pathlib import Path
from typing import Union

import pandas as pd
import plotly.graph_objects as go


def load_path_data(csv_file: Union[str, Path]):
    frame = pd.read_csv(csv_file)
    if frame.empty or len(frame.columns) == 0:
        raise ValueError("Asset-path CSV is empty")
    if not all(str(column).startswith("Step_") for column in frame.columns):
        raise ValueError("Asset-path CSV headers must use Step_0, Step_1, ...")
    numeric = frame.apply(pd.to_numeric, errors="raise")
    if numeric.isna().any().any():
        raise ValueError("Asset-path CSV contains missing numeric values")
    return numeric


def create_path_figure(frame: pd.DataFrame):
    figure = go.Figure()
    time_steps = list(range(frame.shape[1]))
    for path_index, row in frame.iterrows():
        figure.add_trace(
            go.Scatter(
                x=time_steps,
                y=row.to_numpy(),
                mode="lines",
                name=f"Path {path_index + 1}",
                hovertemplate=(
                    f"<b>Path {path_index + 1}</b><br>"
                    "Step: %{x}<br>Price: $%{y:.2f}<extra></extra>"
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
        xaxis_title="Trading Steps",
        yaxis_title="Asset Price ($)",
        template="plotly_dark",
        showlegend=False,
        hovermode="x unified",
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
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        frame = load_path_data(args.input)
    except (FileNotFoundError, pd.errors.EmptyDataError, pd.errors.ParserError, ValueError) as error:
        raise SystemExit(f"Error: {error}") from error

    figure = create_path_figure(frame)
    figure.write_html(args.output)
    print(f"Saved interactive asset paths to '{args.output}'")
    if not args.no_show:
        figure.show()


if __name__ == "__main__":
    main()
