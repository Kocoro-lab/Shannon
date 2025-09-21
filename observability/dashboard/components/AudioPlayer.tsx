"use client";

import React, { useEffect, useMemo, useRef, useState, useId } from 'react';
import type { Track } from '@/lib/audio/tracks';

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
  className?: string;
  showSourceLink?: boolean;
};

export default function AudioPlayer({ tracks, initialIndex = 0, className = '', showSourceLink = false }: Props) {
  const validTracks = useMemo(() => tracks.filter(Boolean), [tracks]);
  const [index, setIndex] = useState(Math.min(Math.max(0, initialIndex), Math.max(0, validTracks.length - 1)));
  const [isPlaying, setIsPlaying] = useState(false);
  const ytPlayerRef = useRef<YouTubePlayer | null>(null);
  const ytWrapperRef = useRef<HTMLDivElement | null>(null);
  const rid = useId();
  const ytInnerId = useMemo(() => 'ytp-' + rid.replace(/[:]/g, ''), [rid]);

  const current = validTracks[index];
  const isYouTube = current?.source.type === 'youtube';

  // --- Helpers ---
  const getYouTubeId = (url: string): string | null => {
    try {
      const u = new URL(url);
      if (u.hostname === 'youtu.be') {
        return u.pathname.split('/')[1] || null;
      }
      if (u.hostname.includes('youtube.com')) {
        if (u.pathname === '/watch') return u.searchParams.get('v');
        // e.g., /live/<id> or /embed/<id>
        const parts = u.pathname.split('/').filter(Boolean);
        if (parts.length >= 2 && (parts[0] === 'live' || parts[0] === 'embed')) return parts[1];
      }
    } catch {}
    return null;
  };

  const ensureYouTubeAPI = (): Promise<YouTubeAPI | null> => {
    return new Promise((resolve) => {
      if (typeof window === 'undefined') return resolve(null);
      const w = window as Window & { YT?: YouTubeAPI };
      if (w.YT && w.YT.Player) return resolve(w.YT);
      // Inject script once
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

  // (local audio removed)

  // Handle YouTube player lifecycle and play/pause sync
  useEffect(() => {
    if (!isYouTube) {
      // Pause YT when leaving a YT track
      if (ytPlayerRef.current) {
        try { ytPlayerRef.current.pauseVideo?.(); } catch {}
      }
      return;
    }

    const src = current && current.source.type === 'youtube' ? current.source.url : '';
    const videoId = src ? getYouTubeId(src) : null;
    if (!videoId) {
      console.warn('AudioPlayer: Could not parse YouTube video ID from', src);
      return;
    }

    let cancelled = false;
    ensureYouTubeAPI().then((YT) => {
      if (cancelled || !YT) return;
      const mountTarget = document.getElementById(ytInnerId) as HTMLElement | null;
      if (!mountTarget) return;
      // Create player if needed
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
            // Ensure correct postMessage origin to avoid console errors
            // like: "origins don't match 'https://www.youtube.com' 'http://localhost:3000'"
            origin: typeof window !== 'undefined' ? window.location.origin : undefined as any,
          },
          events: {
            onReady: () => {
              // Load or cue based on desired state
              if (isPlaying) {
                ytPlayerRef.current?.loadVideoById?.(videoId);
                // Nudge play in case autoplay fails without a direct gesture
                setTimeout(() => { try { ytPlayerRef.current?.playVideo?.(); } catch {} }, 0);
              } else {
                ytPlayerRef.current?.cueVideoById?.(videoId);
              }
            },
            onStateChange: (e: { data: number }) => {
              // Sync UI with player state
              if (e.data === 1) setIsPlaying(true); // playing
              else if (e.data === 2 || e.data === 0) setIsPlaying(false); // paused or ended
              else if (e.data === 5 && isPlaying) {
                // CUED but we want to play
                try { ytPlayerRef.current?.playVideo?.(); } catch {}
              }
            },
          },
        });
      } else {
        // Reuse player
        try {
          if (isPlaying) {
            ytPlayerRef.current.loadVideoById?.(videoId);
            // Ensure it actually starts
            try { ytPlayerRef.current.playVideo?.(); } catch {}
          } else {
            ytPlayerRef.current.cueVideoById?.(videoId);
          }
        } catch {}
      }
    });

    return () => {
      cancelled = true;
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [index, isYouTube]);

  // React to play/pause toggle for YouTube
  useEffect(() => {
    if (!isYouTube || !ytPlayerRef.current) return;
    try {
      if (isPlaying) ytPlayerRef.current.playVideo?.();
      else ytPlayerRef.current.pauseVideo?.();
    } catch {}
  }, [isPlaying, isYouTube]);

  // If autoplay is blocked or fails, sync UI back to Play after a short check
  useEffect(() => {
    if (!isYouTube || !isPlaying || !ytPlayerRef.current) return;
    const id = setTimeout(() => {
      try {
        const state = ytPlayerRef.current?.getPlayerState?.();
        // 1: PLAYING, 3: BUFFERING
        if (state !== 1 && state !== 3) setIsPlaying(false);
      } catch {}
    }, 800);
    return () => clearTimeout(id);
  }, [index, isYouTube, isPlaying]);

  // If track list updates and index goes out of range
  useEffect(() => {
    if (index >= validTracks.length) setIndex(Math.max(0, validTracks.length - 1));
  }, [validTracks.length, index]);

  const onPrev = () => {
    if (!validTracks.length) return;
    setIndex((i) => {
      const nextIdx = (i - 1 + validTracks.length) % validTracks.length;
      const t = validTracks[nextIdx];
      // Always autoplay when navigating
      setIsPlaying(true);
      // If next is YouTube and player exists, load immediately in this user gesture
      if (t?.source.type === 'youtube' && ytPlayerRef.current) {
        const id = getYouTubeId(t.source.url);
        if (id) {
          try {
            ytPlayerRef.current.loadVideoById?.(id);
            ytPlayerRef.current.playVideo?.();
          } catch {}
        }
      }
      return nextIdx;
    });
  };

  const onToggle = () => {
    if (!current || !isYouTube) return;
    setIsPlaying((p) => !p);
  };

  const onNext = () => {
    if (!validTracks.length) return;
    setIndex((i) => {
      const nextIdx = (i + 1) % validTracks.length;
      const t = validTracks[nextIdx];
      // Always autoplay when navigating
      setIsPlaying(true);
      // If next is YouTube and player exists, load immediately in this user gesture
      if (t?.source.type === 'youtube' && ytPlayerRef.current) {
        const id = getYouTubeId(t.source.url);
        if (id) {
          try {
            ytPlayerRef.current.loadVideoById?.(id);
            ytPlayerRef.current.playVideo?.();
          } catch {}
        }
      }
      return nextIdx;
    });
  };

  // No autoplay on initial mount; start on user gesture

  return (
    <div className={`flex flex-col items-end gap-1 text-sm ${className}`}>
      {/* Hidden YouTube player container for YouTube tracks */}
      <div ref={ytWrapperRef} style={{ width: 0, height: 0, overflow: 'hidden' }} aria-hidden>
        <div id={ytInnerId} />
      </div>

      {/* Title row (full length) with optional source link */}
      <div className="text-gray-300 flex items-center gap-2" title={current?.title || ''}>
        <span>{current ? current.title : 'No track'}</span>
        {showSourceLink && isYouTube && current && current.source.type === 'youtube'}
      </div>

      {/* Controls row (fixed position under title) */}
      <div className="flex items-center gap-2">
        <button
          onClick={onPrev}
          className="px-2 py-1 border border-gray-600 text-gray-200"
          title="Back"
          aria-label="Back"
          disabled={!validTracks.length}
        >⏮</button>
        <button
          onClick={onToggle}
          className="px-2 py-1 border border-gray-600 text-gray-200"
          title={isPlaying ? 'Pause' : 'Play'}
          aria-label={isPlaying ? 'Pause' : 'Play'}
        disabled={!current || !isYouTube}
      >{isPlaying ? '⏸' : '▶'}</button>
        <button
          onClick={onNext}
          className="px-2 py-1 border border-gray-600 text-gray-200"
          title="Next"
          aria-label="Next"
          disabled={!validTracks.length}
        >⏭</button>
      </div>
    </div>
  );
}
