import pandas as pd
import plotly.graph_objects as go
import sys

def main():
    csv_file = "AssetPrice.csv"
    
    # 1. Load the CSV file safely
    try:
        df = pd.read_csv(csv_file, header=None)
        
        # Check if row 0 contains text headers (like "Step_0")
        first_cell = str(df.iloc[0, 0])
        if "Step" in first_cell or "Time" in first_cell:
            df = df.iloc[1:].reset_index(drop=True)

        # Force all matrix values to float numbers
        df = df.astype(float)

    except FileNotFoundError:
        print(f"Error: Could not find '{csv_file}'. Make sure your Go program ran first!")
        sys.exit(1)

    # 2. Create Plotly figure
    fig = go.Figure()

    # Extract time step indices for X-axis (0, 1, 2, ..., N)
    time_steps = list(range(df.shape[1]))

    # 3. Add each simulation path as a line on the chart
    for i in range(len(df)):
        path_data = df.iloc[i].values  # Extract row of prices
        
        fig.add_trace(
            go.Scatter(
                x=time_steps,
                y=path_data,
                mode='lines',
                name=f'Path {i + 1}',
                hovertemplate=f'<b>Path {i + 1}</b><br>Step: %{{x}}<br>Price: $%{{y:.2f}}<extra></extra>',
                line=dict(width=1.2),
                opacity=0.7  # Slight transparency helps see dense overlapping paths
            )
        )

    # 4. Customize Layout and Styling
    fig.update_layout(
        title={
            'text': "<b>Monte-Carlo Simulation of Asset Price Paths</b>",
            'y': 0.95,
            'x': 0.5,
            'xanchor': 'center',
            'yanchor': 'top',
            'font': dict(size=20)
        },
        xaxis_title="Time Steps (Days)",
        yaxis_title="Asset Price ($)",
        template="plotly_dark",  # Dark mode theme
        showlegend=False,        
        hovermode="x unified"    
    )

    # 5. Export to HTML & Show Plot
    output_html = "asset_paths_interactive.html"
    fig.write_html(output_html)
    print(f"\tSuccessfully saved interactive plot to '{output_html}'")
    
    # Opens in default web browser automatically
    fig.show()

if __name__ == "__main__":
    main()