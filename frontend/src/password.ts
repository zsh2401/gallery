const TOKENS_KEY = 'seymours-gallery-pw-tokens';

type TokenMap = Record<string, string>; // albumId -> token

function loadTokens(): TokenMap {
  try {
    const raw = sessionStorage.getItem(TOKENS_KEY);
    return raw ? (JSON.parse(raw) as TokenMap) : {};
  } catch {
    return {};
  }
}

function saveTokens(tokens: TokenMap): void {
  sessionStorage.setItem(TOKENS_KEY, JSON.stringify(tokens));
}

export function getAlbumToken(albumId: string): string | null {
  return loadTokens()[albumId] ?? null;
}

export function setAlbumToken(albumId: string, token: string): void {
  const tokens = loadTokens();
  tokens[albumId] = token;
  saveTokens(tokens);
}

export function hasAlbumAccess(albumId: string): boolean {
  return getAlbumToken(albumId) !== null;
}
