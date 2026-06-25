import React, { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { 
  FileText, RefreshCw, LogOut, CheckCircle, 
  Clock, AlertCircle, Play, FileUp, Database, Search, Loader2, Trash2
} from 'lucide-react';
import { apiClient } from '@/shared/api/client';

interface DocumentRecord {
  id: string;
  name: string;
  file_size: number;
  mime_type: string;
  status: string; // 'uploaded', 'queued', 'processing', 'extracted', 'failed', 'verified'
  progress_percentage: number;
  error_message?: string; // reason the OCR job failed (only set when status === 'failed')
  created_at: string;
}

export const DashboardPage: React.FC = () => {
  const navigate = useNavigate();
  const [documents, setDocuments] = useState<DocumentRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isUploading, setIsUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [dragActive, setDragActive] = useState(false);
  
  const fileInputRef = useRef<HTMLInputElement>(null);
  const user = JSON.parse(localStorage.getItem('auth_user') || '{}');

  const fetchDocuments = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient<DocumentRecord[]>('/api/documents?limit=50&offset=0');
      // Sort by created_at descending
      const sorted = Array.isArray(data) 
        ? data.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
        : [];
      setDocuments(sorted);
    } catch (err: any) {
      setError(err.message || 'Failed to fetch documents');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDocuments();
    // Poll documents list every 10 seconds to show dynamic status transitions (e.g. from queued to extracted)
    const interval = setInterval(fetchDocuments, 10000);
    return () => clearInterval(interval);
  }, []);

  const handleLogout = () => {
    localStorage.removeItem('auth_token');
    localStorage.removeItem('auth_user');
    navigate('/login');
  };

  const uploadFile = async (file: File) => {
    const formData = new FormData();
    formData.append('file', file);
    
    setIsUploading(true);
    setUploadProgress('Uploading file to server...');
    setError(null);

    try {
      const token = localStorage.getItem('auth_token');
      const res = await fetch('/api/documents', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`
        },
        body: formData
      });

      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || 'Failed to upload document');
      }

      setUploadProgress('File uploaded successfully. Enqueued for OCR processing!');
      setTimeout(() => {
        setUploadProgress(null);
        setIsUploading(false);
        fetchDocuments();
      }, 2000);
    } catch (err: any) {
      setError(err.message || 'Upload failed');
      setIsUploading(false);
      setUploadProgress(null);
    }
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    uploadFile(files[0]);
  };

  const handleDrag = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === "dragenter" || e.type === "dragover") {
      setDragActive(true);
    } else if (e.type === "dragleave") {
      setDragActive(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);

    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
      uploadFile(e.dataTransfer.files[0]);
    }
  };

  const handleDeleteDocument = async (id: string) => {
    if (!confirm('Are you sure you want to delete this document scan log and all its extracted tabular data?')) {
      return;
    }
    
    try {
      await apiClient(`/api/documents/${id}`, {
        method: 'DELETE'
      });
      fetchDocuments();
    } catch (err: any) {
      alert(err.message || 'Failed to delete document');
    }
  };

  const handleDeleteAll = async () => {
    if (!confirm('WARNING: Are you sure you want to permanently clear the entire document registry? This will delete all files and extractions!')) {
      return;
    }
    
    try {
      await apiClient('/api/documents', {
        method: 'DELETE'
      });
      fetchDocuments();
    } catch (err: any) {
      alert(err.message || 'Failed to clear registry');
    }
  };

  // Stats calculation
  const stats = {
    total: documents.length,
    pending: documents.filter(d => d.status === 'extracted').length,
    processing: documents.filter(d => ['queued', 'processing', 'uploaded'].includes(d.status)).length,
    verified: documents.filter(d => d.status === 'verified').length,
    failed: documents.filter(d => d.status === 'failed').length,
  };

  // Filtered list
  const filteredDocs = documents.filter(doc => 
    doc.name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const getStatusBadge = (doc: DocumentRecord) => {
    switch (doc.status) {
      case 'uploaded':
      case 'queued':
        return <span className="px-2 py-1 bg-slate-800 border border-slate-700 text-slate-300 text-[10px] font-semibold uppercase tracking-wider rounded-md">Queued ({doc.progress_percentage}%)</span>;
      case 'processing':
        return <span className="px-2 py-1 bg-blue-950 border border-blue-900 text-blue-400 text-[10px] font-semibold uppercase tracking-wider rounded-md animate-pulse">Processing ({doc.progress_percentage}%)</span>;
      case 'extracted':
        return <span className="px-2 py-1 bg-amber-950 border border-amber-900 text-amber-400 text-[10px] font-semibold uppercase tracking-wider rounded-md">Pending Review</span>;
      case 'verified':
        return <span className="px-2 py-1 bg-emerald-950 border border-emerald-900 text-emerald-400 text-[10px] font-semibold uppercase tracking-wider rounded-md">Verified</span>;
      case 'failed':
        return <span title={doc.error_message || 'Processing failed'} className="px-2 py-1 bg-red-950 border border-red-900 text-red-400 text-[10px] font-semibold uppercase tracking-wider rounded-md cursor-help">Failed</span>;
      default:
        return <span className="px-2 py-1 bg-slate-700 text-slate-300 text-[10px] font-semibold rounded-md">{doc.status}</span>;
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 font-sans flex flex-col">
      {/* Top Navigation Header */}
      <header className="px-6 py-4 bg-slate-900 border-b border-slate-800/80 flex items-center justify-between shrink-0">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-blue-500/10 border border-blue-500/20 rounded-lg text-blue-400">
            <FileText className="w-5 h-5" />
          </div>
          <div>
            <h1 className="text-sm font-bold tracking-tight text-white">Papyrus Portal</h1>
            <p className="text-[10px] text-slate-500 font-medium uppercase tracking-wider">State University Console</p>
          </div>
        </div>

        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2.5 px-3.5 py-1.5 bg-slate-950/60 border border-slate-800 rounded-xl">
            <div className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            <div className="text-right">
              <p className="text-xs font-semibold text-slate-300">{user.first_name || 'Alice'} {user.last_name || 'Smith'}</p>
              <p className="text-[9px] text-slate-500 font-medium">System Admin</p>
            </div>
          </div>
          
          <button
            onClick={handleLogout}
            className="p-2 bg-slate-800 hover:bg-red-950/40 border border-slate-700 hover:border-red-900/60 rounded-xl text-slate-400 hover:text-red-400 transition shadow-sm"
            title="Log Out"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </div>
      </header>

      {/* Main Container */}
      <main className="flex-1 p-6 max-w-7xl w-full mx-auto space-y-6 overflow-y-auto">
        
        {/* KPI Dashboard Stats Cards */}
        <section className="grid grid-cols-1 md:grid-cols-5 gap-4">
          <div className="p-4 bg-slate-900 border border-slate-800 rounded-2xl flex items-center justify-between shadow-xl">
            <div>
              <p className="text-[10px] font-semibold text-slate-400 uppercase tracking-wider">Total Documents</p>
              <h3 className="text-2xl font-bold text-white mt-1">{stats.total}</h3>
            </div>
            <div className="p-3 bg-slate-800/80 rounded-xl text-slate-400">
              <Database className="w-5 h-5" />
            </div>
          </div>

          <div className="p-4 bg-slate-900 border border-slate-800 rounded-2xl flex items-center justify-between shadow-xl">
            <div>
              <p className="text-[10px] font-semibold text-amber-400 uppercase tracking-wider">Pending Review</p>
              <h3 className="text-2xl font-bold text-amber-400 mt-1">{stats.pending}</h3>
            </div>
            <div className="p-3 bg-amber-500/10 rounded-xl text-amber-400 border border-amber-500/10">
              <AlertCircle className="w-5 h-5" />
            </div>
          </div>

          <div className="p-4 bg-slate-900 border border-slate-800 rounded-2xl flex items-center justify-between shadow-xl">
            <div>
              <p className="text-[10px] font-semibold text-blue-400 uppercase tracking-wider">OCR Processing</p>
              <h3 className="text-2xl font-bold text-blue-400 mt-1">{stats.processing}</h3>
            </div>
            <div className="p-3 bg-blue-500/10 rounded-xl text-blue-400 border border-blue-500/10">
              <Clock className="w-5 h-5 animate-spin-slow" />
            </div>
          </div>

          <div className="p-4 bg-slate-900 border border-slate-800 rounded-2xl flex items-center justify-between shadow-xl">
            <div>
              <p className="text-[10px] font-semibold text-emerald-400 uppercase tracking-wider">Verified Records</p>
              <h3 className="text-2xl font-bold text-emerald-400 mt-1">{stats.verified}</h3>
            </div>
            <div className="p-3 bg-emerald-500/10 rounded-xl text-emerald-400 border border-emerald-500/10">
              <CheckCircle className="w-5 h-5" />
            </div>
          </div>

          <div className="p-4 bg-slate-900 border border-slate-800 rounded-2xl flex items-center justify-between shadow-xl">
            <div>
              <p className="text-[10px] font-semibold text-red-400 uppercase tracking-wider">Failed Executions</p>
              <h3 className="text-2xl font-bold text-red-400 mt-1">{stats.failed}</h3>
            </div>
            <div className="p-3 bg-red-500/10 rounded-xl text-red-400 border border-red-500/10">
              <AlertCircle className="w-5 h-5" />
            </div>
          </div>
        </section>

        {/* Action Panel: Upload & Search */}
        <section className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Upload Card */}
          <div className="lg:col-span-1 p-6 bg-slate-900 border border-slate-800 rounded-2xl flex flex-col justify-between shadow-xl relative overflow-hidden">
            <div>
              <h3 className="text-sm font-bold text-white mb-1">Process New Scan</h3>
              <p className="text-xs text-slate-400 mb-4">Upload scanned marksheet, examiner panel, result registers or attendance lists.</p>
              
              <input 
                type="file" 
                ref={fileInputRef} 
                onChange={handleFileUpload} 
                className="hidden" 
                accept=".pdf,.png,.jpg,.jpeg"
              />

              <button 
                disabled={isUploading}
                onClick={() => fileInputRef.current?.click()}
                onDragEnter={handleDrag}
                onDragOver={handleDrag}
                onDragLeave={handleDrag}
                onDrop={handleDrop}
                className={`w-full py-8 border-2 border-dashed rounded-xl flex flex-col items-center justify-center gap-3 transition cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed group ${
                  dragActive 
                    ? 'border-blue-500 bg-slate-950/85 text-blue-400 shadow-[0_0_15px_rgba(59,130,246,0.15)] scale-[1.01]' 
                    : 'border-slate-700 hover:border-blue-500/50 bg-slate-950/40 hover:bg-slate-950/80 text-slate-400 hover:text-blue-400'
                }`}
              >
                {isUploading ? (
                  <Loader2 className="w-8 h-8 text-blue-500 animate-spin" />
                ) : (
                  <FileUp className={`w-8 h-8 transition-colors ${dragActive ? 'text-blue-500' : 'text-slate-500 group-hover:text-blue-500'}`} />
                )}
                <div className="text-center">
                  <p className="text-xs font-semibold">{isUploading ? 'Uploading Document...' : dragActive ? 'Drop File Here' : 'Drag & Drop or Click to Browse'}</p>
                  <p className="text-[10px] text-slate-600 mt-1">PDF, PNG, JPG up to 500MB</p>
                </div>
              </button>
            </div>

            {uploadProgress && (
              <div className="mt-4 p-3.5 bg-blue-950/40 border border-blue-900/40 text-blue-300 text-xs rounded-xl flex items-center gap-2.5 animate-pulse">
                <Clock className="w-4 h-4 text-blue-400 shrink-0" />
                <span>{uploadProgress}</span>
              </div>
            )}

            {error && (
              <div className="mt-4 p-3.5 bg-red-950/40 border border-red-900/40 text-red-300 text-xs rounded-xl flex items-center gap-2.5">
                <AlertCircle className="w-4 h-4 text-red-400 shrink-0" />
                <span>{error}</span>
              </div>
            )}
          </div>

          {/* List/Table View */}
          <div className="lg:col-span-2 p-6 bg-slate-900 border border-slate-800 rounded-2xl flex flex-col justify-between shadow-xl">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-bold text-white">Document Scan Registry</h3>
                <div className="flex items-center gap-3">
                  <div className="relative">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-500" />
                    <input 
                      type="text"
                      placeholder="Search files..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="pl-8.5 pr-3.5 py-1.5 bg-slate-950/60 border border-slate-800 rounded-lg text-xs placeholder-slate-600 text-white outline-none focus:border-blue-500/50 transition w-44 md:w-56"
                    />
                  </div>
                  <button 
                    onClick={fetchDocuments}
                    className="p-1.5 bg-slate-800 hover:bg-slate-700 border border-slate-700 rounded-lg text-slate-400 hover:text-white transition"
                    title="Refresh List"
                  >
                    <RefreshCw className="w-3.5 h-3.5" />
                  </button>
                  <button 
                    onClick={handleDeleteAll}
                    disabled={documents.length === 0}
                    className="flex items-center gap-1 bg-red-950/20 hover:bg-red-950/40 border border-red-900/40 hover:border-red-900/60 rounded-lg px-2.5 py-1.5 text-[11px] font-semibold text-red-400 transition disabled:opacity-50 disabled:cursor-not-allowed"
                    title="Clear All Scan Logs"
                  >
                    <Trash2 className="w-3.5 h-3.5" /> Clear Registry
                  </button>
                </div>
              </div>

              <div className="border border-slate-850 rounded-xl overflow-hidden bg-slate-950/20 max-h-[300px] overflow-y-auto">
                <table className="w-full text-left border-collapse">
                  <thead>
                    <tr className="bg-slate-950/80 border-b border-slate-800 text-[10px] uppercase font-bold text-slate-400 tracking-wider">
                      <th className="px-4 py-2.5">Document Details</th>
                      <th className="px-4 py-2.5">Size</th>
                      <th className="px-4 py-2.5">Status</th>
                      <th className="px-4 py-2.5 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {loading ? (
                      <tr>
                        <td colSpan={4} className="text-center py-12 text-xs text-slate-500">
                          <Loader2 className="w-5 h-5 animate-spin mx-auto mb-2 text-blue-500" />
                          Loading registry files...
                        </td>
                      </tr>
                    ) : filteredDocs.length === 0 ? (
                      <tr>
                        <td colSpan={4} className="text-center py-12 text-xs text-slate-500">
                          No documents uploaded yet.
                        </td>
                      </tr>
                    ) : (
                      filteredDocs.map((doc) => (
                        <tr key={doc.id} className="border-b border-slate-850/60 hover:bg-slate-900/30 transition text-xs">
                          <td className="px-4 py-3">
                            <p className="font-semibold text-slate-200 truncate max-w-[200px] md:max-w-[300px]" title={doc.name}>
                              {doc.name}
                            </p>
                            <p className="text-[10px] text-slate-500 mt-0.5">
                              Uploaded: {new Date(doc.created_at).toLocaleString()}
                            </p>
                          </td>
                          <td className="px-4 py-3 text-slate-400 font-mono text-[11px]">
                            {formatBytes(doc.file_size)}
                          </td>
                          <td className="px-4 py-3">
                            {getStatusBadge(doc)}
                            {doc.status === 'failed' && doc.error_message && (
                              <p
                                className="text-[10px] text-red-400/80 mt-1 max-w-[220px] line-clamp-2 break-words"
                                title={doc.error_message}
                              >
                                {doc.error_message}
                              </p>
                            )}
                          </td>
                          <td className="px-4 py-3 text-right flex items-center justify-end gap-2">
                            {['extracted', 'verified'].includes(doc.status) ? (
                              <button
                                onClick={() => navigate(`/documents/${doc.id}`)}
                                className="px-3 py-1 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-[11px] font-semibold transition inline-flex items-center gap-1.5"
                              >
                                <Play className="w-2.5 h-2.5 fill-current" /> Verify
                              </button>
                            ) : doc.status === 'failed' ? (
                              <span title={doc.error_message || 'Processing failed'} className="text-[11px] text-red-400 font-semibold bg-red-950/20 border border-red-900/30 px-2 py-0.5 rounded-md cursor-help">Failed</span>
                            ) : (
                              <span className="text-[11px] text-slate-400 animate-pulse font-semibold pr-2">Processing ({doc.progress_percentage}%)</span>
                            )}
                            
                            <button
                              onClick={() => handleDeleteDocument(doc.id)}
                              className="p-1.5 bg-slate-800 hover:bg-red-950/40 border border-slate-700 hover:border-red-900/60 rounded-lg text-slate-400 hover:text-red-400 transition"
                              title="Delete Scan Log"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
            
            <div className="pt-4 border-t border-slate-800 text-[10px] text-slate-500 flex justify-between items-center">
              <span>Showing {filteredDocs.length} of {documents.length} entries</span>
              <span>On-Premise Server: Active</span>
            </div>
          </div>
        </section>
      </main>
    </div>
  );
};
