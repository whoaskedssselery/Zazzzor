// The first-run screen: asks for the instance's address and hands it to the
// app, which checks it, saves it and opens it in this window. Opened again
// from the tray ("Сменить сервер…") it shows the current address and can go
// back without changing it.
"use strict";

const { invoke } = window.__TAURI_INTERNALS__;
const form = document.getElementById("form");
const input = document.getElementById("url");
const error = document.getElementById("error");
const submit = document.getElementById("submit");
const cancel = document.getElementById("cancel");

const showError = (text) => {
  error.textContent = text;
  input.setAttribute("aria-invalid", text ? "true" : "false");
};

invoke("current_instance").then((current) => {
  if (current) {
    input.value = current;
    cancel.hidden = false;
  }
  input.focus();
  input.select();
});

input.addEventListener("input", () => showError(""));

cancel.addEventListener("click", () => {
  invoke("open_instance").catch((e) => showError(String(e)));
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  submit.disabled = true;
  try {
    // On success the window leaves this page for the instance.
    await invoke("set_instance", { url: input.value });
  } catch (e) {
    showError(String(e));
    submit.disabled = false;
    input.focus();
  }
});
