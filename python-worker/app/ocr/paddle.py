import logging
from typing import Dict, Any
from app.ocr.base import OCRProvider

try:
    from paddleocr import PaddleOCR
    PADDLE_AVAILABLE = True
except ImportError:
    PADDLE_AVAILABLE = False

class PaddleOCRProvider(OCRProvider):
    def __init__(self):
        self.logger = logging.getLogger("PaddleOCRProvider")
        if PADDLE_AVAILABLE:
            try:
                # Initialize CPU-only PaddleOCR with english language configuration
                self.ocr = PaddleOCR(use_angle_cls=True, lang='en', enable_mkldnn=False)
                self.logger.info("PaddleOCR engine loaded successfully.")
            except Exception as e:
                self.logger.error(f"Failed to load PaddleOCR engine: {e}")
                self.ocr = None
        else:
            self.logger.warning("paddleocr library not installed. Falling back to mock extraction mode.")
            self.ocr = None

    def is_available(self) -> bool:
        return self.ocr is not None

    def extract_table(self, image_path: str, table_bbox: Dict[str, Any] = None) -> Dict[str, Any]:
        if not self.ocr:
            # Standalone demo mockup for validation
            return {
                "rows": [{"row_index": i} for i in range(4)],
                "columns": [{"column_index": j} for j in range(4)],
                "cells": [
                    {"row_index": 0, "column_index": 0, "value": "Roll No", "confidence": 0.99, "bbox": {"x": 10, "y": 10, "width": 20, "height": 5}},
                    {"row_index": 0, "column_index": 1, "value": "Name", "confidence": 0.99, "bbox": {"x": 30, "y": 10, "width": 30, "height": 5}},
                    {"row_index": 0, "column_index": 2, "value": "Score", "confidence": 0.99, "bbox": {"x": 60, "y": 10, "width": 20, "height": 5}},
                    {"row_index": 1, "column_index": 0, "value": "201", "confidence": 0.98, "bbox": {"x": 10, "y": 15, "width": 20, "height": 5}},
                    {"row_index": 1, "column_index": 1, "value": "John Doe", "confidence": 0.95, "bbox": {"x": 30, "y": 15, "width": 30, "height": 5}},
                    {"row_index": 1, "column_index": 2, "value": "8S", "confidence": 0.62, "bbox": {"x": 60, "y": 15, "width": 20, "height": 5}}, # simulated OCR error
                ]
            }

        # Real PaddleOCR logic
        try:
            result = self.ocr.ocr(image_path)
            cells = []
            row_idx = 0
            col_idx = 0
            
            if result and isinstance(result, list) and len(result) > 0:
                page_res = result[0]
                if isinstance(page_res, dict):
                    # Modern PaddleOCR/PaddleX format
                    rec_texts = page_res.get("rec_texts", [])
                    rec_scores = page_res.get("rec_scores", [])
                    rec_boxes = page_res.get("rec_boxes", [])
                    
                    for i in range(len(rec_texts)):
                        text = rec_texts[i]
                        score = rec_scores[i]
                        box = rec_boxes[i]
                        
                        xmin, ymin, xmax, ymax = float(box[0]), float(box[1]), float(box[2]), float(box[3])
                        
                        cells.append({
                            "row_index": row_idx,
                            "column_index": col_idx,
                            "value": text,
                            "confidence": float(score),
                            "bbox": {
                                "x": xmin,
                                "y": ymin,
                                "width": xmax - xmin,
                                "height": ymax - ymin
                            }
                        })
                        col_idx += 1
                elif isinstance(page_res, list):
                    # Classic PaddleOCR format
                    for line in page_res:
                        box = line[0]
                        val_part = line[1]
                        
                        if isinstance(val_part, (tuple, list)) and len(val_part) >= 2:
                            text, confidence = val_part[0], val_part[1]
                        elif isinstance(val_part, (tuple, list)) and len(val_part) == 1:
                            text, confidence = val_part[0], 0.90
                        else:
                            text, confidence = str(val_part), 0.90
                            
                        x_coords = [p[0] for p in box]
                        y_coords = [p[1] for p in box]
                        min_x, max_x = min(x_coords), max(x_coords)
                        min_y, max_y = min(y_coords), max(y_coords)
                        
                        cells.append({
                            "row_index": row_idx,
                            "column_index": col_idx,
                            "value": text,
                            "confidence": float(confidence),
                            "bbox": {
                                "x": min_x,
                                "y": min_y,
                                "width": max_x - min_x,
                                "height": max_y - min_y
                            }
                        })
                        col_idx += 1
            
            return {
                "rows": [{"row_index": 0}],
                "columns": [{"column_index": i} for i in range(col_idx)],
                "cells": cells
            }
        except Exception as e:
            import traceback
            self.logger.error(f"Error extracting table via PaddleOCR: {e}")
            self.logger.error(traceback.format_exc())
            raise e
