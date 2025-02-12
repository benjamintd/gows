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
      (entries, observer) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            // Build and send a 2-byte request message:
            //  Byte 0: MsgTypeRequest (2)
            //  Byte 1: panel number
            if (ws && ws.readyState === WebSocket.OPEN) {
              const buffer = new ArrayBuffer(2);
              const view = new DataView(buffer);
              view.setUint8(0, 2); // MsgTypeRequest
              view.setUint8(1, panel);
              ws.send(buffer);
            }
            // Once requested, we can unobserve this element.
            observer.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.1 }
    );
    observer.observe(canvas);
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

    // Draw immediately on the canvas.
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = `rgb(${userColor.r}, ${userColor.g}, ${userColor.b})`;
    ctx.fillRect(x, y, 1, 1);

    // Prepare a 7-byte update message:
    //  Byte 0: MsgTypeUpdate (1)
    //  Byte 1: panel number
    //  Byte 2: x-coordinate
    //  Byte 3: y-coordinate
    //  Byte 4: red
    //  Byte 5: green
    //  Byte 6: blue
    const buffer = new ArrayBuffer(7);
    const view = new DataView(buffer);
    view.setUint8(0, 1); // MsgTypeUpdate
    view.setUint8(1, panel);
    view.setUint8(2, x);
    view.setUint8(3, y);
    view.setUint8(4, userColor.r);
    view.setUint8(5, userColor.g);
    view.setUint8(6, userColor.b);
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(buffer);
    }
  };

  return (
    <canvas
      ref={canvasRef}
      data-panel={panel}
      onMouseMove={handleMouseMove}
      className="image-rendering-pixelated"
    />
  );
}

export default CanvasPanel;
