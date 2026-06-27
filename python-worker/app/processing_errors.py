class NonRetryableProcessingError(Exception):
    """A processing failure that retrying cannot fix — e.g. the worker environment
    cannot OCR this document at all (the OCR subprocess crashes on every page for
    lack of memory). The daemon should fail the job permanently with this reason
    instead of burning all its retry attempts on a guaranteed-to-fail run."""
