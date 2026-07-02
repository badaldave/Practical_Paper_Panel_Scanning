import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, GridApi, GridReadyEvent, CellClickedEvent, CellStyle } from 'ag-grid-community';
import { AlertCircle, Download, CheckCircle, ArrowLeft, Loader2, Lock, Unlock, Check, Keyboard } from 'lucide-react';

import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';

import { apiClient } from '@/shared/api/client';
import { CanvasViewer, CanvasCell } from '@/shared/ui/canvas-viewer';
import { verificationApi, settingsApi, examinersApi, type DocumentState } from '@/shared/api/services';
import { useAuthStore } from '@/entities/user/model/store';

interface CellRecord {
  id: string;
  document_id: string;
  page_number: number;
  row_index: number;
  column_index: number;
  original_value: string;
  current_value: string;
  confidence: number;
  is_inferred: boolean;
  bbox: {
    x: number;
    y: number;
    width: number;
    height: number;
  };
  version: number;
}

interface DocumentDetails {
  document: {
    id: string;
    name: string;
    status: string;
    progress_percentage: number;
    created_at: string;
  };
  pages: Array<{
    id: string;
    page_number: number;
    image_path: string;
    college_code?: string;
    college_name?: string;
    subject_code?: string;
    subject_name?: string;
    faculty?: string;
    total_candidates?: number;
  }>;
}

// Small keycap chip used in the shortcuts legend.
const Kbd: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <kbd className="px-1.5 py-0.5 bg-slate-800 border border-slate-700 rounded text-[10px] font-mono font-semibold text-slate-200 leading-none">
    {children}
  </kbd>
);

// Row# cell: shows the row number plus ▲/▼ controls to move a row up/down
// (#10, Excel-like reorder). Reads moveRow/editable from the grid context so the
// column definitions stay stable across renders.
const RowMoveRenderer: React.FC<any> = (params) => {
  const r: number = params.data?.rowIndex ?? 0;
  const ctx = params.context || {};
  const editable = !!ctx.editableRef?.current;
  const pos: number = params.node?.rowIndex ?? r;
  const last = (params.api?.getDisplayedRowCount?.() ?? 1) - 1;
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-slate-400 tabular-nums">{r}</span>
      {editable && (
        <>
          <span className="flex flex-col leading-none">
            <button
              disabled={pos <= 0}
              onClick={() => ctx.moveRowRef?.current?.(r, r - 1)}
              title="Move row up"
              className="text-[9px] text-slate-500 hover:text-white disabled:opacity-20 cursor-pointer disabled:cursor-default"
            >▲</button>
            <button
              disabled={pos >= last}
              onClick={() => ctx.moveRowRef?.current?.(r, r + 1)}
              title="Move row down"
              className="text-[9px] text-slate-500 hover:text-white disabled:opacity-20 cursor-pointer disabled:cursor-default"
            >▼</button>
          </span>
          <button
            onClick={() => ctx.deleteRowRef?.current?.(r)}
            title="Delete row"
            className="text-[12px] leading-none text-slate-500 hover:text-red-400 cursor-pointer"
          >✕</button>
        </>
      )}
    </div>
  );
};

// Stored column_index -> meaning for this fixed marksheet format.
const SUBJECT_CODE_COL = 0;
const BATCH_COL = 1;
const NAME_COL = 2;
const MOBILE_COL = 3;
const SUBJECT_ID_COL = 4;

const onlyDigits = (s: string) => (s || '').replace(/\D/g, '');
const nameLetters = (s: string) => (s || '').replace(/[^A-Za-z]/g, '');
// Batch sequence down a sheet: first examiner row = "1", then R1, R2, R3, …
const batchLabelFor = (pos: number) => (pos === 0 ? '1' : `R${pos}`);
// A row "has data" once it carries a real name or mobile — auto Batch/Subject
// only attach to these, never to the blank trailing entry rows.
const rowHasData = (name: string, mobile: string) =>
  nameLetters(name).length >= 1 || onlyDigits(mobile).length >= 1;

