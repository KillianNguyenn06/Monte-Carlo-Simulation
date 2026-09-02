import tempfile
import unittest
from pathlib import Path

import pandas as pd

from plot_3d import create_surface_figure, load_surface_data
from plot_path import create_path_figure, load_path_data


class SurfaceDataTests(unittest.TestCase):
    def test_non_square_surface_has_correct_axis_orientation(self):
        with tempfile.TemporaryDirectory() as directory:
            csv_path = Path(directory) / "surface.csv"
            pd.DataFrame(
                [
                    {"Strike_X": 90, "Expiration_Y": 7, "OptionPrice_Z": 17, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                    {"Strike_X": 100, "Expiration_Y": 7, "OptionPrice_Z": 10, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                    {"Strike_X": 90, "Expiration_Y": 14, "OptionPrice_Z": 18, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                    {"Strike_X": 100, "Expiration_Y": 14, "OptionPrice_Z": 11, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                    {"Strike_X": 90, "Expiration_Y": 21, "OptionPrice_Z": 19, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                    {"Strike_X": 100, "Expiration_Y": 21, "OptionPrice_Z": 12, "ContractType": "PUT", "ExerciseStyle": "AMERICAN"},
                ]
            ).to_csv(csv_path, index=False)

            x_values, y_values, z_values, metadata = load_surface_data(csv_path)

            self.assertEqual(x_values.tolist(), [90, 100])
            self.assertEqual(y_values.tolist(), [7, 14, 21])
            self.assertEqual(z_values.shape, (3, 2))
            self.assertEqual(z_values[2, 1], 12)
            self.assertEqual(metadata["ExerciseStyle"], "AMERICAN")
            figure = create_surface_figure(x_values, y_values, z_values, metadata)
            self.assertEqual(len(figure.data), 1)
            self.assertIn("American Put", figure.layout.title.text)

    def test_incomplete_surface_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            csv_path = Path(directory) / "surface.csv"
            pd.DataFrame(
                [
                    {"Strike_X": 90, "Expiration_Y": 7, "OptionPrice_Z": 17},
                    {"Strike_X": 100, "Expiration_Y": 14, "OptionPrice_Z": 11},
                ]
            ).to_csv(csv_path, index=False)
            with self.assertRaises(ValueError):
                load_surface_data(csv_path)


class PathDataTests(unittest.TestCase):
    def test_path_headers_and_values(self):
        with tempfile.TemporaryDirectory() as directory:
            csv_path = Path(directory) / "paths.csv"
            pd.DataFrame(
                [[100, 101, 102], [100, 99, 98]],
                columns=["Step_0", "Step_1", "Step_2"],
            ).to_csv(csv_path, index=False)
            frame = load_path_data(csv_path)
            self.assertEqual(frame.shape, (2, 3))
            self.assertEqual(frame.iloc[1, 2], 98)
            figure = create_path_figure(frame)
            self.assertEqual(len(figure.data), 2)

    def test_invalid_headers_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            csv_path = Path(directory) / "paths.csv"
            pd.DataFrame([[100, 101]], columns=["first", "second"]).to_csv(
                csv_path, index=False
            )
            with self.assertRaises(ValueError):
                load_path_data(csv_path)


if __name__ == "__main__":
    unittest.main()
