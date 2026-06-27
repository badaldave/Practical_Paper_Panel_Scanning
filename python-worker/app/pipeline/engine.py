import os
import logging
from app.config import Config
from app.db.repository import WorkerRepository
from app.pipeline.preprocessor import ImagePreprocessor
from app.feedback.memory import FeedbackMemory

class ProcessingEngine:
    def __init__(self):
        self.logger = logging.getLogger("ProcessingEngine")
        self.preprocessor = ImagePreprocessor()
        self.providers = {}

    def select_ocr_engine(self, priority=None):
        """Pick the first available provider in priority order, else the first that
        loads at all (mock mode). Returns (provider, name). Used inside the OCR
        subprocess so provider selection happens where the models actually run."""
        priority = priority or Config.OCR_PRIORITY
        for name in priority:
            p_name = name.strip().lower()
            provider = self._get_provider(p_name)
            if provider and provider.is_available():
                return provider, p_name
        for name in priority:
            p_name = name.strip().lower()
            provider = self._get_provider(p_name)
            if provider:
                return provider, p_name + " (mock)"
        return None, "mock"

    def ocr_page(self, ocr_engine, image_path, img_w, img_h):
        """Run the native, crash-prone OCR for a single page: table extraction
        (Paddle) then handwriting refinement (Surya). Returns the raw cells (for
        metadata) and the aligned+refined tables. Pure-python alignment is kept
        here too so the parent only has to persist the result."""
        extraction_res = ocr_engine.extract_table(image_path)
        cells = extraction_res.get("cells", [])
        grouped = self._align_coordinates(cells, img_w, img_h, drop_empty=False)
        grouped = self._refine_with_handwriting(image_path, grouped, img_w, img_h)
        return {"cells": cells, "tables": grouped.get("tables", [])}

    def _get_provider(self, name: str):
        if name in self.providers:
            return self.providers[name]
        
        self.logger.info(f"Initializing OCR provider: {name}...")
        provider = None
        # Import providers lazily so that a broken/optional backend (e.g. a torch
        # install that fails to load) does not prevent the others from working.
        try:
            if name == "paddle":
                from app.ocr.paddle import PaddleOCRProvider
                provider = PaddleOCRProvider()
            elif name == "surya":
                from app.ocr.surya import SuryaOCRProvider
                provider = SuryaOCRProvider()
            elif name == "doctr":
                from app.ocr.doctr import DocTRProvider
                provider = DocTRProvider()
            elif name == "tesseract":
                from app.ocr.tesseract import TesseractProvider
                provider = TesseractProvider()
        except Exception as e:
            self.logger.error(f"Failed to initialize OCR provider {name}: {e}")
            
        if provider:
            self.providers[name] = provider
        return provider

    def process_document(self, document_id: str, tenant_id: str) -> bool:
        """Executes full image correction, cell extraction, and layout alignment."""
        doc = WorkerRepository.get_document(document_id)
        if not doc:
            raise ValueError(f"Document {document_id} not found in database.")

        original_file_path = doc["file_path"].replace('\\', '/')
        if not os.path.isabs(original_file_path) or not os.path.exists(original_file_path) or "go-backend" not in original_file_path:
            # Try prepending Config.UPLOAD_DIR if not absolute or doesn't exist
            # Strip standard mount folder prefixes to resolve on the host filesystem
            rel_path = original_file_path
            if rel_path.startswith("/var/data/uploads/"):
                rel_path = rel_path[len("/var/data/uploads/"):]
            elif rel_path.startswith("uploads/"):
                rel_path = rel_path[len("uploads/"):]
            
            candidate_path = os.path.join(Config.UPLOAD_DIR, rel_path)
            if os.path.exists(candidate_path):
                original_file_path = candidate_path
            elif not os.path.exists(original_file_path):
                # If neither exists, raise exception with candidate path tried
                raise FileNotFoundError(f"Original file not found at {original_file_path} or {candidate_path}")

        # OCR runs in an isolated child process (SubprocessPageOCR): a native crash
        # in a model (segfault / OOM-kill) on one page takes down only that child
        # and that page, never this daemon. The provider is selected inside the
        # child, per Config.OCR_PRIORITY.
        from app.pipeline.page_ocr import SubprocessPageOCR, PageOCRError
        self.logger.info(f"Starting isolated OCR for document {doc['name']} (priority={Config.OCR_PRIORITY})")
        WorkerRepository.update_document_progress(document_id, 10)

        preprocessed_dir = os.path.join(os.path.dirname(original_file_path), "preprocessed")
        os.makedirs(preprocessed_dir, exist_ok=True)

        base_name = os.path.basename(original_file_path)
        is_pdf = original_file_path.lower().endswith('.pdf')
        all_tables = []

        # If the OCR child crashes on this many pages in a row, give up on the whole
        # document rather than respawning the model stack hundreds of times (the
        # environment almost certainly lacks the memory for it) — fail fast with a
        # clear reason instead of grinding for hours.
        MAX_CONSECUTIVE_PAGE_FAILURES = 5
        failed_pages = []
        consecutive_failures = 0
        # Reuse-and-clean: stop any child left over from a previous document so we
        # never leak more than one OCR subprocess across jobs.
        if getattr(self, "_page_ocr", None) is not None:
            self._page_ocr.stop()
        page_ocr = self._page_ocr = SubprocessPageOCR(Config.OCR_PRIORITY)

        if is_pdf:
            import fitz
            doc_pdf = fitz.open(original_file_path)
            num_pages = len(doc_pdf)
            self.logger.info(f"Processing PDF document with {num_pages} pages")
            # Record the page count up-front so the UI knows it even if a later
            # page crashes processing.
            WorkerRepository.set_page_count(document_id, num_pages)

            for page_idx in range(num_pages):
                page_num = page_idx + 1
                self.logger.info(f"Processing page {page_num}/{num_pages}...")
                
                # Update progress incrementally (from 10% to 90%)
                progress = 10 + int((page_idx / num_pages) * 80)
                WorkerRepository.update_document_progress(document_id, progress)
                
                # Render PDF page to PNG at 150 DPI (exact same as process_all_pages.py)
                page = doc_pdf.load_page(page_idx)
                pix = page.get_pixmap(dpi=150)
                
                # Save page image directly without OpenCV preprocessor
                page_filename = f"page_{page_num}_" + os.path.splitext(base_name)[0] + ".png"
                page_image_path = os.path.join(preprocessed_dir, page_filename)
                pix.save(page_image_path)
                
                # Compute DB paths
                rel_prep_path = os.path.relpath(page_image_path, Config.UPLOAD_DIR).replace('\\', '/')
                db_image_path = "/var/data/uploads/" + rel_prep_path
                
                # Save initial page record
                WorkerRepository.save_page_record(
                    document_id=document_id,
                    page_number=page_num,
                    image_path=db_image_path,
                    width=pix.width,
                    height=pix.height
                )

                # Run OCR in the isolated child. A native crash here raises
                # PageOCRError instead of killing the daemon: mark the page failed
                # and move on (and bail out if too many crash back-to-back).
                try:
                    ocr_result = page_ocr.run_page(page_image_path, pix.width, pix.height)
                except PageOCRError as e:
                    consecutive_failures += 1
                    failed_pages.append(page_num)
                    WorkerRepository.mark_page_failed(document_id, page_num)
                    self.logger.error(f"Page {page_num}/{num_pages} OCR failed (isolated): {e}")
                    if consecutive_failures >= MAX_CONSECUTIVE_PAGE_FAILURES:
                        page_ocr.stop()
                        self._page_ocr = None
                        from app.processing_errors import NonRetryableProcessingError
                        raise NonRetryableProcessingError(
                            f"OCR crashed on {consecutive_failures} consecutive pages (latest: page {page_num}). "
                            f"Aborting — the worker environment lacks the memory to OCR this document. "
                            f"Last error: {e}"
                        )
                    continue
                consecutive_failures = 0

                # Extract page metadata
                raw_cells = ocr_result.get("cells", [])
                meta = self._extract_page_metadata(raw_cells, pix.width, pix.height)
                self.logger.info(f"Extracted page {page_num} metadata: {meta}")

                # Update page record with metadata
                WorkerRepository.save_page_record(
                    document_id=document_id,
                    page_number=page_num,
                    image_path=db_image_path,
                    width=pix.width,
                    height=pix.height,
                    college_code=meta.get("college_code"),
                    college_name=meta.get("college_name"),
                    subject_code=meta.get("subject_code"),
                    subject_name=meta.get("subject_name"),
                    faculty=meta.get("faculty"),
                    total_candidates=meta.get("total_candidates")
                )

                # Map page_number correctly
                for table in ocr_result.get("tables", []):
                    table["page_number"] = page_num
                    all_tables.append(table)
        else:
            # Process single-page image
            page_num = 1
            self.logger.info(f"Processing image document {base_name}")
            WorkerRepository.set_page_count(document_id, 1)
            
            # Apply full preprocessor (deskew, CLAHE, denoising, binarization) for single images
            preprocessed_path = os.path.join(
                preprocessed_dir, 
                "prep_" + base_name
            )
            img_meta = self.preprocessor.process(original_file_path, preprocessed_path)
            
            # Compute DB paths
            rel_prep_path = os.path.relpath(preprocessed_path, Config.UPLOAD_DIR).replace('\\', '/')
            db_image_path = "/var/data/uploads/" + rel_prep_path
            
            # Save initial page record
            WorkerRepository.save_page_record(
                document_id=document_id,
                page_number=page_num,
                image_path=db_image_path,
                width=img_meta["width"],
                height=img_meta["height"]
            )
            WorkerRepository.update_document_progress(document_id, 45)

            # Run OCR in the isolated child (a native crash raises PageOCRError
            # rather than killing the daemon).
            try:
                ocr_result = page_ocr.run_page(preprocessed_path, img_meta["width"], img_meta["height"])
            except PageOCRError as e:
                failed_pages.append(page_num)
                WorkerRepository.mark_page_failed(document_id, page_num)
                self.logger.error(f"Image OCR failed (isolated): {e}")
                ocr_result = None
            WorkerRepository.update_document_progress(document_id, 75)

            if ocr_result is not None:
                # Extract metadata
                raw_cells = ocr_result.get("cells", [])
                meta = self._extract_page_metadata(raw_cells, img_meta["width"], img_meta["height"])
                self.logger.info(f"Extracted image metadata: {meta}")

                # Update page record with metadata
                WorkerRepository.save_page_record(
                    document_id=document_id,
                    page_number=page_num,
                    image_path=db_image_path,
                    width=img_meta["width"],
                    height=img_meta["height"],
                    college_code=meta.get("college_code"),
                    college_name=meta.get("college_name"),
                    subject_code=meta.get("subject_code"),
                    subject_name=meta.get("subject_name"),
                    faculty=meta.get("faculty"),
                    total_candidates=meta.get("total_candidates")
                )

                for table in ocr_result.get("tables", []):
                    table["page_number"] = page_num
                    all_tables.append(table)

        # OCR finished (or every page failed) — release the child process.
        page_ocr.stop()
        self._page_ocr = None

        # If nothing could be OCR'd, fail the whole job with a clear reason rather
        # than silently saving an empty document.
        if failed_pages and not all_tables:
            from app.processing_errors import NonRetryableProcessingError
            raise NonRetryableProcessingError(
                f"OCR failed on all {len(failed_pages)} page(s); no data was extracted. The worker "
                f"subprocess crashed on every page (most likely out of memory for this document)."
            )
        if failed_pages:
            self.logger.warning(
                f"{len(failed_pages)} page(s) failed OCR and were skipped: {failed_pages[:20]}"
                f"{'…' if len(failed_pages) > 20 else ''}"
            )

        # Apply feedback correction memory mappings
        feedback_rules = WorkerRepository.load_correction_memory(tenant_id)
        memory_layer = FeedbackMemory(feedback_rules)
        
        # Apply corrections to cells
        doc_type = doc.get("mime_type", "general")
        for table in all_tables:
            for row in table.get("rows", []):
                row["cells"] = memory_layer.apply_corrections(doc_type, row["cells"])

        # Cross-row consensus: the same examiner recurs across rows keyed by mobile,
        # so vote a single name/mobile per examiner and backfill poorly-read rows.
        # The examiner directory widens this beyond the current sheet — poorly-read
        # rows can also borrow from how the same examiner was read across every
        # other document in the tenant. Backfills are flagged (is_inferred).
        from app.pipeline.consensus import apply_document_consensus, build_examiner_directory
        try:
            examiner_pairs = WorkerRepository.load_examiner_pairs(tenant_id, exclude_document_id=document_id)
            # Seeded historical prior (EXMNAME-style imports) joins the same
            # directory as live cross-document reads — registry rows are weighted
            # by times_seen and ambiguous mobiles are pre-filtered by the query.
            registry_pairs = WorkerRepository.load_registry_pairs(tenant_id)
            examiner_directory = build_examiner_directory(examiner_pairs + registry_pairs)
        except Exception as e:
            self.logger.warning(f"Examiner directory unavailable, falling back to in-document consensus: {e}")
            examiner_directory = None
        consensus_stats = apply_document_consensus(all_tables, examiner_directory=examiner_directory)
        known = len(examiner_directory["mob_to_names"]) if examiner_directory else 0
        self.logger.info(f"Consensus pass: {consensus_stats} (examiner directory: {known} known mobiles)")
        WorkerRepository.update_document_progress(document_id, 95)

        # Commit structured tabular outputs to PostgreSQL
        WorkerRepository.save_extractions(tenant_id, document_id, {"tables": all_tables})
        
        return True

    def _extract_page_metadata(self, raw_cells: list, img_w: int, img_h: int) -> dict:
        import re
        
        # Group raw cells into lines by Y coordinate overlap in the top 40% of the page
        top_cells = [c for c in raw_cells if c["bbox"]["y"] < (img_h * 0.4)]
        if not top_cells:
            return {}
            
        top_cells.sort(key=lambda c: c["bbox"]["y"])
        
        lines = []
        current_line = []
        for cell in top_cells:
            if not current_line:
                current_line.append(cell)
                continue
                
            avg_y = sum(c["bbox"]["y"] for c in current_line) / len(current_line)
            avg_h = sum(c["bbox"]["height"] for c in current_line) / len(current_line)
            
            if abs(cell["bbox"]["y"] - avg_y) < (avg_h * 0.7):
                current_line.append(cell)
            else:
                lines.append(current_line)
                current_line = [cell]
        if current_line:
            lines.append(current_line)
            
        text_lines = []
        for line_cells in lines:
            line_cells.sort(key=lambda c: c["bbox"]["x"])
            line_text = " ".join(c["value"] for c in line_cells).strip()
            if line_text:
                text_lines.append(line_text)
                
        college_code = None
        college_name = None
        subject_code = None
        subject_name = None
        faculty = None
        total_candidates = None
        
        # College code is printed as "<code>-<NAME>"; OCR often substitutes
        # letters for digits in the code (O->0, B->8, S->5, I/l->1, Z->2, G->6).
        def _norm_digits(tok):
            m = {"O": "0", "o": "0", "B": "8", "S": "5", "s": "5", "I": "1",
                 "l": "1", "L": "1", "i": "1", "Z": "2", "z": "2", "G": "6"}
            return "".join(m.get(ch, ch) for ch in tok)

        for line in text_lines:
            match = re.match(r'^([0-9OoBSsIiLlZzG]{3,5})\s*[-–]\s*(.*)$', line.strip())
            if match:
                code = _norm_digits(match.group(1).strip())
                if code.isdigit():
                    college_code = code
                    college_name = match.group(2).strip()
                    break
            match_cc = re.search(r'\b(\d{3,})\b', line)
            if match_cc:
                if any(k in line.upper() for k in ["CENTRE", "COLLEGE", "MAHAVIDHYALAYA", "INSTITUTE"]):
                    college_code = match_cc.group(1).strip()
                    name_part = line.replace(college_code, "").replace("CENTRE", "").replace("COLLEGE", "").replace(":", "").replace("-", "").strip()
                    college_name = re.sub(r'[\(\)]', '', name_part).strip()
                    break
        
        for line in text_lines:
            if "FACULTY OF" in line.upper():
                faculty = line.upper().replace("FACULTY OF", "").replace(":", "").strip()
                break
                
        for idx, line in enumerate(text_lines):
            if "TOTAL CANDIDATE" in line.upper():
                match = re.search(r'TOTAL CANDIDATE\s*:?\s*(\d+)', line.upper())
                if match:
                    total_candidates = int(match.group(1))
                elif idx + 1 < len(text_lines):
                    next_line = text_lines[idx+1].strip()
                    try:
                        total_candidates = int(re.sub(r'\D', '', next_line))
                    except ValueError:
                        pass
                break
                
        subject_code_candidates = []
        for line in text_lines:
            match = re.search(r'\b([A-Z]{3,4}-\w+-\d+)\b', line.strip().upper())
            if match:
                subject_code_candidates.append(match.group(1))
            else:
                match = re.search(r'\b([A-Z]{2,4}-\d+[A-Z]?-\d+)\b', line.strip().upper())
                if match:
                    subject_code_candidates.append(match.group(1))
                    
        if subject_code_candidates:
            subject_code = min(subject_code_candidates, key=len)
            for line in text_lines:
                line_strip = line.strip()
                if line_strip.upper().startswith(subject_code) and len(line_strip) > len(subject_code):
                    subject_name = line_strip[len(subject_code):].replace("-", "").replace(":", "").strip()
                    break
            if not subject_name:
                subject_name = subject_code
                
        if total_candidates is not None and (total_candidates > 2147483647 or total_candidates < 0):
            total_candidates = None

        return {
            "college_code": college_code,
            "college_name": college_name,
            "subject_code": subject_code,
            "subject_name": subject_name,
            "faculty": faculty,
            "total_candidates": total_candidates
        }

    # ---- value cleaners for the practical-exam panel ----
    def _clean_subcode(self, s):
        import re
        s = re.sub(r"\s+", "", (s or "").upper())
        s = s.replace("7SP", "75P").replace("75SP", "75P")
        if re.search(r"BOT", s) and "302" in s:
            return "BOT-75P-302"
        m = re.match(r"([A-Z]{2,4})-?(\d{2,3}[A-Z]?)-?(\d{3})", s)
        if m:
            return f"{m.group(1)}-{m.group(2)}-{m.group(3)}"
        return s

    def _clean_batch(self, s):
        import re
        s = re.sub(r"\s+", "", (s or "").upper()).replace("-", "")
        m = re.match(r"(R?\d{1,2})", s)
        return m.group(1) if m else s

    def _clean_mobile(self, s):
        import re
        d = re.sub(r"\D", "", s or "")
        if len(d) > 10:
            d = d[-10:]
        return d

    def _split_trailing_mobile(self, s):
        """If a name-column cell has a trailing phone number glued on, separate it."""
        import re
        m = re.search(r"[\d][\d\s.\-]{5,}\d\s*$", s or "")
        if m:
            digits = re.sub(r"\D", "", m.group(0))
            if len(digits) >= 7:
                return s[:m.start()].strip(), digits
        return (s or "").strip(), ""

    # Honorifics / common OCR misreads of "Dr."/"For" removed anywhere in a name.
    _NAME_HON = {"DR", "MR", "MRS", "MS", "PROF", "FR", "SR", "MISS", "PROFF",
                 "LOR", "HDR", "POR", "PRI", "FOR", "DRS", "PRS", "HD", "PORS",
                 "PRO", "BRO", "IDR", "IPR", "IDE", "DE", "LDR", "EDR", "IPRS",
                 "IIDR", "IDRS", "IDE", "PORS", "FPR", "BR", "PR"}

    # Canonical forms for very common surnames frequently garbled by handwriting
    # OCR. Applied per token; chosen to be distinctive enough to avoid false hits.
    _SURNAME_FIX = {
        "SHARING": "SHARMA", "SHARMG": "SHARMA", "SHARMY": "SHARMA", "SHARNA": "SHARMA",
        "SHORMA": "SHARMA", "SHARMR": "SHARMA", "SHARMHA": "SHARMA", "SHARMHG": "SHARMA",
        "MEENG": "MEENA", "MEENE": "MEENA", "MENA": "MEENA", "MEHNA": "MEENA", "MEENNA": "MEENA",
        "SINGLE": "SINGH", "SINGLY": "SINGH", "SINEH": "SINGH", "SINGH": "SINGH",
        "KYMAR": "KUMAR", "KUMQR": "KUMAR", "KUMAER": "KUMAR",
        "AGGRWAL": "AGARWAL", "AGARWAY": "AGARWAL", "AGRWAL": "AGARWAL",
        "AGARWAF": "AGARWAL", "AGGRWAF": "AGARWAL", "AGGARWAL": "AGARWAL",
        "GRUPT": "GUPTA", "GUPT": "GUPTA", "GRUPTA": "GUPTA",
        "VEMG": "VERMA", "VERMG": "VERMA", "VRMA": "VERMA", "VEMA": "VERMA",
        "SQINI": "SAINI", "SAINE": "SAINI",
    }

    def _clean_name(self, s):
        import re
        s = (s or "").upper()
        s = re.sub(r"\d[\d\s.\-]{4,}\d", " ", s)   # strip long digit runs (stray mobiles)
        s = re.sub(r"[^A-Z ]", " ", s)              # keep letters only (single-char initials kept)
        s = re.sub(r"\s+", " ", s).strip()
        toks = s.split()
        # Drop honorific / "Dr."-misread tokens anywhere, but keep genuine
        # single-letter initials (e.g. "S P SINGH", "RAMESH C MEENA").
        toks = [t for t in toks if t not in self._NAME_HON]
        # Normalize common OCR-garbled surnames.
        toks = [self._SURNAME_FIX.get(t, t) for t in toks]
        return " ".join(toks)

    def _align_coordinates(self, raw_cells: list, img_w: int, img_h: int, drop_empty: bool = True) -> dict:
        """Route OCR cells into examiner rows for the university practical-exam
        panel. The table has a fixed 4-column layout:
            col0 SUBCODE (printed)   col1 BATCH (printed: 1, R-1 ..)
            col2 EXAMINER NAME (handwritten)   col3 MOBILE NO (handwritten)
        Printed BATCH labels are used as per-row anchors; handwritten name/mobile
        cells are attached to the nearest anchor. Pre-printed batch slots that
        were never filled in (no name and no mobile) are dropped when drop_empty
        is True; set it False to keep all anchor rows (e.g. so a handwriting OCR
        pass can read names/mobiles Paddle missed before the empties are removed).
        """
        import re

        def wrap(rows):
            return {"tables": [{"page_number": 1, "table_index": 0,
                                "bbox": {"x": 0.0, "y": 0.0, "width": float(img_w), "height": float(img_h)},
                                "rows": rows}]}

        if not raw_cells:
            return wrap([])

        # Normalize any relative (0..1) bboxes to absolute pixels.
        for c in raw_cells:
            b = c["bbox"]
            if b["x"] < 1.0 and b["width"] < 1.0:
                b["x"] *= img_w; b["y"] *= img_h
                b["width"] *= img_w; b["height"] *= img_h

        def cx(c): return c["bbox"]["x"] + c["bbox"]["width"] / 2.0
        def cy(c): return c["bbox"]["y"] + c["bbox"]["height"] / 2.0
        def xp(c): return cx(c) / img_w

        # Column x right-edges (fraction of page width), from the observed layout.
        SUB_R, BATCH_R, NAME_R = 0.27, 0.42, 0.73

        # 1. Locate the table header row.
        header_y = None
        for c in raw_cells:
            u = re.sub(r"\s+", "", c["value"].upper())
            if "SUBCODE" in u or "EXAMINERNAME" in u or "EXAMINERMOBILE" in u:
                y = c["bbox"]["y"]
                header_y = y if header_y is None else min(header_y, y)
        data_top = (header_y + 45) if header_y is not None else img_h * 0.36

        batch_re = re.compile(r"^R?\d{1,2}$")
        def _alnum(v): return re.sub(r"[^A-Z0-9]", "", v.upper())

        # 2. Anchor rows. Every batch slot prints a SUBCODE in the left column,
        # so subcodes are the most reliable per-row anchor; batch labels are used
        # both as anchors and to locate the batch column. Both are detected by
        # VALUE (not a fixed x) so left/right-shifted page layouts still align.
        sub_cells = [c for c in raw_cells if cy(c) >= data_top and xp(c) < 0.32
                     and re.search(r"[A-Z].*\d", _alnum(c["value"])) and len(_alnum(c["value"])) >= 5]
        batch_cells = [c for c in raw_cells if cy(c) >= data_top and 0.16 < xp(c) < 0.50
                       and batch_re.match(_alnum(c["value"]))]

        # Adaptive column boundaries from detected positions.
        if batch_cells:
            bx = sorted(xp(c) for c in batch_cells)[len(batch_cells) // 2]
            SUB_R, BATCH_R = bx - 0.05, bx + 0.08
        elif sub_cells:
            sx = sorted(xp(c) for c in sub_cells)[len(sub_cells) // 2]
            SUB_R, BATCH_R = sx + 0.06, sx + 0.21

        # Cluster subcode + batch cells into rows along the y axis.
        sub_ids = set(id(c) for c in sub_cells)
        clusters = []
        for c in sorted(sub_cells + batch_cells, key=cy):
            if clusters and abs(cy(c) - clusters[-1]["y"]) < 45:
                cl = clusters[-1]; cl["cells"].append(c)
                cl["y"] = sum(cy(x) for x in cl["cells"]) / len(cl["cells"])
            else:
                clusters.append({"y": cy(c), "cells": [c]})
        # Representative anchor cell per row: prefer the subcode cell for its y.
        anchors = []
        for cl in clusters:
            subs_in = [x for x in cl["cells"] if id(x) in sub_ids]
            anchors.append(subs_in[0] if subs_in else cl["cells"][0])
        anchors.sort(key=cy)
        if not anchors:
            return wrap([])

        ys = [cy(a) for a in anchors]
        gaps = [b - a for a, b in zip(ys, ys[1:]) if b - a > 5]
        spacing = sorted(gaps)[len(gaps) // 2] if gaps else 90.0
        half = max(spacing * 0.55, 28.0)

        def nearest(c):
            ay = cy(c)
            i = min(range(len(anchors)), key=lambda k: abs(ys[k] - ay))
            return i if abs(ys[i] - ay) <= half else None

        # 3. Bucket every cell into a column of its nearest anchor row.
        buckets = [{"sub": [], "batch": [], "name": [], "mobile": []} for _ in anchors]
        for c in raw_cells:
            if cy(c) < data_top:
                continue
            x = xp(c)
            col = "sub" if x <= SUB_R else "batch" if x <= BATCH_R else "name" if x <= NAME_R else "mobile"
            i = nearest(c)
            if i is not None:
                buckets[i][col].append(c)

        def bbox_of(cells, anchor):
            if not cells:
                return {"x": 0.0, "y": cy(anchor), "width": 0.0, "height": 0.0}, 0.0
            x0 = min(c["bbox"]["x"] for c in cells); y0 = min(c["bbox"]["y"] for c in cells)
            x1 = max(c["bbox"]["x"] + c["bbox"]["width"] for c in cells)
            y1 = max(c["bbox"]["y"] + c["bbox"]["height"] for c in cells)
            conf = sum(c["confidence"] for c in cells) / len(cells)
            return {"x": x0, "y": y0, "width": x1 - x0, "height": y1 - y0}, conf

        rows = []
        for ai, a in enumerate(anchors):
            b = buckets[ai]
            subcode = self._clean_subcode(" ".join(c["value"] for c in sorted(b["sub"], key=cx)))
            batch = self._clean_batch(" ".join(c["value"] for c in sorted(b["batch"], key=cx)))
            name_cells = sorted(b["name"], key=cx)
            mobile_cells = sorted(b["mobile"], key=cx)
            name_raw, extra_mobile = self._split_trailing_mobile(" ".join(c["value"] for c in name_cells))
            mobile_raw = " ".join(c["value"] for c in mobile_cells)
            if extra_mobile and len(re.sub(r"\D", "", mobile_raw)) < 6:
                mobile_raw = (extra_mobile + " " + mobile_raw).strip()
            name = self._clean_name(name_raw)
            mobile = self._clean_mobile(mobile_raw)

            # Drop pre-printed but unfilled slots (unless a later pass will decide).
            if drop_empty and len(re.sub(r"[^A-Za-z]", "", name)) < 2 and len(re.sub(r"\D", "", mobile)) < 6:
                continue

            sb, sc = bbox_of(b["sub"] or [a], a)
            bb, bc = bbox_of(b["batch"] or [a], a)
            nb, nc = bbox_of(name_cells, a)
            mb, mc = bbox_of(mobile_cells, a)
            rows.append({"row_index": len(rows), "cells": [
                {"column_index": 0, "value": subcode, "confidence": sc, "bbox": sb},
                {"column_index": 1, "value": batch, "confidence": bc, "bbox": bb},
                {"column_index": 2, "value": name, "confidence": nc, "bbox": nb},
                {"column_index": 3, "value": mobile, "confidence": mc, "bbox": mb},
            ]})

        # The subcode is printed and identical for every row on a page; fill any
        # row whose subcode failed to OCR with the page's dominant value.
        from collections import Counter
        subs = [r["cells"][0]["value"] for r in rows if r["cells"][0]["value"]]
        if subs:
            mode_sub = Counter(subs).most_common(1)[0][0]
            for r in rows:
                if not r["cells"][0]["value"]:
                    r["cells"][0]["value"] = mode_sub

        return wrap(rows)

    # ---- handwriting refinement (Surya) ----
    def _get_surya(self):
        """Lazily load Surya predictors; cache on the instance. Returns None if
        Surya is unavailable (the pipeline then keeps Paddle's name/mobile)."""
        if hasattr(self, "_surya"):
            return self._surya
        self._surya = None
        try:
            from surya.detection import DetectionPredictor
            from surya.foundation import FoundationPredictor
            from surya.recognition import RecognitionPredictor
            from surya.common.surya.schema import TaskNames
            fp = FoundationPredictor()
            self._surya = {"rec": RecognitionPredictor(fp),
                           "det": DetectionPredictor(),
                           "task": TaskNames.ocr_with_boxes}
            self.logger.info("Surya handwriting OCR loaded.")
        except Exception as e:
            self.logger.warning(f"Surya handwriting OCR unavailable; using Paddle only for names/mobiles: {e}")
            self._surya = None
        return self._surya

    def _parse_surya_lines(self, lines, ay, tol):
        """Resolve (name, mobile) from a row strip's Surya text lines, keeping
        only lines near this row's anchor to avoid neighbour-row contamination."""
        import re
        lines = [l for l in lines if abs(l["y"] - ay) <= tol]
        name_parts, mobile = [], ""
        for l in sorted(lines, key=lambda l: (round(l["y"] / 15), l["x"])):
            t = re.sub(r"[|]+", " ", l["t"])
            t = re.sub(r"[_]{2,}", " ", t)
            t = re.sub(r"[-]{3,}", " ", t).strip()
            if not t:
                continue
            digits = re.sub(r"\D", "", t); letters = re.sub(r"[^A-Za-z]", "", t)
            if len(digits) >= 7 and len(digits) >= len(letters):
                nm, mob = self._split_trailing_mobile(t)
                if not mobile:
                    mobile = re.sub(r"\D", "", mob or t)
                if nm and len(re.sub(r"[^A-Za-z]", "", nm)) >= 2:
                    name_parts.append(nm)
            elif len(letters) >= 2:
                nm, mob = self._split_trailing_mobile(t)
                name_parts.append(nm or t)
                if mob and not mobile:
                    mobile = mob
        return self._clean_name(" ".join(name_parts)), self._clean_mobile(mobile)

    def _refine_with_handwriting(self, image_path, grouped, img_w, img_h):
        """Re-read the handwritten name/mobile columns with Surya (better at
        cursive than Paddle), then drop slots with no examiner from either engine."""
        import re
        surya = self._get_surya()
        for table in grouped.get("tables", []):
            rows = table.get("rows", [])
            if rows and surya is not None:
                try:
                    self._apply_surya(surya, image_path, rows, img_w, img_h)
                except Exception as e:
                    self.logger.error(f"Handwriting refinement failed: {e}")
            kept = []
            for r in rows:
                cm = {c["column_index"]: c for c in r["cells"]}
                name = cm.get(2, {}).get("value", "")
                mobile = cm.get(3, {}).get("value", "")
                if len(re.sub(r"[^A-Za-z]", "", name)) >= 2 or len(re.sub(r"\D", "", mobile)) >= 6:
                    kept.append(r)
            for i, r in enumerate(kept):
                r["row_index"] = i
            table["rows"] = kept
        return grouped

    def _apply_surya(self, surya, image_path, rows, img_w, img_h):
        import numpy as np
        from PIL import Image
        X_LEFT = 0.40
        img = Image.open(image_path)
        ys = []
        for r in rows:
            cm = {c["column_index"]: c for c in r["cells"]}
            b = cm[1]["bbox"]
            ys.append(b["y"] + b["height"] / 2.0)
        if not ys:
            return
        sd = sorted(b - a for a, b in zip(sorted(ys), sorted(ys)[1:]) if b - a > 5)
        sp = sd[len(sd) // 2] if sd else 90.0
        half = sp * 0.62; tol = sp * 0.42
        crops, idxs, boxes = [], [], []
        for i, ay in enumerate(ys):
            # Skip blank slots: measure ink on a tight name band to avoid the
            # printed row-rule lines (filled rows ~6-10%, empty ~<1.5%).
            band = img.crop((int(0.45 * img_w), int(max(0, ay - 0.32 * sp)),
                             int(0.96 * img_w), int(min(img_h, ay + 0.32 * sp))))
            if float((np.asarray(band.convert("L")) < 140).mean()) < 0.02:
                continue
            box = (int(X_LEFT * img_w), int(max(0, ay - half)), int(0.99 * img_w), int(min(img_h, ay + half)))
            crops.append(img.crop(box)); idxs.append(i); boxes.append(box)
        if not crops:
            return
        res = surya["rec"](crops, task_names=[surya["task"]] * len(crops), det_predictor=surya["det"])
        for i, box, r in zip(idxs, boxes, res):
            lines = [{"t": l.text, "x": float(l.bbox[0]) + box[0], "y": float(l.bbox[1]) + box[1]}
                     for l in r.text_lines]
            name, mobile = self._parse_surya_lines(lines, ys[i], tol)
            cm = {c["column_index"]: c for c in rows[i]["cells"]}
            ay = ys[i]
            if name:
                cm[2]["value"] = name
                cm[2]["confidence"] = 0.85
                cm[2]["bbox"] = {"x": X_LEFT * img_w, "y": ay - half, "width": (0.73 - X_LEFT) * img_w, "height": 2 * half}
            if mobile:
                cm[3]["value"] = mobile
                cm[3]["confidence"] = 0.85
                cm[3]["bbox"] = {"x": 0.73 * img_w, "y": ay - half, "width": (0.99 - 0.73) * img_w, "height": 2 * half}
