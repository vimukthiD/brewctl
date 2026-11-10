import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { BrewProvider } from "./state";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <BrewProvider>
      <App />
    </BrewProvider>
  </React.StrictMode>,
);
