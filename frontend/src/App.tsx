import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  Aperture,
  ArrowLeft,
  Camera,
  Cookie,
  Download,
  Eye,
  Folder,
  FolderOpen,
  Grid3X3,
  Home,
  Languages,
  Lock,
  MapPin,
  Menu,
  Moon,
  Shield,
  Star,
  Sun,
  ThumbsDown,
  ThumbsUp,
  X
} from 'lucide-react';
import { mediaURL, postAPI, setApiDeviceId } from './api';
import { hasCopyrightConsent, hasPrivacyConsent, setCopyrightConsent, setPrivacyConsent } from './consent';
import { getDeviceId } from './deviceId';
import { hasAlbumAccess, setAlbumToken } from './password';
import { getUserReaction, loadReactions, toggleReaction } from './reactions';
import type { Album, GalleryImage, ImageDetail, ItemStats, Reaction, StatsTargetType } from './types';

type ImagesResponse = {
  images: GalleryImage[];
  nextCursor: string;
  total: number;
};

type RouteState = {
  albumPath: string | null;
  fileName: string | null;
};

type Language = 'en' | 'zh';
type ThemePreference = 'auto' | 'light' | 'dark';

const LANGUAGE_KEY = 'seymours-gallery-language';
const THEME_KEY = 'seymours-gallery-theme';
const COPYRIGHT_COUNTDOWN_SECONDS = 5;

const COPY = {
  en: {
    appTitle: "Seym's",
    gallery: 'Gallery',
    noCollections: 'No collections found.',
    noContent: 'No displayable collections or photos here.',
    collections: 'Collections',
    photos: 'photos',
    folders: 'folders',
    direct: 'direct',
    total: 'total',
    views: 'views',
    like: 'Like',
    dislike: 'Dislike',
    openMenu: 'Open menu',
    closeMenu: 'Close menu',
    backToGrid: 'Back to grid',
    loading: 'Loading...',
    raw: 'RAW',
    files: 'Files',
    exif: 'EXIF',
    noExif: 'No EXIF fields found.',
    settings: 'Settings',
    language: 'Language',
    theme: 'Theme',
    english: 'English',
    chinese: '中文',
    auto: 'Auto',
    light: 'Light',
    dark: 'Dark',
    camera: 'Camera',
    lens: 'Lens',
    exposure: 'Exposure',
    aperture: 'Aperture',
    iso: 'ISO',
    focal: 'Focal',
    shutterCount: 'Shutter count',
    rating: 'Rating',
    original: 'Original',
    downloadOriginal: 'Download original',
    copyrightShort: 'No commercial use without permission.',
    copyrightTitle: 'Copyright',
    copyrightBody: 'Commercial use is prohibited unless I grant permission.',
    copyrightCase:
      'Case: a user on an online platform used my image without permission and published it; final compensation was 4500 yuan.',
    readme: 'README',
    startupFailed: 'Startup failed',
    loadImagesFailed: 'Failed to load photos',
    loadDetailFailed: 'Failed to load photo detail',
    unknownTime: 'Unknown time',
    privacyTitle: 'Privacy & Cookies',
    privacyBody:
      'This site uses anonymous device identification (browser fingerprinting) to prevent duplicate likes and count views. No personal data is collected or shared. By continuing, you agree to this anonymous tracking.',
    privacyAccept: 'Got it',
    copyrightDownloadTitle: 'Copyright Notice',
    copyrightDownloadBody:
      'All photos on this site are protected by copyright. Commercial use is strictly prohibited without explicit permission from the photographer. Unauthorized commercial use may result in legal action and compensation claims (prior case: ¥4,500 awarded).',
    copyrightAgree: 'I Understand & Agree',
    passwordTitle: 'Password Required',
    passwordHint: 'Hint',
    passwordPlaceholder: 'Enter password',
    passwordSubmit: 'Unlock',
    passwordCancel: 'Cancel',
    passwordWrong: 'Incorrect password'
  },
  zh: {
    appTitle: "Seym's 相册",
    gallery: '相册',
    noCollections: '未找到合集。',
    noContent: '这里没有可展示的合集或图片。',
    collections: '合集',
    photos: '张照片',
    folders: '个文件夹',
    direct: '直接',
    total: '总计',
    views: '浏览',
    like: '点赞',
    dislike: '点踩',
    openMenu: '打开菜单',
    closeMenu: '关闭菜单',
    backToGrid: '返回网格',
    loading: '加载中...',
    raw: 'RAW',
    files: '文件',
    exif: 'EXIF',
    noExif: '未找到 EXIF 字段。',
    settings: '设置',
    language: '语言',
    theme: '主题',
    english: 'English',
    chinese: '中文',
    auto: '自动',
    light: '浅色',
    dark: '深色',
    camera: '相机',
    lens: '镜头',
    exposure: '曝光',
    aperture: '光圈',
    iso: 'ISO',
    focal: '焦段',
    shutterCount: '快门数',
    rating: '评分',
    original: '原图',
    downloadOriginal: '下载原图',
    copyrightShort: '未经允许禁止商用。',
    copyrightTitle: '版权声明',
    copyrightBody: '除非我允许，否则禁止商用。',
    copyrightCase: '案例：某网络平台用户盗用我图片并出版，最终赔偿 4500 元。',
    readme: 'README',
    startupFailed: '启动失败',
    loadImagesFailed: '加载照片失败',
    loadDetailFailed: '加载照片详情失败',
    unknownTime: '未知时间',
    privacyTitle: '隐私与 Cookie 声明',
    privacyBody:
      '本站使用匿名设备标识（浏览器指纹）以防止重复点赞和统计浏览数据。我们不收集或分享任何个人信息。继续使用即表示您同意此项匿名追踪。',
    privacyAccept: '知道了',
    copyrightDownloadTitle: '版权声明',
    copyrightDownloadBody:
      '本站所有照片均受版权保护。未经摄影师明确许可，严禁任何商业使用。未经授权的商业使用可能导致法律诉讼和赔偿（此前案例判决赔偿 ¥4,500 元）。',
    copyrightAgree: '我已阅读并同意',
    passwordTitle: '需要密码',
    passwordHint: '提示',
    passwordPlaceholder: '请输入密码',
    passwordSubmit: '解锁',
    passwordCancel: '取消',
    passwordWrong: '密码错误'
  }
} satisfies Record<Language, Record<string, string>>;

