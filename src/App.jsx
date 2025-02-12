import React, { useEffect, useRef, useState } from "react";
import CanvasPanel from "./CanvasPanel";

const NUM_PANELS = 1920;
const CANVAS_SIZE = 128;

function App() {
  const [ws, setWs] = useState(null);
  // We'll store a mapping of panel number to its canvas element.
  const canvasesRef = useRef([]);
  // Assign a random color to the user (remains constant).
  const userColor = useRef({
    r: Math.floor(Math.random() * 256),
    g: Math.floor(Math.random() * 256),
    b: Math.floor(Math.random() * 256),
  });

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
    };
    websocket.onerror = (err) => {
      console.error("WebSocket error:", err);
    };
    websocket.onmessage = (e) => {
      const buffer = e.data;
      if (!(buffer instanceof ArrayBuffer)) return;
      const view = new DataView(buffer);
      const msgType = view.getUint8(0);

      // Handle broadcast update messages (15 bytes).
      if (msgType === 4 && buffer.byteLength === 15) {
        const panel = view.getUint8(1);
        const x = view.getUint8(2);
        const y = view.getUint8(3);
        const r = view.getUint8(4);
        const g = view.getUint8(5);
        const b = view.getUint8(6);
        const canvas = canvasesRef.current[panel];
        if (canvas) {
          const ctx = canvas.getContext("2d");
          ctx.fillStyle = `rgb(${r}, ${g}, ${b})`;
          ctx.fillRect(x, y, 1, 1);
        }
      }
      // Handle full panel sync messages (2-byte header + 128×128×3 bytes).
      else if (
        msgType === 5 &&
        buffer.byteLength === 2 + CANVAS_SIZE * CANVAS_SIZE * 3
      ) {
        const panel = view.getUint8(1);
        const canvas = canvasesRef.current[panel];
        if (canvas) {
          const ctx = canvas.getContext("2d");
          const imageData = ctx.createImageData(CANVAS_SIZE, CANVAS_SIZE);
          let srcIdx = 2;
          for (let i = 0; i < imageData.data.length; i += 4) {
            imageData.data[i] = view.getUint8(srcIdx);
            imageData.data[i + 1] = view.getUint8(srcIdx + 1);
            imageData.data[i + 2] = view.getUint8(srcIdx + 2);
            imageData.data[i + 3] = 255;
            srcIdx += 3;
          }
          ctx.putImageData(imageData, 0, 0);
        }
      }
    };
    setWs(websocket);
    return () => {
      if (websocket.readyState === WebSocket.OPEN) {
        websocket.close();
      }
    };
  }, []);

  return (
    <div className="min-h-screen flex flex-col items-center bg-black">
      <div
        className="border-2 w-12 h-4 border-white m-4 rounded-lg"
        style={{
          backgroundColor: `rgb(${userColor.current.r}, ${userColor.current.g}, ${userColor.current.b})`,
        }}
      ></div>
      {/* Adjust grid columns as needed; here we use 10 columns */}
      <div className="grid grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10 gap-0">
        {Array.from({ length: NUM_PANELS }).map((_, i) => (
          <CanvasPanel
            key={i}
            panel={i}
            ws={ws}
            userColor={userColor.current}
            onMount={handleCanvasMount}
          />
        ))}
      </div>
    </div>
  );
}

export default App;
