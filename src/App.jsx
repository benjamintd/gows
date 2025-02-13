import pako from "pako";
import React, { useEffect, useRef, useState } from "react";
import CanvasPanel from "./CanvasPanel";

const NUM_PANELS = 840;
const CANVAS_SIZE = 128;

function App() {
  const [ws, setWs] = useState(null);
  const [assignedColor, setAssignedColor] = useState(null);
  // Mapping of panel number to its canvas element.
  const canvasesRef = useRef([]);

  // Callback to capture the canvas element for each panel.
  const handleCanvasMount = (panel, canvas) => {
    canvasesRef.current[panel] = canvas;
  };

  // Establish the WebSocket connection using a relative URL.
  useEffect(() => {
    const loc = window.location;
    const wsProtocol = loc.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${wsProtocol}//${loc.host}/ws`;
    const websocket = new WebSocket(wsUrl);
    websocket.binaryType = "arraybuffer";
    websocket.onopen = () => {
      console.log("Connected to websocket");
      setWs(websocket);
    };
    websocket.onerror = (err) => {
      console.error("WebSocket error:", err);
    };
    websocket.onmessage = (e) => {
      const buffer = e.data;
      if (!(buffer instanceof ArrayBuffer)) return;
      const view = new DataView(buffer);
      const msgType = view.getUint8(0);

      // Handle assign-color message (4 bytes).
      if (msgType === 6 && buffer.byteLength === 4) {
        const r = view.getUint8(1);
        const g = view.getUint8(2);
        const b = view.getUint8(3);
        setAssignedColor({ r, g, b });
        console.log("Assigned color from server:", { r, g, b });
        return;
      }

      // Handle broadcast update messages (16 bytes).
      if (msgType === 4 && buffer.byteLength === 16) {
        const panel = view.getUint16(1);
        const x = view.getUint8(3);
        const y = view.getUint8(4);
        const r = view.getUint8(5);
        const g = view.getUint8(6);
        const b = view.getUint8(7);
        const canvas = canvasesRef.current[panel];
        if (canvas) {
          const ctx = canvas.getContext("2d");
          ctx.fillStyle = `rgb(${r}, ${g}, ${b})`;
          ctx.fillRect(x, y, 1, 1);
        }
      }
      // Handle full panel sync messages (3-byte header + deflated panel).
      else if (msgType === 5) {
        const panel = view.getUint16(1);
        const canvas = canvasesRef.current[panel];
        if (canvas) {
          // Get the compressed data as a Uint8Array.
          // Use the original ArrayBuffer and start at offset 3.
          const compressedData = new Uint8Array(buffer, 3);
          console.log("Compressed data length:", compressedData.length);

          // Decompress using pako (returns a Uint8Array)
          const decompressedData = pako.inflate(compressedData, {
            to: "Uint8Array",
          });

          // Now create the image data. decompressedData should have CANVAS_SIZE*CANVAS_SIZE*3 bytes.
          const ctx = canvas.getContext("2d");
          const imageData = ctx.createImageData(CANVAS_SIZE, CANVAS_SIZE);
          let srcIdx = 0;
          for (let i = 0; i < imageData.data.length; i += 4) {
            imageData.data[i] = decompressedData[srcIdx];
            imageData.data[i + 1] = decompressedData[srcIdx + 1];
            imageData.data[i + 2] = decompressedData[srcIdx + 2];
            imageData.data[i + 3] = 255; // opaque alpha
            srcIdx += 3;
          }
          ctx.putImageData(imageData, 0, 0);
        }
      }
    };

    return () => {
      if (websocket.readyState === WebSocket.OPEN) {
        websocket.close();
      }
    };
  }, []);

  return (
    <div className="min-h-screen flex flex-col items-center bg-black">
      <div className="m-6">
        {assignedColor && (
          <div
            className="fixed top-6 mx-auto w-12 h-6 rounded-lg border border-white"
            style={{
              backgroundColor: `rgb(${assignedColor.r}, ${assignedColor.g}, ${assignedColor.b})`,
            }}
          />
        )}
      </div>
      {/* Grid layout: adjust as needed */}
      <div className="w-full grid grid-cols-3 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8 gap-0">
        {Array.from({ length: NUM_PANELS }).map((_, i) => (
          <CanvasPanel
            key={i}
            panel={i}
            ws={ws}
            // Pass the assigned color once received, or a default if not yet assigned.
            userColor={assignedColor || { r: 255, g: 255, b: 255 }}
            onMount={handleCanvasMount}
          />
        ))}
      </div>
    </div>
  );
}

export default App;
