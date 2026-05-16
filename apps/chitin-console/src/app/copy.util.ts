/**
 * Cross-context clipboard write.
 *
 * navigator.clipboard.writeText only works in secure contexts (HTTPS
 * or localhost). When the console is accessed over the Tailscale IP
 * via plain HTTP, that API throws or silently fails. Fall back to
 * the legacy execCommand('copy') against a temporary textarea — works
 * on every modern browser, no secure-context requirement.
 *
 * Resolves true on success, false on failure (caller can flash an
 * error or surface the prompt for manual copy).
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  // Try the modern API first if the context allows it.
  if (typeof navigator !== 'undefined'
      && navigator.clipboard
      && typeof navigator.clipboard.writeText === 'function'
      && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to the legacy path
    }
  }

  // Fallback: temporary off-screen textarea + execCommand.
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.top = '0';
    ta.style.left = '0';
    ta.style.width = '1px';
    ta.style.height = '1px';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    // execCommand is deprecated but still works everywhere modern.
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}
