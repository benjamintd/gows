import React, { useEffect } from "react";

const TurnstileWidget = ({ onVerify }) => {
  useEffect(() => {
    // Create and insert the Cloudflare Turnstile script if not already present.
    if (!document.getElementById("cf-turnstile-script")) {
      const script = document.createElement("script");
      script.id = "cf-turnstile-script";
      script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js";
      script.async = true;
      script.defer = true;
      document.body.appendChild(script);
    }

    // Define the global callback that Turnstile will call when the user completes the challenge.
    (window as any).onTurnstileSuccess = (token) => {
      onVerify(token);
    };

    // Cleanup the global callback on unmount.
    return () => {
      delete (window as any).onTurnstileSuccess;
    };
  }, [onVerify]);

  return (
    <div
      className="cf-turnstile"
      data-sitekey={"0x4AAAAAAA9lA7CUtHlQGAaW"}
      data-callback="onTurnstileSuccess"
    ></div>
  );
};

export default TurnstileWidget;
