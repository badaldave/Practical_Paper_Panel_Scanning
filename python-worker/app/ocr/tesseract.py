import logging
from typing import Dict, Any
from app.ocr.base import OCRProvider

try:
    import pytesseract
    from PIL import Image
    TESSERACT_AVAILABLE = True
except ImportError:
    TESSERACT_AVAILABLE = False

class TesseractProvider(OCRProvider):
    def __init__(self):
        self.logger = logging.getLogger("TesseractProvider")
        if TESSERACT_AVAILABLE:
            self.logger.info("Tesseract fallback engine loaded successfully.")
        else:
            self.logger.warning("pytesseract library or PIL not installed. Falling back to mock extraction mode.")

    def is_available(self) -> bool:
        return TESSERACT_AVAILABLE

    def extract_table(self, image_path: str, table_bbox: Dict[str, Any] = None) -> Dict[str, Any]:
        if not TESSERACT_AVAILABLE:
            # Standalone demo mockup
            return {
                "rows": [{"row_index": i} for i in range(2)],
                "columns": [{"column_index": j} for j in range(2)],
                "cells": [
                    {"row_index": 0, "column_index": 0, "value": "Name", "confidence": 0.95, "bbox": {"x": 10, "y": 10, "width": 40, "height": 5}},
                    {"row_index": 0, "column_index": 1, "value": "Grade", "confidence": 0.95, "bbox": {"x": 60, "y": 10, "width": 20, "height": 5}},
                    {"row_index": 1, "column_index": 0, "value": "David", "confidence": 0.92, "bbox": {"x": 10, "y": 15, "width": 40, "height": 5}},
                    {"row_index": 1, "column_index": 1, "value": "A", "confidence": 0.94, "bbox": {"x": 60, "y": 15, "width": 20, "height": 5}},
                ]
            }

        try:
            image = Image.open(image_path)
            # Run pytesseract OCR with page segmentation mode 6 (Assume single uniform block of text)
            config = "--psm 6"
            data = pytesseract.image_to_data(image, config=config, output_type=pytesseract.Output.DICT)
            
            n_boxes = len(data['text'])
            lines_dict = {}
            for i in range(n_boxes):
                if data['level'][i] != 5: # Only word level
                    continue
                
                text = data['text'][i].strip()
                if not text:
                    continue
                
                conf = float(data['conf'][i]) / 100.0
                if conf < 0.1: # filter out space/noise words
                    continue
                
                # Filter out standalone vertical/horizontal lines and punctuation noise
                noise_chars = {'|', '_', '-', ',', '.', '~', '=', '—', '+', ':', ';', '/', '\\', '‘', '’', "'"}
                if text in noise_chars:
                    continue
                
                key = (data['page_num'][i], data['block_num'][i], data['par_num'][i], data['line_num'][i])
                if key not in lines_dict:
                    lines_dict[key] = []
                
                lines_dict[key].append({
                    "text": text,
                    "left": data['left'][i],
                    "top": data['top'][i],
                    "width": data['width'][i],
                    "height": data['height'][i],
                    "conf": conf
                })

            cells = []
            row_idx = 0
            
            # Sort keys by page, block, paragraph, line number to process top-to-bottom
            sorted_keys = sorted(lines_dict.keys())
            
            for key in sorted_keys:
                words = lines_dict[key]
                words.sort(key=lambda w: w["left"])
                
                # Split words into cells based on flat 35px X-gap column separation
                line_cells = []
                current_cell_words = []
                
                for w in words:
                    if not current_cell_words:
                        current_cell_words.append(w)
                        continue
                    
                    prev_w = current_cell_words[-1]
                    gap = w["left"] - (prev_w["left"] + prev_w["width"])
                    
                    gap_threshold = int(image.width * 0.027)
                    if gap > gap_threshold:
                        line_cells.append(current_cell_words)
                        current_cell_words = [w]
                    else:
                        current_cell_words.append(w)
                
                if current_cell_words:
                    line_cells.append(current_cell_words)
                
                # Create final Cell records for this line
                for col_idx, cell_words in enumerate(line_cells):
                    min_x = min(w["left"] for w in cell_words)
                    max_x = max(w["left"] + w["width"] for w in cell_words)
                    min_y = min(w["top"] for w in cell_words)
                    max_y = max(w["top"] + w["height"] for w in cell_words)
                    
                    combined_text = " ".join(w["text"] for w in cell_words)
                    avg_conf = sum(w["conf"] for w in cell_words) / len(cell_words)
                    
                    cells.append({
                        "row_index": row_idx,
                        "column_index": col_idx,
                        "value": combined_text,
                        "confidence": avg_conf,
                        "bbox": {
                            "x": float(min_x),
                            "y": float(min_y),
                            "width": float(max_x - min_x),
                            "height": float(max_y - min_y)
                        }
                    })
                row_idx += 1

            # Count max columns
            max_cols = 0
            if cells:
                max_cols = max(c["column_index"] for c in cells) + 1

            return {
                "rows": [{"row_index": r} for r in range(row_idx)],
                "columns": [{"column_index": c} for c in range(max_cols)],
                "cells": cells
            }
        except Exception as e:
            self.logger.error(f"Error running Tesseract fallback: {e}")
            raise e
