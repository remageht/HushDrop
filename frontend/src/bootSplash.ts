// Boot splash watchdog. Lives in a module (not inline in index.html) so the
// production CSP can keep script-src 'self' without 'unsafe-inline'.

declare global {
  interface Window {
    __hushdropBootStart?: number;
    __hushdropBootTimer?: number;
  }
}

window.__hushdropBootStart =
  window.performance && performance.now ? performance.now() : Date.now();

window.__hushdropBootTimer = window.setTimeout(() => {
  const splash = document.getElementById('boot-splash');
  if (splash && !splash.classList.contains('done')) {
    const error = document.getElementById('boot-error');
    const progress = document.getElementById('boot-progress');
    if (error) error.style.display = 'flex';
    if (progress) progress.style.display = 'none';
  }
}, 8000);

document.getElementById('boot-reload-btn')?.addEventListener('click', () => {
  window.location.reload();
});

export {};