type Copy = (typeof COPY)[Language];
type CopyKey = keyof Copy;

export function App() {
  const [albums, setAlbums] = useState<Album[]>([]);
  const [currentAlbum, setCurrentAlbum] = useState<Album | null>(null);
  const [images, setImages] = useState<GalleryImage[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [totalImages, setTotalImages] = useState(0);
  const [detail, setDetail] = useState<ImageDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [menuOpen, setMenuOpen] = useState(false);
  const [language, setLanguage] = useState<Language>(initialLanguage);
  const [themePreference, setThemePreference] = useState<ThemePreference>(initialThemePreference);
  const [privacyConsent, setPrivacyConsentState] = useState(hasPrivacyConsent);
  const [showCopyrightModal, setShowCopyrightModal] = useState(false);
  const [copyrightCountdown, setCopyrightCountdown] = useState(COPYRIGHT_COUNTDOWN_SECONDS);
  const [userReactions, setUserReactions] = useState<Record<string, Reaction>>(loadReactions);
  const [passwordAlbum, setPasswordAlbum] = useState<Album | null>(null);
  const [passwordError, setPasswordError] = useState('');
  const [passwordInput, setPasswordInput] = useState('');
  const viewedRef = useRef<Set<string>>(new Set());
  const pendingDownloadRef = useRef<string | null>(null);
  const copyrightTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const copy = COPY[language];

  useEffect(() => {
    localStorage.setItem(LANGUAGE_KEY, language);
    document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en';
  }, [language]);

  useEffect(() => {
    localStorage.setItem(THEME_KEY, themePreference);
    document.documentElement.dataset.themePreference = themePreference;
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const applyTheme = () => {
      const resolved = themePreference === 'auto' ? (media.matches ? 'dark' : 'light') : themePreference;
      document.documentElement.dataset.theme = resolved;
      document.documentElement.style.colorScheme = resolved;
    };
    applyTheme();
    if (themePreference !== 'auto') return;
    media.addEventListener('change', applyTheme);
    return () => media.removeEventListener('change', applyTheme);
  }, [themePreference]);

  useEffect(() => {
    // Initialize anonymous device identifier for dedup and tracking
    getDeviceId().then((id) => setApiDeviceId(id)).catch(() => {});
  }, []);

  useEffect(() => {
    const sync = () => {
      void bootstrap(parseRoute());
    };
    sync();
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, []);

  async function bootstrap(route = parseRoute()) {
    setError('');
    try {
      const albumData = await postAPI<{ albums: Album[] }>('/api/list-albums');
      setAlbums(albumData.albums);
      await applyRoute(route, albumData.albums);
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.startupFailed);
    }
  }

  async function applyRoute(route: RouteState, albumList = albums) {
    const album = route.albumPath === null ? null : (findAlbum(albumList, route.albumPath) ?? null);
    setCurrentAlbum(album);
    setDetail(null);
    if (!album) {
      setImages([]);
      setNextCursor('');
      setTotalImages(0);
      return;
    }
    const loaded = await loadImages(album.albumId, '', false);
    void recordView('album', album.albumId, album.albumId);
    if (route.fileName) {
      const image = loaded.find((item) => routeFileSegment(item) === route.fileName);
      await openImageByRoute(album.path, route.fileName, image?.imageId, false, albumList);
    }
  }

  async function openCollection(album: Album) {
    if (album.hasPassword && !hasAlbumAccess(album.albumId)) {
      setPasswordAlbum(album);
      setPasswordInput('');
      setPasswordError('');
      setMenuOpen(false);
      return;
    }
    pushPath(collectionHref(album.path));
    setMenuOpen(false);
    await applyRoute({ albumPath: album.path, fileName: null });
  }

  async function handleVerifyPassword() {
    if (!passwordAlbum) return;
    try {
      const data = await postAPI<{ token: string }>('/api/verify-album-password', {
        albumId: passwordAlbum.albumId,
        password: passwordInput
      });
      setAlbumToken(passwordAlbum.albumId, data.token);
      setPasswordAlbum(null);
      // Now open the album
      pushPath(collectionHref(passwordAlbum.path));
      await applyRoute({ albumPath: passwordAlbum.path, fileName: null });
    } catch {
      setPasswordError(copy.passwordWrong);
    }
  }

  function handlePasswordCancel() {
    setPasswordAlbum(null);
  }

  async function openImage(image: GalleryImage) {
    pushPath(imageHref(image));
    await openImageByRoute(image.albumPath, routeFileSegment(image), image.imageId);
  }

  async function openImageByRoute(
    albumPath: string,
    fileName: string,
    imageId?: string,
    keepImages = true,
    albumList = albums
  ) {
    setLoading(true);
    setError('');
    try {
      const albumForToken = imageId ? undefined : albumPath ? (findAlbum(albumList, albumPath)?.albumId) : undefined;
      const data = await postAPI<ImageDetail>(
        '/api/get-image-detail',
        {
          imageId,
          albumPath,
          fileName: decodeURIComponent(fileName)
        },
        albumForToken
      );
      setDetail(data);
      const album = findAlbum(albumList, data.albumPath);
      if (album) {
        setCurrentAlbum(album);
        if (!keepImages) {
          await loadImages(album.albumId, '', false);
        }
      }
      void recordView('image', data.imageId, data.albumId);
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.loadDetailFailed);
    } finally {
      setLoading(false);
    }
  }

  async function loadImages(albumId: string, cursor = nextCursor, append = true) {
    setLoading(true);
    setError('');
    try {
      const data = await postAPI<ImagesResponse>(
        '/api/list-images',
        { albumId, cursor, limit: 120 },
        albumId
      );
      setImages((prev) => (append ? [...prev, ...data.images] : data.images));
      setNextCursor(data.nextCursor);
      setTotalImages(data.total);
      return data.images;
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.loadImagesFailed);
      return [];
    } finally {
      setLoading(false);
    }
  }

  async function recordView(targetType: StatsTargetType, targetId: string, albumId?: string) {
    const key = `${targetType}:${targetId}`;
    if (viewedRef.current.has(key)) return;
    viewedRef.current.add(key);
    try {
      const data = await postAPI<{ stats: ItemStats }>(
        '/api/record-view',
        { targetType, targetId },
        albumId
      );
      updateStats(targetType, targetId, data.stats);
    } catch {
      // Stats should never interrupt browsing.
    }
  }

  const reactTo = useCallback(
    async (targetType: StatsTargetType, targetId: string, reaction: Reaction, albumId?: string) => {
      // Toggle locally first for instant feedback
      const newReaction = toggleReaction(targetType, targetId, reaction);
      setUserReactions(loadReactions());

      try {
        const data = await postAPI<{ stats: ItemStats }>(
          '/api/react-item',
          {
            targetType,
            targetId,
            reaction,
            // Tell backend whether this is a toggle-on or toggle-off
            active: newReaction !== null
          },
          albumId
        );
        updateStats(targetType, targetId, data.stats);
      } catch (err) {
        // Revert on error
        if (newReaction !== null) {
          toggleReaction(targetType, targetId, reaction);
        } else {
          toggleReaction(targetType, targetId, reaction);
        }
        setUserReactions(loadReactions());
        setError(err instanceof Error ? err.message : copy.startupFailed);
      }
    },
    [copy.startupFailed]
  );

  function updateStats(targetType: StatsTargetType, targetId: string, stats: ItemStats) {
    if (targetType === 'album') {
      setAlbums((prev) =>
        prev.map((album) => (album.albumId === targetId ? { ...album, stats } : album))
      );
      setCurrentAlbum((prev) => (prev?.albumId === targetId ? { ...prev, stats } : prev));
      return;
    }
    setImages((prev) => prev.map((image) => (image.imageId === targetId ? { ...image, stats } : image)));
    setDetail((prev) => (prev?.imageId === targetId ? { ...prev, stats } : prev));
  }

  function handleDownloadOriginal(url: string) {
    if (hasCopyrightConsent()) {
      window.open(mediaURL(url), '_blank');
      return;
    }
    pendingDownloadRef.current = url;
    setCopyrightCountdown(COPYRIGHT_COUNTDOWN_SECONDS);
    setShowCopyrightModal(true);
  }

  function handleCopyrightAgree() {
    setCopyrightConsent();
    setShowCopyrightModal(false);
    if (copyrightTimerRef.current) {
      clearInterval(copyrightTimerRef.current);
      copyrightTimerRef.current = null;
    }
    const url = pendingDownloadRef.current;
    if (url) {
      pendingDownloadRef.current = null;
      window.open(mediaURL(url), '_blank');
    }
  }

  function handleCopyrightCancel() {
    setShowCopyrightModal(false);
    if (copyrightTimerRef.current) {
      clearInterval(copyrightTimerRef.current);
      copyrightTimerRef.current = null;
    }
    pendingDownloadRef.current = null;
  }

  function handleAcceptPrivacy() {
    setPrivacyConsent();
    setPrivacyConsentState(true);
  }

  function goHome() {
    pushPath('/');
    setMenuOpen(false);
    setCurrentAlbum(null);
    setImages([]);
    setDetail(null);
    setNextCursor('');
    setTotalImages(0);
  }

  function goBackToGrid() {
    if (!currentAlbum) return;
    pushPath(collectionHref(currentAlbum.path));
    setDetail(null);
  }

  const rootAlbums = useMemo(() => childrenOf(albums, ''), [albums]);
  const childAlbums = useMemo(
    () => childrenOf(albums, currentAlbum?.path ?? ''),
    [albums, currentAlbum]
  );
  const breadcrumbs = currentAlbum ? breadcrumbAlbums(albums, currentAlbum.path) : [];
  return (
    <main className="app-shell">
      <section className={detail ? 'shell image-mode' : 'shell'}>
        <header className="navbar">
          <button
            className="menu-toggle"
            onClick={() => setMenuOpen((value) => !value)}
            aria-label={menuOpen ? copy.closeMenu : copy.openMenu}
          >
            {menuOpen ? <X size={18} /> : <Menu size={18} />}
          </button>
          <button className="brand brand-button" onClick={goHome}>
            <Camera size={18} />
            <span>{copy.appTitle}</span>
          </button>
          <span className="navbar-spacer" />
          <MenuSettings
            copy={copy}
            language={language}
            onLanguageChange={setLanguage}
            themePreference={themePreference}
            onThemeChange={setThemePreference}
          />
          <div className="copyright-pill">
            <Shield size={14} />
            <span>{copy.copyrightShort}</span>
          </div>
        </header>

        <div className="toolbar">
          <div className="crumbs">
            <button onClick={goHome}>
              <Home size={14} />
              {copy.gallery}
            </button>
            {breadcrumbs.map((album) => (
              <button key={album.path} onClick={() => void openCollection(album)}>
                / {album.name}
              </button>
            ))}
            {detail && <span>/ {detail.fileName}</span>}
          </div>
        </div>

        {error && (
          <div className="notice" role="alert">
            {error}
          </div>
        )}

        <div className="workspace">
          {menuOpen && <button className="menu-scrim" onClick={() => setMenuOpen(false)} />}
          <aside className={menuOpen ? 'menu-pane open' : 'menu-pane'}>
            <nav className="album-tree">
              {albums.length === 0 ? (
                <p className="empty">{copy.noCollections}</p>
              ) : (
                rootAlbums.map((album) => (
                  <AlbumTreeNode
                    key={album.albumId}
                    album={album}
                    albums={albums}
                    activePath={currentAlbum?.path ?? ''}
                    copy={copy}
                    onOpen={(target) => void openCollection(target)}
                  />
                ))
              )}
            </nav>
          </aside>

          <section className="content-pane">
            {detail && currentAlbum ? (
              <ImageBrowser
                detail={detail}
                images={images}
                nextCursor={nextCursor}
                loading={loading}
                copy={copy}
                language={language}
                userReactions={userReactions}
                onBack={goBackToGrid}
                onOpenImage={(image) => void openImage(image)}
                onReact={(targetType, targetId, reaction) => void reactTo(targetType, targetId, reaction)}
                onLoadMore={() => currentAlbum && void loadImages(currentAlbum.albumId)}
                onDownloadOriginal={handleDownloadOriginal}
              />
            ) : currentAlbum ? (
              <CollectionView
                album={currentAlbum}
                childAlbums={childAlbums}
                images={images}
                totalImages={totalImages}
                nextCursor={nextCursor}
                loading={loading}
                copy={copy}
                language={language}
                userReactions={userReactions}
                onOpenAlbum={(album) => void openCollection(album)}
                onOpenImage={(image) => void openImage(image)}
                onReact={(targetType, targetId, reaction) => void reactTo(targetType, targetId, reaction)}
                onLoadMore={() => void loadImages(currentAlbum.albumId)}
              />
            ) : (
              <TimelineHome
                albums={rootAlbums}
                copy={copy}
                language={language}
                userReactions={userReactions}
                onOpenAlbum={(album) => void openCollection(album)}
                onReact={(targetType, targetId, reaction) => void reactTo(targetType, targetId, reaction)}
              />
            )}
          </section>
        </div>
      </section>
      {!privacyConsent && (
        <ConsentBanner copy={copy} onAccept={handleAcceptPrivacy} />
      )}
      {passwordAlbum && (
        <PasswordDialog
          album={passwordAlbum}
          copy={copy}
          input={passwordInput}
          error={passwordError}
          onInputChange={setPasswordInput}
          onSubmit={() => void handleVerifyPassword()}
          onCancel={handlePasswordCancel}
        />
      )}
      {showCopyrightModal && (
        <CopyrightModal
          copy={copy}
          language={language}
          countdown={copyrightCountdown}
          onCountdownTick={() => setCopyrightCountdown((prev) => Math.max(0, prev - 1))}
          onAgree={handleCopyrightAgree}
          onCancel={handleCopyrightCancel}
          timerRef={copyrightTimerRef}
        />
      )}
    </main>
  );
}

