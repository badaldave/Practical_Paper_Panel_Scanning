import cv2
import numpy as np
import logging
import os

class ImagePreprocessor:
    def __init__(self):
        self.logger = logging.getLogger("ImagePreprocessor")

    def process(self, image_path: str, output_path: str) -> dict:
        """
        Executes OpenCV preprocessing pipeline to optimize the scan.
        Returns metadata about operations applied.
        """
        self.logger.info(f"Preprocessing image: {image_path}")
        
        # Check if PDF and convert to temp image if needed
        is_pdf = image_path.lower().endswith('.pdf')
        temp_png = None
        if is_pdf:
            import fitz
            doc = fitz.open(image_path)
            if len(doc) == 0:
                raise ValueError(f"PDF document is empty: {image_path}")
            page = doc.load_page(0)
            pix = page.get_pixmap(dpi=150)
            temp_png = image_path + "_temp.png"
            pix.save(temp_png)
            read_path = temp_png
        else:
            read_path = image_path

        # Load image
        img = cv2.imread(read_path)
        
        # Clean up temp png
        if temp_png and os.path.exists(temp_png):
            try:
                os.remove(temp_png)
            except Exception as cleanup_err:
                self.logger.warning(f"Failed to clean up temp file {temp_png}: {cleanup_err}")

        if img is None:
            raise ValueError(f"Failed to read image at {read_path}")
        
        gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
        
        # 1. Rotation Correction & Deskew
        angle, rotated = self._deskew(gray)
        
        # 2. Contrast Enhancement (CLAHE)
        clahe = cv2.createCLAHE(clipLimit=2.0, tileGridSize=(8, 8))
        enhanced = clahe.apply(rotated)
        
        # 3. Denoising
        denoised = cv2.fastNlMeansDenoising(enhanced, None, h=10, templateWindowSize=7, searchWindowSize=21)
        
        # 4. Sharpening Filter (Bypassed to keep characters solid)
        sharpened = denoised
        
        # 5. Large-Block Adaptive Binarization to suppress background stain noise
        binarized = cv2.adaptiveThreshold(
            sharpened, 
            255, 
            cv2.ADAPTIVE_THRESH_GAUSSIAN_C, 
            cv2.THRESH_BINARY, 
            101, 
            15
        )
        
        # Save preprocessed image
        cv2.imwrite(output_path, binarized)
        
        h, w = binarized.shape
        return {
            "width": w,
            "height": h,
            "deskew_angle": float(angle),
            "denoised": True,
            "binarized": True
        }

    def _deskew(self, gray_img: np.ndarray) -> tuple:
        """Computes rotation angle and performs rotation alignment."""
        # Threshold the image
        _, thresh = cv2.threshold(gray_img, 0, 255, cv2.THRESH_BINARY_INV + cv2.THRESH_OTSU)
        
        # Find all white pixels (the text)
        coords = np.column_stack(np.where(thresh > 0))
        
        # Compute bounding box rotation angle
        if len(coords) == 0:
            return 0.0, gray_img
            
        angle = cv2.minAreaRect(coords)[-1]
        
        # Adjust angle range for OpenCV 4.5+ range (0, 90]
        if angle > 45:
            angle = angle - 90
            
        # Rotate image if angle is significant but not too large
        if 0.5 < abs(angle) < 20.0:
            (h, w) = gray_img.shape[:2]
            center = (w // 2, h // 2)
            M = cv2.getRotationMatrix2D(center, angle, 1.0)
            rotated = cv2.warpAffine(
                gray_img, 
                M, 
                (w, h), 
                flags=cv2.INTER_CUBIC, 
                borderMode=cv2.BORDER_REPLICATE
            )
            return angle, rotated
            
        return 0.0, gray_img
