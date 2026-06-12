import { useState } from 'react';

interface CopyInstallProps {
  snippet: string;
  label?: string;
}

/**
 * Click-to-copy button rendered next to the install snippet. Future iteration
 * of the landing page; not yet mounted on the static index.html.
 */
export function CopyInstall({ snippet, label = 'Copy' }: CopyInstallProps) {
  const [copied, setCopied] = useState(false);

  async function onClick() {
    try {
      await navigator.clipboard.writeText(snippet);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button
      type="button"
      data-testid="copy-install"
      onClick={onClick}
      aria-label="Copy install snippet to clipboard"
    >
      {copied ? 'Copied ✓' : label}
    </button>
  );
}
