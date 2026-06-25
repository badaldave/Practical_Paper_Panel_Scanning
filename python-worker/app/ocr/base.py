from abc import ABC, abstractmethod
from typing import Dict, Any, List

class OCRProvider(ABC):
    @abstractmethod
    def extract_table(self, image_path: str, table_bbox: Dict[str, Any] = None) -> Dict[str, Any]:
        """
        Extracts structural text cells from a table region of an image.
        Returns:
        {
            "rows": [ {"row_index": 0} ],
            "columns": [ {"column_index": 0} ],
            "cells": [
                {
                    "row_index": int,
                    "column_index": int,
                    "value": str,
                    "confidence": float (0.0 to 1.0),
                    "bbox": {"x": float, "y": float, "width": float, "height": float}
                }
            ]
        }
        """
        pass

    def is_available(self) -> bool:
        """Returns True if the underlying OCR library and models are installed and loaded."""
        return False

