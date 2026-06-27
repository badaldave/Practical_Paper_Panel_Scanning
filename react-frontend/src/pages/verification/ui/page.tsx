import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, GridApi, GridReadyEvent, CellClickedEvent } from 'ag-grid-community';
import { AlertCircle, Download, CheckCircle, ArrowLeft, Loader2, Lock, Unlock, Check, Keyboard } from 'lucide-react';

import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';

import { apiClient } from '@/shared/api/client';
import { CanvasViewer, CanvasCell } from '@/shared/ui/canvas-viewer';
import { verificationApi, settingsApi, type DocumentState } from '@/shared/api/services';
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

export const VerificationPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [docDetails, setDocDetails] = useState<DocumentDetails | null>(null);
  const [cells, setCells] = useState<CellRecord[]>([]);
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
  const gridApiRef = useRef<GridApi | null>(null);

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

  const submitDoc = async () => {
    if (!id) return;
    setVBusy(true);
    try {
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
  //   Alt+→ / Alt+←  next / previous page
  //   Alt+V          toggle the current page as verified
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
          setCurrentPage((prev) => Math.min(prev + 1, pages));
          break;
        case 'ArrowLeft':
          e.preventDefault();
          setCurrentPage((prev) => Math.max(prev - 1, 1));
          break;
        case 'v':
        case 'V':
          if (editable) {
            e.preventDefault();
            toggleCurrentPage();
          }
          break;
        case 'Enter':
          if (
            editable &&
            (vState?.total_pages || 0) > 0 &&
            verifiedPageSet.size >= (vState?.total_pages || pages)
          ) {
            e.preventDefault();
            submitDoc();
          }
          break;
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [docDetails, editable, toggleCurrentPage, submitDoc, vState, verifiedPageSet]);

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
        width: 80,
        pinned: 'left',
        editable: false,
      }
    ];

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

      cols.push({
        headerName: colName,
        field: `col_${c}`,
        editable: editable,
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
        cellStyle: (params) => {
          if (!params.data) return null;
          const cellObj = params.data[`col_${c}`];
          if (!cellObj) return null;

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

  const handleSaveMetadata = async () => {
    if (!id) return;
    try {
      setIsSavingMetadata(true);
      await apiClient(`/api/documents/${id}/pages/${currentPage}`, {
        method: 'PUT',
        json: {
          college_code: pageMetadata.college_code || null,
          college_name: pageMetadata.college_name || null,
          subject_code: pageMetadata.subject_code || null,
          subject_name: pageMetadata.subject_name || null,
          faculty: pageMetadata.faculty || null,
          total_candidates: pageMetadata.total_candidates ? parseInt(pageMetadata.total_candidates as any) : null,
        }
      });

      setDocDetails(prev => {
        if (!prev) return null;
        return {
          ...prev,
          pages: prev.pages.map(p => {
            if (p.page_number === currentPage) {
              return {
                ...p,
                college_code: pageMetadata.college_code,
                college_name: pageMetadata.college_name,
                subject_code: pageMetadata.subject_code,
                subject_name: pageMetadata.subject_name,
                faculty: pageMetadata.faculty,
                total_candidates: pageMetadata.total_candidates,
              };
            }
            return p;
          })
        };
      });
    } catch (err) {
      console.error('Failed to save page metadata:', err);
    } finally {
      setIsSavingMetadata(false);
    }
  };

  // Save changes
  const handleCellValueChanged = async (event: any) => {
    const colField = event.colDef.field || '';
    const colIdx = parseInt(colField.replace('col_', ''));
    const rowIdx = event.data.rowIndex;
    const newValue = event.newValue;

    const cellObj = cells.find(c => c.page_number === currentPage && c.row_index === rowIdx && c.column_index === colIdx);
    // No backing cell yet (e.g. a pre-seeded blank row) — upsert a new one.
    const bbox = cellObj?.bbox || { x: 0, y: 0, width: 0, height: 0 };

    try {
      setIsSaving(true);
      await apiClient(`/api/documents/${id}/cells`, {
        method: 'PUT',
        json: {
          page_number: currentPage,
          row_index: rowIdx,
          column_index: colIdx,
          value: newValue,
          bbox
        }
      });

      // Update local cells state (boost edited cell confidence to 100%), or add
      // the cell if it didn't exist yet so the grid/canvas stay in sync.
      setCells(prev => {
        if (cellObj) {
          return prev.map(c => {
            if (c.page_number === currentPage && c.row_index === rowIdx && c.column_index === colIdx) {
              return { ...c, current_value: newValue, confidence: 1.0, is_inferred: false };
            }
            return c;
          });
        }
        return [
          ...prev,
          {
            id: `new-${rowIdx}-${colIdx}-${Date.now()}`,
            document_id: id || '',
            page_number: currentPage,
            row_index: rowIdx,
            column_index: colIdx,
            original_value: '',
            current_value: newValue,
            confidence: 1.0,
            is_inferred: false,
            bbox,
            version: 1,
          },
        ];
      });
    } catch (err) {
      console.error('Failed to save cell correction:', err);
    } finally {
      setIsSaving(false);
    }
  };

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
            The OCR engine is currently segmenting cells and extracting tabular marksheet data. This will take a moment.
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
            onClick={() => setCurrentPage(prev => Math.max(prev - 1, 1))}
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
            onClick={() => setCurrentPage(prev => Math.min(prev + 1, totalPages))}
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
            <span className="flex items-center gap-1.5 px-3 py-1.5 bg-emerald-950 border border-emerald-900 rounded-lg text-xs font-semibold text-emerald-400">
              <CheckCircle className="w-3.5 h-3.5" /> Submitted
            </span>
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
                disabled={vBusy || !((vState?.total_pages || 0) > 0 && verifiedPageSet.size >= (vState?.total_pages || totalPages))}
                title={verifiedPageSet.size < (vState?.total_pages || totalPages) ? 'Mark every page verified before submitting' : 'Submit verified document (Alt+Enter)'}
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
      {!isSubmitted && !lockedByMe && (
        <div className="bg-slate-900/80 border-b border-slate-800 px-6 py-2 text-xs text-slate-400 shrink-0">
          {vState?.locked_by
            ? `This file is being verified by ${vState.locked_by_name || 'another user'}. You are viewing it read-only.`
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
        {/* Left Panel: Image Canvas */}
        <div className="w-1/2 h-full">
          <CanvasViewer
            imageUrl={imageUrl}
            cells={canvasCells}
            selectedCell={selectedCell}
            onCellSelect={handleCellSelectFromCanvas}
          />
        </div>

        {/* Right Panel: Spreadsheet Editor */}
        <div className="w-1/2 h-full flex flex-col bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-2xl">
          {/* Metadata Form Section */}
          <div className="p-4 border-b border-slate-800 bg-slate-950/40">
            <h3 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3">Marksheet Header Information</h3>
            <div className="grid grid-cols-2 gap-3 mb-3">
              <div>
                <label className="block text-[10px] font-semibold text-slate-500 mb-1">College Code</label>
                <input
                  type="text"
                  value={pageMetadata.college_code}
                  onChange={(e) => setPageMetadata(prev => ({ ...prev, college_code: e.target.value }))}
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
                  className="w-full bg-slate-900 border border-slate-800 rounded px-2.5 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition"
                  placeholder="e.g. 15"
                />
              </div>
            </div>
            <div className="flex justify-end">
              <button
                onClick={handleSaveMetadata}
                disabled={isSavingMetadata || !editable}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:bg-blue-800 disabled:opacity-50 text-white text-xs font-semibold rounded transition cursor-pointer disabled:cursor-not-allowed"
              >
                {isSavingMetadata ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" /> Saving...
                  </>
                ) : (
                  'Save Header Info'
                )}
              </button>
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
              title={editable ? 'Toggle this page as verified (Alt+V)' : 'Claim the file to mark pages verified'}
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
            <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>←</Kbd>/<Kbd>→</Kbd> change page</span>
            {editable && (
              <>
                <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>V</Kbd> mark page verified</span>
                <span className="flex items-center gap-1"><Kbd>Alt</Kbd>+<Kbd>Enter</Kbd> submit</span>
              </>
            )}
          </div>

          <div className="flex-1 w-full ag-theme-quartz-dark">
            <AgGridReact
              rowData={gridData}
              columnDefs={columnDefs}
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