export const VerificationPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [docDetails, setDocDetails] = useState<DocumentDetails | null>(null);
  const [cells, setCells] = useState<CellRecord[]>([]);
  // Always-current mirror of `cells` so the autosave/auto-fill handlers read the
  // latest data even when edits interleave (rapid Tab entry) before React commits.
  const cellsRef = useRef<CellRecord[]>([]);
  const [selectedCell, setSelectedCell] = useState<{ row: number; col: number } | null>(null);
  const [isSaving, setIsSaving] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(true);
  const [currentPage, setCurrentPage] = useState<number>(1);
  const [pageMetadata, setPageMetadata] = useState({
    college_code: '',
    college_name: '',
    subject_code: '',
    subject_name: '',
    faculty: '',
    total_candidates: 0,
  });
  const [isSavingMetadata, setIsSavingMetadata] = useState<boolean>(false);
  // Always-current mirror of `pageMetadata` so page-switch can flush whatever was
  // last typed even if the field never blurred (e.g. Alt+→ while still focused).
  const pageMetadataRef = useRef(pageMetadata);
  const gridApiRef = useRef<GridApi | null>(null);
  // Rows whose Batch the user edited by hand (keyed `page:row`) — auto Batch
  // numbering must not overwrite these. Pages already auto-synced once on open.
  const manualBatchRef = useRef<Set<string>>(new Set());
  const autoSyncedPagesRef = useRef<Set<number>>(new Set());

  // --- Verification lock & per-page progress ---
  const myUserId = useAuthStore((s) => s.user?.id);
  const canVerify = useAuthStore((s) => s.hasPermission('verification.perform'));
  const [vState, setVState] = useState<DocumentState | null>(null);
  const [vBusy, setVBusy] = useState(false);

  // Tenant-wide verification preferences (drive grid highlighting).
  const [lowConfThreshold, setLowConfThreshold] = useState(0.85);
  const [flagInferred, setFlagInferred] = useState(true);
  useEffect(() => {
    settingsApi
      .get()
      .then((s) => {
        setLowConfThreshold(s.settings.low_confidence_threshold ?? 0.85);
        setFlagInferred(s.settings.flag_inferred_values ?? true);
      })
      .catch(() => {});
  }, []);

  const lockedByMe = !!vState && vState.locked_by === myUserId;
  const isSubmitted = vState?.verification_status === 'submitted';
  const editable = lockedByMe && !isSubmitted && canVerify;

  const refreshVState = useCallback(async () => {
    if (!id) return;
    try {
      setVState(await verificationApi.state(id));
    } catch {
      /* ignore */
    }
  }, [id]);

  const claimDoc = async () => {
    if (!id) return;
    setVBusy(true);
    try {
      setVState(await verificationApi.claim(id));
    } catch (e: any) {
      alert(e.message || 'This file is already locked by another verifier.');
      await refreshVState();
    } finally {
      setVBusy(false);
    }
  };

  const releaseDoc = async () => {
    if (!id) return;
    if (!confirm('Release this file back to the pool? Your saved corrections are kept, but page-review progress resets for whoever takes it next.')) return;
    setVBusy(true);
    try {
      await verificationApi.release(id);
      navigate('/queue');
    } catch (e: any) {
      alert(e.message);
    } finally {
      setVBusy(false);
    }
  };

  const verifiedPageSet = useMemo(() => {
    const s = new Set<number>();
    vState?.pages?.forEach((p) => p.is_verified && s.add(p.page_number));
    return s;
  }, [vState]);

  const currentPageVerified = verifiedPageSet.has(currentPage);

  // Whether the document is ready to submit. The page currently on screen counts
  // even if it hasn't been explicitly (or switch-)verified yet, so a verifier can
  // land on the last page and submit directly without an extra round trip away
  // and back — submitDoc marks it verified for real before calling the API.
  const canSubmit = useMemo(() => {
    const total = vState?.total_pages || 0;
    if (total <= 0) return false;
    const covered = new Set(verifiedPageSet);
    covered.add(currentPage);
    return covered.size >= total;
  }, [verifiedPageSet, currentPage, vState]);

  const toggleCurrentPage = async () => {
    if (!id || !editable) return;
    setVBusy(true);
    try {
      await verificationApi.markPage(id, currentPage, !currentPageVerified);
      await refreshVState();
    } catch (e: any) {
      alert(e.message);
    } finally {
      setVBusy(false);
    }
  };

  // Submitting also covers the page currently on screen even if the user never
  // left it (so the last page of a document doesn't need a round-trip switch
  // away-and-back just to count as verified).
  const submitDoc = async () => {
    if (!id) return;
    setVBusy(true);
    try {
      if (!currentPageVerified) {
        await verificationApi.markPage(id, currentPage, true);
      }
      await verificationApi.submit(id);
      alert('Document submitted — verification complete.');
      navigate('/queue');
    } catch (e: any) {
      alert(e.message || 'Could not submit. Ensure every page is marked verified.');
      await refreshVState();
    } finally {
      setVBusy(false);
    }
  };

  // Persist a given page's header metadata. Takes explicit page/metadata (rather
  // than reading currentPage/pageMetadata state) so a page-switch can flush the
  // page being LEFT even after currentPage has already moved on.
  const persistMetadata = async (page: number, metadata: typeof pageMetadata) => {
    if (!id || !editable) return;
    try {
      setIsSavingMetadata(true);
      await apiClient(`/api/documents/${id}/pages/${page}`, {
        method: 'PUT',
        json: {
          college_code: metadata.college_code || null,
          college_name: metadata.college_name || null,
          subject_code: metadata.subject_code || null,
          subject_name: metadata.subject_name || null,
          faculty: metadata.faculty || null,
          total_candidates: metadata.total_candidates ? parseInt(metadata.total_candidates as any) : null,
        }
      });

      setDocDetails(prev => {
        if (!prev) return null;
        return {
          ...prev,
          pages: prev.pages.map(p => (p.page_number === page ? { ...p, ...metadata } : p)),
        };
      });
    } catch (err) {
      console.error('Failed to save page metadata:', err);
    } finally {
      setIsSavingMetadata(false);
    }
  };

  const handleSaveMetadata = () => persistMetadata(currentPage, pageMetadataRef.current);

  // Single entry point for every page-navigation trigger (Prev/Next buttons,
  // Alt+←/→). Flushes any unsaved header edits for the page being left — fixing
  // header data not persisting across page switches — and treats leaving a page
  // as verifying it, so Alt+V / the manual toggle are no longer required to
  // progress through a document.
  const changePage = useCallback((newPage: number) => {
    if (newPage === currentPage) return;
    if (editable && id) {
      persistMetadata(currentPage, pageMetadataRef.current);
      verificationApi.markPage(id, currentPage, true).then(refreshVState).catch(() => {});
    }
    setCurrentPage(newPage);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentPage, editable, id, refreshVState]);

  // Sync page metadata state when currentPage or docDetails changes
  useEffect(() => {
    if (!docDetails || !docDetails.pages) return;
    const pageObj = docDetails.pages.find(p => p.page_number === currentPage);
    if (pageObj) {
      setPageMetadata({
        college_code: pageObj.college_code || '',
        college_name: pageObj.college_name || '',
        subject_code: pageObj.subject_code || '',
        subject_name: pageObj.subject_name || '',
        faculty: pageObj.faculty || '',
        total_candidates: pageObj.total_candidates || 0,
      });
    }
  }, [currentPage, docDetails]);

  // Keep the metadata mirror in sync after every committed change.
  useEffect(() => { pageMetadataRef.current = pageMetadata; }, [pageMetadata]);

  // Load Data
  useEffect(() => {
    if (!id) return;

    const fetchData = async () => {
      try {
        setLoading(true);
        const details = await apiClient<DocumentDetails>(`/api/documents/${id}`);
        setDocDetails(details);

        const activeCells = await apiClient<CellRecord[]>(`/api/documents/${id}/cells`);
        setCells(activeCells);
      } catch (err) {
        console.error('Failed to load document details:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [id]);

  // Poll for document status updates if currently processing
  useEffect(() => {
    if (!id || !docDetails) return;
    const status = docDetails.document.status;
    if (!['uploaded', 'queued', 'processing'].includes(status)) return;

    const interval = setInterval(async () => {
      try {
        const details = await apiClient<DocumentDetails>(`/api/documents/${id}`);
        setDocDetails(details);
        
        // If it just transitioned to 'extracted', fetch the cells too
        if (details.document.status === 'extracted') {
          const activeCells = await apiClient<CellRecord[]>(`/api/documents/${id}/cells`);
          setCells(activeCells);
        }
      } catch (err) {
        console.error('Failed to poll document status:', err);
      }
    }, 2000);

    return () => clearInterval(interval);
  }, [id, docDetails]);

  // Keep the cells mirror in sync after every committed change.
  useEffect(() => { cellsRef.current = cells; }, [cells]);

  // Load verification lock/progress state on open.
  useEffect(() => {
    refreshVState();
  }, [refreshVState]);

  // Broadcast live presence (which page I'm on) while I hold the lock.
  useEffect(() => {
    if (!id || !lockedByMe || isSubmitted) return;
    verificationApi.presence(id, currentPage).catch(() => {});
  }, [id, currentPage, lockedByMe, isSubmitted]);

  // Keyboard shortcuts so verifiers can work mouse-free:
  //   Alt+→ / Alt+←  next / previous page (also auto-verifies the page you leave)
  //   Alt+V          manually toggle the current page's verified flag (optional —
  //                  paging away already verifies it, this is just an override)
  //   Alt+Enter      submit the document (once every page is verified)
  // Alt-modified keys don't clash with AG Grid cell editing/navigation, so they
  // work even while a cell editor is focused.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.altKey) return;
      const pages = docDetails?.pages?.length || 1;
      switch (e.key) {
        case 'ArrowRight':
          e.preventDefault();
          changePage(Math.min(currentPage + 1, pages));
          break;
        case 'ArrowLeft':
          e.preventDefault();
          changePage(Math.max(currentPage - 1, 1));
          break;
        case 'v':
        case 'V':
          if (editable) {
            e.preventDefault();
            toggleCurrentPage();
          }
          break;
        case 'Enter':
          if (editable && canSubmit) {
            e.preventDefault();
            submitDoc();
          }
          break;
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [docDetails, editable, toggleCurrentPage, submitDoc, changePage, currentPage, canSubmit]);

  // Pivot flat cell list into 2D row structures for AG Grid
  const gridData = useMemo(() => {
    const pageCells = cells.filter(c => c.page_number === currentPage);

    // Minimum blank rows to surface on a page with little/nothing read, so the
    // verifier can type straight in (no "Add Row" clicking first). Editing a
    // blank cell upserts a fresh cell via handleCellValueChanged.
    const MIN_ENTRY_ROWS = 5;

    // Find max row index
    let maxRow = -1;
    pageCells.forEach(c => {
      if (c.row_index > maxRow) maxRow = c.row_index;
    });

    let rowCount: number;
    if (!editable) {
      // Read-only: show exactly what was read, nothing more.
      rowCount = maxRow + 1;
    } else {
      // Editable: keep one spare trailing blank row (auto-grow — a new row appears
      // as soon as the last is used) and never drop below the entry-row floor.
      rowCount = Math.max(maxRow + 2, MIN_ENTRY_ROWS);
    }

    const rows: any[] = [];
    for (let r = 0; r < rowCount; r++) {
      rows.push({ rowIndex: r });
    }

    // Populate rows with cell objects
    pageCells.forEach(c => {
      rows[c.row_index][`col_${c.column_index}`] = {
        value: c.current_value,
        confidence: c.confidence,
        is_inferred: c.is_inferred,
        id: c.id,
        bbox: c.bbox
      };
    });

    return rows;
  }, [cells, currentPage, editable]);

  // Dynamically generate column definitions for AG Grid
  const columnDefs = useMemo(() => {
    const pageCells = cells.filter(c => c.page_number === currentPage);
    
    let maxCol = 4; // default to showing 5 columns (Subject ID/Code, Batch, Name, Mobile)
    pageCells.forEach(c => {
      if (c.column_index > maxCol) maxCol = c.column_index;
    });

    const cols: ColDef[] = [
      {
        headerName: 'Row #',
        field: 'rowIndex',
        width: 112,
        pinned: 'left',
        editable: false,
        cellRenderer: RowMoveRenderer,
      }
    ];

    // All data columns flex so they always fill the panel width — no horizontal
    // scrollbar. Examiner Name gets the most room; Batch the least. minWidth keeps
    // each readable if the panel gets very narrow.
    const colFlex: Record<number, { flex: number; minWidth: number }> = {
      [SUBJECT_ID_COL]: { flex: 1, minWidth: 90 },
      [SUBJECT_CODE_COL]: { flex: 1, minWidth: 100 },
      [BATCH_COL]: { flex: 0.6, minWidth: 64 },
      [NAME_COL]: { flex: 2.2, minWidth: 140 },
      [MOBILE_COL]: { flex: 1.3, minWidth: 120 },
    };

    // Display order: Subject ID (col 4), Subject Code (col 0), Batch (1),
    // Examiner Name (2), Mobile Number (3), then any extra columns. Subject ID and
    // Subject Code come from one merged cell that the worker now splits.
    const colLabels: Record<number, string> = {
      4: 'Subject ID', 0: 'Subject Code', 1: 'Batch', 2: 'Examiner Name', 3: 'Mobile Number',
    };
    const preferred = [4, 0, 1, 2, 3];
    const order = [
      ...preferred.filter((i) => i <= maxCol),
      ...Array.from({ length: maxCol + 1 }, (_, i) => i).filter((i) => !preferred.includes(i)),
    ];

    for (const c of order) {
      const colName = colLabels[c] ?? `Column ${c}`;

      const f = colFlex[c] ?? { flex: 1, minWidth: 100 };
      cols.push({
        headerName: colName,
        field: `col_${c}`,
        editable: editable,
        resizable: true,
        flex: f.flex,
        minWidth: f.minWidth,
        // Custom value getter/setter to read cell nested details
        valueGetter: (params) => {
          if (!params.data) return '';
          return params.data[`col_${c}`]?.value || '';
        },
        valueSetter: (params) => {
          if (!params.data) return false;
          if (!params.data[`col_${c}`]) {
            params.data[`col_${c}`] = { value: '', confidence: 1.0 };
          }
          params.data[`col_${c}`].value = params.newValue;
          return true;
        },
        // Styling based on OCR confidence ranges
        cellStyle: (params): CellStyle | null => {
          if (!params.data) return null;
          const cellObj = params.data[`col_${c}`];
          if (!cellObj) return null;

          // A mobile number must be exactly 10 digits. Anything else that's been
          // filled in (too short OR too long, e.g. 11) is almost certainly wrong —
          // flag it solid dark red so the verifier spots it instantly. This wins
          // over confidence/inferred styling for the mobile column.
          if (c === MOBILE_COL) {
            const d = onlyDigits(cellObj.value);
            if (d.length > 0 && d.length !== 10) {
              return { backgroundColor: '#7f1d1d', color: '#ffffff', borderLeft: '3px solid #ef4444' };
            }
          }

          // Inferred (auto-filled from a matching mobile) takes precedence: flag it
          // distinctly so a reviewer knows the value was voted, not read.
          if (cellObj.is_inferred && flagInferred) {
            return { backgroundColor: 'rgba(139, 92, 246, 0.14)', borderLeft: '3px solid #8b5cf6' }; // purple = inferred
          }

          const conf = cellObj.confidence;
          if (conf < lowConfThreshold) {
            return { backgroundColor: 'rgba(239, 68, 68, 0.15)', borderLeft: '3px solid #ef4444' }; // soft red for low conf
          } else if (conf < Math.min(1, lowConfThreshold + 0.1)) {
            return { backgroundColor: 'rgba(245, 158, 11, 0.12)', borderLeft: '3px solid #f59e0b' }; // soft yellow
          }
          return { backgroundColor: 'rgba(16, 185, 129, 0.05)', borderLeft: 'none' }; // soft green
        },
        // Show tooltip details
        tooltipValueGetter: (params) => {
          if (!params.data) return '';
          const cellObj = params.data[`col_${c}`];
          if (!cellObj) return '';
          if (cellObj.is_inferred) {
            return `Auto-filled by consensus from matching examiner rows (inferred) — please verify. Confidence: ${(cellObj.confidence * 100).toFixed(1)}%`;
          }
          return `Confidence: ${(cellObj.confidence * 100).toFixed(1)}%`;
        }
      });
    }
    return cols;
  }, [cells, currentPage, editable, lowConfThreshold, flagInferred]);

  // Keep grid references
  const onGridReady = (params: GridReadyEvent) => {
    gridApiRef.current = params.api;
  };

  // Click row/cell in AG Grid highlights the canvas bbox
  const onCellClicked = (event: CellClickedEvent) => {
    if (event.colDef.field === 'rowIndex') return;
    
    const colField = event.colDef.field || '';
    const colIdx = parseInt(colField.replace('col_', ''));
    const rowIdx = event.data.rowIndex;

    setSelectedCell({ row: rowIdx, col: colIdx });
  };

  // Click bbox on Canvas selects cell in AG Grid
  const handleCellSelectFromCanvas = (row: number, col: number) => {
    setSelectedCell({ row, col });
    if (gridApiRef.current) {
      gridApiRef.current.ensureIndexVisible(row);
      gridApiRef.current.setFocusedCell(row, `col_${col}`);
    }
  };

  const handleAddRow = () => {
    const pageCells = cells.filter(c => c.page_number === currentPage);
    let maxCol = 4; // default to 5 columns (Subject ID/Code, Batch, Name, Mobile)
    let maxRow = -1;
    pageCells.forEach(c => {
      if (c.column_index > maxCol) maxCol = c.column_index;
      if (c.row_index > maxRow) maxRow = c.row_index;
    });

    const colsCount = maxCol;
    const newRowIdx = maxRow + 1;
    const newCells: CellRecord[] = [];

    for (let c = 0; c <= colsCount; c++) {
      newCells.push({
        id: `new-${newRowIdx}-${c}-${Date.now()}`,
        document_id: id || '',
        page_number: currentPage,
        row_index: newRowIdx,
        column_index: c,
        original_value: '',
        current_value: '',
        confidence: 1.0,
        is_inferred: false,
        bbox: { x: 0, y: 0, width: 0, height: 0 },
        version: 1,
      });
    }

    setCells(prev => [...prev, ...newCells]);
  };

  // ---- cell helpers used by the editing + auto-fill logic ----
  const ZERO_BBOX = { x: 0, y: 0, width: 0, height: 0 };
  const cellAt = (list: CellRecord[], page: number, row: number, col: number) =>
    list.find(c => c.page_number === page && c.row_index === row && c.column_index === col);
  const valAt = (list: CellRecord[], page: number, row: number, col: number) =>
    cellAt(list, page, row, col)?.current_value ?? '';
  const bboxAt = (list: CellRecord[], page: number, row: number, col: number) =>
    cellAt(list, page, row, col)?.bbox ?? ZERO_BBOX;

  // Sorted indices of rows on a page that carry real examiner content.
  const dataRowsOf = (list: CellRecord[], page: number) => {
    const rows = new Set<number>();
    list.forEach(c => { if (c.page_number === page) rows.add(c.row_index); });
    return [...rows]
      .filter(r => rowHasData(valAt(list, page, r, NAME_COL), valAt(list, page, r, MOBILE_COL)))
      .sort((a, b) => a - b);
  };

  // Immutable upsert of a single cell into a cells array.
  const upsertLocal = (
    list: CellRecord[], page: number, row: number, col: number, value: string,
    opts?: { isInferred?: boolean; confidence?: number; bbox?: CellRecord['bbox'] },
  ): CellRecord[] => {
    const idx = list.findIndex(c => c.page_number === page && c.row_index === row && c.column_index === col);
    const confidence = opts?.confidence ?? 1.0;
    const isInferred = opts?.isInferred ?? false;
    if (idx >= 0) {
      const next = [...list];
      next[idx] = { ...next[idx], current_value: value, confidence, is_inferred: isInferred, ...(opts?.bbox ? { bbox: opts.bbox } : {}) };
      return next;
    }
    return [...list, {
      id: `new-${page}-${row}-${col}-${Date.now()}-${Math.round(Math.random() * 1e6)}`,
      document_id: id || '', page_number: page, row_index: row, column_index: col,
      original_value: '', current_value: value, confidence, is_inferred: isInferred,
      bbox: opts?.bbox || { ...ZERO_BBOX }, version: 1,
    }];
  };

  // Persist a single cell to the backend. `auto` flags machine-derived fills so the
  // server skips the correction-feedback loop.
  const persistRemote = async (
    page: number, row: number, col: number, value: string, bbox: CellRecord['bbox'],
    opts?: { isInferred?: boolean; confidence?: number; auto?: boolean },
  ) => {
    await apiClient(`/api/documents/${id}/cells`, {
      method: 'PUT',
      json: {
        page_number: page, row_index: row, column_index: col, value, bbox,
        ...(opts?.isInferred != null ? { is_inferred: opts.isInferred } : {}),
        ...(opts?.confidence != null ? { confidence: opts.confidence } : {}),
        ...(opts?.auto ? { auto: true } : {}),
      },
    });
  };

  // #6 + #7: derive Batch and Subject values for data rows. Batch follows the
  // positional sequence (1, R1, R2, …) unless the user edited it; Subject Code/ID
  // fill from the first data row only when empty (first-row overwrite of all rows
  // is handled in handleCellValueChanged).
  type AutoWrite = { row: number; col: number; value: string };
  const computeAutoWrites = (list: CellRecord[], page: number): AutoWrite[] => {
    const dataRows = dataRowsOf(list, page);
    if (!dataRows.length) return [];
    const writes: AutoWrite[] = [];
    dataRows.forEach((r, i) => {
      const expectedBatch = batchLabelFor(i);
      if (!manualBatchRef.current.has(`${page}:${r}`) && valAt(list, page, r, BATCH_COL) !== expectedBatch) {
        writes.push({ row: r, col: BATCH_COL, value: expectedBatch });
      }
      [SUBJECT_CODE_COL, SUBJECT_ID_COL].forEach(col => {
        const established = valAt(list, page, dataRows[0], col);
        if (established && valAt(list, page, r, col).trim() === '') {
          writes.push({ row: r, col, value: established });
        }
      });
    });
    return writes;
  };

  // Save edits and apply all derived fills (#5 lookup, #6 batch, #7 subject).
  const handleCellValueChanged = async (event: any) => {
    const colField = event.colDef.field || '';
    const colIdx = parseInt(colField.replace('col_', ''));
    const rowIdx = event.data.rowIndex;
    const newValue = String(event.newValue ?? '');

    if (colIdx === BATCH_COL) manualBatchRef.current.add(`${currentPage}:${rowIdx}`);
    const editedBBox = bboxAt(cellsRef.current, currentPage, rowIdx, colIdx);
    let working = upsertLocal(cellsRef.current, currentPage, rowIdx, colIdx, newValue, { bbox: editedBBox });

    try {
      setIsSaving(true);
      await persistRemote(currentPage, rowIdx, colIdx, newValue, editedBBox);

      // Collect derived writes; later rules keep priority over auto-fill.
      const writeMap = new Map<string, { row: number; col: number; value: string; isInferred?: boolean; confidence?: number }>();
      const put = (w: { row: number; col: number; value: string; isInferred?: boolean; confidence?: number }) =>
        writeMap.set(`${w.row}:${w.col}`, w);

      // #7 — first data row's Subject Code/ID overwrites every data row.
      if (colIdx === SUBJECT_CODE_COL || colIdx === SUBJECT_ID_COL) {
        const dr = dataRowsOf(working, currentPage);
        if (dr.length && rowIdx === dr[0]) {
          dr.forEach(r => { if (r !== rowIdx && valAt(working, currentPage, r, colIdx) !== newValue) put({ row: r, col: colIdx, value: newValue }); });
        }
      }

      // #5 — a corrected 10-digit mobile fetches the examiner name from the DB.
      // Only fills an empty/inferred name; never overwrites a human-typed one.
      if (colIdx === MOBILE_COL) {
        const digits = onlyDigits(newValue);
        if (digits.length === 10) {
          const nameCellObj = cellAt(working, currentPage, rowIdx, NAME_COL);
          const nameVal = nameCellObj?.current_value ?? '';
          const nameInferred = nameCellObj?.is_inferred ?? false;
          // Fill unless the name is one the human themselves typed/confirmed
          // (confidence 1.0, not inferred). Empty, inferred, or low-confidence OCR
          // names are refreshed from the directory.
          const humanConfirmed = !nameInferred && (nameCellObj?.confidence ?? 0) >= 1.0 && nameLetters(nameVal).length >= 2;
          if (!humanConfirmed) {
            try {
              const res = await examinersApi.lookup(digits);
              if (res?.name && !res.ambiguous) {
                put({ row: rowIdx, col: NAME_COL, value: res.name, isInferred: true, confidence: 0.95 });
              }
            } catch { /* lookup is best-effort */ }
          }
        }
      }

      // #6 — auto Batch + fill empty Subject (without clobbering #5/#7 entries).
      computeAutoWrites(working, currentPage).forEach(w => { if (!writeMap.has(`${w.row}:${w.col}`)) put(w); });

      const writes = [...writeMap.values()];
      if (writes.length) {
        await Promise.all(writes.map(w =>
          persistRemote(currentPage, w.row, w.col, w.value, bboxAt(working, currentPage, w.row, w.col),
            { isInferred: w.isInferred, confidence: w.confidence, auto: true })));
      }

      // Rebase onto the freshest local state before committing. `working` is a
      // snapshot taken before the awaits above (persistRemote, examinersApi.lookup)
      // — other rows may have been edited and already saved while we were waiting,
      // and blindly writing back `working` would silently erase those edits (#2:
      // typing a second mobile number reverts the first once this lookup resolves).
      let latest = cellsRef.current;
      latest = upsertLocal(latest, currentPage, rowIdx, colIdx, newValue, { bbox: editedBBox });
      writes.forEach(w => {
        latest = upsertLocal(latest, currentPage, w.row, w.col, w.value,
          { isInferred: w.isInferred, confidence: w.confidence, bbox: bboxAt(working, currentPage, w.row, w.col) });
      });
      cellsRef.current = latest;
      setCells(latest);
    } catch (err) {
      console.error('Failed to save cell correction:', err);
    } finally {
      setIsSaving(false);
    }
  };

  // Run the auto Batch/Subject fill once when a page is opened (so existing data
  // rows get their sequence/subject even before the user touches anything).
  const syncAutoFields = async (page: number) => {
    const base = cellsRef.current;
    const writes = computeAutoWrites(base, page);
    if (!writes.length) return;
    try {
      setIsSaving(true);
      await Promise.all(writes.map(w =>
        persistRemote(page, w.row, w.col, w.value, bboxAt(base, page, w.row, w.col), { auto: true })));
      // Rebase onto the freshest state — the user may have started typing while
      // this ran (see the identical fix in handleCellValueChanged above).
      let latest = cellsRef.current;
      writes.forEach(w => { latest = upsertLocal(latest, page, w.row, w.col, w.value, { bbox: bboxAt(base, page, w.row, w.col) }); });
      cellsRef.current = latest;
      setCells(latest);
    } catch (e) {
      console.error('Auto-field sync failed:', e);
    } finally {
      setIsSaving(false);
    }
  };

  // #10 — Excel-like row move: swap every content column (value + bbox + flags)
  // between two rows, then renumber Batch by position. Used by the ▲/▼ controls.
  const moveRow = async (rowA: number, rowB: number) => {
    if (rowA === rowB || rowB < 0) return;
    const pageCells = cellsRef.current.filter(c => c.page_number === currentPage);
    if (!pageCells.length) return;
    const maxCol = Math.max(SUBJECT_ID_COL, ...pageCells.map(c => c.column_index));
    let working = cellsRef.current;
    const swaps: { row: number; col: number; value: string; bbox: CellRecord['bbox']; isInferred: boolean; confidence: number }[] = [];
    for (let col = 0; col <= maxCol; col++) {
      if (col === BATCH_COL) continue; // Batch is positional — renumbered below.
      const a = cellAt(working, currentPage, rowA, col);
      const b = cellAt(working, currentPage, rowB, col);
      const aVal = a?.current_value ?? '', bVal = b?.current_value ?? '';
      if (aVal === '' && bVal === '') continue;
      swaps.push({ row: rowA, col, value: bVal, bbox: b?.bbox ?? { ...ZERO_BBOX }, isInferred: b?.is_inferred ?? false, confidence: b?.confidence ?? 1.0 });
      swaps.push({ row: rowB, col, value: aVal, bbox: a?.bbox ?? { ...ZERO_BBOX }, isInferred: a?.is_inferred ?? false, confidence: a?.confidence ?? 1.0 });
    }
    if (!swaps.length) return;
    manualBatchRef.current.delete(`${currentPage}:${rowA}`);
    manualBatchRef.current.delete(`${currentPage}:${rowB}`);
    try {
      setIsSaving(true);
      await Promise.all(swaps.map(s => persistRemote(currentPage, s.row, s.col, s.value, s.bbox, { isInferred: s.isInferred, confidence: s.confidence, auto: true })));
      swaps.forEach(s => { working = upsertLocal(working, currentPage, s.row, s.col, s.value, { isInferred: s.isInferred, confidence: s.confidence, bbox: s.bbox }); });
      const batchWrites = computeAutoWrites(working, currentPage).filter(w => w.col === BATCH_COL);
      if (batchWrites.length) {
        await Promise.all(batchWrites.map(w => persistRemote(currentPage, w.row, w.col, w.value, bboxAt(working, currentPage, w.row, w.col), { auto: true })));
        batchWrites.forEach(w => { working = upsertLocal(working, currentPage, w.row, w.col, w.value, { bbox: bboxAt(working, currentPage, w.row, w.col) }); });
      }
      cellsRef.current = working;
      setCells(working);
    } catch (e) {
      console.error('Row move failed:', e);
    } finally {
      setIsSaving(false);
    }
  };

  // Delete a row: remove its cells on the server (which shifts rows below up),
  // mirror that locally, then renumber Batch by the new positions.
  const deleteRow = async (row: number) => {
    if (!id) return;
    if (!window.confirm('Delete this row? Rows below it shift up.')) return;
    try {
      setIsSaving(true);
      await apiClient(`/api/documents/${id}/rows/${currentPage}/${row}`, { method: 'DELETE' });
      let working = cellsRef.current
        .filter(c => !(c.page_number === currentPage && c.row_index === row))
        .map(c => (c.page_number === currentPage && c.row_index > row) ? { ...c, row_index: c.row_index - 1 } : c);
      // Positions changed — drop this page's manual-batch markers and renumber.
      [...manualBatchRef.current].forEach(k => { if (k.startsWith(`${currentPage}:`)) manualBatchRef.current.delete(k); });
      const batchWrites = computeAutoWrites(working, currentPage).filter(w => w.col === BATCH_COL);
      if (batchWrites.length) {
        await Promise.all(batchWrites.map(w => persistRemote(currentPage, w.row, w.col, w.value, bboxAt(working, currentPage, w.row, w.col), { auto: true })));
        batchWrites.forEach(w => { working = upsertLocal(working, currentPage, w.row, w.col, w.value, { bbox: bboxAt(working, currentPage, w.row, w.col) }); });
      }
      cellsRef.current = working;
      setCells(working);
      setSelectedCell(null);
    } catch (e) {
      console.error('Row delete failed:', e);
    } finally {
      setIsSaving(false);
    }
  };

  // Refs so the grid's Row# renderer can call the latest moveRow / deleteRow /
  // editable without rebuilding the column definitions on every render.
  const moveRowRef = useRef(moveRow);
  moveRowRef.current = moveRow;
  const deleteRowRef = useRef(deleteRow);
  deleteRowRef.current = deleteRow;
  const editableRef = useRef(editable);
  editableRef.current = editable;

  // #6/#7 fire on load: auto-fill Batch/Subject for the page's data rows once,
  // when its cells are present and the file is editable.
  useEffect(() => {
    if (!editable) return;
    const pageCells = cells.filter(c => c.page_number === currentPage);
    if (!pageCells.length) return;
    if (autoSyncedPagesRef.current.has(currentPage)) return;
    autoSyncedPagesRef.current.add(currentPage);
    syncAutoFields(currentPage);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editable, currentPage, cells]);

  const handleDownload = (format: 'csv' | 'excel') => {
    if (!id) return;
    const token = localStorage.getItem('auth_token') || '';
    window.open(`/api/documents/${id}/export/${format}?token=${encodeURIComponent(token)}`, '_blank');
  };

  // Convert cells list to canvas viewer interface
  const canvasCells = useMemo((): CanvasCell[] => {
    const pageCells = cells.filter(c => c.page_number === currentPage);
    return pageCells.map(c => ({
      row_index: c.row_index,
      column_index: c.column_index,
      bbox: c.bbox,
      value: c.current_value
    }));
  }, [cells, currentPage]);

  // Count low-confidence cells (below the tenant-configured threshold)
  const lowConfidenceCount = useMemo(() => {
    return cells.filter(c => c.confidence < lowConfThreshold).length;
  }, [cells, lowConfThreshold]);

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-slate-950 text-white">
        <div className="flex flex-col items-center gap-3">
          <Loader2 className="w-12 h-12 text-blue-500 animate-spin" />
          <h2 className="text-lg font-medium text-slate-300">Loading workspace files...</h2>
        </div>
      </div>
    );
  }

  const docStatus = docDetails?.document.status;
  const progressPercentage = docDetails?.document.progress_percentage || 0;

  if (docStatus === 'failed') {
    return (
      <div className="flex h-screen items-center justify-center bg-slate-950 text-white">
        <div className="flex flex-col items-center gap-4 max-w-md text-center p-8 bg-slate-900 border border-slate-800 rounded-2xl shadow-2xl">
          <AlertCircle className="w-16 h-16 text-red-500" />
          <h2 className="text-xl font-bold text-white">Document Processing Failed</h2>
          <p className="text-sm text-slate-400">
            The layout parsing and OCR pipeline failed for this document. This typically happens if the file is corrupted, has unreadable text structure, or contains unsupported formatting.
          </p>
          <button
            onClick={() => navigate('/documents')}
            className="mt-2 px-5 py-2 bg-slate-850 hover:bg-slate-700 text-white text-xs font-semibold rounded-lg transition"
          >
            Back to Dashboard
          </button>
        </div>
      </div>
    );
  }

  if (['uploaded', 'queued', 'processing'].includes(docStatus || '')) {
    return (
      <div className="flex h-screen items-center justify-center bg-slate-950 text-white">
        <div className="flex flex-col items-center gap-4 text-center p-8 bg-slate-900 border border-slate-800 rounded-2xl shadow-2xl w-full max-w-sm">
          <Loader2 className="w-12 h-12 text-blue-500 animate-spin" />
          <h2 className="text-lg font-bold text-white">Extracting Document... {progressPercentage}%</h2>
          
          <div className="w-full bg-slate-800 rounded-full h-1.5 mt-1 overflow-hidden">
            <div 
              className="bg-blue-500 h-1.5 rounded-full transition-all duration-500" 
              style={{ width: `${progressPercentage}%` }}
            />
          </div>

          <p className="text-xs text-slate-400">
            The OCR engine is currently segmenting cells and extracting tabular panel data. This will take a moment.
          </p>
          <button
            onClick={() => navigate('/documents')}
            className="mt-2 px-4 py-2 bg-slate-950 hover:bg-slate-900 text-slate-400 hover:text-white border border-slate-800 text-xs font-semibold rounded-lg transition"
          >
            Back to Dashboard
          </button>
        </div>
      </div>
    );
  }

  const token = localStorage.getItem('auth_token') || '';
  const pageObj = docDetails?.pages?.find(p => p.page_number === currentPage) || docDetails?.pages?.[0];
  const totalPages = docDetails?.pages?.length || 1;
  
  let cleanPath = pageObj?.image_path ? pageObj.image_path.replace(/\\/g, '/') : '';
  if (cleanPath.startsWith('/var/data/uploads/')) {
    cleanPath = cleanPath.substring('/var/data/uploads/'.length);
  } else if (cleanPath.startsWith('uploads/')) {
    cleanPath = cleanPath.substring('uploads/'.length);
  }

  const imageUrl = cleanPath
    ? `/api/uploads/${cleanPath}?token=${encodeURIComponent(token)}`
    : 'https://via.placeholder.com/800x1100'; // fallback

  return (
    <div className="flex flex-col h-screen bg-slate-950 text-slate-100 font-sans overflow-hidden">
      {/* Top Header */}
      <header className="flex items-center justify-between px-6 py-4 bg-slate-900 border-b border-slate-800 shrink-0">
        <div className="flex items-center gap-3">
          <button 
            onClick={() => navigate('/documents')}
            className="p-2 bg-slate-800 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white transition"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div>
            <h1 className="text-md font-bold tracking-tight">{docDetails?.document.name || 'Loading Document...'}</h1>
            <p className="text-xs text-slate-500">Status: {docDetails?.document.status || 'N/A'} | Uploaded: {docDetails?.document.created_at ? new Date(docDetails.document.created_at).toLocaleDateString() : 'N/A'}</p>
          </div>
        </div>

        {/* Page Selector Navigation */}
        <div className="flex items-center gap-2 bg-slate-950 border border-slate-800 px-3 py-1.5 rounded-lg select-none">
          <button
            onClick={() => changePage(Math.max(currentPage - 1, 1))}
            disabled={currentPage === 1}
            title="Previous page (Alt+←)"
            className="px-2 py-1 bg-slate-800 hover:bg-slate-700 disabled:bg-slate-900 disabled:text-slate-600 text-xs font-semibold rounded transition-all cursor-pointer disabled:cursor-not-allowed"
          >
            Prev
          </button>
          <span className="text-xs font-semibold text-slate-300">
            Page {currentPage} of {totalPages}
          </span>
          <button
            onClick={() => changePage(Math.min(currentPage + 1, totalPages))}
            disabled={currentPage === totalPages}
            title="Next page (Alt+→)"
            className="px-2 py-1 bg-slate-800 hover:bg-slate-700 disabled:bg-slate-900 disabled:text-slate-600 text-xs font-semibold rounded transition-all cursor-pointer disabled:cursor-not-allowed"
          >
            Next
          </button>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={() => handleDownload('csv')}
            className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer"
          >
            <Download className="w-3.5 h-3.5" /> CSV
          </button>
          <button
            onClick={() => handleDownload('excel')}
            className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer"
          >
            <Download className="w-3.5 h-3.5" /> Excel
          </button>

          {/* Verification lock controls */}
          {isSubmitted ? (
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1.5 px-3 py-1.5 bg-emerald-950 border border-emerald-900 rounded-lg text-xs font-semibold text-emerald-400">
                <CheckCircle className="w-3.5 h-3.5" /> Submitted
              </span>
              {canVerify && (
                <button
                  onClick={claimDoc}
                  disabled={vBusy}
                  title="Files can be verified more than once — reopening keeps whatever you submit next as the latest status"
                  className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer disabled:opacity-50"
                >
                  <Lock className="w-3.5 h-3.5" /> Reopen to re-verify
                </button>
              )}
            </div>
          ) : lockedByMe ? (
            <>
              <span className="px-2.5 py-1.5 bg-slate-950 border border-slate-800 rounded-lg text-xs font-semibold text-slate-300">
                {verifiedPageSet.size}/{vState?.total_pages || totalPages} pages
              </span>
              <button
                onClick={releaseDoc}
                disabled={vBusy}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer disabled:opacity-50"
              >
                <Unlock className="w-3.5 h-3.5" /> Release
              </button>
              <button
                onClick={submitDoc}
                disabled={vBusy || !canSubmit}
                title={canSubmit ? 'Submit verified document (Alt+Enter)' : 'Mark every page verified before submitting'}
                className="flex items-center gap-2 px-4 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:bg-emerald-900 disabled:text-emerald-600/60 rounded-lg text-xs font-semibold text-white transition shadow-lg shadow-emerald-600/20 cursor-pointer disabled:cursor-not-allowed disabled:shadow-none"
              >
                <CheckCircle className="w-3.5 h-3.5" /> Submit
              </button>
            </>
          ) : vState?.locked_by ? (
            <span className="flex items-center gap-1.5 px-3 py-1.5 bg-amber-950 border border-amber-900 rounded-lg text-xs font-semibold text-amber-400">
              <Lock className="w-3.5 h-3.5" /> Locked by {vState.locked_by_name || 'another user'}
            </span>
          ) : canVerify ? (
            <button
              onClick={claimDoc}
              disabled={vBusy}
              className="flex items-center gap-2 px-4 py-1.5 bg-blue-600 hover:bg-blue-500 rounded-lg text-xs font-semibold text-white transition shadow-lg shadow-blue-600/20 cursor-pointer disabled:opacity-50"
            >
              <Lock className="w-3.5 h-3.5" /> Claim to verify
            </button>
          ) : null}
        </div>
      </header>

      {/* Lock state banner */}
      {!lockedByMe && (
        <div className="bg-slate-900/80 border-b border-slate-800 px-6 py-2 text-xs text-slate-400 shrink-0">
          {vState?.locked_by
            ? `This file is being verified by ${vState.locked_by_name || 'another user'}. You are viewing it read-only.`
            : isSubmitted
              ? canVerify
                ? 'This file has already been submitted. Reopen it to make corrections and resubmit — the latest submission is what counts.'
                : 'This file has been submitted and is read-only.'
              : canVerify
                ? 'Read-only preview. Claim the file to lock it to you and start verifying.'
                : 'You have read-only access to this document.'}
        </div>
      )}

      {/* Warnings & Notices */}
      {lowConfidenceCount > 0 && (
        <div className="bg-amber-950/80 border-b border-amber-900/60 px-6 py-2.5 flex items-center gap-2 text-amber-300 text-xs shrink-0">
          <AlertCircle className="w-4 h-4 text-amber-400 shrink-0" />
          <span>
            Warning: <strong>{lowConfidenceCount} cells</strong> contain low confidence extractions (&lt; {Math.round(lowConfThreshold * 100)}%). Please review highlighted grid entries before submitting.
          </span>
        </div>
      )}

      {/* Split-Screen Workspace */}
      <main className="flex-1 flex overflow-hidden p-6 gap-6">
        {/* Left Panel: Image Canvas — narrower than the editor so the grid (esp.
            the Mobile Number column) never needs a horizontal scrollbar. */}
        <div className="w-[45%] h-full shrink-0">
          <CanvasViewer
            imageUrl={imageUrl}
            cells={canvasCells}
            selectedCell={selectedCell}
            onCellSelect={handleCellSelectFromCanvas}
          />
        </div>

        {/* Right Panel: Spreadsheet Editor */}
        <div className="flex-1 min-w-0 h-full flex flex-col bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-2xl">
          {/* Metadata Form Section */}
          <div className="p-4 border-b border-slate-800 bg-slate-950/40">
            <div className="flex items-center gap-2 mb-3">
              <h3 className="text-xs font-bold text-slate-400 uppercase tracking-wider">Panel Header Information</h3>
              {isSavingMetadata && (
                <span className="flex items-center gap-1 text-[10px] text-blue-400 font-semibold">
                  <Loader2 className="w-3 h-3 animate-spin" /> Saving…
                </span>
              )}
            </div>
            <div className="grid grid-cols-2 gap-3 mb-3">
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">College Code</label>
                <input
                  type="text"
                  value={pageMetadata.college_code}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, college_code: e.target.value }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. 123"
                />
              </div>
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">College Name</label>
                <input
                  type="text"
                  value={pageMetadata.college_name}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, college_name: e.target.value }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. AAKASH MAHAVIDHYALAYA"
                />
              </div>
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">Subject Code</label>
                <input
                  type="text"
                  value={pageMetadata.subject_code}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, subject_code: e.target.value }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. BOT-75P-302"
                />
              </div>
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">Subject Name</label>
                <input
                  type="text"
                  value={pageMetadata.subject_name}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, subject_name: e.target.value }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. BOT-PRACTICAL-V"
                />
              </div>
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">Faculty</label>
                <input
                  type="text"
                  value={pageMetadata.faculty}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, faculty: e.target.value }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. SCIENCE"
                />
              </div>
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">Total Candidates</label>
                <input
                  type="number"
                  value={pageMetadata.total_candidates}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, total_candidates: parseInt(e.target.value) || 0 }))}
                  onBlur={handleSaveMetadata}
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. 15"
                />
              </div>
            </div>
          </div>

          <div className="px-4 py-3 border-b border-slate-800 bg-slate-900/60 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <span className="text-xs font-semibold text-slate-300">Tabular Editor</span>
              {editable && (
                <button
                  onClick={handleAddRow}
                  className="px-2.5 py-1 bg-slate-800 hover:bg-slate-700 hover:text-white text-slate-300 text-[10px] font-bold rounded transition border border-slate-700 cursor-pointer"
                >
                  + Add Row
                </button>
              )}
              {isSaving && <span className="text-[10px] text-blue-400 animate-pulse font-semibold">Autosaving…</span>}
            </div>
            {/* Per-page verification toggle (gates Submit) */}
            <button
              onClick={toggleCurrentPage}
              disabled={!editable || vBusy}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold transition cursor-pointer disabled:cursor-not-allowed ${
                currentPageVerified
                  ? 'bg-emerald-950 border border-emerald-900 text-emerald-400'
                  : 'bg-slate-800 border border-slate-700 text-slate-300 hover:text-white disabled:opacity-50'
              }`}
              title={editable ? 'Changing page already marks it verified — use this only to unmark it (Alt+V)' : 'Claim the file to mark pages verified'}
            >
              <Check className="w-3.5 h-3.5" />
              {currentPageVerified ? `Page ${currentPage} verified` : `Mark page ${currentPage} verified`}
            </button>
          </div>
          
          {/* Keyboard shortcuts legend — keeps verification mouse-free */}
          <div className="px-4 py-2 border-b border-slate-800 bg-slate-950/40 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[10px] text-slate-400">
            <span className="flex items-center gap-1.5 font-semibold text-slate-300">
              <Keyboard className="w-3.5 h-3.5" /> Shortcuts
            </span>
            <span className="flex items-center gap-1"><Kbd>Enter</Kbd> next row</span>
            <span className="flex items-center gap-1"><Kbd>Tab</Kbd> next cell</span>
            <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>←</Kbd>/<Kbd>→</Kbd> change page (auto-verifies it)</span>
            {editable && (
              <>
                <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>V</Kbd> unmark/mark page (optional)</span>
                <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>Enter</Kbd> submit</span>
              </>
            )}
          </div>

          {/* Cell colour legend — what each highlight in the grid means */}
          <div className="px-4 py-2 border-b border-slate-800 bg-slate-950/40 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[10px] text-slate-400">
            <span className="font-semibold text-slate-300">Legend</span>
            <span className="flex items-center gap-1.5"><span className="inline-block h-3 w-3 rounded-sm" style={{ backgroundColor: '#7f1d1d' }} /> mobile not 10 digits</span>
            <span className="flex items-center gap-1.5"><span className="inline-block h-3 w-3 rounded-sm" style={{ backgroundColor: 'rgba(239, 68, 68, 0.45)' }} /> low confidence</span>
            <span className="flex items-center gap-1.5"><span className="inline-block h-3 w-3 rounded-sm" style={{ backgroundColor: 'rgba(245, 158, 11, 0.4)' }} /> medium confidence</span>
            <span className="flex items-center gap-1.5"><span className="inline-block h-3 w-3 rounded-sm" style={{ backgroundColor: 'rgba(139, 92, 246, 0.45)' }} /> auto-filled (verify)</span>
          </div>

          <div className="flex-1 w-full ag-theme-quartz-dark">
            <AgGridReact
              rowData={gridData}
              columnDefs={columnDefs}
              getRowId={(p) => String(p.data.rowIndex)}
              context={{ moveRowRef, editableRef, deleteRowRef }}
              onGridReady={onGridReady}
              onCellClicked={onCellClicked}
              onCellValueChanged={handleCellValueChanged}
              singleClickEdit={true}
              stopEditingWhenCellsLoseFocus={true}
              enterNavigatesVertically={true}
              enterNavigatesVerticallyAfterEdit={true}
              enableCellTextSelection={true}
              suppressCellFocus={false}
              pagination={true}
              paginationPageSize={50}
              tooltipShowDelay={200}
            />
          </div>
        </div>
      </main>
    </div>
  );
};
export default VerificationPage;
