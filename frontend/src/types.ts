export type ApiEnvelope<T> =
  | { ok: true; data: T; error?: never }
  | { ok: false; data?: never; error: string };

export type Album = {
  albumId: string;
  name: string;
  path: string;
  parentPath: string;
  depth: number;
  readme: string;
  coverThumbUrl: string;
  coverThumbUrls: string[];
  photoCount: number;
  totalPhotoCount: number;
  firstTakenAt: string;
  takenAt: string;
  stats: ItemStats;
  hasPassword: boolean;
  passwordHint?: string;
};

export type GalleryImage = {
  imageId: string;
  albumId: string;
  albumPath: string;
  title: string;
  fileName: string;
  path: string;
  takenAt: string;
  width: number;
  height: number;
  thumbUrl: string;
  originalUrl: string;
  hasRaw: boolean;
  stats: ItemStats;
};

export type ImageDetail = GalleryImage & {
  originalDownloadUrl: string;
  rawDownloadUrl?: string;
  files: string[];
  exif: Record<string, string>;
  summary: {
    camera?: string;
    lens?: string;
    exposureTime?: string;
    aperture?: string;
    iso?: string;
    focalLength?: string;
    shutterCount?: string;
    rating?: string;
  };
  gps?: {
    latitude: number;
    longitude: number;
    mapUrl: string;
  };
};

export type ItemStats = {
  views: number;
  likes: number;
  dislikes: number;
};

export type StatsTargetType = 'album' | 'image';
export type Reaction = 'like' | 'dislike';

export type Status = {
  ready: boolean;
  cache: {
    backend: string;
    ttlSeconds: number;
    thumbnailMaxBytes: number;
    thumbnailBytes: number;
  };
  features: {
    rawPreview: string;
    mapProvider: string;
  };
};
