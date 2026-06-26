import React, { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { RefreshCw, FileUp, Search, Loader2, Trash2, Play } from 'lucide-react';
import { apiClient } from '@/shared/api/client';
import { useAuthStore } from '@/entities/user/model/store';
import { PageHeader, Card, StatCard, Button, Input } from '@/shared/ui/primitives';

interface DocumentRecord {
  id: string;
  name: string;
  file_size: number;
  mime_type: string;
  status: string; // 'uploaded', 'queued', 'processing', 'extracted', 'failed', 'verified'
  progress_percentage: number;
  error_message?: string;
  created_at: string;
}

export const DashboardPage: React.FC = () => {
  const navigate = useNavigate();
  const { hasPermission } = useAuthStore();
  const canUpload = hasPermission('documents.upload');
  const canDelete = hasPermission('documents.delete');

  const [documents, setDocuments] = useState<DocumentRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isUploading, setIsUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [dragActive, setDragActive] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const fetchDocuments = async () => {
    try {
      setError(null);
      const data = await apiClient<DocumentRecord[]>('/api/documents?limit=100&offset=0');
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
    const interval = setInterval(fetchDocuments, 10000);
    return () => clearInterval(interval);
  }, []);

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
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      });
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || 'Failed to upload document');
      }
      setUploadProgress('Uploaded successfully — enqueued for OCR processing.');
      setTimeout(() => {
        setUploadProgress(null);
        setIsUploading(false);
        fetchDocuments();
      }, 1500);
    } catch (err: any) {
      setError(err.message || 'Upload failed');
      setIsUploading(false);
      setUploadProgress(null);
    }
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files && files.length > 0) uploadFile(files[0]);
  };

  const handleDrag = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === 'dragenter' || e.type === 'dragover') setDragActive(true);
    else if (e.type === 'dragleave') setDragActive(false);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);
    if (e.dataTransfer.files && e.dataTransfer.files[0]) uploadFile(e.dataTransfer.files[0]);
  };

  const handleDeleteDocument = async (id: string) => {
    if (!confirm('Delete this document and all its extracted data?')) return;
    try {
      await apiClient(`/api/documents/${id}`, { method: 'DELETE' });
      fetchDocuments();
    } catch (err: any) {
      alert(err.message || 'Failed to delete document');
    }
  };

  const handleDeleteAll = async () => {
    if (!confirm('WARNING: permanently clear the entire document registry (all files and extractions)?')) return;
    try {
      await apiClient('/api/documents', { method: 'DELETE' });
      fetchDocuments();
    } catch (err: any) {
      alert(err.message || 'Failed to clear registry');
    }
  };

  const stats = {
    total: documents.length,
    pending: documents.filter((d) => d.status === 'extracted').length,
    processing: documents.filter((d) => ['queued', 'processing', 'uploaded'].includes(d.status)).length,
    verified: documents.filter((d) => d.status === 'verified').length,
    failed: documents.filter((d) => d.status === 'failed').length,
  };

  const filteredDocs = documents.filter((doc) => doc.name.toLowerCase().includes(searchQuery.toLowerCase()));

  const statusBadge = (doc: DocumentRecord) => {
    const map: Record<string, string> = {
      uploaded: 'bg-slate-800 text-slate-300',
      queued: 'bg-slate-800 text-slate-300',
      processing: 'bg-blue-950 text-blue-400 animate-pulse',
      extracted: 'bg-amber-950 text-amber-400',
      verified: 'bg-emerald-950 text-emerald-400',
      failed: 'bg-red-950 text-red-400',
    };
    const label =
      doc.status === 'extracted'
        ? 'Pending Review'
        : doc.status === 'verified'
          ? 'Verified'
          : doc.status === 'processing'
            ? `Processing ${doc.progress_percentage}%`
            : doc.status === 'failed'
              ? 'Failed'
              : `Queued ${doc.progress_percentage}%`;
    return (
      <span
        title={doc.status === 'failed' ? doc.error_message : undefined}
        className={`rounded-md px-2 py-1 text-[10px] font-semibold uppercase tracking-wider ${map[doc.status] ?? 'bg-slate-700 text-slate-300'}`}
      >
        {label}
      </span>
    );
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  return (
    <>
      <PageHeader
        title="Documents"
        subtitle="Upload scans and track OCR processing status"
        actions={
          <Button variant="ghost" onClick={fetchDocuments}>
            <RefreshCw size={15} /> Refresh
          </Button>
        }
      />

      <div className="space-y-6 p-6">
        <section className="grid grid-cols-2 gap-4 md:grid-cols-5">
          <StatCard label="Total" value={stats.total} />
          <StatCard label="Pending Review" value={stats.pending} accent="text-amber-400" />
          <StatCard label="OCR Processing" value={stats.processing} accent="text-blue-400" />
          <StatCard label="Verified" value={stats.verified} accent="text-emerald-400" />
          <StatCard label="Failed" value={stats.failed} accent="text-red-400" />
        </section>

        {canUpload && (
          <Card className="p-5">
            <h3 className="mb-1 text-sm font-semibold text-white">Process New Scan</h3>
            <p className="mb-3 text-xs text-slate-400">Marksheets, examiner panels, result registers or attendance lists.</p>
            <input type="file" ref={fileInputRef} onChange={handleFileUpload} className="hidden" accept=".pdf,.png,.jpg,.jpeg" />
            <button
              disabled={isUploading}
              onClick={() => fileInputRef.current?.click()}
              onDragEnter={handleDrag}
              onDragOver={handleDrag}
              onDragLeave={handleDrag}
              onDrop={handleDrop}
              className={`flex w-full flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed py-8 transition ${
                dragActive ? 'border-blue-500 bg-slate-950/80 text-blue-400' : 'border-slate-700 bg-slate-950/40 text-slate-400 hover:border-blue-500/50'
              }`}
            >
              {isUploading ? <Loader2 className="h-7 w-7 animate-spin text-blue-500" /> : <FileUp className="h-7 w-7" />}
              <span className="text-xs font-semibold">
                {isUploading ? 'Uploading…' : dragActive ? 'Drop file here' : 'Drag & drop or click to browse'}
              </span>
              <span className="text-[10px] text-slate-600">PDF, PNG, JPG up to 500MB</span>
            </button>
            {uploadProgress && <div className="mt-3 rounded-lg bg-blue-950/40 px-3 py-2 text-xs text-blue-300">{uploadProgress}</div>}
            {error && <div className="mt-3 rounded-lg bg-red-950/40 px-3 py-2 text-xs text-red-300">{error}</div>}
          </Card>
        )}

        <Card className="p-5">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-white">Document Registry</h3>
            <div className="flex items-center gap-2">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500" />
                <Input
                  placeholder="Search files…"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="w-48 pl-8"
                />
              </div>
              {canDelete && (
                <Button variant="danger" onClick={handleDeleteAll} disabled={documents.length === 0}>
                  <Trash2 size={14} /> Clear
                </Button>
              )}
            </div>
          </div>

          <div className="overflow-hidden rounded-lg border border-slate-800">
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/80 text-[10px] uppercase tracking-wider text-slate-400">
                <tr>
                  <th className="px-4 py-2.5">Document</th>
                  <th className="px-4 py-2.5">Size</th>
                  <th className="px-4 py-2.5">Status</th>
                  <th className="px-4 py-2.5 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr>
                    <td colSpan={4} className="py-10 text-center text-slate-500">
                      <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin text-blue-500" /> Loading…
                    </td>
                  </tr>
                ) : filteredDocs.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="py-10 text-center text-slate-500">
                      No documents found.
                    </td>
                  </tr>
                ) : (
                  filteredDocs.map((doc) => (
                    <tr key={doc.id} className="border-t border-slate-800 hover:bg-slate-900/40">
                      <td className="px-4 py-3">
                        <p className="max-w-[280px] truncate font-medium text-slate-200" title={doc.name}>
                          {doc.name}
                        </p>
                        <p className="mt-0.5 text-[10px] text-slate-500">{new Date(doc.created_at).toLocaleString()}</p>
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-slate-400">{formatBytes(doc.file_size)}</td>
                      <td className="px-4 py-3">{statusBadge(doc)}</td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-2">
                          {['extracted', 'verified'].includes(doc.status) && (
                            <Button onClick={() => navigate(`/documents/${doc.id}`)}>
                              <Play size={12} className="fill-current" /> Open
                            </Button>
                          )}
                          {canDelete && (
                            <Button variant="ghost" onClick={() => handleDeleteDocument(doc.id)} title="Delete">
                              <Trash2 size={14} />
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <div className="mt-3 text-[10px] text-slate-500">
            Showing {filteredDocs.length} of {documents.length} entries
          </div>
        </Card>
      </div>
    </>
  );
};
