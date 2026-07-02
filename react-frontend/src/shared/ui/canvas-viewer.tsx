import React, { useRef, useEffect, useState } from 'react';
import { ZoomIn, ZoomOut, RotateCw, RefreshCw, Hand } from 'lucide-react';

export interface BBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface CanvasCell {
  row_index: number;
  column_index: number;
  bbox: BBox;
  value: string;
}

interface CanvasViewerProps {
  imageUrl: string;
  cells: CanvasCell[];
  selectedCell: { row: number; col: number } | null;
  onCellSelect?: (row: number, col: number) => void;
}

export const CanvasViewer: React.FC<CanvasViewerProps> = ({
  imageUrl,
  cells,
  selectedCell,
  onCellSelect,
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [image, setImage] = useState<HTMLImageElement | null>(null);
  
  // Transform States
  const [zoom, setZoom] = useState<number>(1.0);
  const [panX, setPanX] = useState<number>(0);
  const [panY, setPanY] = useState<number>(0);
  const [rotation, setRotation] = useState<number>(0); // 0, 90, 180, 270
  
  // Mouse Drag State for Panning
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [dragStart, setDragStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });

  // Zoom that fits the whole page inside the current container size — recomputed
  // (not a fixed guess) so the scan always fits regardless of panel width or
  // rotation, and the verifier never has to pan to see the rest of the page.
  const FIT_MARGIN = 0.98;
  const computeFitZoom = (img: HTMLImageElement, rot: number): number => {
    const container = containerRef.current;
    const cw = container?.clientWidth || 800;
    const ch = container?.clientHeight || 600;
    const rotated = rot % 180 !== 0;
    const iw = rotated ? img.height : img.width;
    const ih = rotated ? img.width : img.height;
    if (!iw || !ih) return 1;
    return Math.min(cw / iw, ch / ih) * FIT_MARGIN;
  };

  // Load Image
  useEffect(() => {
    const img = new Image();
    img.src = imageUrl;
    img.onload = () => {
      setImage(img);
      resetTransforms(img);
    };
  }, [imageUrl]);

  const resetTransforms = (img?: HTMLImageElement | null) => {
    const target = img ?? image;
    setRotation(0);
    setPanX(0);
    setPanY(0);
    if (target) setZoom(computeFitZoom(target, 0));
  };

  // Re-fit whenever the container is resized (e.g. the panel split changes, or
  // the window resizes) so the page keeps fitting without manual zoom/pan.
  useEffect(() => {
    const container = containerRef.current;
    if (!container || !image) return;
    const ro = new ResizeObserver(() => {
      setZoom(computeFitZoom(image, rotation));
      setPanX(0);
      setPanY(0);
    });
    ro.observe(container);
    return () => ro.disconnect();
  }, [image, rotation]);

  // Render Loop
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !image) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Set canvas dimensions
    canvas.width = containerRef.current?.clientWidth || 800;
    canvas.height = containerRef.current?.clientHeight || 600;

    // Clear Canvas
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    ctx.save();
    
    // Apply pan & zoom
    ctx.translate(panX, panY);
    
    // Calculate rotation transformations around the image center
    if (rotation !== 0) {
      const rad = (rotation * Math.PI) / 180;
      ctx.translate(image.width * zoom / 2, image.height * zoom / 2);
      ctx.rotate(rad);
      ctx.translate(-image.width * zoom / 2, -image.height * zoom / 2);
    }
    
    ctx.scale(zoom, zoom);

    // 1. Draw PDF / Scan Image
    ctx.drawImage(image, 0, 0);

    // 2. Draw Bounding Boxes
    cells.forEach((cell) => {
      const { x, y, width, height } = cell.bbox;
      const isSelected = selectedCell && selectedCell.row === cell.row_index && selectedCell.col === cell.column_index;

      if (isSelected) {
        ctx.strokeStyle = 'rgba(239, 68, 68, 0.95)'; // vibrant red for active
        ctx.fillStyle = 'rgba(239, 68, 68, 0.15)';
        ctx.lineWidth = 3 / zoom; // Maintain consistent border relative to screen
      } else {
        ctx.strokeStyle = 'rgba(59, 130, 246, 0.50)'; // translucent blue for non-selected
        ctx.fillStyle = 'rgba(59, 130, 246, 0.03)';
        ctx.lineWidth = 1.5 / zoom;
      }

      ctx.beginPath();
      ctx.rect(x, y, width, height);
      ctx.fill();
      ctx.stroke();

      // Show cell confidence overlay in red if confidence is low (< 85%)
      // In canvas coord space
    });

    ctx.restore();
  }, [image, zoom, panX, panY, rotation, cells, selectedCell]);

  // Click handler to select coordinates
  const handleCanvasClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas || !image) return;
    
    const rect = canvas.getBoundingClientRect();
    const clickX = e.clientX - rect.left;
    const clickY = e.clientY - rect.top;

    // Inverse transform the screen coordinate to get image coordinate
    // (x - panX) / zoom
    let imgX = (clickX - panX) / zoom;
    let imgY = (clickY - panY) / zoom;

    // Apply inverse rotation
    if (rotation === 90) {
      const temp = imgX;
      imgX = imgY;
      imgY = image.height - temp;
    } else if (rotation === 180) {
      imgX = image.width - imgX;
      imgY = image.height - imgY;
    } else if (rotation === 270) {
      const temp = imgY;
      imgY = imgX;
      imgX = image.width - temp;
    }

    // Find clicked cell
    const clicked = cells.find((cell) => {
      const { x, y, width, height } = cell.bbox;
      return imgX >= x && imgX <= x + width && imgY >= y && imgY <= y + height;
    });

    if (clicked && onCellSelect) {
      onCellSelect(clicked.row_index, clicked.column_index);
    }
  };

  const handleMouseDown = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (e.button !== 0) return; // Left click drag
    setIsDragging(true);
    setDragStart({ x: e.clientX - panX, y: e.clientY - panY });
  };

  const handleMouseMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!isDragging) return;
    setPanX(e.clientX - dragStart.x);
    setPanY(e.clientY - dragStart.y);
  };

  const handleMouseUp = () => {
    setIsDragging(false);
  };

  return (
    <div className="flex flex-col h-full bg-slate-950 border border-slate-800 rounded-xl overflow-hidden shadow-2xl relative">
      {/* Top Toolbar */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-900/80 backdrop-blur-md border-b border-slate-800 z-10">
        <div className="flex items-center gap-1">
          <Hand className="w-4 h-4 text-slate-400 mr-2" />
          <span className="text-xs font-semibold text-slate-300">Document Scan Workspace</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setZoom((prev) => Math.max(prev - 0.1, 0.1))}
            className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 transition-all active:scale-95"
            title="Zoom Out"
          >
            <ZoomOut className="w-4 h-4" />
          </button>
          <span className="text-xs text-slate-400 font-mono w-12 text-center">
            {Math.round(zoom * 100)}%
          </span>
          <button
            onClick={() => setZoom((prev) => Math.min(prev + 0.1, 5.0))}
            className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 transition-all active:scale-95"
            title="Zoom In"
          >
            <ZoomIn className="w-4 h-4" />
          </button>
          <div className="w-[1px] h-4 bg-slate-800 mx-1" />
          <button
            onClick={() => setRotation((prev) => (prev + 90) % 360)}
            className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 transition-all active:scale-95"
            title="Rotate Clockwise"
          >
            <RotateCw className="w-4 h-4" />
          </button>
          <button
            onClick={() => image && resetTransforms()}
            className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 transition-all active:scale-95"
            title="Reset Fit"
          >
            <RefreshCw className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Canvas Area */}
      <div ref={containerRef} className="flex-1 w-full h-full relative cursor-grab active:cursor-grabbing">
        <canvas
          ref={canvasRef}
          onClick={handleCanvasClick}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={handleMouseUp}
          className="absolute inset-0 w-full h-full"
        />
        {!image && (
          <div className="absolute inset-0 flex items-center justify-center bg-slate-900/90 z-20">
            <div className="flex flex-col items-center gap-2">
              <div className="w-8 h-8 border-4 border-blue-500 border-t-transparent rounded-full animate-spin" />
              <p className="text-sm text-slate-400">Loading document image...</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
