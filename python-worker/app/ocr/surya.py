import logging
from typing import Dict, Any
from app.ocr.base import OCRProvider

try:
    from surya.detection import DetectionPredictor
    from surya.foundation import FoundationPredictor
    from surya.recognition import RecognitionPredictor
    from surya.common.surya.schema import TaskNames
    from PIL import Image
    SURYA_AVAILABLE = True
except ImportError:
    SURYA_AVAILABLE = False

class SuryaOCRProvider(OCRProvider):
    def __init__(self):
        self.logger = logging.getLogger("SuryaOCRProvider")
        if SURYA_AVAILABLE:
            try:
                self.foundation_predictor = FoundationPredictor()
                self.det_predictor = DetectionPredictor()
                self.rec_predictor = RecognitionPredictor(self.foundation_predictor)
                # Surya/torch auto-select CUDA when a usable GPU is present and
                # fall back to CPU otherwise. Log which path we got so operators
                # can confirm GPU acceleration without a separate check.
                try:
                    import torch
                    if torch.cuda.is_available():
                        device = f"GPU ({torch.cuda.get_device_name(0)})"
                    else:
                        device = "CPU (no usable CUDA device — auto fallback)"
                except Exception:
                    device = "unknown (torch not importable)"
                self.logger.info(f"Surya OCR models loaded successfully. Inference device: {device}")
            except Exception as e:
                self.logger.error(f"Failed to load Surya OCR models: {e}")
                self.foundation_predictor = None
                self.det_predictor = None
                self.rec_predictor = None
        else:
            self.logger.warning("surya-ocr library not installed. Falling back to mock extraction mode.")
            self.foundation_predictor = None
            self.det_predictor = None
            self.rec_predictor = None

    def is_available(self) -> bool:
        return self.rec_predictor is not None

    def extract_table(self, image_path: str, table_bbox: Dict[str, Any] = None) -> Dict[str, Any]:
        if not SURYA_AVAILABLE or not self.rec_predictor:
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

        # Real Surya OCR Pipeline
        try:
            image = Image.open(image_path)
            # Crop image if bounding box is provided
            if table_bbox:
                w, h = image.size
                left = table_bbox["x"] * w if table_bbox["x"] < 1.0 else table_bbox["x"]
                top = table_bbox["y"] * h if table_bbox["y"] < 1.0 else table_bbox["y"]
                right = left + (table_bbox["width"] * w if table_bbox["width"] < 1.0 else table_bbox["width"])
                bottom = top + (table_bbox["height"] * h if table_bbox["height"] < 1.0 else table_bbox["height"])
                image = image.crop((left, top, right, bottom))

            # Run Surya OCR
            ocr_results = self.rec_predictor(
                [image],
                task_names=[TaskNames.ocr_with_boxes],
                det_predictor=self.det_predictor,
                langs=[["en"]]
            )
            
            cells = []
            col_idx = 0
            if ocr_results:
                page_result = ocr_results[0]
                for text_line in page_result.text_lines:
                    bbox = text_line.bbox # [x1, y1, x2, y2]
                    text = text_line.text
                    confidence = getattr(text_line, "confidence", 0.90)

                    cells.append({
                        "row_index": 0,
                        "column_index": col_idx,
                        "value": text,
                        "confidence": float(confidence),
                        "bbox": {
                            "x": bbox[0],
                            "y": bbox[1],
                            "width": bbox[2] - bbox[0],
                            "height": bbox[3] - bbox[1]
                        }
                    })
                    col_idx += 1

            return {
                "rows": [{"row_index": 0}],
                "columns": [{"column_index": i} for i in range(col_idx)],
                "cells": cells
            }
        except Exception as e:
            self.logger.error(f"Error running Surya OCR: {e}")
            raise e
