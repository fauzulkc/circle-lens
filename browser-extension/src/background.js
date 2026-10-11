/**
 * The background (a service worker in Chrome, an event page in Firefox). It takes screenshots for the
 * content script, and turns the toolbar button and keyboard shortcut into "toggle the lens" messages.
 */

const api = globalThis.browser ?? globalThis.chrome;

api.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type !== 'circle-lens:capture') return undefined;
  const tab = sender.tab;
  // Only the visible tab's own top frame may ask, so a background tab never receives another tab's pixels.
  if (sender.id !== api.runtime.id || !tab?.active || sender.frameId !== 0) {
    sendResponse({ error: 'not-visible' });
    return undefined;
  }
  api.tabs.captureVisibleTab(tab.windowId, { format: 'png' }).then(
    (url) => sendResponse({ url }),
    (error) => sendResponse({ error: String(error?.message ?? error) }),
  );
  return true;
});

const toggle = (tab) => {
  if (tab?.id == null) return;
  api.tabs.sendMessage(tab.id, { type: 'circle-lens:toggle' }).catch(() => {
    // No content script here (a browser page, the extension store, or a tab opened before install).
  });
};

api.action.onClicked.addListener(toggle);
api.commands.onCommand.addListener(async (command, tab) => {
  if (command !== 'toggle-lens') return;
  toggle(tab ?? (await api.tabs.query({ active: true, currentWindow: true }))[0]);
});
