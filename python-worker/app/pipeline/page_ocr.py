"""Run each page's native OCR in an isolated, reusable child process.

The OCR models (Paddle, Surya/torch) occasionally die with a *native* crash —
a segmentation fault or an OS OOM-kill — when a page is too large/complex for
available memory. A native crash cannot be caught by a Python `try/except`: it
takes down the whole interpreter. Before this, that meant the worker daemon
itself died mid-document and the job had to be recovered and retried wholesale.

`SubprocessPageOCR` moves the native work into a separate process:

  * Healthy pages cost nothing extra — the child loads its models once and is
    reused across every page of the document.
  * A page that crashes the child kills only the child. The parent sees the
    broken pipe, records that one page as failed, respawns a fresh child, and
    carries on with the next page. The daemon never dies.
  * A page that merely raises a Python error is reported back over the pipe and
    the child stays alive (no respawn needed).

The parent always stays responsive, so the daemon's heartbeat keeps the job lock
fresh even while a page is being OCR'd.
"""
import logging
import multiprocessing as mp

logger = logging.getLogger("PageOCR")

# `spawn` gives the child a clean interpreter rather than a fork of the parent's
# (already torch-initialised) memory — safer and avoids inherited CUDA/threading
# state. Works the same on Linux (container) and Windows (host worker).
_ctx = mp.get_context("spawn")


class PageOCRError(Exception):
    """Raised when a page could not be OCR'd — either the child crashed natively
    (segfault / OOM-kill), timed out, or returned a handled error."""


def _child_main(conn, ocr_priority):
    """Child entrypoint: build the engine once, then serve one page per message.

    Protocol: parent sends {image_path,width,height} dicts and finally None to
    quit; child replies {ok:True, cells, tables} or {ok:False, error}. A native
    crash simply closes the pipe, which the parent detects."""
    import os
    # Best-effort memory/CPU caps applied before torch/surya import. setdefault so
    # they can still be overridden from the environment. These reduce peak memory
    # of the model stacks; they do not guarantee a crash-free run, the process
    # isolation is what makes a crash survivable.
    os.environ.setdefault("OMP_NUM_THREADS", "4")
    os.environ.setdefault("MKL_NUM_THREADS", "4")
    os.environ.setdefault("RECOGNITION_BATCH_SIZE", "16")
    os.environ.setdefault("DETECTOR_BATCH_SIZE", "8")

    import logging as _logging
    _logging.basicConfig(level=_logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
    log = _logging.getLogger("PageOCRChild")

    try:
        from app.pipeline.engine import ProcessingEngine
        engine = ProcessingEngine()
        ocr_engine, name = engine.select_ocr_engine(ocr_priority)
        if ocr_engine is None:
            conn.send({"ready": False, "error": "no OCR provider could be loaded"})
            return
        log.info(f"Isolated OCR child ready (engine={name})")
        conn.send({"ready": True, "engine": name})
    except Exception as e:
        try:
            conn.send({"ready": False, "error": repr(e)})
        except Exception:
            pass
        return

    while True:
        try:
            msg = conn.recv()
        except (EOFError, KeyboardInterrupt):
            break
        if msg is None:
            break
        try:
            result = engine.ocr_page(ocr_engine, msg["image_path"], msg["width"], msg["height"])
            conn.send({"ok": True, "cells": result["cells"], "tables": result["tables"]})
        except Exception as e:
            # Python-level failure: report and keep the child alive for next page.
            log.error(f"OCR error on {msg.get('image_path')}: {e}")
            try:
                conn.send({"ok": False, "error": repr(e)})
            except Exception:
                break


class SubprocessPageOCR:
    def __init__(self, ocr_priority, page_timeout=600):
        self.ocr_priority = ocr_priority
        self.page_timeout = page_timeout
        self.proc = None
        self.conn = None
        self._log = logging.getLogger("PageOCR")

    def _spawn(self):
        parent_conn, child_conn = _ctx.Pipe()
        proc = _ctx.Process(target=_child_main, args=(child_conn, self.ocr_priority), daemon=True)
        proc.start()
        child_conn.close()  # parent keeps only its end
        if not parent_conn.poll(self.page_timeout):
            self._terminate(proc)
            raise PageOCRError(f"OCR child did not become ready within {self.page_timeout}s")
        try:
            msg = parent_conn.recv()
        except EOFError:
            self._terminate(proc)
            raise PageOCRError("OCR child died during initialisation")
        if not msg.get("ready"):
            self._terminate(proc)
            raise PageOCRError(f"OCR child failed to initialise: {msg.get('error')}")
        self.proc, self.conn = proc, parent_conn
        self._log.info(f"Isolated OCR subprocess started (engine={msg.get('engine')})")

    def start(self):
        if self.proc is None or not self.proc.is_alive():
            self._spawn()

    def run_page(self, image_path, width, height):
        """OCR one page in the child. Raises PageOCRError if the child crashed,
        timed out, or returned an error; the caller should record the page as
        failed and continue (a fresh child is spawned on the next call)."""
        self.start()
        try:
            self.conn.send({"image_path": image_path, "width": width, "height": height})
        except (BrokenPipeError, OSError) as e:
            self._kill()
            raise PageOCRError(f"OCR child pipe broke before send: {e}")
        if not self.conn.poll(self.page_timeout):
            self._kill()
            raise PageOCRError(f"OCR timed out after {self.page_timeout}s")
        try:
            msg = self.conn.recv()
        except (EOFError, OSError):
            self._kill()
            raise PageOCRError("OCR child died during processing (native crash / OOM-kill)")
        if not msg.get("ok"):
            raise PageOCRError(msg.get("error", "unknown OCR error"))
        return msg

    def _terminate(self, proc):
        try:
            if proc and proc.is_alive():
                proc.terminate()
                proc.join(timeout=5)
        except Exception:
            pass

    def _kill(self):
        self._terminate(self.proc)
        self.proc = None
        self.conn = None

    def stop(self):
        try:
            if self.conn:
                self.conn.send(None)
        except Exception:
            pass
        self._kill()
