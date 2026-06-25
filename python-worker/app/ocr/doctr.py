import logging
from typing import Dict, Any
from app.ocr.base import OCRProvider

try:
    from doctr.models import ocr_predictor
    from doctr.io import DocumentFile
    DOCTR_AVAILABLE = True
except ImportError:
    DOCTR_AVAILABLE = False

class DocTRProvider(OCRProvider):
    def __init__(self):
        self.logger = logging.getLogger("DocTRProvider")
        if DOCTR_AVAILABLE:
            try:
                # Load pre-trained models
                self.predictor = ocr_predictor(pretrained=True)
                self.logger.info("DocTR models loaded successfully.")
            except Exception as e:
                self.logger.error(f"Failed to load DocTR models: {e}")
                self.predictor = None
        else:
            self.logger.warning("doctr library not installed. Falling back to mock extraction mode.")
            self.predictor = None

    def is_available(self) -> bool:
        return self.predictor is not None

    def extract_table(self, image_path: str, table_bbox: Dict[str, Any] = None) -> Dict[str, Any]:
        if not DOCTR_AVAILABLE or not self.predictor:
            # Standalone demo mockup
            return {
                "rows": [{"row_index": i} for i in range(2)],
                "columns": [{"column_index": j} for j in range(3)],
                "cells": [
                    {"row_index": 0, "column_index": 0, "value": "ID", "confidence": 0.99, "bbox": {"x": 10, "y": 10, "width": 10, "height": 5}},
                    {"row_index": 0, "column_index": 1, "value": "Subject", "confidence": 0.99, "bbox": {"x": 25, "y": 10, "width": 40, "height": 5}},
                    {"row_index": 0, "column_index": 2, "value": "Marks", "confidence": 0.99, "bbox": {"x": 70, "y": 10, "width": 20, "height": 5}},
                    {"row_index": 1, "column_index": 0, "value": "MAT101", "confidence": 0.97, "bbox": {"x": 10, "y": 15, "width": 10, "height": 5}},
                    {"row_index": 1, "column_index": 1, "value": "Mathematics I", "confidence": 0.96, "bbox": {"x": 25, "y": 15, "width": 40, "height": 5}},
                    {"row_index": 1, "column_index": 2, "value": "90", "confidence": 0.98, "bbox": {"x": 70, "y": 15, "width": 20, "height": 5}},
                ]
            }

        try:
            doc = DocumentFile.from_images(image_path)
            result = self.predictor(doc)
            
            cells = []
            col_idx = 0
            
            # DocTR returns pages, blocks, lines, words
            for page in result.pages:
                for block in page.blocks:
                    for line in block.lines:
                        for word in line.words:
                            text = word.value
                            confidence = word.confidence
                            # DocTR bbox format is (xmin, ymin, xmax, ymax) relative coordinates
                            geom = word.geometry
                            
                            cells.append({
                                "row_index": 0,
                                "column_index": col_idx,
                                "value": text,
                                "confidence": float(confidence),
                                "bbox": {
                                    "x": geom[0][0],
                                    "y": geom[0][1],
                                    "width": geom[1][0] - geom[0][0],
                                    "height": geom[1][1] - geom[0][1]
                                }
                            })
                            col_idx += 1

            return {
                "rows": [{"row_index": 0}],
                "columns": [{"column_index": i} for i in range(col_idx)],
                "cells": cells
            }
        except Exception as e:
            self.logger.error(f"Error running DocTR predictor: {e}")
            raise e
