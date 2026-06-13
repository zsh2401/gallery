const PRIVACY_CONSENT_KEY = 'seymours-gallery-privacy-consent';
const COPYRIGHT_CONSENT_KEY = 'seymours-gallery-copyright-consent';

/**
 * Privacy consent (EU cookie banner).
 * Returns true if the user has accepted the privacy notice.
 */
export function hasPrivacyConsent(): boolean {
  return localStorage.getItem(PRIVACY_CONSENT_KEY) === 'true';
}

export function setPrivacyConsent(): void {
  localStorage.setItem(PRIVACY_CONSENT_KEY, 'true');
}

/**
 * Copyright consent (download original).
 * Returns true if the user has agreed to the copyright terms.
 */
export function hasCopyrightConsent(): boolean {
  return localStorage.getItem(COPYRIGHT_CONSENT_KEY) === 'true';
}

export function setCopyrightConsent(): void {
  localStorage.setItem(COPYRIGHT_CONSENT_KEY, 'true');
}
