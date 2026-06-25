import React, { useState, useEffect, useMemo, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, GridApi, GridReadyEvent, CellClickedEvent } from 'ag-grid-community';
import { AlertCircle, Download, CheckCircle, ArrowLeft, Loader2 } from 'lucide-react';

import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';

import { apiClient } from '@/shared/api/client';
import { CanvasViewer, CanvasCell } from '@/shared/ui/canvas-viewer';

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

  // Pivot flat cell list into 2D row structures for AG Grid
  const gridData = useMemo(() => {
    const pageCells = cells.filter(c => c.page_number === currentPage);
    if (pageCells.length === 0) return [];
    
    // Find max row and column
    let maxRow = 0;
    pageCells.forEach(c => {
      if (c.row_index > maxRow) maxRow = c.row_index;
    });

    const rows: any[] = [];
    for (let r = 0; r <= maxRow; r++) {
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
  }, [cells, currentPage]);

  // Dynamically generate column definitions for AG Grid
  const columnDefs = useMemo(() => {
    const pageCells = cells.filter(c => c.page_number === currentPage);
    
    let maxCol = 3; // default to showing 4 columns (0 to 3) even if empty
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

    for (let c = 0; c <= maxCol; c++) {
      let colName = `Column ${c}`;
      if (c === 0) colName = 'Subject Code';
      else if (c === 1) colName = 'Batch';
      else if (c === 2) colName = 'Examiner Name';
      else if (c === 3) colName = 'Mobile Number';

      cols.push({
        headerName: colName,
        field: `col_${c}`,
        editable: true,
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
          if (cellObj.is_inferred) {
            return { backgroundColor: 'rgba(139, 92, 246, 0.14)', borderLeft: '3px solid #8b5cf6' }; // purple = inferred
          }

          const conf = cellObj.confidence;
          if (conf < 0.85) {
            return { backgroundColor: 'rgba(239, 68, 68, 0.15)', borderLeft: '3px solid #ef4444' }; // soft red for low conf
          } else if (conf < 0.95) {
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
            return `Auto-filled from a matching mobile number (inferred) — please verify. Confidence: ${(cellObj.confidence * 100).toFixed(1)}%`;
          }
          return `Confidence: ${(cellObj.confidence * 100).toFixed(1)}%`;
        }
      });
    }
    return cols;
  }, [cells, currentPage]);

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
    let maxCol = 3; // default to 4 columns (0 to 3)
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
    if (!cellObj) return;

    try {
      setIsSaving(true);
      await apiClient(`/api/documents/${id}/cells`, {
        method: 'PUT',
        json: {
          page_number: currentPage,
          row_index: rowIdx,
          column_index: colIdx,
          value: newValue,
          bbox: cellObj.bbox
        }
      });

      // Update local cells state to boost edited cell confidence to 100%
      setCells(prev => prev.map(c => {
        if (c.page_number === currentPage && c.row_index === rowIdx && c.column_index === colIdx) {
          return { ...c, current_value: newValue, confidence: 1.0, is_inferred: false };
        }
        return c;
      }));
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

  // Count low-confidence cells
  const lowConfidenceCount = useMemo(() => {
    return cells.filter(c => c.confidence < 0.85).length;
  }, [cells]);

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
            className="px-2 py-1 bg-slate-800 hover:bg-slate-700 disabled:bg-slate-900 disabled:text-slate-600 text-xs font-semibold rounded transition-all cursor-pointer disabled:cursor-not-allowed"
          >
            Next
          </button>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={() => handleDownload('csv')}
            className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer"
          >
            <Download className="w-3.5 h-3.5" /> Export CSV
          </button>
          <button
            onClick={() => handleDownload('excel')}
            className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 rounded-lg text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer"
          >
            <Download className="w-3.5 h-3.5" /> Export Excel
          </button>
          <button
            onClick={() => navigate('/documents')}
            className="flex items-center gap-2 px-4 py-1.5 bg-emerald-600 hover:bg-emerald-500 rounded-lg text-xs font-semibold text-white transition shadow-lg shadow-emerald-600/20 cursor-pointer"
          >
            <CheckCircle className="w-3.5 h-3.5" /> Submit Batch
          </button>
        </div>
      </header>

      {/* Warnings & Notices */}
      {lowConfidenceCount > 0 && (
        <div className="bg-amber-950/80 border-b border-amber-900/60 px-6 py-2.5 flex items-center gap-2 text-amber-300 text-xs shrink-0">
          <AlertCircle className="w-4 h-4 text-amber-400 shrink-0" />
          <span>
            Warning: <strong>{lowConfidenceCount} cells</strong> contain low confidence extractions (&lt; 85%). Please review highlighted grid entries before submitting.
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
                disabled={isSavingMetadata}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:bg-blue-800 text-white text-xs font-semibold rounded transition cursor-pointer"
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
              <span className="text-xs font-semibold text-slate-300">Tabular Editor (AG Grid CE)</span>
              <button
                onClick={handleAddRow}
                className="px-2.5 py-1 bg-slate-800 hover:bg-slate-700 hover:text-white text-slate-300 text-[10px] font-bold rounded transition border border-slate-700 cursor-pointer"
              >
                + Add Row
              </button>
            </div>
            {isSaving && (
              <span className="text-[10px] text-blue-400 animate-pulse font-semibold">Autosaving changes...</span>
            )}
          </div>
          
          <div className="flex-1 w-full ag-theme-quartz-dark">
            <AgGridReact
              rowData={gridData}
              columnDefs={columnDefs}
              onGridReady={onGridReady}
              onCellClicked={onCellClicked}
              onCellValueChanged={handleCellValueChanged}
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
