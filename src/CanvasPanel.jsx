import React, { useEffect, useRef } from "react";

function CanvasPanel({ panel, ws, userColor, onMount }) {
  const canvasRef = useRef(null);

  // Initialize the canvas (fill with black)
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "black";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    if (onMount) onMount(panel, canvas);
  }, [panel, onMount]);

  // Use IntersectionObserver to request a full sync when the panel comes into view.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            console.log("entries", panel);
            // Build and send a 3-byte request message:
            //   Byte 0: MsgTypeRequest (2)
            //   Bytes 1-2: panel number (uint16, BigEndian)
            if (ws && ws.readyState === WebSocket.OPEN) {
              const buffer = new ArrayBuffer(3);
              const view = new DataView(buffer);
              view.setUint8(0, 2); // MsgTypeRequest
              view.setUint16(1, panel);
              ws.send(buffer);
            }
          }
        });
      },
      { threshold: 0.1 }
    );
    observer.observe(canvas);
    // Force check immediately for any pending intersection records.
    const initialRecords = observer.takeRecords();
    initialRecords.forEach((entry) => {
      if (entry.isIntersecting) {
        console.log("initial", panel);

        if (ws && ws.readyState === WebSocket.OPEN) {
          const buffer = new ArrayBuffer(3);
          const view = new DataView(buffer);
          view.setUint8(0, 2); // MsgTypeRequest
          view.setUint16(1, panel);
          ws.send(buffer);
        }
      }
    });
    return () => observer.disconnect();
  }, [ws, panel]);

  // Handle drawing: update immediately and send an update message on every mouse move.
  const handleMouseMove = (e) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;
    const x = Math.floor((e.clientX - rect.left) * scaleX);
    const y = Math.floor((e.clientY - rect.top) * scaleY);

    // Draw immediately on the canvas using the assigned color.
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = `rgb(${userColor.r}, ${userColor.g}, ${userColor.b})`;
    ctx.fillRect(x, y, 1, 1);

    // Prepare a 5-byte update message:
    //   Byte 0: MsgTypeUpdate (1)
    //   Bytes 1-2: panel number (uint16, BigEndian)
    //   Byte 3: x-coordinate
    //   Byte 4: y-coordinate
    const buffer = new ArrayBuffer(5);
    const view = new DataView(buffer);
    view.setUint8(0, 1); // MsgTypeUpdate
    view.setUint16(1, panel);
    view.setUint8(3, x);
    view.setUint8(4, y);
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(buffer);
    }
  };

  return (
    <canvas
      ref={canvasRef}
      width={128}
      height={128}
      data-panel={panel}
      onMouseMove={handleMouseMove}
      className="w-full h-full image-rendering-pixelated"
    />
  );
}

export default CanvasPanel;
