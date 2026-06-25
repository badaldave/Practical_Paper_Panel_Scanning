import logging

class FeedbackMemory:
    def __init__(self, feedback_mapping: dict):
        """
        feedback_mapping format:
        {
            "document_type_name": {
                "original_extracted_value": "corrected_value"
            }
        }
        """
        self.mapping = feedback_mapping or {}
        self.logger = logging.getLogger("FeedbackMemory")

    def apply_corrections(self, document_type: str, cells: list) -> list:
        """
        Scans cells and matches values against past corrections.
        If a match is found, the value is updated and confidence is adjusted.
        """
        doc_rules = self.mapping.get(document_type, {})
        # Also support general fallback rules
        general_rules = self.mapping.get("general", {})

        corrected_count = 0
        for cell in cells:
            val = cell["value"]
            
            # 1. Check template-specific rules
            if val in doc_rules:
                corrected_val = doc_rules[val]
                cell["value"] = corrected_val
                cell["confidence"] = min(cell["confidence"] + 0.20, 0.98) # Boost confidence
                corrected_count += 1
                
            # 2. Check general character-mapping rules
            elif val in general_rules:
                corrected_val = general_rules[val]
                cell["value"] = corrected_val
                cell["confidence"] = min(cell["confidence"] + 0.15, 0.95)
                corrected_count += 1

        if corrected_count > 0:
            self.logger.info(f"Applied {corrected_count} rule-based feedback corrections for document type: {document_type}")
            
        return cells
