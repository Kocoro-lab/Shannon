'use client';

import React, { useEffect, useMemo, useRef, useState, useId } from 'react';

export type AudioSource =
  | { type: 'local'; src: string }
  | { type: 'youtube'; url: string };

export type Track = {
  id: string;
  title: string;
  source: AudioSource;
};

type YouTubePlayer = {
  pauseVideo?: () => void;
  playVideo?: () => void;
  loadVideoById?: (videoId: string) => void;
  cueVideoById?: (videoId: string) => void;
  getPlayerState?: () => number;
};

type YouTubeAPI = {
  Player: new (element: HTMLElement, options: {
    width: number;
    height: number;
    playerVars: Record<string, number>;
    events: {
      onReady: () => void;
      onStateChange: (e: { data: number }) => void;
    };
  }) => YouTubePlayer;
};

type Props = {
  tracks: Track[];
  initialIndex?: number;
  label?: string;
};

export function AudioPlayer({ tracks, initialIndex = 0, label = 'Music' }: Props) {
  const validTracks = useMemo(() => tracks.filter(Boolean), [tracks]);
  const [index, setIndex] = useState(Math.min(Math.max(0, initialIndex), Math.max(0, validTracks.length - 1)));
  const [isPlaying, setIsPlaying] = useState(false);
  const ytPlayerRef = useRef<YouTubePlayer | null>(null);
  const rid = useId();
  const ytInnerId = useMemo(() => 'ytp-' + rid.replace(/[:]/g, ''), [rid]);

  const current = validTracks[index];
  const isYouTube = current?.source.type === 'youtube';

  // Helper to extract YouTube video ID
  const getYouTubeId = (url: string): string | null => {
    try {
      const u = new URL(url);
      if (u.hostname === 'youtu.be') {
        return u.pathname.split('/')[1] || null;
      }
      if (u.hostname.includes('youtube.com')) {
        if (u.pathname === '/watch') return u.searchParams.get('v');
        const parts = u.pathname.split('/').filter(Boolean);
        if (parts.length >= 2 && (parts[0] === 'live' || parts[0] === 'embed')) return parts[1];
      }
    } catch {}
    return null;
  };

  // Ensure YouTube IFrame API is loaded
  const ensureYouTubeAPI = (): Promise<YouTubeAPI | null> => {
    return new Promise((resolve) => {
      if (typeof window === 'undefined') return resolve(null);
      const w = window as Window & { YT?: YouTubeAPI };
      if (w.YT && w.YT.Player) return resolve(w.YT);

      const existing = document.getElementById('yt-iframe-api');
      if (!existing) {
        const s = document.createElement('script');
        s.id = 'yt-iframe-api';
        s.src = 'https://www.youtube.com/iframe_api';
        document.head.appendChild(s);
      }

      const check = () => {
        if (w.YT && w.YT.Player) resolve(w.YT);
        else setTimeout(check, 50);
      };
      check();
    });
  };

  // Handle YouTube player lifecycle
  useEffect(() => {
    if (!isYouTube) {
      if (ytPlayerRef.current) {
        try { ytPlayerRef.current.pauseVideo?.(); } catch {}
      }
      return;
    }

    const src = current && current.source.type === 'youtube' ? current.source.url : '';
    const videoId = src ? getYouTubeId(src) : null;
    if (!videoId) return;

    let cancelled = false;
    ensureYouTubeAPI().then((YT) => {
      if (cancelled || !YT) return;
      const mountTarget = document.getElementById(ytInnerId) as HTMLElement | null;
      if (!mountTarget) return;

      if (!ytPlayerRef.current) {
        ytPlayerRef.current = new YT.Player(mountTarget, {
          width: 0,
          height: 0,
          playerVars: {
            autoplay: 0,
            controls: 0,
            rel: 0,
            modestbranding: 1,
            playsinline: 1,
            origin: window.location.origin,
          },
          events: {
            onReady: () => {
              if (isPlaying) {
                ytPlayerRef.current?.loadVideoById?.(videoId);
                setTimeout(() => { try { ytPlayerRef.current?.playVideo?.(); } catch {} }, 0);
              } else {
                ytPlayerRef.current?.cueVideoById?.(videoId);
              }
            },
            onStateChange: (e: { data: number }) => {
              if (e.data === 1) setIsPlaying(true);
              else if (e.data === 2 || e.data === 0) setIsPlaying(false);
              else if (e.data === 5 && isPlaying) {
                try { ytPlayerRef.current?.playVideo?.(); } catch {}
              }
            },
          },
        });
      } else {
        try {
          if (isPlaying) {
            ytPlayerRef.current.loadVideoById?.(videoId);
            try { ytPlayerRef.current.playVideo?.(); } catch {}
          } else {
            ytPlayerRef.current.cueVideoById?.(videoId);
          }
        } catch {}
      }
    });

    return () => { cancelled = true; };
  }, [current, isYouTube, ytInnerId, isPlaying]);

  // Handle play/pause toggle
  const togglePlayPause = () => {
    if (!current) return;

    if (isYouTube) {
      if (isPlaying) {
        try { ytPlayerRef.current?.pauseVideo?.(); } catch {}
        setIsPlaying(false);
      } else {
        try { ytPlayerRef.current?.playVideo?.(); } catch {}
        setIsPlaying(true);
      }
    }
  };

  // Handle track navigation
  const prevTrack = () => {
    setIsPlaying(false);
    setIndex((prev) => (prev - 1 + validTracks.length) % validTracks.length);
  };

  const nextTrack = () => {
    setIsPlaying(false);
    setIndex((prev) => (prev + 1) % validTracks.length);
  };

  if (!current) return null;

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="text-xs text-[#c89225ff] uppercase tracking-[0.2em]">{label}</div>
      <div className="flex items-center gap-1">
        {/* Hidden YouTube player mount point */}
        {isYouTube && (
          <div id={ytInnerId} className="hidden" />
        )}

        {/* Prev button */}
        <button
          onClick={prevTrack}
          className="h-6 w-6 grid place-items-center border border-[#352b19ff] bg-black hover:bg-[#0a0a0a] text-[#a4a4a4ff]"
          aria-label="Previous track"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <path d="M11 18V6l-8.5 6L11 18zm.5-6l8.5 6V6l-8.5 6z"/>
          </svg>
        </button>

        {/* Play/Pause button */}
        <button
          onClick={togglePlayPause}
          className={`h-6 w-6 grid place-items-center border ${
            isPlaying
              ? 'border-[#2ec27e] text-[#2ec27e]'
              : 'border-[#352b19ff] text-[#a4a4a4ff]'
          } bg-black hover:bg-[#0a0a0a]`}
          aria-label={isPlaying ? 'Pause' : 'Play'}
        >
          {isPlaying ? (
            <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
              <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/>
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
              <path d="M8 5v14l11-7z"/>
            </svg>
          )}
        </button>

        {/* Next button */}
        <button
          onClick={nextTrack}
          className="h-6 w-6 grid place-items-center border border-[#352b19ff] bg-black hover:bg-[#0a0a0a] text-[#a4a4a4ff]"
          aria-label="Next track"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <path d="M4 18l8.5-6L4 6v12zm9-12v12l8.5-6L13 6z"/>
          </svg>
        </button>

        {/* Track title */}
        <div className="text-[10px] text-[#666] font-mono max-w-[150px] truncate px-2">
          {current.title}
        </div>
      </div>
    </div>
  );
}