function PasswordDialog({
  album,
  copy,
  input,
  error,
  onInputChange,
  onSubmit,
  onCancel
}: {
  album: Album;
  copy: Copy;
  input: string;
  error: string;
  onInputChange: (value: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="copyright-modal-overlay">
      <div className="copyright-modal">
        <h2>
          <Lock size={20} />
          {copy.passwordTitle}
        </h2>
        <p className="copyright-modal-body">{album.name}</p>
        {album.passwordHint && (
          <p className="password-hint">
            {copy.passwordHint}: {album.passwordHint}
          </p>
        )}
        <input
          type="password"
          className="password-input"
          placeholder={copy.passwordPlaceholder}
          value={input}
          onChange={(e) => onInputChange(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') onSubmit(); }}
          autoFocus
        />
        {error && <p className="password-error">{error}</p>}
        <div className="copyright-modal-actions">
          <button type="button" className="copyright-cancel" onClick={onCancel}>
            {copy.passwordCancel}
          </button>
          <button type="button" className="copyright-agree" onClick={onSubmit}>
            {copy.passwordSubmit}
          </button>
        </div>
      </div>
    </div>
  );
}

function ConsentBanner({ copy, onAccept }: { copy: Copy; onAccept: () => void }) {
  return (
    <div className="consent-banner">
      <div className="consent-banner-inner">
        <Cookie size={18} className="consent-icon" />
        <div className="consent-text">
          <strong>{copy.privacyTitle}</strong>
          <p>{copy.privacyBody}</p>
        </div>
        <button type="button" className="consent-accept" onClick={onAccept}>
          {copy.privacyAccept}
        </button>
      </div>
    </div>
  );
}

function CopyrightModal({
  copy,
  language,
  countdown,
  onCountdownTick,
  onAgree,
  onCancel,
  timerRef
}: {
  copy: Copy;
  language: Language;
  countdown: number;
  onCountdownTick: () => void;
  onAgree: () => void;
  onCancel: () => void;
  timerRef: React.MutableRefObject<ReturnType<typeof setInterval> | null>;
}) {
  useEffect(() => {
    timerRef.current = setInterval(() => {
      onCountdownTick();
    }, 1000);
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [onCountdownTick, timerRef]);

  const canAgree = countdown <= 0;
  const countdownText =
    language === 'zh'
      ? `请先阅读上述声明（${countdown}秒）`
      : `Please read the notice above (${countdown}s)`;

  return (
    <div className="copyright-modal-overlay">
      <div className="copyright-modal">
        <h2>
          <Shield size={20} />
          {copy.copyrightDownloadTitle}
        </h2>
        <p className="copyright-modal-body">{copy.copyrightDownloadBody}</p>
        <div className="copyright-modal-actions">
          <button type="button" className="copyright-cancel" onClick={onCancel}>
            {copy.closeMenu}
          </button>
          <button
            type="button"
            className={`copyright-agree${canAgree ? '' : ' disabled'}`}
            disabled={!canAgree}
            onClick={onAgree}
          >
            {canAgree ? copy.copyrightAgree : countdownText}
          </button>
        </div>
      </div>
    </div>
  );
}

function MenuSettings({
  copy,
  language,
  onLanguageChange,
  themePreference,
  onThemeChange
}: {
  copy: Copy;
  language: Language;
  onLanguageChange: (language: Language) => void;
  themePreference: ThemePreference;
  onThemeChange: (theme: ThemePreference) => void;
}) {
  return (
    <div className="settings-panel">
      <strong>{copy.settings}</strong>
      <label>
        <span>
          <Languages size={13} />
          {copy.language}
        </span>
        <select value={language} onChange={(event) => onLanguageChange(event.target.value as Language)}>
          <option value="en">{copy.english}</option>
          <option value="zh">{copy.chinese}</option>
        </select>
      </label>
      <label>
        <span>
          {themePreference === 'dark' ? <Moon size={13} /> : <Sun size={13} />}
          {copy.theme}
        </span>
        <select
          value={themePreference}
          onChange={(event) => onThemeChange(event.target.value as ThemePreference)}
        >
          <option value="auto">{copy.auto}</option>
          <option value="light">{copy.light}</option>
          <option value="dark">{copy.dark}</option>
        </select>
      </label>
    </div>
  );
}

function AlbumTreeNode({
  album,
  albums,
  activePath,
  copy,
  onOpen
}: {
  album: Album;
  albums: Album[];
  activePath: string;
  copy: Copy;
  onOpen: (album: Album) => void;
}) {
  const children = childrenOf(albums, album.path);
  const active = album.path === activePath;
  const treeThumb = album.coverThumbUrls?.[0] || album.coverThumbUrl || '';
  return (
    <div className="tree-node">
      <button className={active ? 'tree-row active' : 'tree-row'} onClick={() => onOpen(album)}>
        {treeThumb ? (
          <img className="tree-thumb" src={smallThumbURL(treeThumb)} alt="" loading="lazy" />
        ) : children.length > 0 ? (
          <FolderOpen size={16} />
        ) : (
          <Folder size={16} />
        )}
        <span>
          <strong>
            {album.name}
            {album.hasPassword && <Lock size={11} className="tree-lock" />}
          </strong>
          <small>
            {album.totalPhotoCount} {copy.photos}
          </small>
        </span>
      </button>
      {children.length > 0 && (
        <div className="tree-children">
          {children.map((child) => (
            <AlbumTreeNode
              key={child.albumId}
              album={child}
              albums={albums}
              activePath={activePath}
              copy={copy}
              onOpen={onOpen}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function TimelineHome({
  albums,
  copy,
  language,
  userReactions,
  onOpenAlbum,
  onReact
}: {
  albums: Album[];
  copy: Copy;
  language: Language;
  userReactions: Record<string, Reaction>;
  onOpenAlbum: (album: Album) => void;
  onReact: (targetType: StatsTargetType, targetId: string, reaction: Reaction, albumId?: string) => void;
}) {
  const timelineAlbums = [...albums].sort(compareAlbums);
  return (
    <section className="timeline-view">
      {timelineAlbums.length === 0 ? (
        <p className="empty">{copy.noCollections}</p>
      ) : (
        <div className="timeline-list">
          {timelineAlbums.map((album) => {
            const safeStats = album.stats ?? { views: 0, likes: 0, dislikes: 0 };
            const reaction = userReactions[`album:${album.albumId}`] ?? null;
            return (
              <article key={album.albumId} className="timeline-card">
                <button className="timeline-cover" onClick={() => onOpenAlbum(album)}>
                  <CollectionCover album={album} />
                  <span className="timeline-views">
                    <Eye size={12} />
                    {formatCount(safeStats.views)}
                  </span>
                </button>
                <div className="timeline-body">
                  {album.readme ? (
                    <div className="timeline-readme">
                      <MarkdownView markdown={album.readme} compact />
                    </div>
                  ) : (
                    <span className="timeline-name">{album.name}</span>
                  )}
                  <div className="timeline-footer">
                    <span className="timeline-meta">
                      {formatDate(album.firstTakenAt, language, copy)} · {album.totalPhotoCount} {copy.photos}
                    </span>
                    <div className="timeline-actions">
                      <button
                        type="button"
                        className={`timeline-reaction${reaction === 'like' ? ' active-like' : ''}`}
                        onClick={(e) => { e.stopPropagation(); onReact('album', album.albumId, 'like', album.albumId); }}
                        aria-label={copy.like}
                      >
                        <ThumbsUp size={14} />
                        {formatCount(safeStats.likes)}
                      </button>
                      <button
                        type="button"
                        className={`timeline-reaction${reaction === 'dislike' ? ' active-dislike' : ''}`}
                        onClick={(e) => { e.stopPropagation(); onReact('album', album.albumId, 'dislike', album.albumId); }}
                        aria-label={copy.dislike}
                      >
                        <ThumbsDown size={14} />
                        {formatCount(safeStats.dislikes)}
                      </button>
                    </div>
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

function CollectionView({
  album,
  childAlbums,
  images,
  totalImages,
  nextCursor,
  loading,
  copy,
  language,
  userReactions,
  onOpenAlbum,
  onOpenImage,
  onReact,
  onLoadMore
}: {
  album: Album;
  childAlbums: Album[];
  images: GalleryImage[];
  totalImages: number;
  nextCursor: string;
  loading: boolean;
  copy: Copy;
  language: Language;
  userReactions: Record<string, Reaction>;
  onOpenAlbum: (album: Album) => void;
  onOpenImage: (image: GalleryImage) => void;
  onReact: (targetType: StatsTargetType, targetId: string, reaction: Reaction, albumId?: string) => void;
  onLoadMore: () => void;
}) {
  return (
    <section className="collection-view">
      <div className="section-heading">
        <Grid3X3 size={18} />
        <div>
          <h1>{album.name}</h1>
          <p>
            {images.length}/{totalImages} {copy.photos}, {childAlbums.length} {copy.folders}
          </p>
          <StatsBar
            stats={album.stats}
            copy={copy}
            userReaction={userReactions[`album:${album.albumId}`] ?? null}
            onLike={() => onReact('album', album.albumId, 'like', album.albumId)}
            onDislike={() => onReact('album', album.albumId, 'dislike', album.albumId)}
          />
        </div>
      </div>
      {album.readme && (
        <section className="readme-block" aria-label={copy.readme}>
          <MarkdownView markdown={album.readme} />
        </section>
      )}
      <MixedGrid
        albums={childAlbums}
        images={images}
        nextCursor={nextCursor}
        loading={loading}
        copy={copy}
        language={language}
        userReactions={userReactions}
        onOpenAlbum={onOpenAlbum}
        onOpenImage={onOpenImage}
        onReact={onReact}
        onLoadMore={onLoadMore}
      />
    </section>
  );
}

function MixedGrid({
  albums,
  images,
  nextCursor,
  loading,
  copy,
  language,
  userReactions,
  onOpenAlbum,
  onOpenImage,
  onReact,
  onLoadMore
}: {
  albums: Album[];
  images: GalleryImage[];
  nextCursor: string;
  loading: boolean;
  copy: Copy;
  language: Language;
  userReactions: Record<string, Reaction>;
  onOpenAlbum: (album: Album) => void;
  onOpenImage: (image: GalleryImage) => void;
  onReact: (targetType: StatsTargetType, targetId: string, reaction: Reaction, albumId?: string) => void;
  onLoadMore: () => void;
}) {
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!nextCursor || loading || !sentinelRef.current) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        onLoadMore();
      }
    });
    observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [nextCursor, loading, onLoadMore]);

  if (albums.length === 0 && images.length === 0) {
    return <p className="empty">{copy.noContent}</p>;
  }

  return (
    <>
      <div className="mixed-grid">
        {albums.map((child) => (
          <article key={child.albumId} className="collection-card">
            <button className="card-main" onClick={() => onOpenAlbum(child)}>
              <CollectionCover album={child} />
              <span>
                <strong>{child.name}</strong>
                <small>
                  {child.photoCount} {copy.direct} / {child.totalPhotoCount} {copy.total}
                </small>
              </span>
            </button>
            <StatsBar
              stats={child.stats}
              copy={copy}
              userReaction={userReactions[`album:${child.albumId}`] ?? null}
              onLike={() => onReact('album', child.albumId, 'like', child.albumId)}
              onDislike={() => onReact('album', child.albumId, 'dislike', child.albumId)}
            />
          </article>
        ))}
        {images.map((image) => (
          <article key={image.imageId} className="photo-tile">
            <button className="card-main" onClick={() => onOpenImage(image)}>
              <img src={mediaURL(image.thumbUrl)} alt={image.title} loading="lazy" />
              <span>
                <strong>{image.title}</strong>
                <small>{formatDate(image.takenAt, language, copy)}</small>
              </span>
            </button>
            <StatsBar
              stats={image.stats}
              copy={copy}
              userReaction={userReactions[`image:${image.imageId}`] ?? null}
              onLike={() => onReact('image', image.imageId, 'like', image.albumId)}
              onDislike={() => onReact('image', image.imageId, 'dislike', image.albumId)}
            />
          </article>
        ))}
      </div>
      {nextCursor && (
        <div ref={sentinelRef} className="load-sentinel">
          {loading ? copy.loading : ''}
        </div>
      )}
    </>
  );
}

function CollectionCover({ album }: { album: Album }) {
  const thumbs = album.coverThumbUrls?.length
    ? album.coverThumbUrls
    : album.coverThumbUrl
      ? [album.coverThumbUrl]
      : [];
  if (thumbs.length === 0) {
    return (
      <div className="folder-art">
        <Folder size={34} />
      </div>
    );
  }
  return (
    <div className={`cover-mosaic cover-count-${Math.min(thumbs.length, 9)}`}>
      {thumbs.slice(0, 9).map((thumb, index) => (
        <img key={`${thumb}-${index}`} src={mediaURL(thumb)} alt="" loading="lazy" />
      ))}
    </div>
  );
}

function ImageBrowser({
  detail,
  images,
  nextCursor,
  loading,
  copy,
  language,
  userReactions,
  onBack,
  onOpenImage,
  onReact,
  onLoadMore,
  onDownloadOriginal
}: {
  detail: ImageDetail;
  images: GalleryImage[];
  nextCursor: string;
  loading: boolean;
  copy: Copy;
  language: Language;
  userReactions: Record<string, Reaction>;
  onBack: () => void;
  onOpenImage: (image: GalleryImage) => void;
  onReact: (targetType: StatsTargetType, targetId: string, reaction: Reaction, albumId?: string) => void;
  onLoadMore: () => void;
  onDownloadOriginal: (url: string) => void;
}) {
  const sentinelRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    if (!nextCursor || loading || !sentinelRef.current) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        onLoadMore();
      }
    });
    observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [nextCursor, loading, onLoadMore]);

  return (
    <article className="browser">
      <div className="browser-main">
        <section className="viewer">
          <div className="viewer-toolbar">
            <button className="back-button" onClick={onBack}>
              <ArrowLeft size={16} />
              {copy.backToGrid}
            </button>
            <div className="viewer-actions">
              <button className="download" onClick={() => onDownloadOriginal(detail.originalDownloadUrl)}>
                <Download size={16} />
                {copy.downloadOriginal}
              </button>
              {detail.rawDownloadUrl && (
                <a className="download secondary" href={mediaURL(detail.rawDownloadUrl)}>
                  <Download size={16} />
                  {copy.raw}
                </a>
              )}
            </div>
          </div>
          <div className="viewer-frame">
            <img src={mediaURL(detail.originalUrl)} alt={detail.title} />
          </div>
        </section>
        <aside className="exif-pane">
          <DetailPanel
            detail={detail}
            copy={copy}
            language={language}
            userReaction={userReactions[`image:${detail.imageId}`] ?? null}
            onReact={(reaction) => onReact('image', detail.imageId, reaction, detail.albumId)}
          />
        </aside>
      </div>

      <div className="filmstrip" aria-label="Current collection thumbnails">
        {images.map((image) => (
          <button
            key={image.imageId}
            className={image.imageId === detail.imageId ? 'film-thumb active' : 'film-thumb'}
            onClick={() => onOpenImage(image)}
          >
            <img src={mediaURL(image.thumbUrl)} alt={image.title} loading="lazy" />
          </button>
        ))}
        {nextCursor && <span ref={sentinelRef} className="film-sentinel" />}
        {loading && <span className="film-loading">{copy.loading}</span>}
      </div>
    </article>
  );
}

function DetailPanel({
  detail,
  copy,
  language,
  userReaction,
  onReact
}: {
  detail: ImageDetail;
  copy: Copy;
  language: Language;
  userReaction?: Reaction | null;
  onReact: (reaction: Reaction) => void;
}) {
  const exifEntries = Object.entries(detail.exif).sort(([a], [b]) => a.localeCompare(b));
  return (
    <article className="detail">
      <div className="detail-title">
        <div>
          <h1>{detail.title}</h1>
          <p>{formatDate(detail.takenAt, language, copy)}</p>
          <StatsBar
            stats={detail.stats}
            copy={copy}
            userReaction={userReaction}
            onLike={() => onReact('like')}
            onDislike={() => onReact('dislike')}
          />
        </div>
      </div>

      <section className="copyright-note">
        <h2>
          <Shield size={14} />
          {copy.copyrightTitle}
        </h2>
        <p>{copy.copyrightBody}</p>
        <p>{copy.copyrightCase}</p>
      </section>

      <dl className="summary-grid">
        <SummaryItem icon={<Camera size={15} />} label={copy.camera} value={detail.summary.camera} />
        <SummaryItem icon={<Aperture size={15} />} label={copy.lens} value={detail.summary.lens} />
        <SummaryItem label={copy.exposure} value={detail.summary.exposureTime} />
        <SummaryItem label={copy.aperture} value={detail.summary.aperture} />
        <SummaryItem label={copy.iso} value={detail.summary.iso} />
        <SummaryItem label={copy.focal} value={detail.summary.focalLength} />
        <SummaryItem label={copy.shutterCount} value={detail.summary.shutterCount} />
        <SummaryItem icon={<Star size={15} />} label={copy.rating} value={detail.summary.rating} />
      </dl>

      {detail.gps && (
        <a className="map-link" href={detail.gps.mapUrl} target="_blank" rel="noreferrer">
          <MapPin size={15} />
          {detail.gps.latitude.toFixed(5)}, {detail.gps.longitude.toFixed(5)}
        </a>
      )}

      <section className="file-list">
        <h2>{copy.files}</h2>
        {detail.files.map((file) => (
          <code key={file}>{file}</code>
        ))}
      </section>

      <section className="exif">
        <h2>{copy.exif}</h2>
        {exifEntries.length === 0 ? (
          <p className="empty">{copy.noExif}</p>
        ) : (
          <table>
            <tbody>
              {exifEntries.map(([key, value]) => (
                <tr key={key}>
                  <th>{key}</th>
                  <td>{value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </article>
  );
}

function SummaryItem({
  icon,
  label,
  value
}: {
  icon?: ReactNode;
  label: string;
  value?: string;
}) {
  return (
    <div>
      <dt>
        {icon}
        {label}
      </dt>
      <dd>{value || '-'}</dd>
    </div>
  );
}

function StatsBar({
  stats,
  copy,
  userReaction,
  onLike,
  onDislike
}: {
  stats: ItemStats;
  copy: Copy;
  userReaction?: Reaction | null;
  onLike: () => void;
  onDislike: () => void;
}) {
  const safeStats = stats ?? { views: 0, likes: 0, dislikes: 0 };
  return (
    <div className="stats-bar">
      <span className="stats-pill" title={copy.views}>
        <Eye size={14} />
        {formatCount(safeStats.views)}
      </span>
      <button
        type="button"
        className={`stats-pill action${userReaction === 'like' ? ' active-like' : ''}`}
        onClick={onLike}
        aria-label={copy.like}
      >
        <ThumbsUp size={14} />
        {formatCount(safeStats.likes)}
      </button>
      <button
        type="button"
        className={`stats-pill action${userReaction === 'dislike' ? ' active-dislike' : ''}`}
        onClick={onDislike}
        aria-label={copy.dislike}
      >
        <ThumbsDown size={14} />
        {formatCount(safeStats.dislikes)}
      </button>
    </div>
  );
}

function MarkdownView({ markdown, compact = false }: { markdown: string; compact?: boolean }) {
  const blocks = markdown.trim().split(/\n{2,}/).filter(Boolean);
  return (
    <div className={compact ? 'markdown compact' : 'markdown'}>
      {blocks.map((block, index) => renderMarkdownBlock(block, index))}
    </div>
  );
}

function renderMarkdownBlock(block: string, key: number) {
  const lines = block.split('\n').map((line) => line.trimEnd());
  const first = lines[0]?.trim() ?? '';
  const heading = /^(#{1,3})\s+(.+)$/.exec(first);
  if (heading) {
    const text = heading[2];
    if (heading[1].length === 1) return <h1 key={key}>{text}</h1>;
    if (heading[1].length === 2) return <h2 key={key}>{text}</h2>;
    return <h3 key={key}>{text}</h3>;
  }
  if (lines.every((line) => line.trim().startsWith('- '))) {
    return (
      <ul key={key}>
        {lines.map((line, index) => (
          <li key={`${key}-${index}`}>{line.trim().slice(2)}</li>
        ))}
      </ul>
    );
  }
  if (lines.every((line) => line.trim().startsWith('>'))) {
    return <blockquote key={key}>{renderInlineLines(lines.map((line) => line.replace(/^>\s?/, '')))}</blockquote>;
  }
  return <p key={key}>{renderInlineLines(lines)}</p>;
}

function renderInlineLines(lines: string[]) {
  return lines.map((line, index) => (
    <span key={index}>
      {index > 0 && <br />}
      {line}
    </span>
  ));
}

function initialLanguage(): Language {
  const saved = localStorage.getItem(LANGUAGE_KEY);
  if (saved === 'en' || saved === 'zh') return saved;
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

function initialThemePreference(): ThemePreference {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved === 'auto' || saved === 'light' || saved === 'dark') return saved;
  return 'auto';
}

function parseRoute(): RouteState {
  const parts = window.location.pathname.split('/').filter(Boolean).map(decodeURIComponent);
  if (parts[0] !== 'collection') {
    return { albumPath: null, fileName: null };
  }
  if (parts.length === 1) {
    return { albumPath: '.', fileName: null };
  }
  if (parts.length === 2) {
    return { albumPath: parts[1], fileName: null };
  }
  return {
    albumPath: parts.slice(1, -1).join('/'),
    fileName: parts[parts.length - 1]
  };
}

function collectionHref(albumPath: string) {
  if (albumPath === '.' || albumPath === '') return '/collection';
  return `/collection/${albumPath.split('/').map(encodeURIComponent).join('/')}`;
}

function imageHref(image: GalleryImage) {
  return `${collectionHref(image.albumPath)}/${encodeURIComponent(routeFileSegment(image))}`;
}

function routeFileSegment(image: Pick<GalleryImage, 'fileName' | 'title'>) {
  return image.fileName || image.title;
}

function pushPath(path: string) {
  if (window.location.pathname !== path) {
    window.history.pushState({}, '', path);
  }
}

function findAlbum(albums: Album[], path: string) {
  return albums.find((album) => album.path === path);
}

function childrenOf(albums: Album[], parentPath: string) {
  return albums
    .filter((album) => album.parentPath === parentPath)
    .sort(compareAlbums);
}

function breadcrumbAlbums(albums: Album[], path: string) {
  if (path === '.' || path === '') return findAlbum(albums, '.') ? [findAlbum(albums, '.')!] : [];
  const parts = path.split('/');
  const crumbs: Album[] = [];
  for (let i = 0; i < parts.length; i += 1) {
    const current = parts.slice(0, i + 1).join('/');
    const album = findAlbum(albums, current);
    if (album) crumbs.push(album);
  }
  return crumbs;
}

function smallThumbURL(path: string) {
  const separator = path.includes('?') ? '&' : '?';
  return mediaURL(`${path}${separator}size=72`);
}

function compareDateDesc(a: string, b: string) {
  return new Date(b || 0).getTime() - new Date(a || 0).getTime();
}

function compareAlbums(a: Album, b: Album) {
  const timeOrder = compareDateDesc(a.firstTakenAt, b.firstTakenAt);
  if (timeOrder !== 0) return timeOrder;
  const nameOrder = a.name.localeCompare(b.name);
  if (nameOrder !== 0) return nameOrder;
  return a.path.localeCompare(b.path);
}

function formatCount(value: number) {
  if (value >= 10000) return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)}w`;
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`;
  return String(value);
}

function formatDate(value: string, language: Language, copy: Copy) {
  if (!value) return copy.unknownTime;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(language === 'zh' ? 'zh-CN' : 'en', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date);
}
