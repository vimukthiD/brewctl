import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { AssistantProvider } from "./assistant";
import { BrewProvider } from "./state";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <BrewProvider>
      <AssistantProvider>
        <App />
      </AssistantProvider>
    </BrewProvider>
  </React.StrictMode>,
);
